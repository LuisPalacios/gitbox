package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// TestRemotePathValidation: a folder typed for the host is absolute,
// cleaned and free of control characters, and every method that takes one
// refuses an invalid folder itself, even when the window skipped the check
// (#241). RestoreFromBackup takes only a file the backup list names.
func TestRemotePathValidation(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := hostPath("  ~/a/../b  "); err != nil || got != filepath.Join(home, "b") {
		t.Errorf("hostPath(~/a/../b) = %q, %v", got, err)
	}
	for _, bad := range []string{"", "   ", "../x", "x/y", "/a\nb", "/a\x00b", "/" + strings.Repeat("a", maxHostPath)} {
		if _, err := hostPath(bad); !codeIs(err, string(remotePathInvalid)) {
			t.Errorf("hostPath(%q) = %v, want remote_path_invalid", bad, err)
		}
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		path string
		want RemotePathDTO
	}{
		{"a writable folder", dir, RemotePathDTO{Abs: dir, Exists: true, IsDir: true, Writable: true}},
		{"a missing folder under a writable one", filepath.Join(dir, "new", "deep"), RemotePathDTO{Abs: filepath.Join(dir, "new", "deep"), Writable: true}},
		{"a file", file, RemotePathDTO{Abs: file, Exists: true}},
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		ro := filepath.Join(dir, "ro")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
		cases = append(cases, struct {
			name string
			path string
			want RemotePathDTO
		}{"a read-only folder", ro, RemotePathDTO{Abs: ro, Exists: true, IsDir: true}})
	}
	for _, c := range cases {
		if got := validateRemotePathLocal(c.path); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	if got := validateRemotePathLocal("relative"); got.Error == "" || got.Abs != "" {
		t.Errorf("a relative path = %+v, want an error", got)
	}

	path := seedConfig(t, "gitbox.json", nil)
	a := windowOn(t, path)
	for _, bad := range []string{"relative/root", "../up", "/a\nb", filepath.Join(dir, "bad\x00name")} {
		if err := a.AddRoot(bad); !codeIs(err, string(remotePathInvalid)) {
			t.Errorf("AddRoot(%q) = %v, want remote_path_invalid", bad, err)
		}
		if err := a.InitRoot(bad); !codeIs(err, string(remotePathInvalid)) {
			t.Errorf("InitRoot(%q) = %v, want remote_path_invalid", bad, err)
		}
		if err := a.SetDefaultRoot(bad); !codeIs(err, string(remotePathInvalid)) {
			t.Errorf("SetDefaultRoot(%q) = %v, want remote_path_invalid", bad, err)
		}
		if _, err := a.RemoveRoot(bad, false); !codeIs(err, string(remotePathInvalid)) {
			t.Errorf("RemoveRoot(%q) = %v, want remote_path_invalid", bad, err)
		}
		_, invalid := hostPath(bad)
		for _, req := range []AdoptRequestDTO{{Path: bad}, {Path: dir, WorkspaceRoot: bad}} {
			if got := a.AdoptClones([]AdoptRequestDTO{req}); got.Error != userMsg(invalid) {
				t.Errorf("invalid adoption path = %q, want %q", got.Error, userMsg(invalid))
			}
		}
		if err := a.IgnoreClone(bad, true); !codeIs(err, string(remotePathInvalid)) {
			t.Errorf("IgnoreClone(%q) = %v, want remote_path_invalid", bad, err)
		}
		if err := a.be().AddDiscoveredReposToFolder("acct", []string{"o/r"}, bad); !codeIs(err, string(remotePathInvalid)) {
			t.Errorf("AddDiscoveredReposToFolder(%q) = %v, want remote_path_invalid", bad, err)
		}
	}
	if roots := loadConfig(t, path).Global.Roots; len(roots) != 1 {
		t.Errorf("a refused folder reached the config: %v", roots)
	}

	// A valid config in the config folder that is not one of the backups.
	valid, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(filepath.Dir(path), "not-a-backup.json")
	if err := os.WriteFile(stray, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	if res := a.RestoreFromBackup(stray); res.Success || !strings.Contains(res.Error, "not one of the config backups") {
		t.Errorf("RestoreFromBackup of an unlisted file = %+v", res)
	}
	edited := loadConfig(t, path)
	edited.Accounts["b"] = config.Account{Provider: "github", URL: "https://github.com", Username: "b", Name: "B", Email: "b@e"}
	if err := config.Save(edited, path); err != nil {
		t.Fatal(err)
	}
	backups, err := config.ListBackups(path)
	if err != nil || len(backups) == 0 {
		t.Fatalf("no backup to restore: %v", err)
	}
	if !listedBackup(path, backups[0].Path) {
		t.Errorf("listedBackup refused a listed backup %s", backups[0].Path)
	}
}

// TestInitRootMixed: in a remote window the host creates and records the
// root, and the client half starts this computer's preferences in Simple
// mode, which the host's save would otherwise lose (#241).
func TestInitRootMixed(t *testing.T) {
	var asked string
	h := startHostile(t, map[string]rpc.Handler{
		"InitRoot": rpc.Func(func(folder string) error { asked = folder; return nil }),
	})
	if err := h.app.InitRoot("~/00.git"); err != nil {
		t.Fatal(err)
	}
	if asked != "~/00.git" {
		t.Errorf("the host got InitRoot(%q)", asked)
	}
	if got := h.app.clientPrefs().Get().UIMode; got != uiModeSimple {
		t.Errorf("client ui_mode = %q, want %q", got, uiModeSimple)
	}
}
