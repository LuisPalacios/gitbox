package config

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// casFixture saves a small valid config and returns it with its path and
// its domain hash, as a writer that just read it would hold them.
func casFixture(t *testing.T) (*Config, string, Hash) {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{
		Version: CurrentVersion,
		Global:  GlobalConfig{Roots: []string{filepath.Join(dir, "git")}},
		Accounts: map[string]Account{
			"a": {Provider: "github", URL: "https://github.com", Username: "a", Name: "A", Email: "a@e"},
		},
		Sources: map[string]Source{"a": {Account: "a"}},
	}
	path := filepath.Join(dir, "gitbox.json")
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	read, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return read, path, DomainHash(read)
}

func withAccount(t *testing.T, path, key string) {
	t.Helper()
	other, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	other.Accounts[key] = Account{Provider: "github", URL: "https://github.com", Username: key, Name: key, Email: key + "@e"}
	if err := Save(other, path); err != nil {
		t.Fatal(err)
	}
}

// TestCASSave: a writer saves only when the file still holds the domain it
// read (#241). A domain change by another writer is refused; a change of
// preferences only is merged; a stale lock is broken; a held lock is
// waited for and then refused; and of several writers from one base,
// exactly one wins.
func TestCASSave(t *testing.T) {
	t.Run("an external domain change is refused", func(t *testing.T) {
		cfg, path, base := casFixture(t)
		withAccount(t, path, "other")
		cfg.Accounts["mine"] = Account{Provider: "github", URL: "https://github.com", Username: "m", Name: "M", Email: "m@e"}
		_, err := SaveIfUnchanged(cfg, path, base, nil)
		var ce *ChangedError
		if !errors.As(err, &ce) || ce.Locked {
			t.Fatalf("SaveIfUnchanged = %v, want a ChangedError", err)
		}
		disk, _ := Load(path)
		if _, ok := disk.Accounts["other"]; !ok {
			t.Error("the other writer's change was lost")
		}
		if _, ok := disk.Accounts["mine"]; ok {
			t.Error("the refused change reached the file")
		}
	})

	t.Run("an external preference change is merged", func(t *testing.T) {
		cfg, path, base := casFixture(t)
		other, _ := Load(path)
		other.Global.Theme = "dark"
		if err := Save(other, path); err != nil {
			t.Fatal(err)
		}
		cfg.Accounts["mine"] = Account{Provider: "github", URL: "https://github.com", Username: "m", Name: "M", Email: "m@e"}
		h, err := SaveIfUnchanged(cfg, path, base, func(onDisk *Config) error {
			PreserveClientPrefs(cfg, onDisk)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		disk, _ := Load(path)
		if _, ok := disk.Accounts["mine"]; !ok || disk.Global.Theme != "dark" {
			t.Errorf("on disk: mine=%v theme=%q", ok, disk.Global.Theme)
		}
		if h != DomainHash(disk) || h == base {
			t.Error("the returned hash is not the domain just written")
		}
	})

	t.Run("a zero base saves whatever the file holds", func(t *testing.T) {
		cfg, path, _ := casFixture(t)
		withAccount(t, path, "other")
		if _, err := SaveIfUnchanged(cfg, path, Hash{}, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a stale lock is broken", func(t *testing.T) {
		cfg, path, base := casFixture(t)
		lock := path + ".lock"
		child := exec.Command(os.Args[0], "-test.run=^$")
		if err := child.Run(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lock, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * lockStale)
		if err := os.Chtimes(lock, old, old); err != nil {
			t.Fatal(err)
		}
		warned := false
		prev := lockWarn
		lockWarn = func(string) { warned = true }
		t.Cleanup(func() { lockWarn = prev })
		if _, err := SaveIfUnchanged(cfg, path, base, nil); err != nil {
			t.Fatal(err)
		}
		if !warned {
			t.Error("breaking a stale lock was not reported")
		}
		if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
			t.Error("the lock was left behind")
		}
	})

	t.Run("a held lock is waited for and then refused", func(t *testing.T) {
		cfg, path, base := casFixture(t)
		if err := os.WriteFile(path+".lock", []byte("1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Age alone cannot evict a live writer.
		lock := path + ".lock"
		if err := os.WriteFile(lock, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-2 * lockStale)
		if err := os.Chtimes(lock, old, old); err != nil {
			t.Fatal(err)
		}
		prev := lockWait
		lockWait = 100 * time.Millisecond
		t.Cleanup(func() { lockWait = prev })
		_, err := SaveIfUnchanged(cfg, path, base, nil)
		var ce *ChangedError
		if !errors.As(err, &ce) || !ce.Locked {
			t.Fatalf("SaveIfUnchanged under a held lock = %v, want ChangedError{Locked}", err)
		}
	})

	t.Run("a failing merge writes nothing and releases the lock", func(t *testing.T) {
		cfg, path, base := casFixture(t)
		before, _ := os.ReadFile(path)
		cfg.Accounts["mine"] = Account{Provider: "github", URL: "https://github.com", Username: "m", Name: "M", Email: "m@e"}
		boom := errors.New("boom")
		if _, err := SaveIfUnchanged(cfg, path, base, func(*Config) error { return boom }); !errors.Is(err, boom) {
			t.Fatalf("SaveIfUnchanged = %v", err)
		}
		if after, _ := os.ReadFile(path); string(after) != string(before) {
			t.Error("a failed save changed the file")
		}
		if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
			t.Error("a failed save left the lock")
		}
	})

	t.Run("of several writers from one base exactly one wins", func(t *testing.T) {
		_, path, base := casFixture(t)
		const writers = 8
		var wg sync.WaitGroup
		var mu sync.Mutex
		won := 0
		for i := range writers {
			wg.Go(func() {
				mine, err := Load(path)
				if err != nil {
					t.Error(err)
					return
				}
				key := string(rune('b' + i))
				mine.Accounts[key] = Account{Provider: "github", URL: "https://github.com", Username: key, Name: key, Email: key + "@e"}
				if _, err := SaveIfUnchanged(mine, path, base, nil); err == nil {
					mu.Lock()
					won++
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		if won != 1 {
			t.Errorf("%d writers won from one base, want exactly 1", won)
		}
	})
}

// TestUpdateFile: a read-modify-write under the lock keeps another
// writer's domain change and refuses a file that does not load.
func TestUpdateFile(t *testing.T) {
	_, path, _ := casFixture(t)
	withAccount(t, path, "other")
	got, err := UpdateFile(path, func(c *Config) error {
		c.Global.Theme = "dark"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	disk, _ := Load(path)
	if _, ok := disk.Accounts["other"]; !ok || disk.Global.Theme != "dark" || got.Global.Theme != "dark" {
		t.Errorf("UpdateFile lost a change: other=%v theme=%q", ok, disk.Global.Theme)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateFile(path, func(*Config) error { return nil }); err == nil {
		t.Error("UpdateFile saved over a file that does not parse")
	}
}

// A window comparing the bytes it read refuses external rewrites, invalid
// content and removal. A first-run save refuses a file that appeared.
func TestCASSaveOverUnreadableFile(t *testing.T) {
	for _, tc := range []string{"formatting", "invalid JSON", "removed", "appeared", "changed after repair"} {
		t.Run(tc, func(t *testing.T) {
			cfg, path, _ := casFixture(t)
			if tc == "appeared" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			_, _, base, err := LoadRevision(path, tc == "changed after repair")
			if err != nil && !(tc == "appeared" && errors.Is(err, os.ErrNotExist)) {
				t.Fatal(err)
			}
			var data []byte
			switch tc {
			case "invalid JSON":
				data = []byte("{")
			case "removed":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			default:
				data, err = Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, '\n')
			}
			if data != nil {
				if err := os.WriteFile(path, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc == "invalid JSON" || tc == "removed" {
				if _, err := SaveIfUnchanged(cfg, path, DomainHash(cfg), nil); err == nil {
					t.Fatal("domain-only save accepted a missing or invalid replacement")
				}
			}
			_, err = base.Save(cfg, path, func(*Config) error { t.Fatal("merging refused save"); return nil })
			if _, ok := errors.AsType[*ChangedError](err); !ok {
				t.Fatalf("expected a conflict, got %v", err)
			}
			fresh, err := os.ReadFile(path)
			if data == nil && !errors.Is(err, os.ErrNotExist) || data != nil && (err != nil || !bytes.Equal(data, fresh)) {
				t.Fatalf("external file changed: %v", err)
			}
		})
	}
}
