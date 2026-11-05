package servicehost

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSupervisorRestartsCrashedComponentAndStopsOwnedChild(t *testing.T) {
	if os.Getenv("VMS_SUPERVISOR_CHILD") == "1" {
		os.Exit(7)
	}
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := Config{Executable: executable, Arguments: []string{"-test.run=^TestSupervisorRestartsCrashedComponentAndStopsOwnedChild$"}, Directory: root, Environment: map[string]string{"VMS_SUPERVISOR_CHILD": "1"}, Log: filepath.Join(root, "component.log")}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pids := make(chan int, 4)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c, func(pid int) { pids <- pid }) }()
	first, second := 0, 0
	select {
	case first = <-pids:
	case <-time.After(5 * time.Second):
		t.Fatal("component did not start")
	}
	select {
	case second = <-pids:
	case <-time.After(5 * time.Second):
		t.Fatal("component did not restart")
	}
	if first == second {
		t.Fatal("restart reused process identity")
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("supervisor failed to stop")
	}
}

func TestSupervisorAllowsChildToFlushBeforeStopping(t *testing.T) {
	if os.Getenv("VMS_FLUSH_CHILD") == "1" {
		io.Copy(io.Discard, os.Stdin)
		if os.WriteFile(os.Getenv("VMS_FLUSH_MARKER"), []byte("flushed"), 0600) != nil {
			os.Exit(9)
		}
		os.Exit(0)
	}
	root := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "flushed.txt")
	c := Config{Executable: exe, Arguments: []string{"-test.run=^TestSupervisorAllowsChildToFlushBeforeStopping$"}, Directory: root, Log: filepath.Join(root, "log.txt"), Environment: map[string]string{"VMS_FLUSH_CHILD": "1", "VMS_FLUSH_MARKER": marker}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan int, 1)
	done := make(chan error, 1)
	go func() { done <- Run(ctx, c, func(pid int) { started <- pid }) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("child did not start")
	}
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not stop")
	}
	raw, err := os.ReadFile(marker)
	if err != nil || string(raw) != "flushed" {
		t.Fatal("child was killed before flushing")
	}
}
