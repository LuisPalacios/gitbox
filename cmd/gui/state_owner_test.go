package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// ownerApp is a loaded App on its own gitbox.json, whose file on disk has a
// client preference (the theme) that another process saved after this one
// loaded: a domain save must keep it (#240, #173).
func ownerApp(t *testing.T) (*App, string) {
	t.Helper()
	path := seedConfig(t, "gitbox.json", nil)
	a := &App{cfg: loadConfig(t, path), cfgPath: path, cfgLoaded: true}
	onDisk := loadConfig(t, path)
	onDisk.Global.Theme = "dark"
	if err := config.Save(onDisk, path); err != nil {
		t.Fatal(err)
	}
	return a, path
}

func addAccount(key string) func(*config.Config) error {
	return func(c *config.Config) error {
		c.Accounts[key] = config.Account{Provider: "github", URL: "https://github.com", Username: key, Name: key, Email: key + "@e"}
		return nil
	}
}

// TestCommitConfig: work that ran on a snapshot writes its outcome through
// commitConfig, which changes the live config under the lock, saves it with
// saveConfig's guards and the preferences on disk, and hands back a copy
// (#173). A failing change or a refused save leaves the live config, its
// pointer and the file as they were.
func TestCommitConfig(t *testing.T) {
	a, path := ownerApp(t)
	live := a.cfg

	got, err := a.commitConfig(addAccount("bob"))
	if err != nil {
		t.Fatal(err)
	}
	if got == a.cfg {
		t.Error("commitConfig returned the live config instead of a copy")
	}
	if _, ok := a.cfg.Accounts["bob"]; !ok {
		t.Error("the live config did not get the change")
	}
	disk := loadConfig(t, path)
	if _, ok := disk.Accounts["bob"]; !ok || disk.Global.Theme != "dark" {
		t.Errorf("on disk: bob=%v theme=%q, want the change and the theme another process saved", ok, disk.Global.Theme)
	}

	before, _ := a.cfg.Clone()
	boom := errors.New("the entry moved meanwhile")
	if _, err := a.commitConfig(func(c *config.Config) error {
		_ = addAccount("half")(c)
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("a failing change = %v", err)
	}
	if !config.EqualIgnoringUIState(before, a.cfg) || a.cfg != live {
		t.Error("a failing change left a partial edit or replaced the live pointer")
	}

	a.readOnly = true
	if _, err := a.commitConfig(addAccount("carol")); err == nil {
		t.Fatal("a read-only window committed a change")
	}
	if _, ok := a.cfg.Accounts["carol"]; ok || a.cfg != live {
		t.Error("a refused commit changed the live config")
	}
	if _, ok := loadConfig(t, path).Accounts["carol"]; ok {
		t.Error("a refused commit reached the file")
	}
}

// TestFixGlobalGCMConfigSavesThroughOwner: the GCM defaults that the fix
// backfills are saved by the window, through saveConfig, keeping the
// preferences on disk; pkg/credential no longer saves (#173). A refused
// save takes the backfill back out of memory.
func TestFixGlobalGCMConfigSavesThroughOwner(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	a, path := ownerApp(t)
	if err := a.fixGlobalGCMConfigLocal(); err != nil {
		t.Fatal(err)
	}
	disk := loadConfig(t, path)
	if disk.Global.CredentialGCM == nil || disk.Global.CredentialGCM.Helper == "" || disk.Global.Theme != "dark" {
		t.Errorf("on disk: credential_gcm=%+v theme=%q", disk.Global.CredentialGCM, disk.Global.Theme)
	}

	ro, roPath := ownerApp(t)
	ro.readOnly = true
	if err := ro.fixGlobalGCMConfigLocal(); err == nil {
		t.Error("a read-only window saved the GCM backfill")
	}
	if ro.cfg.Global.CredentialGCM != nil {
		t.Error("a refused save left the backfill in memory")
	}
	if loadConfig(t, roPath).Global.CredentialGCM != nil {
		t.Error("a refused save reached the file")
	}
}
