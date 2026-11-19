//go:build linux

package media_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"vms.local/backend/internal/media"
)

func TestLinuxOwnedCommandResultAndCancellation(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "printf owned-output; exit 7")
	var out strings.Builder
	cmd.Stdout = &out
	p, err := media.StartOwned(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	var exit *exec.ExitError
	if err = cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 7 || out.String() != "owned-output" {
		t.Fatalf("result=%v output=%q", err, out.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd = exec.CommandContext(ctx, "/bin/sleep", "60")
	p, err = media.StartOwned(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("cancelled command succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("context cancellation left media running")
	}
}

func TestLinuxOwnedSupervisorHelper(t *testing.T) {
	root := os.Getenv("VMS_LINUX_OWNER_TEST")
	if root == "" {
		return
	}
	cmd := exec.Command("/bin/sh", "-c", `echo $$ > "$1/child"; sleep 60 & echo $! > "$1/grandchild"; wait`, "sh", root)
	p, err := media.StartOwned(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	if err = os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
}

func TestLinuxOwnerDeathAndStopTerminateDescendants(t *testing.T) {
	for _, abrupt := range []bool{false, true} {
		t.Run(strconv.FormatBool(abrupt), func(t *testing.T) {
			root := t.TempDir()
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(exe, "-test.run=^TestLinuxOwnedSupervisorHelper$")
			cmd.Env = append(os.Environ(), "VMS_LINUX_OWNER_TEST="+root)
			var owned *media.OwnedProcess
			if abrupt {
				err = cmd.Start()
			} else {
				owned, err = media.StartOwned(cmd)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if owned != nil {
					owned.Stop()
				}
				cmd.Process.Kill()
				cmd.Wait()
			}()
			pids := []int{}
			for _, name := range []string{"child", "grandchild"} {
				var raw []byte
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					raw, err = os.ReadFile(filepath.Join(root, name))
					if err == nil && len(strings.TrimSpace(string(raw))) > 0 {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
				pid, e := strconv.Atoi(strings.TrimSpace(string(raw)))
				if err != nil || e != nil {
					t.Fatalf("%s not ready: %v %v", name, err, e)
				}
				pids = append(pids, pid)
			}
			if abrupt {
				err = cmd.Process.Kill()
			} else {
				err = owned.Stop()
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, pid := range pids {
				deadline := time.Now().Add(3 * time.Second)
				for processAlive(pid) && time.Now().Before(deadline) {
					time.Sleep(20 * time.Millisecond)
				}
				if processAlive(pid) {
					t.Fatalf("descendant %d outlived owner", pid)
				}
			}
		})
	}
}

func processAlive(pid int) bool {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	_, state, ok := strings.Cut(string(raw), ") ")
	return ok && !strings.HasPrefix(state, "Z ")
}
