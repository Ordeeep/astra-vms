//go:build windows

package media

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// OwnedProcess puts the media server and descendants in a kill-on-close job.
// If the supervisor process dies, Windows closes its job handle and kills
// the media children; an old authenticated stream cannot outlive the guard.
type OwnedProcess struct {
	Cmd       *exec.Cmd
	job       windows.Handle
	stopOnce  sync.Once
	stopError error
}

func StartOwned(cmd *exec.Cmd) (*OwnedProcess, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err = cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		windows.CloseHandle(process)
	}
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		windows.CloseHandle(job)
		return nil, err
	}
	return &OwnedProcess{Cmd: cmd, job: job}, nil
}
func (p *OwnedProcess) Stop() error {
	p.stopOnce.Do(func() { p.stopError = windows.CloseHandle(p.job) })
	return p.stopError
}
