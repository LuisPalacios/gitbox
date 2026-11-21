package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/ops"
)

// newTestApp builds an App the way a non-local backend will: the backend
// owns its config, and the client preferences live in the gitbox.json at
// localPath.
func newTestApp(backend Backend, localPath string) *App {
	return &App{backend: backend, prefs: newFilePrefs(localPath)}
}

// seedConfig writes a valid config with one account and one source, changed
// by edit, and returns its path.
func seedConfig(t *testing.T, name string, edit func(*config.Config)) string {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Roots: []string{filepath.Join(dir, "git")}},
		Accounts: map[string]config.Account{
			"acct": {Provider: "github", URL: "https://github.com", Username: "alice", Name: "Alice", Email: "a@e"},
		},
		Sources: map[string]config.Source{"acct": {Account: "acct"}},
	}
	if edit != nil {
		edit(cfg)
	}
	path := filepath.Join(dir, name)
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("seeding %s: %v", name, err)
	}
	return path
}

// fileBackend is a Backend whose config lives in its own file: the local
// backend of a second App, standing in for a gitbox-server on another host.
func fileBackend(t *testing.T, path string) Backend {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return localBackend{&App{cfg: cfg, cfgPath: path, cfgLoaded: true}}
}

func loadConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading %s: %v", path, err)
	}
	return cfg
}

// fileStamp records a file's content hash and modification time.
type fileStamp struct {
	sum   [sha256.Size]byte
	mtime time.Time
}

func stamp(t *testing.T, path string) fileStamp {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileStamp{sha256.Sum256(data), info.ModTime()}
}

