package rpc

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"testing"
)

// FuzzDecodeFrame feeds arbitrary bytes to the frame reader. Whatever they
// are, it must not panic, must return a body within the cap, and must end
// with io.EOF or a malformed-frame error, never anything else. A frame it
// accepts must survive being written and read again.
func FuzzDecodeFrame(f *testing.F) {
	for _, seed := range []string{
		"Content-Length: 2\r\n\r\n{}",
		"Content-Length: 0\r\n\r\n",
		"content-length: 17\r\nX-Other: y\r\n\r\n{\"jsonrpc\":\"2.0\"}",
		"Content-Length: 16777217\r\n\r\n",
		"Content-Length: 5\r\n\r\n{}",
		"Content-Length: -1\r\n\r\n",
		"\x00\xfegbContent-Length: 2\r\n\r\n{}",
		"Content-Length: 2\n\n{}",
		"",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bufio.NewReader(bytes.NewReader(data))
		for range 64 {
			body, err := ReadFrame(r)
			if err != nil {
				if !errors.Is(err, io.EOF) && !errors.Is(err, ErrMalformedFrame) {
					t.Fatalf("unexpected error kind: %v", err)
				}
				return
			}
			if len(body) > MaxFrameBytes || len(body) > len(data) {
				t.Fatalf("a body of %d bytes came out of %d bytes of input", len(body), len(data))
			}
			var again bytes.Buffer
			if err := WriteFrame(&again, body); err != nil {
				t.Fatalf("an accepted body cannot be written back: %v", err)
			}
			back, err := ReadFrame(bufio.NewReader(&again))
			if err != nil || !bytes.Equal(back, body) {
				t.Fatalf("round trip changed the body: %q -> %q (%v)", body, back, err)
			}
		}
	})
}
