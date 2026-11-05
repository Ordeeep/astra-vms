// Package servicehost supervises a single installation component. Children
// inherit a kill-on-close Windows job; configuration and secrets stay on disk.
package servicehost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vms.local/backend/internal/media"
)

type Config struct {
	Executable  string            `json:"executable"`
	Arguments   []string          `json:"arguments"`
	Directory   string            `json:"directory"`
	Environment map[string]string `json:"environment"`
	Log         string            `json:"log"`
}

func Load(path string) (Config, error) {
	var c Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	if !filepath.IsAbs(c.Executable) || !filepath.IsAbs(c.Directory) || !filepath.IsAbs(c.Log) {
		return c, errors.New("component paths must be absolute")
	}
	if _, err = os.Stat(c.Executable); err != nil {
		return c, errors.New("component executable unavailable")
	}
	for key := range c.Environment {
		if strings.ContainsAny(key, "=\x00") || key == "" {
			return c, errors.New("invalid environment name")
		}
	}
	return c, nil
}

// Run restarts only the exact executable in its immutable component config.
// Closing stdin lets VMS components flush and stop; unresponsive descendants
// are terminated after the bounded shutdown allowance.
func Run(ctx context.Context, c Config, started func(int)) error {
	if err := os.MkdirAll(filepath.Dir(c.Log), 0700); err != nil {
		return err
	}
	log := &rotatingLog{path: c.Log, limit: 5 << 20}
	defer log.Close()
	delay := time.Second
	for ctx.Err() == nil {
		cmd := exec.Command(c.Executable, c.Arguments...)
		cmd.Dir = c.Directory
		environment := map[string]string{}
		for _, value := range os.Environ() {
			k, v, ok := strings.Cut(value, "=")
			if ok {
				environment[strings.ToUpper(k)] = v
			}
		}
		for k, v := range c.Environment {
			environment[strings.ToUpper(k)] = v
		}
		environment["VMS_SUPERVISED"] = "1"
		for k, v := range environment {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Stdout = log
		cmd.Stderr = log
		input, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		began := time.Now()
		process, err := media.StartOwned(cmd)
		if err == nil {
			if started != nil {
				started(cmd.Process.Pid)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-ctx.Done():
				input.Close()
				select {
				case <-done:
				case <-time.After(8 * time.Second):
					process.Stop()
					<-done
				}
				process.Stop()
				return nil
			case <-done:
				input.Close()
				process.Stop()
			}
		} else {
			input.Close()
			log.Write([]byte("Component start failed; retry scheduled.\n"))
		}
		if time.Since(began) > time.Minute {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		delay = min(30*time.Second, delay*2)
	}
	return nil
}

type rotatingLog struct {
	mu          sync.Mutex
	path        string
	limit, size int64
	file        *os.File
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		f, e := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return 0, e
		}
		l.file = f
		if info, e := f.Stat(); e == nil {
			l.size = info.Size()
		}
	}
	if l.size+int64(len(p)) > l.limit {
		l.file.Close()
		l.file = nil
		// Only these two log generations, inside the configured component folder.
		if e := os.Remove(l.path + ".1"); e != nil && !errors.Is(e, os.ErrNotExist) {
			return 0, e
		}
		if e := os.Rename(l.path, l.path+".1"); e != nil {
			return 0, e
		}
		f, e := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if e != nil {
			return 0, e
		}
		l.file = f
		l.size = 0
	}
	n, e := l.file.Write(p)
	l.size += int64(n)
	return n, e
}
func (l *rotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// WatchParent is enabled only for the installed supervisor. Interactive CLI
// sessions keep their existing signal behavior.
func WatchParent(cancel context.CancelFunc) {
	if os.Getenv("VMS_SUPERVISED") == "1" {
		go func() { io.Copy(io.Discard, os.Stdin); cancel() }()
	}
}
