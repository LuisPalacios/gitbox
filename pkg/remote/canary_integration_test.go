//go:build sshd_integration

package remote_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// canaryHostConfig has two token accounts on the fake forge of the
// container, a repository to clone and a pull mirror from the first to the
// second.
const canaryHostConfig = `{
  "version": 5,
  "global": {"roots": ["` + testHome + `/git"], "theme": "dark"},
  "accounts": {
    "acct": {"provider": "gitea", "url": "http://127.0.0.1:3000", "username": "alice", "name": "Alice", "email": "a@example.invalid", "default_credential_type": "token"},
    "bak": {"provider": "gitea", "url": "http://127.0.0.1:3000", "username": "bob", "name": "Bob", "email": "b@example.invalid", "default_credential_type": "token"}
  },
  "sources": {
    "acct": {"account": "acct", "repos": {"org/small": {}}},
    "bak": {"account": "bak", "repos": {}}
  },
  "mirrors": {
    "mir": {"account_src": "acct", "account_dst": "bak", "repos": {"org/small": {"direction": "pull", "origin": "src"}}}
  }
}
`

// lockedBuffer is a bytes.Buffer that several goroutines write.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// tapWriter copies what the client writes to the server.
type tapWriter struct {
	w   io.WriteCloser
	tap *lockedBuffer
}

func (t tapWriter) Write(p []byte) (int, error) {
	_, _ = t.tap.Write(p)
	return t.w.Write(p)
}

func (t tapWriter) Close() error { return t.w.Close() }

// writeFileAsTest writes content to path as the test user, through stdin,
// so it is on no command line.
func (c *container) writeFileAsTest(path, content string) {
	c.t.Helper()
	cmd := exec.Command("docker", "exec", "-i", "-u", "test", c.id, "sh", "-c", "umask 077 && cat > "+path)
	cmd.Stdin = strings.NewReader(content)
	if out, err := cmd.CombinedOutput(); err != nil {
		c.t.Fatalf("writing %s: %v\n%s", path, err, out)
	}
}

