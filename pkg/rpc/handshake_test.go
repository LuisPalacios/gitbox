package rpc

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

// helloStream is what a server writes at the start, after an optional
// banner: the magic, its Hello and its verdict.
func helloStream(t *testing.T, banner []byte, ok bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write(banner)
	buf.Write(Magic)
	if err := writeJSONFrame(&buf, Hello{Version: testVersion, Commit: testCommit, OS: "linux", Arch: "amd64", PID: 7}); err != nil {
		t.Fatal(err)
	}
	v := verdict{OK: ok}
	if !ok {
		v.Error = ErrCodeVersionMismatch
	}
	if err := writeJSONFrame(&buf, v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestHandshake_ClientSkipsABanner(t *testing.T) {
	banners := map[string][]byte{
		"none":               nil,
		"a login message":    []byte("Welcome to Ubuntu 24.04 LTS\r\nLast login: today\r\n"),
		"64 KiB of garbage":  bytes.Repeat([]byte{0x1b, '[', '0', 'm', 0xff, 0x00, 'x', '\n'}, 8192),
		"false starts":       bytes.Repeat([]byte{0x00, 0xFE, 'g', 0x00, 0x00, 0xFE}, 500),
		"exactly at the cap": bytes.Repeat([]byte("b"), MaxBannerBytes),
	}
	for name, banner := range banners {
		t.Run(name, func(t *testing.T) {
			var sent bytes.Buffer
			hello, err := clientHandshake(&sent, bufio.NewReader(bytes.NewReader(helloStream(t, banner, true))), testVersion, testCommit)
			if err != nil {
				t.Fatalf("handshake after %d banner bytes: %v", len(banner), err)
			}
			if hello.Version != testVersion || hello.Commit != testCommit || hello.OS != "linux" || hello.PID != 7 {
				t.Errorf("hello = %+v", hello)
			}
			var mine clientHello
			if err := readJSONFrame(bufio.NewReader(&sent), &mine); err != nil || mine.Version != testVersion || mine.Commit != testCommit {
				t.Errorf("the client answered %+v, %v", mine, err)
			}
		})
	}
}

func TestHandshake_BannerOverflow(t *testing.T) {
	banner := bytes.Repeat([]byte("b"), MaxBannerBytes+1)
	_, err := clientHandshake(&bytes.Buffer{}, bufio.NewReader(bytes.NewReader(helloStream(t, banner, true))), testVersion, testCommit)
	if !errors.Is(err, ErrBannerOverflow) {
		t.Fatalf("err = %v, want the banner overflow", err)
	}
	// An endless stream that never greets ends the same way, without
	// reading it all.
	endless := &countingReader{}
	_, err = clientHandshake(&bytes.Buffer{}, bufio.NewReader(endless), testVersion, testCommit)
	if !errors.Is(err, ErrBannerOverflow) {
		t.Errorf("err = %v, want the banner overflow", err)
	}
	if endless.n > 2*MaxBannerBytes {
		t.Errorf("%d bytes were read from a stream that never greets", endless.n)
	}
}

type countingReader struct{ n int }

func (r *countingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.n += len(p)
	return len(p), nil
}

func TestHandshake_StreamEndsEarly(t *testing.T) {
	for name, stream := range map[string]string{
		"nothing at all":     "",
		"only a banner":      "Permission denied (publickey).\n",
		"magic and no hello": string(Magic),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := clientHandshake(&bytes.Buffer{}, bufio.NewReader(strings.NewReader(stream)), testVersion, testCommit)
			if err == nil || errors.Is(err, ErrBannerOverflow) || errors.Is(err, ErrVersionMismatch) {
				t.Errorf("err = %v, want a plain failure", err)
			}
		})
	}
}

func TestHandshake_VersionMismatch(t *testing.T) {
	cases := map[string]ClientOptions{
		"another commit":  {Version: testVersion, Commit: strings.Repeat("f", 40)},
		"another version": {Version: "v9.9.8", Commit: testCommit},
		"a short commit":  {Version: testVersion, Commit: testCommit[:7]},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := tryConnect(t, newServer(nil), opts)
			wantCode(t, err, CodeApplication, ErrCodeVersionMismatch)
		})
	}

	// The server's Serve reports the mismatch too, so its process can exit.
	var out bytes.Buffer
	var in bytes.Buffer
	if err := writeJSONFrame(&in, clientHello{Version: "other", Commit: testCommit}); err != nil {
		t.Fatal(err)
	}
	if err := serverHandshake(&out, bufio.NewReader(&in), newServer(nil).Self); !errors.Is(err, ErrVersionMismatch) {
		t.Errorf("serverHandshake = %v, want the version mismatch", err)
	}
}

func TestHandshake_MatchingBuildsConnect(t *testing.T) {
	l := connect(t, newServer(nil))
	if got := l.Client.Server(); got.Version != testVersion || got.Commit != testCommit || got.PID != 4242 {
		t.Errorf("Server() = %+v", got)
	}
}
