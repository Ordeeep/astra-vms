package media

import (
	"bytes"
	"io"
	"regexp"
	"sync"
)

var privateLogURL = regexp.MustCompile(`(?i)(?:https?|rtsps?)://[^\s]+`)

// RedactedLog buffers one bounded line from a media process. Source URLs and
// authentication callback URLs must never reach persistent process logs.
// Buffering is necessary because os/exec can split a URL across Write calls.
type RedactedLog struct {
	mu       sync.Mutex
	output   io.Writer
	pending  []byte
	dropping bool
}

func NewRedactedLog(output io.Writer) *RedactedLog { return &RedactedLog{output: output} }

func (l *RedactedLog) emit() error {
	line := privateLogURL.ReplaceAll(l.pending, []byte("[URL redacted]"))
	_, err := l.output.Write(line)
	l.pending = l.pending[:0]
	return err
}

func (l *RedactedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	size := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, '\n')
		n := len(p)
		if end >= 0 {
			n = end + 1
		}
		if !l.dropping && len(l.pending)+n > 64<<10 {
			l.pending = l.pending[:0]
			l.dropping = true
			if _, err := io.WriteString(l.output, "[oversized media log line omitted]\n"); err != nil {
				return size, err
			}
		}
		if !l.dropping {
			l.pending = append(l.pending, p[:n]...)
		}
		p = p[n:]
		if end >= 0 {
			if !l.dropping {
				if err := l.emit(); err != nil {
					return size, err
				}
			}
			l.dropping = false
		}
	}
	return size, nil
}

func (l *RedactedLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.pending) > 0 && !l.dropping {
		return l.emit()
	}
	return nil
}
