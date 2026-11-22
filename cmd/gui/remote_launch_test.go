package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/launch"
	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// codeIs reports whether err is the remote failure code.
func codeIs(err error, code string) bool { return toUserError(err).Code == "remote."+code }

// recordRemoteSpawn replaces remoteSpawn, the one exit of every remote
// launch (#243), with a recorder: nothing starts.
func recordRemoteSpawn(t *testing.T) *[]remoteSpawnSpec {
	t.Helper()
	var got []remoteSpawnSpec
	prev := remoteSpawn
	remoteSpawn = func(s remoteSpawnSpec) error {
		got = append(got, s)
		return nil
	}
	t.Cleanup(func() { remoteSpawn = prev })
	return &got
}

// The terminal profile of attachSession: a terminal that takes its program
// after -e, the same on every OS.
const testProfile = "xt-p"

// attachSession makes a an attached remote window of the alias my-server,
// on a Linux host whose login shell is bash, with a local remotes list that
// names it and one terminal profile.
func attachSession(t *testing.T, a *App) {
	t.Helper()
	a.prefs = newFilePrefs(seedConfig(t, "client-remote.json", func(c *config.Config) {
		c.Global.Remotes = []config.RemoteEntry{{Alias: "my-server"}}
		c.Global.TerminalApps = []config.TerminalApp{{ID: "xt", Name: "XT", Command: "x-term", ArgsTemplate: []string{"-e", launch.TokenCommand}}}
		c.Global.TerminalProfiles = []config.TerminalProfile{{ID: testProfile, Name: "XT", TerminalID: "xt", Default: true}}
	}))
	a.remote = &remoteSession{a: a, alias: "my-server", probe: remote.Probe{OS: "linux", Shell: "/bin/bash"}}
}

