package ops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

// sshAccount adds an SSH account at a host that does not resolve, so the
// connection test fails fast.
func sshAccount(t *testing.T, cfg *config.Config, key string) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not on PATH")
	}
	acct := testAccount("ssh")
	acct.URL = "https://forge.example.invalid"
	if err := AddAccount(cfg, key, acct); err != nil {
		t.Fatal(err)
	}
}

// TestSetupSSHCredential: the setup writes the Host entry and a key pair in
// the configured SSH folder, returns the public key and the provider's page,
// and a second call keeps the same key (#244).
func TestSetupSSHCredential(t *testing.T) {
	_, cfg := isolate(t)
	sshAccount(t, cfg, "acct")
	s, err := SetupSSHCredential(context.Background(), cfg, "acct")
	if err != nil {
		t.Fatal(err)
	}
	folder := credential.SSHFolder(cfg)
	keyPath := credential.SSHKeyPath(folder, "acct")
	pub, err := os.ReadFile(keyPath + ".pub")
	if err != nil || s.PublicKey != strings.TrimSpace(string(pub)) || !strings.HasPrefix(s.PublicKey, "ssh-ed25519 ") {
		t.Errorf("PublicKey = %q, the file has %q (%v)", s.PublicKey, pub, err)
	}
	if s.Host != "gitbox-acct" || s.Hostname != "forge.example.invalid" || s.KeyType != "ed25519" || s.Verified {
		t.Errorf("setup = %+v, want gitbox-acct at forge.example.invalid, not verified", s)
	}
	if found, err := credential.FindSSHConfigEntry(folder, "gitbox-acct"); !found || err != nil {
		t.Errorf("Host entry found = %v, %v", found, err)
	}
	if runtime.GOOS != "windows" {
		for p, mode := range map[string]os.FileMode{folder: 0o700, keyPath: 0o600} {
			if info, err := os.Stat(p); err != nil || info.Mode().Perm() != mode {
				t.Errorf("%s: mode %v (%v), want %o", p, info.Mode().Perm(), err, mode)
			}
		}
	}

	again, err := SetupSSHCredential(context.Background(), cfg, "acct")
	if err != nil || again.PublicKey != s.PublicKey {
		t.Errorf("a second setup changed the key: %q, %v", again.PublicKey, err)
	}
	data, _ := os.ReadFile(filepath.Join(folder, "config"))
	if n := strings.Count(string(data), "Host gitbox-acct\n"); n != 1 {
		t.Errorf("the config has %d Host entries for the account, want 1", n)
	}

	if _, err := SetupSSHCredential(context.Background(), cfg, "missing"); !errors.Is(err, config.ErrNotFound) {
		t.Errorf("a missing account = %v, want config.ErrNotFound", err)
	}
}

// TestRegenerateSSHCredential: the regenerate makes a new key and keeps one
// Host entry.
func TestRegenerateSSHCredential(t *testing.T) {
	_, cfg := isolate(t)
	sshAccount(t, cfg, "acct")
	first, err := SetupSSHCredential(context.Background(), cfg, "acct")
	if err != nil {
		t.Fatal(err)
	}
	second, err := RegenerateSSHCredential(context.Background(), cfg, "acct")
	if err != nil {
		t.Fatal(err)
	}
	if second.PublicKey == "" || second.PublicKey == first.PublicKey {
		t.Errorf("regenerated key %q, was %q: want a new one", second.PublicKey, first.PublicKey)
	}
	data, _ := os.ReadFile(filepath.Join(credential.SSHFolder(cfg), "config"))
	if n := strings.Count(string(data), "Host gitbox-acct\n"); n != 1 {
		t.Errorf("the config has %d Host entries for the account, want 1", n)
	}
}

// TestApplySSHSetup: the SSH block is replaced, not written through a
// pointer a copy shares, and an unchanged block reports no change.
func TestApplySSHSetup(t *testing.T) {
	shared := &config.SSHConfig{Host: "old", KeyType: "rsa"}
	acct := config.Account{SSH: shared}
	s := SSHSetup{Host: "gitbox-acct", Hostname: "github.com", KeyType: "ed25519"}
	if !ApplySSHSetup(&acct, s) {
		t.Fatal("ApplySSHSetup reported no change")
	}
	if shared.Host != "old" || acct.SSH == shared || acct.SSH.Host != "gitbox-acct" || acct.SSH.Hostname != "github.com" || acct.SSH.KeyType != "ed25519" {
		t.Errorf("block = %+v, shared = %+v", acct.SSH, shared)
	}
	if ApplySSHSetup(&acct, s) {
		t.Error("a second apply reported a change")
	}
}

// TestDeleteAccount_RemovesTheSSHKey: deleting an account removes its key
// pair and Host entry, so a new account with the same key gets a new key
// (#244). Another account's key and entry stay.
func TestDeleteAccount_RemovesTheSSHKey(t *testing.T) {
	_, cfg := isolate(t)
	for _, key := range []string{"acct", "other"} {
		sshAccount(t, cfg, key)
		if _, err := SetupSSHCredential(context.Background(), cfg, key); err != nil {
			t.Fatal(err)
		}
	}
	if err := DeleteAccount(cfg, "acct"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	folder := credential.SSHFolder(cfg)
	for _, p := range []string{credential.SSHKeyPath(folder, "acct"), credential.SSHKeyPath(folder, "acct") + ".pub"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still on disk: %v", p, err)
		}
	}
	if found, _ := credential.FindSSHConfigEntry(folder, "gitbox-acct"); found {
		t.Error("the deleted account's Host entry is still in the config")
	}
	if found, _ := credential.FindSSHConfigEntry(folder, "gitbox-other"); !found {
		t.Error("the other account's Host entry is gone")
	}
	if _, err := os.Stat(credential.SSHKeyPath(folder, "other")); err != nil {
		t.Errorf("the other account's key is gone: %v", err)
	}
}
