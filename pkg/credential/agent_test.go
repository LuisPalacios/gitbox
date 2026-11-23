package credential

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// TestParseAgentList keeps only the fingerprints of ssh-add -l, once each,
// and nothing of the comments or of the messages for no keys or no agent.
func TestParseAgentList(t *testing.T) {
	out := strings.Join([]string{
		"256 SHA256:AAAAbbbbCCCCdddd1111 alice@laptop /home/alice/.ssh/id_ed25519 (ED25519)",
		"3072 SHA256:ZZZZyyyyXXXXwwww2222 work key (RSA)",
		"256 SHA256:AAAAbbbbCCCCdddd1111 alice@laptop (ED25519)",
		"The agent has no identities.",
		"Could not open a connection to your authentication agent.",
		"",
	}, "\n")
	got := parseAgentList(out)
	want := []string{"SHA256:AAAAbbbbCCCCdddd1111", "SHA256:ZZZZyyyyXXXXwwww2222"}
	if !slices.Equal(got, want) {
		t.Errorf("parseAgentList = %q, want %q", got, want)
	}
	if got := parseAgentList("The agent has no identities.\n"); len(got) != 0 {
		t.Errorf("no identities = %q, want none", got)
	}
}

// agentTestConfig is a config whose SSH folder is in the test, with one SSH
// account whose key and Host entry gitbox wrote.
func agentTestConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen is not on PATH")
	}
	folder := filepath.Join(t.TempDir(), "ssh")
	cfg := &config.Config{
		Global:   config.GlobalConfig{CredentialSSH: &config.SSHGlobal{SSHFolder: folder}},
		Accounts: map[string]config.Account{"acct": {Provider: "github", URL: "https://github.com", Username: "alice"}},
	}
	keyPath, err := GenerateSSHKey(folder, "acct", "ed25519")
	if err != nil {
		t.Fatal(err)
	}
	opts := SSHConfigEntryOpts{Host: SSHHostAlias("acct"), Hostname: "github.com", KeyFile: keyPath, Username: "alice"}
	if err := WriteSSHConfigEntry(folder, opts); err != nil {
		t.Fatal(err)
	}
	return cfg, keyPath
}

// TestSSHAgentReportWithoutAgent: the report finds the key the Host entry
// names, with the fingerprint ssh-keygen prints for it, and sees that the
// entry pins it. With no agent there is nothing forwarded and no match.
func TestSSHAgentReportWithoutAgent(t *testing.T) {
	cfg, keyPath := agentTestConfig(t)
	t.Setenv("SSH_AUTH_SOCK", "")
	r := SSHAgentReport(context.Background(), cfg, "acct")
	if r.KeyFile != keyPath {
		t.Errorf("KeyFile = %q, want %q", r.KeyFile, keyPath)
	}
	out, err := exec.Command("ssh-keygen", "-l", "-E", "sha256", "-f", keyPath+".pub").Output()
	if err != nil {
		t.Fatal(err)
	}
	if fields := strings.Fields(string(out)); len(fields) < 2 || r.KeyFileFingerprint != fields[1] {
		t.Errorf("KeyFileFingerprint = %q, ssh-keygen says %q", r.KeyFileFingerprint, out)
	}
	if !r.IdentitiesOnly || r.AgentForwarded || r.Match || len(r.AgentFingerprints) != 0 {
		t.Errorf("report = %+v, want the key pinned and no agent", r)
	}

	// An entry edited without IdentitiesOnly does not pin the key.
	conf := filepath.Join(filepath.Dir(keyPath), "config")
	data, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conf, []byte(strings.ReplaceAll(string(data), "IdentitiesOnly yes", "")), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := SSHAgentReport(context.Background(), cfg, "acct"); r.IdentitiesOnly {
		t.Error("IdentitiesOnly = true for an entry without it")
	}
}

// TestSSHAgentReportWithAgent: an agent that holds the account's key and
// another one lists both and matches.
func TestSSHAgentReportWithAgent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows agent is a system service")
	}
	if _, err := exec.LookPath("ssh-agent"); err != nil {
		t.Skip("ssh-agent is not on PATH")
	}
	cfg, keyPath := agentTestConfig(t)
	// Socket paths must be short.
	dir, err := os.MkdirTemp("/tmp", "gbxag")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s")
	agent := exec.Command("ssh-agent", "-D", "-a", sock)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Process.Kill(); _ = agent.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ssh-agent did not create its socket")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Setenv("SSH_AUTH_SOCK", sock)
	other := filepath.Join(dir, "other")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", other).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v %s", err, out)
	}
	for _, k := range []string{keyPath, other} {
		if out, err := exec.Command("ssh-add", k).CombinedOutput(); err != nil {
			t.Fatalf("ssh-add: %v %s", err, out)
		}
	}
	r := SSHAgentReport(context.Background(), cfg, "acct")
	if !r.AgentForwarded || !r.Match || len(r.AgentFingerprints) != 2 || !slices.Contains(r.AgentFingerprints, publicKeyFingerprint(other+".pub")) {
		t.Errorf("report = %+v, want both keys and a match", r)
	}
}
