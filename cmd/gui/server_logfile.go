//go:build gitboxserver

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
)

// The server log under the server root (#239): <root>/logs/<build>.log,
// rotated at serverLogMax and keeping serverLogKeep older files. Garbage
// collection removes the logs of builds that are gone.

const (
	serverLogMax  = 1 << 20
	serverLogKeep = 3
)

func init() {
	serverLog = func(stderr io.Writer) io.Writer {
		return openServerLog(installedExe(), stderr)
	}
}

// openServerLog returns a writer that sends every line to stderr, which the
// client shows in its connection log, and to the log file. A binary that is
// not installed under a server root logs to stderr only.
func openServerLog(exe string, stderr io.Writer) io.Writer {
	root, build, ok := remote.Locate(exe)
	if !ok {
		return stderr
	}
	dir := filepath.Join(root, remote.LogsDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return stderr
	}
	f, err := newRotatingLog(filepath.Join(dir, build+".log"), serverLogMax, serverLogKeep)
	if err != nil {
		return stderr
	}
	return teeLog{stderr, f}
}

// teeLog writes to both writers. A failing file never stops the copy that
// goes to the client, and the other way round.
type teeLog struct{ primary, file io.Writer }

func (t teeLog) Write(p []byte) (int, error) {
	_, _ = t.file.Write(p)
	_, _ = t.primary.Write(p)
	return len(p), nil
}

// rotatingLog is a log file that rotates itself: when the next write would
// pass max bytes, path becomes path.1, path.1 becomes path.2, and so on up
// to keep files. Each line starts with its time.
type rotatingLog struct {
	mu        sync.Mutex
	path      string
	max       int64
	keep      int
	f         *os.File
	size      int64
	lineStart bool
	now       func() time.Time
}

func newRotatingLog(path string, max int64, keep int) (*rotatingLog, error) {
	l := &rotatingLog{path: path, max: max, keep: keep, lineStart: true, now: time.Now}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *rotatingLog) open() error {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	l.f, l.size = f, info.Size()
	return nil
}

func (l *rotatingLog) rotate() error {
	_ = l.f.Close()
	for i := l.keep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
	return l.open()
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	written := len(p)
	var b bytes.Buffer
	for len(p) > 0 {
		if l.lineStart {
			b.WriteString(l.now().UTC().Format(time.RFC3339) + " ")
		}
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			b.Write(p)
			l.lineStart = false
			break
		}
		b.Write(p[:i+1])
		p = p[i+1:]
		l.lineStart = true
	}
	if l.size > 0 && l.size+int64(b.Len()) > l.max {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(b.Bytes())
	l.size += int64(n)
	return written, err
}
