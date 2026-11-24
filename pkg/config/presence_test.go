package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowPresence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "windows")
	if open, _ := WindowOpen(dir, "my-server"); open {
		t.Fatal("open before any window held it")
	}
	p, err := HoldWindow(dir, "my-server")
	if err != nil {
		t.Fatal(err)
	}
	if open, state := WindowOpen(dir, "my-server"); !open || state != "" {
		t.Fatalf("held, no state: open=%v state=%q", open, state)
	}
	if err := p.SetState("connected"); err != nil {
		t.Fatal(err)
	}
	if open, state := WindowOpen(dir, "my-server"); !open || state != "connected" {
		t.Fatalf("after SetState: open=%v state=%q", open, state)
	}
	if _, err := HoldWindow(dir, "my-server"); err == nil {
		t.Fatal("a second holder took the presence")
	}
	if open, _ := WindowOpen(dir, "other"); open {
		t.Fatal("another alias counts as open")
	}
	p.Release()
	p.Release()
	if open, state := WindowOpen(dir, "my-server"); open || state != "" {
		t.Fatalf("after Release: open=%v state=%q", open, state)
	}
	again, err := HoldWindow(dir, "my-server")
	if err != nil {
		t.Fatalf("hold after release: %v", err)
	}
	again.Release()
}

// A state file a crashed window left behind does not make it open.
func TestWindowPresenceStale(t *testing.T) {
	dir := t.TempDir()
	lock, state, err := presencePaths(dir, "box")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{lock, state} {
		if err := os.WriteFile(path, []byte(`{"state":"connected","updatedAt":"`+time.Now().Format(time.RFC3339)+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if open, st := WindowOpen(dir, "box"); open || st != "" {
		t.Fatalf("stale files: open=%v state=%q", open, st)
	}
}

func TestWindowPresenceRejectsBadAlias(t *testing.T) {
	dir := t.TempDir()
	for _, alias := range []string{"", "../x", "-o", "a/b"} {
		if _, err := HoldWindow(dir, alias); err == nil {
			t.Errorf("HoldWindow(%q) succeeded", alias)
		}
		if open, _ := WindowOpen(dir, alias); open {
			t.Errorf("WindowOpen(%q) = true", alias)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("bad aliases created files: %v", entries)
	}
}

func TestSetRemoteFacts(t *testing.T) {
	p := ClientPrefs{Remotes: []RemoteEntry{{Alias: "box", Editor: "code"}}}
	when := time.Date(2026, 11, 24, 18, 30, 0, 0, time.FixedZone("CET", 3600))
	if !p.SetRemoteFacts("box", RemoteFacts{ServerOS: "linux", ServerArch: "amd64", ServerVersion: "v2.9.0", LastConnected: when}) {
		t.Fatal("SetRemoteFacts(box) = false")
	}
	got := p.Remotes[0]
	want := RemoteEntry{Alias: "box", Editor: "code", ServerOS: "linux", ServerArch: "amd64", ServerVersion: "v2.9.0", LastConnected: "2026-11-24T17:30:00Z"}
	if got.Alias != want.Alias || got.Editor != want.Editor || got.ServerOS != want.ServerOS || got.ServerArch != want.ServerArch || got.ServerVersion != want.ServerVersion || got.LastConnected != want.LastConnected {
		t.Fatalf("entry = %+v, want %+v", got, want)
	}
	if p.SetRemoteFacts("missing", RemoteFacts{}) {
		t.Fatal("SetRemoteFacts(missing) = true")
	}
	// The facts survive a save and a load as client keys.
	var c Config
	ApplyPrefs(&c, p)
	if back := ExtractPrefs(&c); back.Remotes[0].LastConnected != want.LastConnected || back.Remotes[0].ServerVersion != "v2.9.0" {
		t.Fatalf("round trip lost the facts: %+v", back.Remotes[0])
	}
}
