//go:build !gitboxserver

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// recordStarts replaces startWindow and returns the argument lists it got.
func recordStarts(t *testing.T, fail error) func() [][]string {
	t.Helper()
	var mu sync.Mutex
	var got [][]string
	prev := startWindow
	startWindow = func(args []string) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, slices.Clone(args))
		return fail
	}
	t.Cleanup(func() { startWindow = prev })
	return func() [][]string { mu.Lock(); defer mu.Unlock(); return slices.Clone(got) }
}

// remoteHostsApp is a local window's App with remote mode on and the given
// remotes in its config file.
func remoteHostsApp(t *testing.T, remotes ...config.RemoteEntry) (*App, string) {
	t.Helper()
	path := seedConfig(t, "gitbox.json", func(c *config.Config) {
		c.Global.ExperimentalRemote = true
		c.Global.Remotes = remotes
	})
	return &App{cfg: loadConfig(t, path), cfgPath: path, cfgLoaded: true}, path
}

func TestRemotesWindowArgs(t *testing.T) {
	if got := remotesWindowArgs(false, "/home/me/gitbox.json"); !slices.Equal(got, []string{"--remotes-window", "--experimental-remote"}) {
		t.Errorf("production args = %q", got)
	}
	got := remotesWindowArgs(true, "/tmp/test/gitbox.json")
	if !slices.Equal(got, []string{"--remotes-window", "--experimental-remote", "--test-mode", "--prefs-file=/tmp/test/gitbox.json"}) {
		t.Errorf("test-mode args = %q", got)
	}
	for _, args := range [][]string{got, remotesWindowArgs(false, "")} {
		f, err := parseRemoteFlags(args)
		if err != nil || !f.remotesWindow {
			t.Errorf("the spawned window would refuse its own args %q: %+v, %v", args, f, err)
		}
		if slices.ContainsFunc(args, func(s string) bool { return strings.HasPrefix(s, "--parent-pid") }) {
			t.Errorf("the Remote hosts window must outlive the window that opened it: %q", args)
		}
	}
}

