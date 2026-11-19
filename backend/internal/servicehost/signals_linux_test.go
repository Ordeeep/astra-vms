//go:build linux

package servicehost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestSIGTERMDrainsApplicationContext(t *testing.T) {
	if marker := os.Getenv("VMS_SIGNAL_TEST"); marker != "" {
		ctx, cancel := SignalContext(context.Background())
		defer cancel()
		if err := os.WriteFile(marker, nil, 0600); err != nil {
			os.Exit(2)
		}
		<-ctx.Done()
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(exe, "-test.run=^TestSIGTERMDrainsApplicationContext$")
	cmd.Env = append(os.Environ(), "VMS_SIGNAL_TEST="+marker)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = os.Stat(marker); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("child did not install signal handler")
	}
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("SIGTERM bypassed graceful context: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SIGTERM did not cancel context")
	}
}
