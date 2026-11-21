package rpc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// A frame is the LSP base protocol unit:
//
//	Content-Length: N\r\n
//	\r\n
//	<N bytes of UTF-8 JSON>
//
// Other headers are allowed and ignored. A frame that breaks the rules ends
// the connection: there is no attempt to find the next frame in the stream.

// ErrMalformedFrame is the cause of every framing error.
var ErrMalformedFrame = errors.New("malformed frame")

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformedFrame, fmt.Sprintf(format, args...))
}

// ReadFrame reads one frame from r and returns its body. It returns io.EOF
// only when the stream ends cleanly between frames.
func ReadFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	headerBytes := 0
	for first := true; ; first = false {
		line, n, err := readHeaderLine(r, MaxHeaderBytes-headerBytes)
		headerBytes += n
		if err != nil {
			if errors.Is(err, io.EOF) && first && n == 0 {
				return nil, io.EOF
			}
			if errors.Is(err, io.EOF) {
				return nil, malformed("stream ended inside the headers")
			}
			return nil, err
		}
		if line == "" {
			break // the blank line that ends the headers
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, malformed("header without a colon")
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		if length >= 0 {
			return nil, malformed("Content-Length given twice")
		}
		length, err = parseLength(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
	}
	if length < 0 {
		return nil, malformed("no Content-Length")
	}

	// The body grows as bytes arrive, so a header that announces a large
	// frame costs nothing until the bytes are really there.
	var body bytes.Buffer
	if _, err := io.CopyN(&body, r, int64(length)); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, malformed("stream ended inside a body of %d bytes", length)
		}
		return nil, err
	}
	return body.Bytes(), nil
}

// readHeaderLine reads one header line, without its line ending, and reports
// how many bytes it consumed. It refuses to read more than limit bytes.
func readHeaderLine(r *bufio.Reader, limit int) (line string, n int, err error) {
	var b strings.Builder
	for {
		if n >= limit {
			return "", n, malformed("headers longer than %d bytes", MaxHeaderBytes)
		}
		c, err := r.ReadByte()
		if err != nil {
			return "", n, err
		}
		n++
		if c == '\n' {
			s := b.String()
			if !strings.HasSuffix(s, "\r") {
				return "", n, malformed("header line not ended by CRLF")
			}
			return strings.TrimSuffix(s, "\r"), n, nil
		}
		b.WriteByte(c)
	}
}

// parseLength accepts only plain decimal digits within the frame cap.
func parseLength(s string) (int, error) {
	if s == "" || len(s) > 9 {
		return 0, malformed("Content-Length is not a valid size")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, malformed("Content-Length is not a number")
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, malformed("Content-Length is not a number")
	}
	if n > MaxFrameBytes {
		return 0, malformed("frame of %d bytes exceeds the %d-byte limit", n, MaxFrameBytes)
	}
	return n, nil
}

// WriteFrame writes body as one frame. The header and the body go out in a
// single Write, so frames from different goroutines never interleave as
// long as the writer serialises its writes.
func WriteFrame(w io.Writer, body []byte) error {
	if len(body) > MaxFrameBytes {
		return malformed("frame of %d bytes exceeds the %d-byte limit", len(body), MaxFrameBytes)
	}
	buf := make([]byte, 0, len(body)+32)
	buf = append(buf, "Content-Length: "...)
	buf = strconv.AppendInt(buf, int64(len(body)), 10)
	buf = append(buf, "\r\n\r\n"...)
	buf = append(buf, body...)
	_, err := w.Write(buf)
	return err
}