func TestPrefsOverlay(t *testing.T) {
	backendPath := seedConfig(t, "backend.json", func(c *config.Config) {
		c.Global.Theme = "dark"
		c.Global.Language = "es"
		c.Global.IgnoredClones = []string{"/on/the/backend"}
	})
	localPath := seedConfig(t, "local.json", func(c *config.Config) {
		c.Global.Theme = "light"
		c.Global.IgnoredClones = []string{"/on/the/client"}
	})
	a := newTestApp(fileBackend(t, backendPath), localPath)

	check := func(what string, dto ConfigDTO, err error, theme string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if dto.Global.Theme != theme {
			t.Errorf("%s: theme = %q, want the client's %q", what, dto.Global.Theme, theme)
		}
		if dto.Global.Language != "" {
			t.Errorf("%s: language = %q, want the client's (unset)", what, dto.Global.Language)
		}
		if got := dto.Global.IgnoredClones; len(got) != 1 || got[0] != "/on/the/backend" {
			t.Errorf("%s: ignored_clones = %v, want the backend's", what, got)
		}
	}
	dto, err := a.GetConfig()
	check("GetConfig", dto, err, "light")
	snap, err := a.GetStartupSnapshot()
	check("GetStartupSnapshot", snap.Config, err, "light")

	before := stamp(t, backendPath)
	if err := a.SetTheme("dark"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetLanguage("es"); err != nil {
		t.Fatal(err)
	}
	if after := stamp(t, backendPath); after != before {
		t.Errorf("setting a preference touched the backend's file")
	}
	local := loadConfig(t, localPath)
	if local.Global.Theme != "dark" || local.Global.Language != "es" {
		t.Errorf("local file: theme %q, language %q; want dark, es", local.Global.Theme, local.Global.Language)
	}
	if got := local.Global.IgnoredClones; len(got) != 1 || got[0] != "/on/the/client" {
		t.Errorf("saving a preference changed the local file's domain keys: %v", got)
	}

	// An edit made outside the app shows up after a reload.
	local.Global.Theme = "light"
	local.Global.Language = ""
	if err := config.Save(local, localPath); err != nil {
		t.Fatal(err)
	}
	dto, err = a.ReloadConfig()
	check("ReloadConfig", dto, err, "light")
}

func TestCollapsedSplit(t *testing.T) {
	backendPath := seedConfig(t, "backend.json", nil)
	// The local file knows the same source, so a leaked source key would
	// survive the prune on save and fail the test.
	localPath := seedConfig(t, "local.json", nil)
	a := newTestApp(fileBackend(t, backendPath), localPath)

	state := config.CollapsedState{Sidebar: true, Sources: []string{"acct"}, View: "all"}
	if err := a.SetCollapsed(state); err != nil {
		t.Fatal(err)
	}

	local := loadConfig(t, localPath).Global.Collapsed
	if want := (&config.CollapsedState{Sidebar: true}); !reflect.DeepEqual(local, want) {
		t.Errorf("local collapsed = %+v, want only the sidebar", local)
	}
	backend := loadConfig(t, backendPath).Global.Collapsed
	if want := (&config.CollapsedState{Sources: []string{"acct"}, View: "all"}); !reflect.DeepEqual(backend, want) {
		t.Errorf("backend collapsed = %+v, want everything but the sidebar", backend)
	}
	if got := a.GetCollapsed(); !reflect.DeepEqual(got, state) {
		t.Errorf("GetCollapsed = %+v, want %+v", got, state)
	}

	// A change that leaves the sidebar alone does not write the local file.
	before := stamp(t, localPath)
	state.Sources = nil
	if err := a.SetCollapsed(state); err != nil {
		t.Fatal(err)
	}
	if stamp(t, localPath) != before {
		t.Errorf("a domain-only change wrote the local file")
	}
	if got := a.GetCollapsed(); !reflect.DeepEqual(got, state) {
		t.Errorf("GetCollapsed = %+v, want %+v", got, state)
	}
}

func TestLocalModeSingleFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	path := seedConfig(t, "gitbox.json", nil)
	seed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{cfg: loadConfig(t, path), cfgPath: path, cfgLoaded: true}
	if _, ok := a.clientPrefs().(sharedPrefs); !ok {
		t.Fatalf("a local App must share its config with the preferences, got %T", a.clientPrefs())
	}

	apps := []TerminalAppInfo{{ID: "wez", Name: "WezTerm", Command: "wezterm", ArgsTemplate: []string{"start", "--cwd", "{path}"}}}
	shells := []ShellInfo{{ID: "zsh", Name: "zsh", Command: "/bin/zsh", Args: []string{"-l"}}}
	profiles := []TerminalProfileInfo{{ID: "wez-zsh", Name: "WezTerm + zsh", TerminalID: "wez", ShellID: "zsh", Default: true, Source: "user"}}
	collapsed := config.CollapsedState{Sidebar: true, Sources: []string{"acct"}, View: "all"}
	account := config.Account{Provider: "github", URL: "https://github.com", Username: "bob", Name: "Bob", Email: "b@e", DefaultCredentialType: "token"}

	steps := []struct {
		name string
		run  func() error
	}{
		{"SetTheme", func() error { return a.SetTheme("dark") }},
		{"SetLanguage", func() error { return a.SetLanguage("es") }},
		{"SetCollapsed", func() error { return a.SetCollapsed(collapsed) }},
		{"SaveTerminalProfilesDTO", func() error { return a.SaveTerminalProfilesDTO(apps, shells, profiles) }},
		{"AddAccount", func() error {
			return a.AddAccount(AddAccountRequest{Key: "second", Provider: account.Provider, URL: account.URL,
				Username: account.Username, Name: account.Name, Email: account.Email, CredentialType: account.DefaultCredentialType})
		}},
	}
	for _, step := range steps {
		if err := step.run(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
	}

	// The expected file: the seed, the four preferences and the account.
	wantPath := filepath.Join(t.TempDir(), "want.json")
	if err := os.WriteFile(wantPath, seed, 0o644); err != nil {
		t.Fatal(err)
	}
	want := loadConfig(t, wantPath)
	want.Global.Theme = "dark"
	want.Global.Language = "es"
	want.Global.Collapsed = &collapsed
	want.Global.TerminalApps = []config.TerminalApp{{ID: "wez", Name: "WezTerm", Command: "wezterm", ArgsTemplate: []string{"start", "--cwd", "{path}"}}}
	want.Global.Shells = []config.ShellEntry{{ID: "zsh", Name: "zsh", Command: "/bin/zsh", Args: []string{"-l"}}}
	want.Global.TerminalProfiles = []config.TerminalProfile{{ID: "wez-zsh", Name: "WezTerm + zsh", TerminalID: "wez", ShellID: "zsh", Default: true, Source: "user"}}
	if err := ops.AddAccount(want, "second", account); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(want, wantPath); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantData, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantData) {
		t.Errorf("config on disk after the session:\n%s\nwant:\n%s", got, wantData)
	}
}

