package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// remoteEditorApp is a main window with one remote, my-server, whose entry
// has editor set (empty for Auto), on a gitbox.json of its own.
func remoteEditorApp(t *testing.T, editor string) (*App, string) {
	t.Helper()
	path := seedConfig(t, "gitbox.json", func(c *config.Config) {
		c.Global.Remotes = []config.RemoteEntry{{Alias: "my-server", Editor: editor}}
	})
	return newTestApp(withCfg(loadConfig(t, path)), withCfgPath(path), withCfgLoaded()), path
}

// TestRemoteEditorSetting: the editor of a remote is saved in this
// computer's gitbox.json, listed by ListRemotes, kept by a new process
// that reads the same file, and cleared back to Auto (#287).
func TestRemoteEditorSetting(t *testing.T) {
	a, path := remoteEditorApp(t, "")
	if list := a.ListRemotes(); len(list.RemoteEditors) == 0 || list.Remotes[0].Editor != "" {
		t.Fatalf("ListRemotes before = %+v", list)
	}
	if res := a.SetRemoteEditor("my-server", "code-insiders"); res.Error != "" {
		t.Fatalf("SetRemoteEditor = %+v", res)
	}
	if got := a.ListRemotes().Remotes[0].Editor; got != "code-insiders" {
		t.Errorf("ListRemotes editor = %q", got)
	}
	if got := loadConfig(t, path).Global.Remotes[0].Editor; got != "code-insiders" {
		t.Errorf("editor on disk = %q", got)
	}
	again := newTestApp(withCfg(loadConfig(t, path)), withCfgPath(path), withCfgLoaded())
	if got := again.ListRemotes().Remotes[0].Editor; got != "code-insiders" {
		t.Errorf("a new process reads editor %q", got)
	}
	if res := a.SetRemoteEditor("my-server", "  "); res.Error != "" {
		t.Fatalf("clearing = %+v", res)
	}
	if got := loadConfig(t, path).Global.Remotes[0].Editor; got != "" {
		t.Errorf("editor on disk after clearing = %q", got)
	}
	if res := a.SetRemoteEditor("nope", "code"); res.Hint != "error.remote.unknown_alias" {
		t.Errorf("SetRemoteEditor of an unknown alias = %+v", res)
	}
}

// TestRemoteEditorValidation: only the command name of an editor that opens
// remote folders is saved; anything else is refused and leaves the file
// alone. A value edited by hand never stops the config from loading.
func TestRemoteEditorValidation(t *testing.T) {
	a, path := remoteEditorApp(t, "code")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"zed", "codium", "/usr/local/bin/code", "Code.exe", "code.cmd", "code --remote x"} {
		if res := a.SetRemoteEditor("my-server", bad); res.Hint != "error.remote.remote_editor_unsupported" {
			t.Errorf("SetRemoteEditor(%q) = %+v", bad, res)
		}
	}
	if after, _ := os.ReadFile(path); !reflect.DeepEqual(before, after) {
		t.Error("a refused editor changed gitbox.json")
	}

	hand, _ := remoteEditorApp(t, "/opt/some editor/bin/ed")
	if got := hand.ListRemotes().Remotes[0].Editor; got != "/opt/some editor/bin/ed" {
		t.Errorf("a hand-edited editor reads back as %q", got)
	}
}

// TestRemoteEditorLaunch: a remote window with an editor chosen opens
// folders only in that editor; another editor is refused and starts
// nothing, and Auto keeps the #243 launch. The remote state carries the
// choice for the launchers.
func TestRemoteEditorLaunch(t *testing.T) {
	spawned := recordRemoteSpawn(t)
	h := startHostile(t, map[string]rpc.Handler{
		"AccountFolder": rpc.Func(func(string) (string, error) { return "/srv/acct", nil }),
	})
	attachSession(t, h.app)
	choose := func(editor string) {
		t.Helper()
		if err := h.app.clientPrefs().Update(func(p *config.ClientPrefs) { p.Remotes[0].Editor = editor }); err != nil {
			t.Fatal(err)
		}
	}

	choose("code-insiders")
	if got := h.app.RemoteInfo().Editor; got != "code-insiders" {
		t.Errorf("remote state editor = %q", got)
	}
	if err := h.app.OpenInApp("/srv/repo", "code-insiders"); err != nil {
		t.Fatal(err)
	}
	if err := h.app.OpenInApp("/srv/repo", "code"); !codeIs(err, "remote_editor_not_chosen") {
		t.Errorf("another editor = %v, want remote_editor_not_chosen", err)
	}
	if err := h.app.OpenAccountInApp("acct", "code"); !codeIs(err, "remote_editor_not_chosen") {
		t.Errorf("another editor for an account = %v, want remote_editor_not_chosen", err)
	}
	if len(*spawned) != 1 || !reflect.DeepEqual((*spawned)[0].Editor, []string{"code-insiders", "--remote", "ssh-remote+my-server", "/srv/repo"}) {
		t.Fatalf("launches = %+v", *spawned)
	}

	choose("")
	if got := h.app.RemoteInfo().Editor; got != "" {
		t.Errorf("remote state editor with Auto = %q", got)
	}
	if err := h.app.OpenInApp("/srv/repo", "code"); err != nil {
		t.Fatal(err)
	}
	if len(*spawned) != 2 || !reflect.DeepEqual((*spawned)[1].Editor[1:], []string{"--remote", "ssh-remote+my-server", "/srv/repo"}) {
		t.Errorf("Auto launch = %+v", (*spawned)[1:])
	}
}
