package rpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// The handshake comes before any frame of the protocol proper.
//
//  1. The server writes the magic bytes and a frame with its Hello.
//  2. The client discards everything up to the magic. A login shell may have
//     printed a banner on the same stream, and after the magic the stream is
//     known to be the server speaking.
//  3. The client answers with its own version and commit.
//  4. The server accepts only an exact match, and says so in one more frame.
//     Client and server are built from the same commit, so nothing promises
//     compatibility across versions: a mismatch means "install the right
//     server".

// Magic marks the start of the server's output: two bytes no terminal prints,
// then "gb". It is deliberately not the gzip header.
var Magic = []byte{0x00, 0xFE, 'g', 'b'}

// Hello is what the server says about itself.
type Hello struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	PID     int    `json:"pid"`
	// Daemon is set by a persistent server (#247).
	Daemon *DaemonHello `json:"daemon,omitempty"`
	// Fallback names, as an identifier, why a host that runs a persistent
	// server serves this session in its own process instead (#247): the
	// work of the session stops when its link drops.
	Fallback string `json:"fallback,omitempty"`
}

// clientHello is the client's answer.
type clientHello struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// verdict is the server's last word of the handshake.
type verdict struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Handshake errors. The client reports them with the matching UserError code.
var (
	ErrBannerOverflow  = errors.New("no gitbox-server answered: too much output before its greeting")
	ErrVersionMismatch = errors.New("client and server are different builds")
)

// serverHandshake runs the server's half. It returns ErrVersionMismatch after
// telling the client, when the builds differ.
func serverHandshake(w io.Writer, r *bufio.Reader, self Hello) error {
	if _, err := w.Write(Magic); err != nil {
		return err
	}
	if err := writeJSONFrame(w, self); err != nil {
		return err
	}
	var peer clientHello
	if err := readJSONFrame(r, &peer); err != nil {
		return fmt.Errorf("reading the client's greeting: %w", err)
	}
	if peer.Version != self.Version || peer.Commit != self.Commit {
		_ = writeJSONFrame(w, verdict{Error: ErrCodeVersionMismatch})
		return ErrVersionMismatch
	}
	return writeJSONFrame(w, verdict{OK: true})
}

// clientHandshake runs the client's half and returns the server's Hello.
func clientHandshake(w io.Writer, r *bufio.Reader, version, commit string) (Hello, error) {
	if err := skipToMagic(r); err != nil {
		return Hello{}, err
	}
	var server Hello
	if err := readJSONFrame(r, &server); err != nil {
		return Hello{}, fmt.Errorf("reading the server's greeting: %w", err)
	}
	if err := writeJSONFrame(w, clientHello{Version: version, Commit: commit}); err != nil {
		return server, err
	}
	var v verdict
	if err := readJSONFrame(r, &v); err != nil {
		return server, fmt.Errorf("reading the server's verdict: %w", err)
	}
	if !v.OK {
		if v.Error == ErrCodeVersionMismatch {
			return server, ErrVersionMismatch
		}
		return server, fmt.Errorf("the server refused the connection: %s", sanitizeToken(v.Error))
	}
	return server, nil
}

// skipToMagic discards bytes until Magic has gone by. It gives up, with
// ErrBannerOverflow, after MaxBannerBytes of anything else.
func skipToMagic(r *bufio.Reader) error {
	matched, discarded := 0, 0
	for {
		c, err := r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("the stream ended before a gitbox-server greeting: %w", io.ErrUnexpectedEOF)
			}
			return err
		}
		switch {
		case c == Magic[matched]:
			matched++
			if matched == len(Magic) {
				return nil
			}
			continue
		case c == Magic[0]:
			// The byte may start the magic again; what matched so far
			// was banner after all.
			discarded += matched
			matched = 1
		default:
			discarded += matched + 1
			matched = 0
		}
		if discarded > MaxBannerBytes {
			return ErrBannerOverflow
		}
	}
}

// sanitizeToken keeps a short identifier sent by the peer printable: the
// peer is untrusted, and its words may end up in a message.
func sanitizeToken(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if len(out) == 64 {
			break
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

func writeJSONFrame(w io.Writer, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFrame(w, body)
}

func readJSONFrame(r *bufio.Reader, v any) error {
	body, err := ReadFrame(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}
