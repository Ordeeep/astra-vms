//go:build linux

package media

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
)

const guardArgument = "--astra-private-process-guard"
const guardFDEnvironment = "VMS_PRIVATE_GUARD_FD"

// OwnedProcess retains a lifetime pipe to a small re-executed guardian.
// EOF (including abrupt owner death) kills the target's entire process group.
// Unlike Pdeathsig alone, this also covers ordinary non-detaching descendants.
// Installed services additionally use systemd cgroup cleanup for forced stops.
type OwnedProcess struct {
	Cmd       *exec.Cmd
	lifetime  *os.File
	stopOnce  sync.Once
	stopError error
}

func (p *OwnedProcess) Stop() error {
	p.stopOnce.Do(func() { p.stopError = p.lifetime.Close() })
	return p.stopError
}

func StartOwned(cmd *exec.Cmd) (*OwnedProcess, error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer read.Close()
	ready, notify, err := os.Pipe()
	if err != nil {
		write.Close()
		return nil, err
	}
	defer ready.Close()
	defer notify.Close()
	p := &OwnedProcess{Cmd: cmd, lifetime: write}
	fd := 3 + len(cmd.ExtraFiles)
	cmd.Args = append([]string{executable, guardArgument, cmd.Path}, cmd.Args...)
	cmd.Path = executable
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, guardFDEnvironment+"="+strconv.Itoa(fd))
	cmd.ExtraFiles = append(cmd.ExtraFiles, read, notify)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// CommandContext must ask the guardian to clean up, rather than kill only it.
	if cmd.Cancel != nil {
		cmd.Cancel = p.Stop
	}
	if err = cmd.Start(); err != nil {
		p.Stop()
		return nil, err
	}
	// Only the guardian retains the reader/notifier. A start error closes ready.
	read.Close()
	notify.Close()
	var status [1]byte
	if _, err = io.ReadFull(ready, status[:]); err != nil || status[0] != 1 {
		p.Stop()
		cmd.Wait()
		return nil, errors.New("owned media executable could not start")
	}
	return p, nil
}

func init() {
	if len(os.Args) < 4 || os.Args[1] != guardArgument {
		return
	}
	fd, err := strconv.Atoi(os.Getenv(guardFDEnvironment))
	if err != nil || fd < 3 {
		os.Exit(125)
	}
	os.Unsetenv(guardFDEnvironment)
	lifetime := os.NewFile(uintptr(fd), "owner-lifetime")
	ready := os.NewFile(uintptr(fd+1), "target-ready")
	// Do not let exec inherit the ownership pipe: only this guardian reads it.
	syscall.CloseOnExec(fd)
	syscall.CloseOnExec(fd + 1)
	child := exec.Command(os.Args[2])
	child.Args = os.Args[3:]
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	for n := 3; n < fd; n++ {
		child.ExtraFiles = append(child.ExtraFiles, os.NewFile(uintptr(n), "inherited"))
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	if child.Start() != nil {
		ready.Close()
		os.Exit(127)
	}
	ready.Write([]byte{1})
	ready.Close()
	ownerGone := make(chan struct{})
	go func() { io.Copy(io.Discard, lifetime); close(ownerGone) }()
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	var result error
	select {
	case result = <-exited:
	case <-ownerGone:
		syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
		result = <-exited
	case <-signals:
		syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
		result = <-exited
	}
	// Clean up descendants left behind even when the main target exited normally.
	syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
	if result == nil {
		os.Exit(0)
	}
	var exit *exec.ExitError
	if errors.As(result, &exit) && exit.ExitCode() > 0 {
		os.Exit(exit.ExitCode())
	}
	os.Exit(1)
}