// TestTokenCanary follows a token typed in a remote window over a real ssh
// session to a real host (#242). It reaches the host once, in the
// CredentialStoreToken request, lands in a 0600 file in a 0700 folder, and
// never comes back: through a clone, a fetch, a pull, a mirror setup, a
// credential check, a type change, a refused GCM setup and the delete, it
// is in no answer or event, in neither end's log, in no error, and on the
// command line of no process of the host (sampled every 50 ms) nor of the
// ssh this side starts. A refused GCM setup starts no git credential.
func TestTokenCanary(t *testing.T) {
	forgeBin := os.Getenv("GITBOX_IT_FAKEFORGE")
	if forgeBin == "" {
		t.Fatal("GITBOX_IT_FAKEFORGE is required; run scripts/remote-it.sh")
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	canary := "gbx-canary-" + hex.EncodeToString(raw[:])

	c := startContainer(t, ctOpts{})
	docker(t, "cp", forgeBin, c.id+":/usr/local/bin/fakeforge")
	c.writeFileAsTest("/tmp/forge-tokens", "alice "+canary+"\nbob bak-token-value\n")
	c.sh("mkdir -p /tmp/forge/org && git init -q -b main /tmp/w && git -C /tmp/w -c user.name=t -c user.email=t@example.invalid commit -q --allow-empty -m one" +
		" && git clone -q --bare /tmp/w /tmp/forge/org/small.git")
	docker(t, "exec", "-d", "-u", "test", c.id, "sh", "-c", "exec /usr/local/bin/fakeforge -listen 127.0.0.1:3000 -root /tmp/forge -tokens /tmp/forge-tokens > /tmp/forge.log 2>&1")
	if !waitFor(10*time.Second, func() bool {
		return exec.Command("docker", "exec", c.id, "bash", "-c", "exec 3<>/dev/tcp/127.0.0.1/3000").Run() == nil
	}) {
		t.Fatalf("the fake forge did not start:\n%s", c.sh("cat /tmp/forge.log 2>/dev/null || true"))
	}
	// The sampler runs as root, so it sees every process of the host.
	docker(t, "exec", "-d", c.id, "sh", "-c", "while :; do ps -eww -o args >> /tmp/ps.log; sleep 0.05; done")
	cfgDir := testHome + "/.config/gitbox"
	c.sh("mkdir -p " + cfgDir + " " + testHome + "/git && echo " + base64.StdEncoding.EncodeToString([]byte(canaryHostConfig)) + " | base64 -d > " + cfgDir + "/gitbox.json")

	// This side's ssh, with every command line it is given.
	s := c.client(t, "client-canary", true)
	argvLog := filepath.Join(t.TempDir(), "ssh-argv.log")
	sshReal, err := exec.LookPath("ssh")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + argvLog + "'\nexec '" + sshReal + "' \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	remote.SetSSHPath(&s.opts, wrapper)

	sess, err := remote.Dial(testCtx(t), alias, s.opts)
	if err != nil {
		s.dump(t)
		t.Fatal(err)
	}
	defer sess.Close()
	toServer, fromServer := &lockedBuffer{}, &lockedBuffer{}
	conn := sess.Conn()
	events := &eventLog{}
	client, err := rpc.Dial(io.TeeReader(conn, fromServer), tapWriter{conn, toServer}, rpc.ClientOptions{
		Version: it.version, Commit: it.commit, Log: s.log, OnEvent: events.record,
		SecretParams: rpc.SecretParams{"CredentialStoreToken": {1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var answers []string
	call := func(method string, params ...any) json.RawMessage {
		t.Helper()
		ctx, cancel := context.WithTimeout(testCtx(t), 90*time.Second)
		defer cancel()
		var out json.RawMessage
		err := client.Call(ctx, method, params, &out, rpc.WithRequestID(newCanaryRequestID(t)))
		answers = append(answers, method+": "+string(out))
		if err != nil {
			answers = append(answers, method+" failed: "+err.Error())
			if method != "CredentialSetupGCM" && !(method == "ChangeCredentialType" && params[1] == "gcm") {
				t.Errorf("%s: %v", method, err)
			}
		}
		return out
	}
	ok := func(raw json.RawMessage) bool {
		var r struct {
			OK bool `json:"ok"`
		}
		return json.Unmarshal(raw, &r) == nil && r.OK
	}

	if !ok(call("CredentialStoreToken", "acct", canary)) || !ok(call("CredentialStoreToken", "bak", "bak-token-value")) {
		t.Fatalf("storing the tokens failed: %v", answers)
	}
	store := cfgDir + "/credentials/acct"
	if mode := c.sh("stat -c %a " + store); mode != "600" {
		t.Errorf("the store file has mode %s, want 600", mode)
	}
	if mode := c.sh("stat -c %a " + cfgDir + "/credentials"); mode != "700" {
		t.Errorf("the credentials folder has mode %s, want 700", mode)
	}
	if n := strings.Count(c.sh("cat "+store), canary); n != 1 {
		t.Errorf("the store file holds the token %d times, want once", n)
	}

	call("CloneRepo", "acct", "org/small", "")
	if !c.exists(testHome + "/git/acct/org/small/.git") {
		t.Fatalf("the clone did not happen: %s", events.last("clone:done"))
	}
	call("FetchRepo", "acct", "org/small")
	if !waitFor(30*time.Second, func() bool { return events.last("fetch:done") != nil }) {
		t.Error("the fetch never finished")
	}
	call("PullRepo", "acct", "org/small")
	call("SetupMirrorRepo", "mir", "org/small")
	if e := doneError(t, events.last("mirror:setup:done")); e != "" {
		t.Errorf("the mirror setup failed: %s", e)
	}
	var status struct {
		Status     string `json:"status"`
		PrimaryMsg string `json:"primaryMsg"`
	}
	if err := json.Unmarshal(call("CredentialVerify", "acct"), &status); err != nil || status.Status != "ok" {
		t.Errorf("the forge did not accept the stored token: %+v %v\nforge log:\n%s", status, err, c.sh("cat /tmp/forge.log"))
	}
	call("ChangeCredentialType", "acct", "token")
	call("ChangeCredentialType", "acct", "gcm")
	call("CredentialSetupGCM", "acct")
	for _, a := range answers {
		if strings.HasPrefix(a, "CredentialSetupGCM failed") && !strings.Contains(a, "Secret Service") {
			t.Errorf("the GCM refusal does not name the store: %s", a)
		}
	}
	if !strings.Contains(strings.Join(answers, "\n"), "CredentialSetupGCM failed") {
		t.Errorf("CredentialSetupGCM was not refused: %v", answers)
	}
	if !ok(call("CredentialDelete", "acct")) {
		t.Errorf("the delete failed: %v", answers)
	}
	if c.exists(store) {
		t.Error("CredentialDelete left the store file")
	}
	time.Sleep(200 * time.Millisecond) // a last sample of the host's processes

	psLog := c.sh("cat /tmp/ps.log")
	serverLog := c.sh("cat " + testHome + "/.gitbox-server/logs/*.log 2>/dev/null || true")
	argv, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatalf("this side's ssh never ran through the wrapper: %v", err)
	}
	// Positive controls: the taps and the sampler saw the run.
	if n := strings.Count(toServer.String(), canary); n != 1 {
		t.Errorf("the token crossed the wire to the host %d times, want once", n)
	}
	if !strings.Contains(fromServer.String(), "clone:done") || !strings.Contains(psLog, "fakeforge -listen") || !strings.Contains(serverLog, "serving client") {
		t.Fatalf("a tap saw nothing: wire %d bytes, ps %d bytes, server log %d bytes", len(fromServer.String()), len(psLog), len(serverLog))
	}
	for where, text := range map[string]string{
		"the host's answers and events": fromServer.String(),
		"the answers and errors":        strings.Join(answers, "\n"),
		"the connection log":            strings.Join(s.log.Lines(), "\n"),
		"the server log":                serverLog,
		"the host's command lines":      psLog,
		"this side's ssh command lines": string(argv),
	} {
		if strings.Contains(text, canary) {
			t.Errorf("the token leaked into %s", where)
		}
	}
	for _, gcm := range []string{"credential fill", "git-credential-manager"} {
		if strings.Contains(psLog, gcm) {
			t.Errorf("a refused GCM setup ran %q on the host", gcm)
		}
	}
}

// newCanaryRequestID is a fresh request id: every call is a new request.
func newCanaryRequestID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
