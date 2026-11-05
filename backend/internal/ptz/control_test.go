package ptz_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"vms.local/backend/internal/camera"
	"vms.local/backend/internal/ptz"
)

type device struct {
	mu    sync.Mutex
	moves []camera.PTZMove
	stops int
}

func TestSourceReplacementStopsOriginalAndRejectsOldOwner(t *testing.T) {
	c := ptz.New(time.Second)
	defer c.Close()
	ctx := context.Background()
	original, replacement := &device{}, &device{}
	target := ptz.Target{Key: "tenant/camera", Revision: "source/1", Driver: original}
	old, err := c.Start(ctx, target, "tab-a", camera.PTZMove{Pan: .4}, false)
	if err != nil {
		t.Fatal(err)
	}
	target.Revision = "replacement/1"
	target.Driver = replacement
	current, err := c.Start(ctx, target, "tab-b", camera.PTZMove{Zoom: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if original.stopCount() != 1 || replacement.stopCount() != 0 {
		t.Fatal("replacement redirected original stop")
	}
	c.Stop(target.Key, "tab-a", old)
	c.Stop(target.Key, "tab-a", current)
	if replacement.stopCount() != 0 {
		t.Fatal("old tab stopped the new command")
	}
	if err = c.Renew(target.Key, "tab-b", current, "replacement/2"); err == nil {
		t.Fatal("changed connection renewed command")
	}
	if replacement.stopCount() != 1 {
		t.Fatal("connection change failed to stop original connection")
	}
}

type blockingDevice struct {
	device
	entered, release chan struct{}
	once             sync.Once
}

func (d *blockingDevice) Move(ctx context.Context, m camera.PTZMove) error {
	d.once.Do(func() { close(d.entered); <-d.release })
	return d.device.Move(ctx, m)
}
func TestOverlappingStartsAreSerializedAndNewestKeepsControl(t *testing.T) {
	c := ptz.New(time.Second)
	defer c.Close()
	d := &blockingDevice{entered: make(chan struct{}), release: make(chan struct{})}
	target := ptz.Target{Key: "t/c", Revision: "1", Driver: d}
	ctx := context.Background()
	type result struct {
		id  string
		err error
	}
	first, second := make(chan result, 1), make(chan result, 1)
	go func() { id, err := c.Start(ctx, target, "a", camera.PTZMove{Pan: 1}, false); first <- result{id, err} }()
	<-d.entered
	go func() {
		id, err := c.Start(ctx, target, "b", camera.PTZMove{Tilt: -1}, false)
		second <- result{id, err}
	}()
	close(d.release)
	a, b := <-first, <-second
	if a.err != nil || b.err != nil {
		t.Fatal(a.err, b.err)
	}
	if len(d.moves) != 2 || d.moves[1] != (camera.PTZMove{Tilt: -1}) {
		t.Fatal("physical requests overtook each other", d.moves)
	}
	c.Stop(target.Key, "a", a.id)
	if err := c.Renew(target.Key, "b", b.id, target.Revision); err != nil {
		t.Fatal(err)
	}
	if d.stopCount() != 0 {
		t.Fatal("old stop reached physical device")
	}
}

type uncertainDevice struct {
	device
	failStop bool
}

func (d *uncertainDevice) Move(context.Context, camera.PTZMove) error {
	return errors.New("camera_api_timeout")
}
func (d *uncertainDevice) Stop(ctx context.Context) error {
	d.device.Stop(ctx)
	if d.failStop {
		d.failStop = false
		return errors.New("camera_api_timeout")
	}
	return nil
}
func TestAmbiguousMovementFailureRetainsStopForRetry(t *testing.T) {
	c := ptz.New(time.Second)
	defer c.Close()
	d := &uncertainDevice{failStop: true}
	_, err := c.Start(context.Background(), ptz.Target{Key: "t/c", Revision: "1", Driver: d}, "a", camera.PTZMove{Pan: 1}, false)
	if err == nil || d.stopCount() != 1 {
		t.Fatal("failed move did not attempt compensation")
	}
	c.Sweep(time.Now().Add(3 * time.Second))
	if d.stopCount() != 2 {
		t.Fatal("uncertain stop discarded instead of retried")
	}
}

type stopFailureDevice struct {
	device
	fail bool
}

func (d *stopFailureDevice) Stop(ctx context.Context) error {
	d.device.Stop(ctx)
	if d.fail {
		d.fail = false
		return errors.New("camera_api_timeout")
	}
	return nil
}
func TestLateRenewalCannotReviveCommandWhoseStopFailed(t *testing.T) {
	c := ptz.New(time.Second)
	defer c.Close()
	d := &stopFailureDevice{fail: true}
	target := ptz.Target{Key: "t/c", Revision: "1", Driver: d}
	token, err := c.Start(context.Background(), target, "a", camera.PTZMove{Pan: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Stop(target.Key, "a", token); err == nil {
		t.Fatal("fixture should fail first stop")
	}
	if err = c.Renew(target.Key, "a", token, "1"); err == nil {
		t.Fatal("renewal revived terminated command")
	}
}

func (d *device) Capabilities(context.Context) (camera.PTZCapabilities, error) {
	return camera.PTZCapabilities{PanTilt: true, Zoom: true, Home: true}, nil
}
func (d *device) Move(_ context.Context, m camera.PTZMove) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.moves = append(d.moves, m)
	return nil
}
func (d *device) Stop(context.Context) error { d.mu.Lock(); defer d.mu.Unlock(); d.stops++; return nil }
func (d *device) Home(context.Context) error { return nil }
func (d *device) stopCount() int             { d.mu.Lock(); defer d.mu.Unlock(); return d.stops }

func TestLatestCommandSurvivesOldStopRenewalAndDeadline(t *testing.T) {
	c := ptz.New(time.Second)
	defer c.Close()
	d := &device{}
	target := ptz.Target{Key: "tenant/camera", Revision: "source/1", Driver: d}
	ctx := context.Background()
	old, err := c.Start(ctx, target, "operator-a", camera.PTZMove{Pan: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	current, err := c.Start(ctx, target, "operator-b", camera.PTZMove{Tilt: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	before := d.stopCount()
	if err = c.Renew(target.Key, "operator-a", old, target.Revision); err == nil {
		t.Fatal("old renewal accepted")
	}
	if _, err = c.Stop(target.Key, "operator-a", old); err != nil {
		t.Fatal(err)
	}
	// Expiring an old command must never leave a timer able to stop its successor.
	c.Sweep(time.Now().Add(-time.Millisecond))
	if d.stopCount() != before {
		t.Fatal("old command stopped successor")
	}
	if err = c.Renew(target.Key, "operator-b", current, target.Revision); err != nil {
		t.Fatal(err)
	}
	c.Sweep(time.Now().Add(2 * time.Second))
	if d.stopCount() != before+1 {
		t.Fatal("lost browser did not stop camera")
	}
	if err = c.Renew(target.Key, "operator-b", current, target.Revision); err == nil {
		t.Fatal("expired command revived")
	}
}
