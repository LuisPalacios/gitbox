//go:build gitboxserver

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func testBuild(n int) string {
	return fmt.Sprintf("v0.%d-%s-%08x", n, strings.Repeat("d", 40), n)
}

func installFake(t *testing.T, root, build string) string {
	t.Helper()
	exe := filepath.Join(root, build, remote.BinaryName)
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestInUseMarker(t *testing.T) {
	root := t.TempDir()
	cur, old := testBuild(1), testBuild(2)
	exe := installFake(t, root, cur)
	installFake(t, root, old)
	other := remote.MarkerPath(filepath.Join(root, cur), "client-b")
	if err := os.WriteFile(other, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var log syncBuffer
	stop := startInUse(exe, "client-a", &log, time.Now)
	path := remote.MarkerPath(filepath.Join(root, cur), "client-a")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no marker: %v", err)
	}
	var m remote.Marker
	if err := json.Unmarshal(data, &m); err != nil || m.ClientID != "client-a" || m.PID != os.Getpid() || m.StartedAt.IsZero() {
		t.Fatalf("marker = %s, %v", data, err)
	}
	stop()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the server's own marker survived its stop")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("the server deleted another client's marker")
	}
	// The collection ran after the start: the old build had no marker.
	if _, err := os.Stat(filepath.Join(root, old)); !os.IsNotExist(err) {
		t.Errorf("the old build survived the collection; log:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "gc: removed "+old) {
		t.Errorf("the collection was not logged:\n%s", log.String())
	}
}

func TestInUseMarkerNotInstalled(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "gitbox-server")
	stop := startInUse(exe, "client-a", &syncBuffer{}, time.Now)
	stop()
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a server outside a server root wrote %v", entries)
	}
	root := t.TempDir()
	exe = installFake(t, root, testBuild(1))
	startInUse(exe, "", &syncBuffer{}, time.Now)()
	if matches, _ := filepath.Glob(filepath.Join(root, "*", "in-use.*")); len(matches) != 0 {
		t.Errorf("a server without a client id wrote %v", matches)
	}
}

func TestServerLogFile(t *testing.T) {
	root := t.TempDir()
	build := testBuild(1)
	exe := installFake(t, root, build)
	var stderr syncBuffer
	w := openServerLog(exe, &stderr)
	fmt.Fprintln(w, "gitbox-server: serving client a")
	logPath := filepath.Join(root, remote.LogsDir, build+".log")
	data, err := os.ReadFile(logPath)
	if err != nil || !strings.HasSuffix(string(data), " gitbox-server: serving client a\n") {
		t.Fatalf("log file = %q, %v", data, err)
	}
	if stderr.String() != "gitbox-server: serving client a\n" {
		t.Errorf("stderr = %q", stderr.String())
	}
	if info, _ := os.Stat(logPath); info.Mode().Perm() != 0o600 {
		t.Errorf("log mode %v", info.Mode().Perm())
	}
	if w := openServerLog(filepath.Join(t.TempDir(), "gitbox-server"), &stderr); w != io.Writer(&stderr) {
		t.Error("a server outside a server root opened a log file")
	}
}

func TestServerLogRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.log")
	l, err := newRotatingLog(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 40) + "\n"
	for range 20 {
		if _, err := l.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{path, path + ".1", path + ".2", path + ".3"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		if info.Size() > 100 {
			t.Errorf("%s is %d bytes, over the limit", filepath.Base(p), info.Size())
		}
	}
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Error("more than three old logs were kept")
	}
}
