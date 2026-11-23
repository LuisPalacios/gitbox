//go:build sshd_integration

package remote_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// The SSH accounts of a remote host (#244). One container is the host the
// window connects to; a second, started with gitHost, is the git provider:
// its git user has git-shell and owns org/small.git. The account's URL names
// the provider's IP, so the Host entry gitbox writes on the host points at
// it.

// sshHostConfig is the host's gitbox.json: one SSH account at gitHostIP.
func sshHostConfig(gitHostIP string) string {
	return `{
  "version": 5,
  "global": {"roots": ["` + testHome + `/git"], "theme": "dark"},
  "accounts": {
    "acct": {"provider": "gitea", "url": "http://` + gitHostIP + `", "username": "org", "name": "Org", "email": "o@example.invalid", "default_credential_type": "ssh"}
  },
  "sources": {"acct": {"account": "acct", "repos": {"org/small": {}}}}
}
`
}

// seedSSHHost writes the host's config for an SSH account at the git host.
func seedSSHHost(c, gitHost *container) {
	cfgDir := testHome + "/.config/gitbox"
	c.sh("mkdir -p " + cfgDir + " " + testHome + "/git && echo " + base64.StdEncoding.EncodeToString([]byte(sshHostConfig(gitHost.ip))) + " | base64 -d > " + cfgDir + "/gitbox.json")
}

// sshSetup is the part of a CredentialSetupResult these tests read.
type sshSetup struct {
	OK           bool   `json:"ok"`
	Message      string `json:"message"`
	SSHPublicKey string `json:"sshPublicKey"`
	SSHVerified  bool   `json:"sshVerified"`
}

// agentReport is the SSHAgentReportDTO as it crosses the wire.
type agentReport struct {
	KeyFile            string   `json:"keyFile"`
	KeyFileFingerprint string   `json:"keyFileFingerprint"`
	AgentFingerprints  []string `json:"agentFingerprints"`
	AgentForwarded     bool     `json:"agentForwarded"`
	Match              bool     `json:"match"`
	IdentitiesOnly     bool     `json:"identitiesOnly"`
}

const hostKey = testHome + "/.ssh/gitbox-acct-sshkey"

// callInto calls method with a fresh request id and decodes its answer.
func callInto(t *testing.T, client *rpc.Client, out any, method string, params ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(testCtx(t), 90*time.Second)
	defer cancel()
	if err := client.Call(ctx, method, params, out, rpc.WithRequestID(newCanaryRequestID(t))); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
}

// authorize lets the host's account key log in as git on the git host.
func authorize(t *testing.T, gitHost *container, pub string) {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", gitHost.id, "sh", "-c", "cat > /etc/ssh/authorized_keys/git && chmod 0644 /etc/ssh/authorized_keys/git")
	cmd.Stdin = strings.NewReader(pub + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installing the key on the git host: %v\n%s", err, out)
	}
}

