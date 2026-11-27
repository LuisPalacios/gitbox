package main

import (
	"bufio"
	"fmt"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"io"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// windowOn is a loaded App on path, holding the domain it just read, as a
// window that started on that file would (#241).
func windowOn(t *testing.T, path string) *App {
	t.Helper()
	cfg, _, base, err := config.LoadRevision(path, false)
	if err != nil {
		t.Fatal(err)
	}
	return newTestApp(withCfg(cfg), withCfgPath(path), withCfgLoaded(), withCfgBase(base))
}

// TestConfigChangedConflict: two windows on one gitbox.json and a plain
// config writer standing in for a GitboxApp on the same host. A domain
// save after another process's domain save is refused with
// config.changed, the window reloads the file, and no update is lost. A
// preferences-only save is never refused: it reloads and applies on top.
func TestConfigChangedConflict(t *testing.T) {
	path := seedConfig(t, "gitbox.json", nil)
	one, two := windowOn(t, path), windowOn(t, path)

	if err := one.SetNestedScanDepth(4); err != nil {
		t.Fatal(err)
	}
	err := two.SetNestedScanDepth(7)
	if code := toUserError(err).Code; code != "config.changed" {
		t.Fatalf("the second window's save = %v (%s), want config.changed", err, code)
	}
	if two.cfg.Global.NestedScanDepth != 4 {
		t.Errorf("after the conflict the window holds depth %d, want the file's 4", two.cfg.Global.NestedScanDepth)
	}
	if err := two.SetNestedScanDepth(7); err != nil {
		t.Fatalf("the retry after the reload = %v", err)
	}
	if got := loadConfig(t, path).Global.NestedScanDepth; got != 7 {
		t.Errorf("depth on disk = %d, want the retried 7", got)
	}

	// A plain writer on the same file (a GitboxApp on that host).
	other := loadConfig(t, path)
	other.Accounts["host"] = config.Account{Provider: "github", URL: "https://github.com", Username: "h", Name: "H", Email: "h@e"}
	if err := config.Save(other, path); err != nil {
		t.Fatal(err)
	}
	if err := one.SetNestedScanDepth(2); toUserError(err).Code != "config.changed" {
		t.Fatalf("a save after a plain writer = %v, want config.changed", err)
	}
	if _, ok := one.cfg.Accounts["host"]; !ok {
		t.Error("the reload did not pick up the plain writer's account")
	}

	// Preferences only: never refused, applied on top of the latest file.
	third := windowOn(t, path)
	if err := one.SetNestedScanDepth(3); err != nil {
		t.Fatal(err)
	}
	third.mu.Lock()
	err = third.savePrefs(func(p *config.ClientPrefs) error { p.Theme = "dark"; return nil })
	third.mu.Unlock()
	if err != nil {
		t.Fatalf("a preference save after another window's domain save = %v", err)
	}
	disk := loadConfig(t, path)
	if disk.Global.Theme != "dark" || disk.Global.NestedScanDepth != 3 {
		t.Errorf("on disk: theme %q depth %d, want both changes", disk.Global.Theme, disk.Global.NestedScanDepth)
	}
}

// TestFilePrefsUpdateUnderLock: a preference store on the file (the
// terminals window) racing domain saves keeps every change (#241).
func TestFilePrefsUpdateUnderLock(t *testing.T) {
	// Start the launcher editor in a second process. Both processes have already
	// loaded the same file before either begins writing.
	const envPath = "GITBOX_TEST_TERMINALS_CONFIG"
	if path := os.Getenv(envPath); path != "" {
		child := newTestApp(withCfgPath(path))
		child.useOwnPrefsFile()
		fmt.Println("ready")
		if _, err := io.ReadAll(os.Stdin); err != nil {
			t.Fatal(err)
		}
		err := child.saveTerminalProfiles(
			[]config.TerminalApp{{ID: "term", Name: "Terminal", Command: "term"}}, nil,
			[]config.TerminalProfile{{ID: "fixture", Name: "Fixture", TerminalID: "term", Source: "user"}})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Run("two processes preserve roots, profiles and layout", func(t *testing.T) {
		path := seedConfig(t, "gitbox.json", nil)
		parent, stale := windowOn(t, path), windowOn(t, path)
		child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestFilePrefsUpdateUnderLock$")
		git.HideWindow(child)
		child.Env = append(os.Environ(), envPath+"="+path)
		child.Stderr = os.Stderr
		input, err := child.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		output, err := child.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		ready, err := bufio.NewReader(output).ReadString('\n')
		if err != nil || ready != "ready\n" {
			t.Fatalf("child readiness: %q %v", ready, err)
		}
		root := t.TempDir()
		if err := parent.AddRoot(root); err != nil {
			t.Fatal(err)
		}
		if err := parent.SetTheme("dark"); err != nil {
			t.Fatal(err)
		}
		if err := parent.clientPrefs().Update(func(p *config.ClientPrefs) { p.Window = &config.WindowState{Width: 900, Height: 600} }); err != nil {
			t.Fatal(err)
		}
		if err := input.Close(); err != nil {
			t.Fatal(err)
		}
		if err := child.Wait(); err != nil {
			t.Fatal(err)
		}
		if err := stale.SetCollapsed(config.CollapsedState{View: "all"}); err != nil {
			t.Fatal(err)
		}
		if err := parent.SetNestedScanDepth(4); err != nil {
			t.Fatal(err)
		}
		g := loadConfig(t, path).Global
		if !slices.Contains(g.Roots, root) || g.Theme != "dark" || g.Window == nil || g.Window.Width != 900 || len(g.TerminalProfiles) != 1 || g.TerminalProfiles[0].ID != "fixture" || g.Collapsed == nil || g.Collapsed.View != "all" || g.NestedScanDepth != 4 {
			t.Fatalf("a file update was lost: %+v", g)
		}
	})
	path := seedConfig(t, "gitbox.json", nil)
	win := windowOn(t, path)
	store := newFilePrefs(path)
	const rounds = 20
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range rounds {
			if err := store.Update(func(p *config.ClientPrefs) { p.Language = []string{"en", "es"}[i%2] }); err != nil {
				t.Error(err)
			}
		}
	})
	wg.Go(func() {
		for depth := 1; depth <= rounds; depth++ {
			for {
				err := win.SetNestedScanDepth(depth)
				if err == nil {
					break
				}
				if toUserError(err).Code != "config.changed" {
					t.Error(err)
					return
				}
			}
		}
	})
	wg.Wait()
	disk := loadConfig(t, path)
	if disk.Global.NestedScanDepth != rounds || disk.Global.Language != "es" {
		t.Errorf("on disk: depth %d language %q, want %d and es", disk.Global.NestedScanDepth, disk.Global.Language, rounds)
	}
}
