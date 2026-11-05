package media_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"vms.local/backend/internal/media"
)

func TestSupervisorHelper(t *testing.T) {
	path := os.Getenv("VMS_JOB_TEST_PID")
	if path == "" {
		return
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 60")
	owned, err := media.StartOwned(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer owned.Stop()
	if err := os.WriteFile(path, []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Second)
}
func TestSupervisorDeathTerminatesOwnedChild(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "child.pid")
	supervisor := exec.Command(exe, "-test.run=^TestSupervisorHelper$")
	supervisor.Env = append(os.Environ(), "VMS_JOB_TEST_PID="+path)
	if err := supervisor.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { supervisor.Process.Kill(); supervisor.Wait() }()
	var raw []byte
	for i := 0; i < 50; i++ {
		raw, err = os.ReadFile(path)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("child did not start")
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := supervisor.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		var code uint32
		windows.GetExitCodeProcess(handle, &code)
		if code != 259 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("media child outlived its supervisor by more than two seconds")
}