// TestSSHAccountOnRemote: a remote window sets up an SSH account's key on
// the host. The key pair lands in the host's ~/.ssh with mode 0600 next to
// its Host entry, and the public key the window shows is the host's file.
// Once the git host knows it, the connection verifies, ssh -T on the host
// reaches git-shell, and a clone over SSH works. The regenerate makes a new
// key the git host does not know yet.
func TestSSHAccountOnRemote(t *testing.T) {
	gitHost := startContainer(t, ctOpts{gitHost: true})
	c := startContainer(t, ctOpts{})
	seedSSHHost(c, gitHost)
	events := &eventLog{}
	conn := c.client(t, "client-ssh", true).connectEvents(t, events)
	defer conn.close()

	var first sshSetup
	callInto(t, conn.client, &first, "CredentialSetupSSH", "acct")
	if !first.OK || !strings.HasPrefix(first.SSHPublicKey, "ssh-ed25519 ") || first.SSHVerified {
		t.Fatalf("the first setup = %+v, want a new key the git host does not know", first)
	}
	if mode := c.sh("stat -c %a " + hostKey); mode != "600" {
		t.Errorf("the private key has mode %s, want 600", mode)
	}
	if mode := c.sh("stat -c %a " + testHome + "/.ssh"); mode != "700" {
		t.Errorf("the SSH folder has mode %s, want 700", mode)
	}
	if pub := c.sh("cat " + hostKey + ".pub"); pub != first.SSHPublicKey {
		t.Errorf("the window shows %q, the host's file has %q", first.SSHPublicKey, pub)
	}
	if n := c.sh("grep -c '^Host gitbox-acct$' " + testHome + "/.ssh/config"); n != "1" {
		t.Errorf("the host's SSH config has %s Host entries for the account, want 1", n)
	}

	authorize(t, gitHost, first.SSHPublicKey)
	var verified sshSetup
	callInto(t, conn.client, &verified, "CredentialSetupSSH", "acct")
	if !verified.OK || !verified.SSHVerified || verified.SSHPublicKey != first.SSHPublicKey {
		t.Fatalf("the setup after the key was added = %+v, want the same key verified", verified)
	}
	if greeting := c.sh("ssh -T gitbox-acct 2>&1 || true"); !strings.Contains(greeting, "git shell") {
		t.Errorf("ssh -T gitbox-acct on the host said %q, want git-shell's greeting", greeting)
	}

	callInto(t, conn.client, nil, "CloneRepo", "acct", "org/small", "")
	clone := testHome + "/git/acct/org/small"
	if e := doneError(t, events.last("clone:done")); e != "" || !c.exists(clone+"/.git") {
		t.Fatalf("the clone over SSH failed: %q", e)
	}
	if origin := c.sh("git -C " + clone + " remote get-url origin"); origin != "git@gitbox-acct:org/small.git" {
		t.Errorf("the clone's origin is %q, want the account's Host alias", origin)
	}

	var regenerated sshSetup
	callInto(t, conn.client, &regenerated, "CredentialRegenerateSSH", "acct")
	if !regenerated.OK || regenerated.SSHPublicKey == first.SSHPublicKey || regenerated.SSHVerified {
		t.Errorf("the regenerate = %+v, want a new key the git host does not know", regenerated)
	}
	if pub := c.sh("cat " + hostKey + ".pub"); pub != regenerated.SSHPublicKey {
		t.Errorf("after the regenerate the window shows %q, the host's file has %q", regenerated.SSHPublicKey, pub)
	}
	if n := c.sh("grep -c '^Host gitbox-acct$' " + testHome + "/.ssh/config"); n != "1" {
		t.Errorf("after the regenerate the SSH config has %s Host entries for the account, want 1", n)
	}
}

// privateKeyLines are the lines of the host's private key that carry key
// material.
func privateKeyLines(t *testing.T, c *container) []string {
	t.Helper()
	var lines []string
	for line := range strings.SplitSeq(c.sh("cat "+hostKey), "\n") {
		if line = strings.TrimSpace(line); len(line) >= 20 && !strings.HasPrefix(line, "-----") {
			lines = append(lines, line)
		}
	}
	if len(lines) < 3 {
		t.Fatalf("the host's private key has %d lines of key material, want a real key", len(lines))
	}
	return lines
}

// TestPrivateKeyNeverOnWire: over a real ssh session, with a tap on both
// directions for the whole flow (setup, verify, agent report, regenerate),
// no line of either private key crosses the wire, reaches the connection
// log or the server log. The public keys do come back (#244, I6).
func TestPrivateKeyNeverOnWire(t *testing.T) {
	gitHost := startContainer(t, ctOpts{gitHost: true})
	c := startContainer(t, ctOpts{})
	seedSSHHost(c, gitHost)
	s := c.client(t, "client-wire", true)
	sess, err := remote.Dial(testCtx(t), alias, s.opts)
	if err != nil {
		s.dump(t)
		t.Fatal(err)
	}
	defer sess.Close()
	toServer, fromServer := &lockedBuffer{}, &lockedBuffer{}
	conn := sess.Conn()
	client, err := rpc.Dial(io.TeeReader(conn, fromServer), tapWriter{conn, toServer}, rpc.ClientOptions{Version: it.version, Commit: it.commit, Log: s.log})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var first, second sshSetup
	callInto(t, client, &first, "CredentialSetupSSH", "acct")
	if !first.OK || first.SSHPublicKey == "" {
		t.Fatalf("the setup = %+v", first)
	}
	secrets := privateKeyLines(t, c)
	authorize(t, gitHost, first.SSHPublicKey)
	callInto(t, client, &sshSetup{}, "CredentialSetupSSH", "acct")
	callInto(t, client, &agentReport{}, "CredentialSSHAgentReport", "acct")
	callInto(t, client, &second, "CredentialRegenerateSSH", "acct")
	if !second.OK || second.SSHPublicKey == first.SSHPublicKey {
		t.Fatalf("the regenerate = %+v, want a new key", second)
	}
	secrets = append(secrets, privateKeyLines(t, c)...)

	serverLog := c.sh("cat " + testHome + "/.gitbox-server/logs/*.log 2>/dev/null || true")
	if !strings.Contains(serverLog, "serving client") || !strings.Contains(toServer.String(), "CredentialRegenerateSSH") {
		t.Fatalf("a tap saw nothing: wire %d bytes, server log %d bytes", len(toServer.String()), len(serverLog))
	}
	for where, text := range map[string]string{
		"the host's answers and events": fromServer.String(),
		"the window's requests":         toServer.String(),
		"the connection log":            strings.Join(s.log.Lines(), "\n"),
		"the server log":                serverLog,
	} {
		for _, line := range secrets {
			if strings.Contains(text, line) {
				t.Errorf("a line of a private key leaked into %s", where)
				break
			}
		}
	}
	// Positive control: the public keys did come back.
	for _, pub := range []string{first.SSHPublicKey, second.SSHPublicKey} {
		if fields := strings.Fields(pub); len(fields) < 2 || !strings.Contains(fromServer.String(), fields[1]) {
			t.Errorf("the public key %q is not in the host's answers", pub)
		}
	}
}