func TestWindowGeometryTarget(t *testing.T) {
	geom := func(x int) func() (int, int, int, int, bool) {
		return func() (int, int, int, int, bool) { return x, x + 1, 900, 700, true }
	}
	ws := func(x int) *config.WindowState { return &config.WindowState{X: x, Y: x + 1, Width: 900, Height: 700} }

	t.Run("local window", func(t *testing.T) {
		path := seedConfig(t, "gitbox.json", nil)
		a := &App{cfg: loadConfig(t, path), cfgPath: path, cfgLoaded: true}

		a.windowGeom = geom(10)
		a.BeforeClose(context.Background())
		if got := loadConfig(t, path).Global; !reflect.DeepEqual(got.Window, ws(10)) || got.CompactWindow != nil {
			t.Fatalf("BeforeClose in full view: window %+v, compact %+v", got.Window, got.CompactWindow)
		}

		a.windowGeom = geom(20)
		if target := a.SetViewMode("compact"); target != nil {
			t.Errorf("SetViewMode(compact) returned %+v with no compact window saved", target)
		}
		a.windowGeom = geom(30)
		a.BeforeClose(context.Background())
		got := loadConfig(t, path).Global
		if got.ViewMode != "compact" || !reflect.DeepEqual(got.Window, ws(20)) || !reflect.DeepEqual(got.CompactWindow, ws(30)) {
			t.Errorf("after switching to compact: view %q, window %+v, compact %+v", got.ViewMode, got.Window, got.CompactWindow)
		}
		if target := a.SetViewMode("full"); target == nil || target.X != 20 {
			t.Errorf("SetViewMode(full) = %+v, want the saved full window", target)
		}
	})

	t.Run("remote window", func(t *testing.T) {
		backendPath := seedConfig(t, "backend.json", nil)
		localPath := seedConfig(t, "local.json", func(c *config.Config) {
			c.Global.Window = ws(1)
			c.Global.Remotes = []config.RemoteEntry{{Alias: "other"}, {Alias: "my-server"}}
		})
		a := newTestApp(fileBackend(t, backendPath), localPath)
		a.remoteAlias = "my-server"
		before := stamp(t, backendPath)

		a.windowGeom = geom(10)
		a.BeforeClose(context.Background())
		a.windowGeom = geom(20)
		a.SetViewMode("compact")
		a.windowGeom = geom(30)
		a.BeforeClose(context.Background())

		got := loadConfig(t, localPath).Global
		if !reflect.DeepEqual(got.Window, ws(1)) || got.CompactWindow != nil {
			t.Errorf("the remote window wrote the local window's slots: %+v, %+v", got.Window, got.CompactWindow)
		}
		if r := got.Remotes[0]; r.Window != nil || r.CompactWindow != nil {
			t.Errorf("the remote window wrote another remote's entry: %+v", r)
		}
		if r := got.Remotes[1]; !reflect.DeepEqual(r.Window, ws(20)) || !reflect.DeepEqual(r.CompactWindow, ws(30)) {
			t.Errorf("remotes[1]: window %+v, compact %+v; want x=20 and x=30", r.Window, r.CompactWindow)
		}
		if stamp(t, backendPath) != before {
			t.Errorf("window geometry touched the backend's file")
		}
	})
}

func TestSaveGuardLocalFile(t *testing.T) {
	setTheme := func(p *config.ClientPrefs) { p.Theme = "dark" }

	t.Run("file that never parsed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gitbox.json")
		broken := []byte(`{"version": 5, "global": {`)
		if err := os.WriteFile(path, broken, 0o644); err != nil {
			t.Fatal(err)
		}
		f := newFilePrefs(path)
		if err := f.Update(setTheme); err == nil || !strings.Contains(err.Error(), "refusing to save") {
			t.Errorf("Update = %v, want a refusal", err)
		}
		if data, _ := os.ReadFile(path); !bytes.Equal(data, broken) {
			t.Errorf("the unparsed file was overwritten: %s", data)
		}
	})

	t.Run("file broken after a good load", func(t *testing.T) {
		path := seedConfig(t, "gitbox.json", func(c *config.Config) { c.Global.Theme = "light" })
		f := newFilePrefs(path)
		broken := []byte(`not json`)
		if err := os.WriteFile(path, broken, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := f.Update(setTheme); err == nil {
			t.Errorf("Update saved over a file it could not reload")
		}
		if data, _ := os.ReadFile(path); !bytes.Equal(data, broken) {
			t.Errorf("the broken file was overwritten: %s", data)
		}
		if got := f.Get().Theme; got != "light" {
			t.Errorf("theme in memory = %q, want the last good one", got)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "gitbox.json")
		if err := newFilePrefs(path).Update(setTheme); err == nil {
			t.Errorf("Update created a config from nothing")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("a file appeared at %s", path)
		}
	})

	t.Run("invalid remote", func(t *testing.T) {
		path := seedConfig(t, "gitbox.json", nil)
		before := stamp(t, path)
		err := newFilePrefs(path).Update(func(p *config.ClientPrefs) {
			p.Remotes = []config.RemoteEntry{{Alias: "-oProxyCommand=x"}}
		})
		if err == nil || stamp(t, path) != before {
			t.Errorf("Update saved a remote that the next load would reject: %v", err)
		}
	})
}
