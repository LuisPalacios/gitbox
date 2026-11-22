package remote

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// LogLines is how many lines the connection log keeps.
const LogLines = 2000

// maxLogLine caps one line of the log, in bytes.
const maxLogLine = 400

// Log is the connection log: a ring of the last LogLines lines, each with
// its time. Every step writes its timing and decisions here, and what ssh
// and the server print on stderr lands here too. Lines are kept printable
// and short; a caller that shows them still scrubs them for secrets.
type Log struct {
	mu     sync.Mutex
	lines  []string
	next   int
	full   bool
	now    func() time.Time
	onLine func(string)
	part   []byte // an unfinished line written through Write
}

// NewLog returns an empty log. onLine, when not nil, receives every line as
// it is added; it must not call back into the log.
func NewLog(onLine func(string)) *Log {
	return &Log{lines: make([]string, LogLines), now: time.Now, onLine: onLine}
}

// Printf adds one line. A nil log discards it.
func (l *Log) Printf(format string, args ...any) {
	if l == nil {
		return
	}
	l.add(fmt.Sprintf(format, args...))
}

// Write adds what p holds, one line per newline, so the log can stand in for
// an io.Writer. A line without its newline waits for the next write.
func (l *Log) Write(p []byte) (int, error) {
	if l == nil {
		return len(p), nil
	}
	l.mu.Lock()
	l.part = append(l.part, p...)
	var done []string
	for {
		i := bytes.IndexByte(l.part, '\n')
		if i < 0 {
			break
		}
		done = append(done, string(l.part[:i]))
		l.part = l.part[i+1:]
	}
	if len(l.part) > maxLogLine {
		done = append(done, string(l.part))
		l.part = nil
	}
	l.mu.Unlock()
	for _, line := range done {
		l.add(line)
	}
	return len(p), nil
}

func (l *Log) add(text string) {
	line := l.now().Format("15:04:05.000") + " " + clean(text)
	l.mu.Lock()
	l.lines[l.next] = line
	l.next = (l.next + 1) % len(l.lines)
	if l.next == 0 {
		l.full = true
	}
	onLine := l.onLine
	l.mu.Unlock()
	if onLine != nil {
		onLine(line)
	}
}

// Lines returns the log, oldest line first.
func (l *Log) Lines() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.full {
		return append([]string(nil), l.lines[:l.next]...)
	}
	out := make([]string, 0, len(l.lines))
	out = append(out, l.lines[l.next:]...)
	return append(out, l.lines[:l.next]...)
}

// clean keeps a line printable and short: control characters go, invalid
// UTF-8 is replaced, and the line is cut at maxLogLine bytes.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "?")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, s)
	s = strings.TrimRight(s, " ")
	if len(s) > maxLogLine {
		cut := maxLogLine
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}