// TestAgentReport: with this side's agent forwarded (ForwardAgent in the
// window's ssh config) and a key in it that is not the account's, the host
// reports the forwarded agent, lists that key's fingerprint, finds no
// match, and sees that the Host entry pins the account's key. A connection
// that does not forward reports no agent.
func TestAgentReport(t *testing.T) {
	gitHost := startContainer(t, ctOpts{gitHost: true})
	c := startContainer(t, ctOpts{})
	seedSSHHost(c, gitHost)

	// This side's agent, with a key the host has never seen. Socket paths
	// must be short.
	dir, err := os.MkdirTemp(it.keyDir, "ag")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s")
	agent := exec.Command("ssh-agent", "-D", "-a", sock)
	if err := agent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Process.Kill(); _ = agent.Wait() })
	if !waitFor(5*time.Second, func() bool { _, err := os.Stat(sock); return err == nil }) {
		t.Fatal("ssh-agent did not create its socket")
	}
	key := filepath.Join(dir, "laptop")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "laptop", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v %s", err, out)
	}
	add := exec.Command("ssh-add", key)
	add.Env = append(os.Environ(), "SSH_AUTH_SOCK="+sock)
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("ssh-add: %v %s", err, out)
	}
	out, err := exec.Command("ssh-keygen", "-l", "-E", "sha256", "-f", key+".pub").Output()
	if err != nil || len(strings.Fields(string(out))) < 2 {
		t.Fatalf("ssh-keygen -l: %v %s", err, out)
	}
	laptop := strings.Fields(string(out))[1]

	forwarded := c.clientWith(t, "client-agent", true, "  ForwardAgent "+sock+"\n").connect(t)
	callInto(t, forwarded.client, &sshSetup{}, "CredentialSetupSSH", "acct")
	var r agentReport
	callInto(t, forwarded.client, &r, "CredentialSSHAgentReport", "acct")
	forwarded.close()
	if !r.AgentForwarded || r.Match || !r.IdentitiesOnly || !slices.Contains(r.AgentFingerprints, laptop) {
		t.Errorf("with the agent forwarded the report = %+v, want %s listed, no match and the key pinned", r, laptop)
	}
	if r.KeyFile != hostKey || !strings.HasPrefix(r.KeyFileFingerprint, "SHA256:") {
		t.Errorf("the report names key %q (%q), want %s", r.KeyFile, r.KeyFileFingerprint, hostKey)
	}
	if fp := strings.Fields(c.sh("ssh-keygen -l -E sha256 -f " + hostKey + ".pub")); len(fp) < 2 || fp[1] != r.KeyFileFingerprint {
		t.Errorf("the report's key fingerprint %q, the host's ssh-keygen says %v", r.KeyFileFingerprint, fp)
	}
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "laptop") {
		t.Errorf("the report carries an agent key's comment: %s", data)
	}

	plain := c.client(t, "client-noagent", true).connect(t)
	defer plain.close()
	var none agentReport
	callInto(t, plain.client, &none, "CredentialSSHAgentReport", "acct")
	if none.AgentForwarded || len(none.AgentFingerprints) != 0 || none.Match {
		t.Errorf("without forwarding the report = %+v, want no agent", none)
	}
}
