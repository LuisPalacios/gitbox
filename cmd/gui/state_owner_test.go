package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
)

// ownerApp is a loaded App on its own gitbox.json, whose file on disk has a
// client preference (the theme) that another process saved after this one
// loaded: a domain save must keep it (#240, #173).
func ownerApp(t *testing.T) (*App, string) {
	t.Helper()
	path := seedConfig(t, "gitbox.json", nil)
	a := newTestApp(withCfg(loadConfig(t, path)), withCfgPath(path), withCfgLoaded())
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

// mirrorApp is a loaded App with one mirror group whose two servers are
// known down, so its status reads come back at once without the network.
func mirrorApp(t *testing.T) (*App, *recordingSink) {
	t.Helper()
	path := seedConfig(t, "gitbox.json", func(c *config.Config) {
		c.Accounts["src"] = config.Account{Provider: "gitea", URL: "https://src.invalid", Username: "s", Name: "S", Email: "s@e"}
		c.Accounts["dst"] = config.Account{Provider: "gitea", URL: "https://dst.invalid", Username: "d", Name: "D", Email: "d@e"}
		c.Mirrors = map[string]config.Mirror{"m": {AccountSrc: "src", AccountDst: "dst", Repos: map[string]config.MirrorRepo{"r0": {Direction: "push", Origin: "src"}}}}
	})
	events := &recordingSink{}
	a := newTestApp(withCfg(loadConfig(t, path)), withCfgPath(path), withCfgLoaded(), withEvents(events))
	a.network.down = map[string]bool{
		serverAddr(a.cfg.Accounts["src"]): true,
		serverAddr(a.cfg.Accounts["dst"]): true,
	}
	return a, events
}

// TestMirrorStatusDuringSetup: the status of a mirror group reads a
// snapshot, so a mirror setup that writes the group's repos under the lock
// at the same time is no data race (#173). Run with -race: before the
// snapshot this was a concurrent map read and write, which aborts Go.
func TestMirrorStatusDuringSetup(t *testing.T) {
	a, events := mirrorApp(t)
	const rounds = 40
	for i := range rounds {
		a.getMirrorStatusLocal("m")
		a.mu.Lock()
		m := a.cfg.Mirrors["m"]
		m.Repos[fmt.Sprintf("r%d", i+1)] = config.MirrorRepo{Direction: "push", Origin: "src", LastSync: "now"}
		a.cfg.Mirrors["m"] = m
		a.mu.Unlock()
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(events.named("mirror:status")) < rounds {
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d status reads finished", len(events.named("mirror:status")), rounds)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestApplyDiscoveredMirrorsUnlocked: the mirror discovery, which talks to
// every server for up to two minutes, runs on a snapshot without the lock,
// and what it found is merged into the live config under the lock (#173).
func TestApplyDiscoveredMirrorsUnlocked(t *testing.T) {
	a, _ := mirrorApp(t)
	var unlocked bool
	a.discoverMirrors = func(_ context.Context, snap *config.Config, _ func(mirror.DiscoverProgress)) ([]mirror.DiscoveryResult, error) {
		if unlocked = a.mu.TryLock(); unlocked {
			a.mu.Unlock()
		}
		if snap == a.cfg {
			t.Error("the discovery got the live config")
		}
		return nil, nil
	}
	if err := a.applyDiscoveredMirrorsLocal(); err != nil {
		t.Fatal(err)
	}
	if !unlocked {
		t.Error("the discovery ran with a.mu held")
	}
}

// TestDetachedCredentials covers the snapshot used after saving an adoption.
func TestDetachedCredentials(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	const key = "github-alice"
	acct := a.cfg.Accounts[key]
	acct.SSH = &config.SSHConfig{Host: "h"}
	acct.GCM = &config.GCMConfig{}
	a.cfg.Accounts[key] = acct
	g := &a.cfg.Global
	g.CredentialSSH = &config.SSHGlobal{}
	g.CredentialGCM = &config.GCMGlobal{Helper: "x"}
	g.CredentialToken = &config.TokenGlobal{}
	snap := a.snapshotRefreshConfig()
	got := snap.Accounts[key]
	if got.SSH == acct.SSH || got.GCM == acct.GCM || got.SSH.Host != "h" {
		t.Errorf("account snapshot shares a pointer or lost a value: %+v", got)
	}
	dg := snap.Global
	// Go can give zero-sized TokenGlobal values the same address.
	if dg.CredentialSSH == g.CredentialSSH || dg.CredentialGCM == g.CredentialGCM || dg.CredentialToken == nil || dg.CredentialGCM.Helper != "x" {
		t.Errorf("global snapshot shares a pointer or lost a value: %+v", dg)
	}
}
