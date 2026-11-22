package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func buildDir(n int) string {
	return fmt.Sprintf("v1.%d.0-%s-%08x", n, strings.Repeat("a", 40), n)
}

type lines struct {
	mu sync.Mutex
	l  []string
}

func (l *lines) logf(format string, args ...any) {
	l.mu.Lock()
	l.l = append(l.l, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *lines) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.l {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func touch(t *testing.T, path string, age time.Duration, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func TestCollectGarbage(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	cur, live, stale, uploading, abandoned := buildDir(1), buildDir(2), buildDir(3), buildDir(4), buildDir(5)

	touch(t, filepath.Join(root, cur, BinaryName), 0, now)
	touch(t, filepath.Join(root, cur, tmpPrefix+"old"+tmpSuffix), 2*time.Hour, now)
	touch(t, filepath.Join(root, cur, tmpPrefix+"new"+tmpSuffix), time.Minute, now)
	touch(t, filepath.Join(root, live, BinaryName), 0, now)
	touch(t, filepath.Join(root, live, markerPrefix+"other"), 9*time.Minute, now)
	touch(t, filepath.Join(root, stale, BinaryName), 0, now)
	touch(t, filepath.Join(root, stale, markerPrefix+"gone"), 11*time.Minute, now)
	touch(t, filepath.Join(root, uploading, tmpPrefix+"x"+tmpSuffix), 10*time.Minute, now)
	touch(t, filepath.Join(root, abandoned, tmpPrefix+"x"+tmpSuffix), 2*time.Hour, now)
	for _, b := range []string{cur, live, stale} {
		touch(t, filepath.Join(root, LogsDir, b+".log"), 0, now)
		touch(t, filepath.Join(root, LogsDir, b+".log.3"), 0, now)
	}
	// Things gitbox did not create stay, whatever their age.
	touch(t, filepath.Join(root, "notes.txt"), 48*time.Hour, now)
	touch(t, filepath.Join(root, "my-tools", "bin"), 48*time.Hour, now)
	touch(t, filepath.Join(root, LogsDir, "other.log"), 48*time.Hour, now)

	var log lines
	if err := CollectGarbage(GC{Root: root, Current: cur, Owner: Marker{PID: 1, ClientID: "me"}, Now: now, Logf: log.logf}); err != nil {
		t.Fatal(err)
	}
	keep := []string{
		filepath.Join(cur, BinaryName), filepath.Join(cur, tmpPrefix+"new"+tmpSuffix),
		filepath.Join(live, BinaryName), uploading,
		filepath.Join(LogsDir, cur+".log"), filepath.Join(LogsDir, live+".log.3"),
		"notes.txt", "my-tools", filepath.Join(LogsDir, "other.log"),
	}
	gone := []string{
		filepath.Join(cur, tmpPrefix+"old"+tmpSuffix), stale, abandoned,
		filepath.Join(LogsDir, stale+".log"), filepath.Join(LogsDir, stale+".log.3"), lockName,
	}
	for _, p := range keep {
		if !exists(filepath.Join(root, p)) {
			t.Errorf("%s was removed", p)
		}
	}
	for _, p := range gone {
		if exists(filepath.Join(root, p)) {
			t.Errorf("%s was kept", p)
		}
	}
	if !log.has("removed " + stale) {
		t.Errorf("the removal was not logged: %q", log.l)
	}
}

func TestCollectGarbageLock(t *testing.T) {
	now := time.Now()
	t.Run("stale lock is broken and logged", func(t *testing.T) {
		root := t.TempDir()
		old := buildDir(2)
		touch(t, filepath.Join(root, old, BinaryName), 0, now)
		touch(t, filepath.Join(root, lockName, lockOwner), 0, now)
		touch(t, filepath.Join(root, lockName), 10*time.Minute, now)
		var log lines
		if err := CollectGarbage(GC{Root: root, Current: buildDir(1), Owner: Marker{ClientID: "me"}, Now: now, Logf: log.logf}); err != nil {
			t.Fatal(err)
		}
		if !log.has("breaking a stale lock") {
			t.Errorf("breaking the lock was not logged: %q", log.l)
		}
		if exists(filepath.Join(root, old)) || exists(filepath.Join(root, lockName)) {
			t.Error("the collection did not run after breaking the lock")
		}
	})
	t.Run("fresh lock skips the collection", func(t *testing.T) {
		root := t.TempDir()
		old := buildDir(2)
		touch(t, filepath.Join(root, old, BinaryName), 0, now)
		touch(t, filepath.Join(root, lockName), time.Minute, now)
		var log lines
		if err := CollectGarbage(GC{Root: root, Current: buildDir(1), Now: now, Logf: log.logf}); err != nil {
			t.Fatal(err)
		}
		if !exists(filepath.Join(root, old)) || !exists(filepath.Join(root, lockName)) {
			t.Error("a collection ran under another one's lock")
		}
		if !log.has("another collection") {
			t.Errorf("the skip was not logged: %q", log.l)
		}
	})
}

func TestCollectGarbageRefusesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	old := buildDir(2)
	touch(t, filepath.Join(home, old, BinaryName), 0, time.Now())
	var log lines
	if err := CollectGarbage(GC{Root: home, Current: buildDir(1), Now: time.Now(), Logf: log.logf}); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(home, old)) {
		t.Error("a collection ran on the home folder")
	}
	if _, _, ok := Locate(filepath.Join(home, buildDir(1), BinaryName)); ok {
		t.Error("Locate accepted a server installed straight under the home folder")
	}
}

func TestLocateAndMarker(t *testing.T) {
	root := t.TempDir()
	b := buildDir(1)
	gotRoot, gotBuild, ok := Locate(filepath.Join(root, b, BinaryName))
	if !ok || gotBuild != b {
		t.Fatalf("Locate = %q %q %v", gotRoot, gotBuild, ok)
	}
	for _, bad := range []string{filepath.Join(root, "dev", BinaryName), filepath.Join(root, b, "gitbox-server-dev")} {
		if _, _, ok := Locate(bad); ok {
			t.Errorf("Locate accepted %s", bad)
		}
	}
	dir := filepath.Join(root, b)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := WriteMarker(dir, Marker{PID: 42, ClientID: "c-1", StartedAt: time.Now()})
	if err != nil || filepath.Base(path) != "in-use.c-1" {
		t.Fatalf("WriteMarker = %q, %v", path, err)
	}
	if liveMarkers(dir, time.Now()) != 1 || liveMarkers(dir, time.Now().Add(MarkerLive+time.Second)) != 0 {
		t.Error("marker liveness is wrong")
	}
	if _, err := WriteMarker(dir, Marker{ClientID: "../x"}); err == nil {
		t.Error("a client id with a path in it was accepted")
	}
}
