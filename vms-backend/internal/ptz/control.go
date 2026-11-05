// Package ptz serializes device control per camera, independent of vendor and
// HTTP. A renewal extends only its own command; it never starts movement again.
package ptz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
	"vms.local/backend/internal/camera"
)

type Target struct {
	Key, Revision string
	Driver        camera.PTZDriver
}
type command struct {
	ending                 bool
	token, owner, revision string
	driver                 camera.PTZDriver
	until                  time.Time
}
type entry struct {
	mu     sync.Mutex
	refs   int
	active *command
}
type Controller struct {
	mu      sync.Mutex
	entries map[string]*entry
	ttl     time.Duration
	closed  bool
}

func New(ttl time.Duration) *Controller { return &Controller{entries: map[string]*entry{}, ttl: ttl} }

func (c *Controller) acquire(key string) (*entry, func(), error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, nil, errors.New("ptz_unavailable")
	}
	e := c.entries[key]
	if e == nil {
		if len(c.entries) >= 1024 {
			c.mu.Unlock()
			return nil, nil, errors.New("ptz_capacity")
		}
		e = &entry{}
		c.entries[key] = e
	}
	e.refs++
	c.mu.Unlock()
	e.mu.Lock()
	release := func() {
		empty := e.active == nil
		e.mu.Unlock()
		c.mu.Lock()
		e.refs--
		if empty && e.refs == 0 {
			delete(c.entries, key)
		}
		c.mu.Unlock()
	}
	return e, release, nil
}
func (c *Controller) isClosed() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.closed }

func stop(e *entry) error {
	if e.active == nil {
		return nil
	}
	// Stop must survive a canceled browser request, using the ORIGINAL driver
	// (including original credentials), never a reloaded replacement source.
	e.active.ending = true
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := e.active.driver.Stop(ctx)
	if err == nil {
		e.active = nil
	} else {
		e.active.until = time.Now().Add(time.Second)
	}
	return err
}

func (c *Controller) Start(ctx context.Context, t Target, owner string, move camera.PTZMove, home bool) (string, error) {
	if (!home && !move.Valid()) || (home && move != (camera.PTZMove{})) {
		return "", errors.New("invalid_ptz_command")
	}
	e, release, err := c.acquire(t.Key)
	if err != nil {
		return "", err
	}
	defer release()
	if c.isClosed() {
		return "", errors.New("ptz_unavailable")
	}
	caps, err := t.Driver.Capabilities(ctx)
	if err != nil {
		return "", err
	}
	if (home && !caps.Home) || ((move.Pan != 0 || move.Tilt != 0) && !caps.PanTilt) || (move.Zoom != 0 && !caps.Zoom) {
		return "", errors.New("ptz_unsupported")
	}
	// Stop the previous command before Home (which has no velocity payload) or
	// switching sources. Same-source velocity commands replace all axes at once.
	if e.active != nil && (home || e.active.revision != t.Revision) {
		if err = stop(e); err != nil {
			return "", err
		}
	}
	var id [24]byte
	if _, err = rand.Read(id[:]); err != nil {
		return "", errors.New("ptz_unavailable")
	}
	token := hex.EncodeToString(id[:])
	e.active = &command{token: token, owner: owner, revision: t.Revision, driver: t.Driver, until: time.Now().Add(c.ttl)}
	if home {
		err = t.Driver.Home(ctx)
	} else {
		err = t.Driver.Move(ctx, move)
	}
	if err != nil {
		stop(e)
		return "", err
	}
	// An already released request may still have reached the device.
	if ctx.Err() != nil {
		stop(e)
		return "", errors.New("ptz_unavailable")
	}
	e.active.until = time.Now().Add(c.ttl)
	return token, nil
}

func (c *Controller) Renew(key, owner, token, revision string) error {
	e, release, err := c.acquire(key)
	if err != nil {
		return err
	}
	defer release()
	a := e.active
	if a == nil || a.owner != owner || a.token != token {
		return errors.New("ptz_superseded")
	}
	if a.ending || a.revision != revision || !time.Now().Before(a.until) {
		stop(e)
		return errors.New("ptz_expired")
	}
	a.until = time.Now().Add(c.ttl)
	return nil
}

func (c *Controller) Stop(key, owner, token string) (bool, error) {
	e, release, err := c.acquire(key)
	if err != nil {
		return false, err
	}
	defer release()
	if e.active == nil || e.active.owner != owner || e.active.token != token {
		return false, nil
	}
	return true, stop(e)
}

func (c *Controller) Sweep(now time.Time) {
	c.mu.Lock()
	keys := make([]string, 0, len(c.entries))
	for key := range c.entries {
		keys = append(keys, key)
	}
	c.mu.Unlock()
	var wg sync.WaitGroup
	// Different cameras must not wait on a disconnected camera's timeout.
	for _, key := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			e, release, err := c.acquire(key)
			if err != nil {
				return
			}
			defer release()
			if e.active != nil && !now.Before(e.active.until) {
				stop(e)
			}
		}(key)
	}
	wg.Wait()
}

func (c *Controller) Close() {
	c.mu.Lock()
	c.closed = true
	entries := make([]*entry, 0, len(c.entries))
	for _, e := range c.entries {
		entries = append(entries, e)
	}
	c.mu.Unlock()
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(e *entry) { defer wg.Done(); e.mu.Lock(); defer e.mu.Unlock(); stop(e) }(e)
	}
	wg.Wait()
}
