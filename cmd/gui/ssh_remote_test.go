package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

// privateKeyLines returns the base64 lines of the private key at path, the
// ones that carry key material.
func privateKeyLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); len(line) >= 20 && !strings.HasPrefix(line, "-----") {
			lines = append(lines, line)
		}
	}
	if len(lines) < 3 {
		t.Fatalf("the private key at %s has %d lines of key material, want a real key", path, len(lines))
	}
	return lines
}

// TestPrivateKeyNeverOnWire is the in-process half of I6 for SSH accounts
// (#244; pkg/remote's runs it over ssh against a real host). A remote window
// sets up an account's key on the host, verifies it, asks for the agent
// report and regenerates the key. Neither private key crosses the wire in
// either direction, reaches an event or a log line of either end; the
// public key does come back.
func TestPrivateKeyNeverOnWire(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not on PATH")
	}
	path := contractConfig(t)
	cfg := loadConfig(t, path)
	folder := filepath.Join(t.TempDir(), "ssh")
	cfg.Global.CredentialSSH = &config.SSHGlobal{SSHFolder: folder}
	acct := cfg.Accounts["acct"]
	acct.URL, acct.DefaultCredentialType = "https://forge.example.invalid", "ssh"
	cfg.Accounts["acct"] = acct
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	link := serveRemote(t, path, nil)
	keyPath := credential.SSHKeyPath(folder, "acct")

	first := link.Remote.CredentialSetupSSH("acct")
	if !first.OK || !strings.HasPrefix(first.SSHPublicKey, "ssh-ed25519 ") {
		t.Fatalf("CredentialSetupSSH = %+v (%v)", first, link.Remote.LastError())
	}
	secrets := privateKeyLines(t, keyPath)
	if again := link.Remote.CredentialSetupSSH("acct"); again.SSHPublicKey != first.SSHPublicKey {
		t.Errorf("a second setup changed the key: %q", again.SSHPublicKey)
	}
	if r := link.Remote.CredentialSSHAgentReport("acct"); r.KeyFile != keyPath || !strings.HasPrefix(r.KeyFileFingerprint, "SHA256:") {
		t.Errorf("CredentialSSHAgentReport = %+v, want the key at %s", r, keyPath)
	}
	second := link.Remote.CredentialRegenerateSSH("acct")
	if !second.OK || second.SSHPublicKey == first.SSHPublicKey {
		t.Fatalf("CredentialRegenerateSSH = %+v, want a new key", second)
	}
	secrets = append(secrets, privateKeyLines(t, keyPath)...)

	var events strings.Builder
	link.Events.mu.Lock()
	for _, e := range link.Events.events {
		events.WriteString(e.name + " " + asJSON(t, e.args) + "\n")
	}
	link.Events.mu.Unlock()
	// The server's events also cross the wire, in its answers.
	seen := map[string]string{
		"the server's answers":        link.Sent.String(),
		"the client's requests":       link.Received.String(),
		"the server log":              link.ServerLog.String(),
		"the client log":              link.ClientLog.String(),
		"the events":                  events.String(),
		"the results the window sees": asJSON(t, []any{first, second}),
	}
	for where, text := range seen {
		for _, line := range secrets {
			if strings.Contains(text, line) {
				t.Errorf("%s hold a line of a private key", where)
				break
			}
		}
	}
	// Positive control: the public keys did come back over the wire.
	for _, pub := range []string{first.SSHPublicKey, second.SSHPublicKey} {
		if fields := strings.Fields(pub); len(fields) < 2 || !strings.Contains(link.Sent.String(), fields[1]) {
			t.Errorf("the public key %q is not in the server's answers", pub)
		}
	}
}
