package rpc

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func readAll(t *testing.T, stream string) ([][]byte, error) {
	t.Helper()
	r := bufio.NewReader(strings.NewReader(stream))
	var frames [][]byte
	for {
		body, err := ReadFrame(r)
		if err != nil {
			return frames, err
		}
		frames = append(frames, body)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	bodies := []string{`{"jsonrpc":"2.0"}`, ``, `{"ñ":"€"}`, strings.Repeat("x", 70000)}
	for _, b := range bodies {
		if err := WriteFrame(&buf, []byte(b)); err != nil {
			t.Fatal(err)
		}
	}
	frames, err := readAll(t, buf.String())
	if !errors.Is(err, io.EOF) {
		t.Fatalf("the stream must end with a clean EOF, got %v", err)
	}
	if len(frames) != len(bodies) {
		t.Fatalf("%d frames read, want %d", len(frames), len(bodies))
	}
	for i, b := range bodies {
		if string(frames[i]) != b {
			t.Errorf("frame %d = %q, want %q", i, frames[i], b)
		}
	}
}

func TestReadFrame_Headers(t *testing.T) {
	ok := []struct{ name, stream, want string }{
		{"other headers are ignored", "Content-Type: application/json\r\nContent-Length: 2\r\nX-Other: y\r\n\r\n{}", "{}"},
		{"header names ignore case", "content-length: 2\r\n\r\n{}", "{}"},
		{"spaces around the value", "Content-Length:   2  \r\n\r\n{}", "{}"},
	}
	for _, tt := range ok {
		t.Run(tt.name, func(t *testing.T) {
			frames, err := readAll(t, tt.stream)
			if !errors.Is(err, io.EOF) || len(frames) != 1 || string(frames[0]) != tt.want {
				t.Errorf("frames = %q, err = %v", frames, err)
			}
		})
	}

	bad := []struct{ name, stream string }{
		{"no Content-Length", "X-Other: y\r\n\r\n{}"},
		{"Content-Length twice", "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}"},
		{"negative length", "Content-Length: -1\r\n\r\n"},
		{"signed length", "Content-Length: +2\r\n\r\n{}"},
		{"length with letters", "Content-Length: 2x\r\n\r\n{}"},
		{"empty length", "Content-Length: \r\n\r\n"},
		{"length over the cap", "Content-Length: 16777217\r\n\r\n"},
		{"absurd length", "Content-Length: 99999999999999999999\r\n\r\n"},
		{"line ended by a bare LF", "Content-Length: 2\n\n{}"},
		{"header without a colon", "Content-Length 2\r\n\r\n{}"},
		{"stream ends inside the headers", "Content-Length: 2\r\n"},
		{"stream ends inside the body", "Content-Length: 10\r\n\r\n{}"},
		{"headers over the cap", "X-Pad: " + strings.Repeat("a", MaxHeaderBytes) + "\r\nContent-Length: 2\r\n\r\n{}"},
		{"endless header line", strings.Repeat("a", 4*MaxHeaderBytes)},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			if frames, err := readAll(t, tt.stream); !errors.Is(err, ErrMalformedFrame) {
				t.Errorf("frames = %q, err = %v; want a malformed-frame error", frames, err)
			}
		})
	}
}

// A header that announces a large frame must not reserve that memory before
// the bytes arrive.
func TestReadFrame_LyingLengthAllocatesNothing(t *testing.T) {
	stream := "Content-Length: 16777216\r\n\r\nshort"
	allocs := testing.AllocsPerRun(20, func() {
		_, _ = ReadFrame(bufio.NewReader(strings.NewReader(stream)))
	})
	// A handful of small allocations (reader, builder, error); one 16 MiB
	// slice would not change the count, so measure bytes too.
	if allocs > 40 {
		t.Errorf("%v allocations for a short stream", allocs)
	}
	var before, after testingMemStats
	before.read()
	for range 50 {
		_, _ = ReadFrame(bufio.NewReader(strings.NewReader(stream)))
	}
	after.read()
	if grown := after.total - before.total; grown > 8<<20 {
		t.Errorf("50 lying headers allocated %d bytes: the announced size was reserved up front", grown)
	}
}

func TestWriteFrame_RefusesOversize(t *testing.T) {
	if err := WriteFrame(io.Discard, make([]byte, MaxFrameBytes+1)); !errors.Is(err, ErrMalformedFrame) {
		t.Errorf("an oversize frame was written: %v", err)
	}
}