// fakeSSH puts a recording ssh first on PATH, so the argv is the same on
// every host, and returns its path.
func fakeSSH(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		p, err := exec.LookPath("ssh")
		if err != nil {
			t.Skip("no ssh on this Windows host")
		}
		return p
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "ssh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}

// sshArgv is what a remote launch hands ssh, whichever shape it took.
func sshArgv(t *testing.T, s remoteSpawnSpec) []string {
	t.Helper()
	if len(s.Console) > 0 {
		return s.Console
	}
	if s.Terminal.Command != "x-term" || len(s.Terminal.Args) < 1 || s.Terminal.Args[0] != "-e" {
		t.Fatalf("the launch did not use the profile's terminal: %+v", s)
	}
	return s.Terminal.Args[1:]
}

// TestDisabledInRemote: the file manager, the account folder and the
// host's config file have no remote equivalent. A remote window refuses
// them with not_available_remote, sends nothing to the host and starts
// nothing.
func TestDisabledInRemote(t *testing.T) {
	spawned := recordRemoteSpawn(t)
	record := ""
	if runtime.GOOS != "windows" {
		record = trapPath(t)
	}
	var mu sync.Mutex
	asked := 0
	h := startHostile(t, map[string]rpc.Handler{
		"OpenAccountFolder": rpc.Func(func(string) error { mu.Lock(); asked++; mu.Unlock(); return nil }),
		"AccountFolder":     rpc.Func(func(string) (string, error) { mu.Lock(); asked++; mu.Unlock(); return "/srv/acct", nil }),
	})
	attachSession(t, h.app)

	for name, call := range map[string]func() error{
		"OpenInExplorer":    func() error { return h.app.OpenInExplorer("/srv/repo") },
		"OpenAccountFolder": func() error { return h.app.OpenAccountFolder("acct") },
		"OpenFileInEditor":  func() error { return h.app.OpenFileInEditor("/home/me/.config/gitbox/gitbox.json") },
	} {
		err := call()
		if ue := toUserError(err); ue.Code != "remote.not_available_remote" || ue.Hint != "error.remote.not_available_remote" {
			t.Errorf("%s = %v (%s), want remote.not_available_remote", name, err, ue.Code)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if asked != 0 {
		t.Errorf("a disabled action reached the host %d times", asked)
	}
	if len(*spawned) != 0 {
		t.Errorf("a disabled action started %+v", *spawned)
	}
	if record != "" {
		if data, err := os.ReadFile(record); err == nil {
			t.Errorf("a disabled action started a process: %s", data)
		}
	}
}

// TestEditorArgv: a remote window opens a folder of the host in VS Code as
// code --remote ssh-remote+<alias> <folder>: the folder is an element of
// its own, and the alias is this window's, from the local remotes list,
// whatever the host sends. An editor without remote support is refused.
func TestEditorArgv(t *testing.T) {
	spawned := recordRemoteSpawn(t)
	const hostile = "/srv/x --remote ssh-remote+evil; $(id)"
	h := startHostile(t, map[string]rpc.Handler{
		"AccountFolder": rpc.Func(func(string) (string, error) { return hostile, nil }),
	})
	attachSession(t, h.app)

	if err := h.app.OpenInApp("/srv/it's here", "code"); err != nil {
		t.Fatal(err)
	}
	if err := h.app.OpenAccountInApp("acct", "code"); err != nil {
		t.Fatal(err)
	}
	if len(*spawned) != 2 {
		t.Fatalf("%d launches, want 2", len(*spawned))
	}
	for i, dir := range []string{"/srv/it's here", hostile} {
		argv := (*spawned)[i].Editor
		if want := []string{"--remote", "ssh-remote+my-server", dir}; len(argv) != 4 || remote.EditorName(argv[0]) != "code" || !reflect.DeepEqual(argv[1:], want) {
			t.Errorf("launch %d: %q, want code %q", i, argv, want)
		}
	}

	for _, c := range []struct {
		name  string
		call  func() error
		code  string
		setup func()
	}{
		{name: "an editor without remote support", call: func() error { return h.app.OpenInApp("/srv/repo", "zed") }, code: "not_available_remote_path"},
		{name: "a relative folder", call: func() error { return h.app.OpenInApp("srv/repo", "code") }, code: "not_available_remote_path"},
		{name: "a folder with a newline", call: func() error { return h.app.OpenInApp("/srv/a\nb", "code") }, code: "not_available_remote_path"},
		{name: "a remote removed from this computer", call: func() error { return h.app.OpenInApp("/srv/repo", "code") }, code: "unknown_alias",
			setup: func() { _ = h.app.clientPrefs().Update(func(p *config.ClientPrefs) { p.Remotes = nil }) }},
	} {
		if c.setup != nil {
			c.setup()
		}
		if err := c.call(); !codeIs(err, c.code) {
			t.Errorf("%s = %v, want %s", c.name, err, c.code)
		}
	}
	if len(*spawned) != 2 {
		t.Errorf("a refused launch started %+v", (*spawned)[2:])
	}
}

// A terminal profile and an AI harness of a remote window open a local
// terminal that runs ssh -t into the folder, quoted for the host's login
// shell; a Windows client starts ssh in a console of its own.
func TestRemoteTerminalLaunch(t *testing.T) {
	spawned := recordRemoteSpawn(t)
	ssh := fakeSSH(t)
	h := startHostile(t, map[string]rpc.Handler{
		"AccountFolder": rpc.Func(func(string) (string, error) { return "/srv/acct", nil }),
	})
	attachSession(t, h.app)

	cases := []struct {
		name  string
		shell string
		call  func() error
		cmd   string
	}{
		{"OpenProfile", "/bin/bash", func() error { return h.app.OpenProfile("/srv/it's here", testProfile) },
			`cd -- '/srv/it'\''s here' && exec "$SHELL" -l`},
		{"OpenProfile with fish", "/usr/bin/fish", func() error { return h.app.OpenProfile(`/srv/a\b`, testProfile) },
			`cd '/srv/a\\b'; and exec "$SHELL" -l`},
		{"OpenProfile with tcsh", "/bin/tcsh", func() error { return h.app.OpenProfile("/srv/a b", testProfile) },
			`exec sh -c 'cd -- "$1" && exec "${SHELL:-/bin/sh}" -l' sh '/srv/a b'`},
		{"OpenAccountProfile", "/bin/zsh", func() error { return h.app.OpenAccountProfile("acct", testProfile) },
			`cd -- '/srv/acct' && exec "$SHELL" -l`},
		{"OpenInAIHarness", "/bin/bash", func() error { return h.app.OpenInAIHarness("/srv/r;id", "/home/me/.local/bin/claude", nil) },
			`cd -- '/srv/r;id' && exec '/home/me/.local/bin/claude'`},
		{"OpenAccountInAIHarness", "/bin/bash", func() error { return h.app.OpenAccountInAIHarness("acct", "codex", []string{}) },
			`cd -- '/srv/acct' && exec 'codex'`},
	}
	for _, c := range cases {
		h.app.remote.probe.Shell = c.shell
		before := len(*spawned)
		if err := c.call(); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if len(*spawned) != before+1 {
			t.Errorf("%s started %d launches", c.name, len(*spawned)-before)
			continue
		}
		s := (*spawned)[before]
		if runtime.GOOS == "windows" && len(s.Console) == 0 {
			t.Errorf("%s: a Windows client must start ssh in a console of its own: %+v", c.name, s)
		}
		want := []string{ssh, "-t", "--", "my-server", c.cmd}
		if got := sshArgv(t, s); !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, want)
		}
	}

	h.app.remote.probe.Shell = "/bin/bash"
	for _, c := range []struct {
		name string
		call func() error
		code string
	}{
		{"a harness outside the catalog", func() error { return h.app.OpenInAIHarness("/srv/r", "/bin/sh", nil) }, "harness_not_allowed"},
		{"a harness with arguments", func() error { return h.app.OpenInAIHarness("/srv/r", "claude", []string{"--yolo"}) }, "harness_not_allowed"},
		{"a relative folder", func() error { return h.app.OpenProfile("srv/r", testProfile) }, "not_available_remote_path"},
		{"a folder with a CR", func() error { return h.app.OpenProfile("/srv/a\rb", testProfile) }, "not_available_remote_path"},
	} {
		before := len(*spawned)
		if err := c.call(); !codeIs(err, c.code) {
			t.Errorf("%s = %v, want %s", c.name, err, c.code)
		}
		if len(*spawned) != before {
			t.Errorf("%s started a launch", c.name)
		}
	}

	// A window that lost its connection launches nothing.
	h.remote.Close()
	if err := h.app.OpenProfile("/srv/r", testProfile); !codeIs(err, "not_connected") {
		t.Errorf("OpenProfile while disconnected = %v, want not_connected", err)
	}
}

// On a macOS client, Terminal.app and iTerm profiles run the ssh line
// through AppleScript (pkg/remote's TestClientLaunchLayers runs that layer
// for real).
func TestRemoteTerminalAppleScript(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Terminal.app exists on macOS only")
	}
	spawned := recordRemoteSpawn(t)
	ssh := fakeSSH(t)
	t.Setenv("SHELL", "/bin/zsh")
	h := startHostile(t, map[string]rpc.Handler{})
	attachSession(t, h.app)
	_ = h.app.clientPrefs().Update(func(p *config.ClientPrefs) {
		p.TerminalApps = []config.TerminalApp{{ID: "terminal", Name: "Terminal", Command: "open", ArgsTemplate: []string{"-a", "Terminal", launch.TokenPath}}}
		p.TerminalProfiles = []config.TerminalProfile{{ID: "t", Name: "Terminal", TerminalID: "terminal", Default: true}}
	})
	if err := h.app.OpenProfile(`/srv/"quoted" \ dir`, "t"); err != nil {
		t.Fatal(err)
	}
	if len(*spawned) != 1 {
		t.Fatalf("%d launches", len(*spawned))
	}
	cmd, _ := remote.RemoteCommand(remote.FamilyPOSIX, `/srv/"quoted" \ dir`, nil)
	line := remote.LocalShellLine("/bin/zsh", []string{ssh, "-t", "--", "my-server", cmd})
	if got, want := (*spawned)[0].Terminal.AppleScript, launch.MacAppleScriptLine("Terminal", line); got != want {
		t.Errorf("AppleScript\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains((*spawned)[0].Terminal.AppleScript, `\"quoted\"`) {
		t.Errorf("the line was not escaped for AppleScript")
	}
}

// The window learns which editors open remote folders from the state it
// shows.
func TestRemoteStateListsRemoteEditors(t *testing.T) {
	s := &remoteSession{a: &App{}, alias: "my-server"}
	if got := s.info().RemoteEditors; !reflect.DeepEqual(got, remote.RemoteEditorCommands()) {
		t.Errorf("RemoteEditors = %q", got)
	}
}