// ListRemotes reads the file each time, so a remote another window added
// shows at once, and it says which remotes have a window open (#330).
func TestListRemotesFreshAndOpen(t *testing.T) {
	a, path := remoteHostsApp(t, config.RemoteEntry{Alias: "my-server"})
	// Another process adds a remote with the facts of its last connection.
	if _, err := config.UpdateFile(path, func(c *config.Config) error {
		c.Global.Remotes = append(c.Global.Remotes, config.RemoteEntry{Alias: "nas",
			ServerOS: "linux", ServerArch: "arm64", ServerVersion: "v2.9.0", LastConnected: "2026-11-22T10:00:00Z"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	list := a.ListRemotes()
	if list.Current != "" || len(list.Remotes) != 2 {
		t.Fatalf("ListRemotes after another writer = %+v", list)
	}
	want := RemoteEntryDTO{Alias: "nas", ServerOS: "linux", ServerArch: "arm64", ServerVersion: "v2.9.0", LastConnected: "2026-11-22T10:00:00Z"}
	if list.Remotes[1] != want {
		t.Errorf("nas = %+v, want %+v", list.Remotes[1], want)
	}

	// A running window of my-server marks it open with its state.
	p, err := config.HoldWindow(config.WindowsDir(path), "my-server")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetState(config.PresenceState{State: remoteConnecting, Waiting: "1password"}); err != nil {
		t.Fatal(err)
	}
	if got := a.ListRemotes().Remotes[0]; !got.Open || got.State != remoteConnecting || got.Waiting != "1password" {
		t.Errorf("my-server with its window open and waiting for 1Password = %+v", got)
	}
	p.Release()
	if got := a.ListRemotes().Remotes[0]; got.Open || got.State != "" {
		t.Errorf("my-server after its window closed = %+v", got)
	}
}

// A remote window records its presence and state for the others, and the
// host's facts in its entry, when it connects (#330).
func TestRemoteWindowPresenceAndFacts(t *testing.T) {
	dial, _, _ := pipeServer(t, contractConfig(t))
	a, sink := remoteApp(t, dial)
	prefsFile := a.prefs.(*filePrefs).path
	a.cfgPath = prefsFile
	dir := config.WindowsDir(prefsFile)
	a.events = sink

	a.remote.holdPresence(dir)
	if open, state := config.WindowOpen(dir, "my-server"); !open || state.State != remoteDisconnected {
		t.Fatalf("before connecting: open=%v state=%q", open, state)
	}
	before := time.Now().Add(-time.Second)
	a.remote.start()
	st := waitState(t, a, remoteConnected)
	if open, state := config.WindowOpen(dir, "my-server"); !open || state.State != remoteConnected || state.Waiting != "" {
		t.Errorf("connected: open=%v state=%q", open, state)
	}
	entry, ok := findRemote(loadConfig(t, prefsFile).Global.Remotes, "my-server")
	if !ok || entry.ServerOS != st.ServerOS || entry.ServerArch != st.ServerArch || entry.ServerVersion != st.ServerVersion || entry.ServerOS == "" {
		t.Fatalf("facts on disk = %+v, state %+v", entry, st)
	}
	when, err := time.Parse(time.RFC3339, entry.LastConnected)
	if err != nil || when.Before(before.Truncate(time.Second)) {
		t.Errorf("last_connected = %q (%v)", entry.LastConnected, err)
	}
	list := a.ListRemotes()
	if list.Current != "my-server" || len(list.Remotes) != 1 || !list.Remotes[0].Open || list.Remotes[0].State != remoteConnected {
		t.Errorf("ListRemotes in the remote window = %+v", list)
	}

	a.RemoteDisconnect()
	waitState(t, a, remoteDisconnected)
	if _, state := config.WindowOpen(dir, "my-server"); state.State != remoteDisconnected {
		t.Errorf("after disconnect: state=%q", state)
	}
	a.remote.presence.Release()
	if open, _ := config.WindowOpen(dir, "my-server"); open {
		t.Error("still open after the window released its presence")
	}
}

func TestListSSHHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a, _ := remoteHostsApp(t, config.RemoteEntry{Alias: "my-server"})
	if got := a.ListSSHHosts(); got == nil || len(got) != 0 {
		t.Errorf("without an ssh configuration = %#v, want an empty list", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host my-server my-server.example.org\nHost *\nHost nas nas.example.org 192.0.2.5\nHost build-box\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []SSHHostDTO{{Alias: "nas", Names: []string{"nas", "nas.example.org", "192.0.2.5"}}, {Alias: "build-box", Names: []string{"build-box"}}}
	got := a.ListSSHHosts()
	if !slices.EqualFunc(got, want, func(x, y SSHHostDTO) bool { return x.Alias == y.Alias && slices.Equal(x.Names, y.Names) }) {
		t.Errorf("ListSSHHosts = %+v, want one line per Host entry not added yet: %+v", got, want)
	}
}

func TestRemoteHostsWindowBindings(t *testing.T) {
	starts := recordStarts(t, nil)
	off, _ := remoteHostsApp(t)
	off.cfg.Global.ExperimentalRemote = false
	if err := off.clientPrefs().Update(func(p *config.ClientPrefs) { p.ExperimentalRemote = false }); err != nil {
		t.Fatal(err)
	}
	for name, res := range map[string]RemoteResultDTO{
		"OpenRemotesWindow": off.OpenRemotesWindow(),
		"ShowRemoteLog":     off.ShowRemoteLog("my-server"),
	} {
		if res.Hint != "error.remote.not_enabled" {
			t.Errorf("%s with remote mode off = %+v", name, res)
		}
	}
	if len(starts()) != 0 {
		t.Fatalf("a refused call started a window: %q", starts())
	}

	a, path := remoteHostsApp(t, config.RemoteEntry{Alias: "my-server"})
	a.testMode = true
	if res := a.OpenRemotesWindow(); res.Error != "" {
		t.Fatalf("OpenRemotesWindow = %+v", res)
	}
	if res := a.ShowRemoteLog("nope"); res.Hint != "error.remote.unknown_alias" {
		t.Errorf("ShowRemoteLog of an unknown alias = %+v", res)
	}
	if res := a.ShowRemoteLog("my-server"); res.Hint != "error.remote.window_closed" {
		t.Errorf("ShowRemoteLog without an open window = %+v", res)
	}
	p, err := config.HoldWindow(config.WindowsDir(path), "my-server")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Release()
	if res := a.ShowRemoteLog("my-server"); res.Error != "" {
		t.Errorf("ShowRemoteLog with the window open = %+v", res)
	}
	if res := a.ShowLocalWindow(); res.Error != "" {
		t.Errorf("ShowLocalWindow = %+v", res)
	}
	want := [][]string{
		remotesWindowArgs(true, path),
		append(remoteWindowArgs("my-server", true, path), "--show-log"),
		{"--test-mode"},
	}
	if got := starts(); !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Errorf("started windows = %q, want %q", got, want)
	}
	if f, err := parseRemoteFlags(want[1]); err != nil || !f.showLog {
		t.Errorf("the remote window would refuse the log request %q: %+v, %v", want[1], f, err)
	}

	// A failed start reaches the caller as a plain error.
	recordStarts(t, errors.New("exec failed"))
	if res := a.OpenRemotesWindow(); res.Error == "" || strings.Contains(res.Error, "exec failed") {
		t.Errorf("OpenRemotesWindow with a failing start = %+v", res)
	}
}

// The Remote hosts window ends once no read has come for the idle timeout;
// every read restarts the countdown, and other windows have none (#330).
func TestRemotesWindowIdleQuit(t *testing.T) {
	prevTimeout, prevQuit := remotesIdleTimeout, quitRemotesWindow
	t.Cleanup(func() { remotesIdleTimeout, quitRemotesWindow = prevTimeout, prevQuit })
	quits := make(chan *App, 4)
	quitRemotesWindow = func(a *App) { quits <- a }
	remotesIdleTimeout = 400 * time.Millisecond

	local, _ := remoteHostsApp(t)
	local.ListRemotes()
	select {
	case <-quits:
		t.Fatal("a local window quit on idle")
	case <-time.After(600 * time.Millisecond):
	}

	a, _ := remoteHostsApp(t)
	a.windowMode = "remotes"
	a.ListRemotes()
	time.Sleep(250 * time.Millisecond)
	a.ListRemotes() // a read before the timeout restarts the countdown
	select {
	case <-quits:
		t.Fatal("the window quit although a read restarted the countdown")
	case <-time.After(200 * time.Millisecond):
	}
	select {
	case got := <-quits:
		if got != a {
			t.Fatal("another App quit")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hidden window never quit")
	}
}
