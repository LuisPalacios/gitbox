//go:build sshd_integration || remote_host

package remote_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// canaryHostConfig has two token accounts and one account awaiting setup,
// a repository to clone and a pull mirror on the fake forge. HOME
// and PORT stand for the host's home and the forge's port.
const canaryHostConfig = `{
  "version": 5,
  "global": {"roots": ["HOME/git"], "theme": "dark"},
  "accounts": {
    "acct": {"provider": "gitea", "url": "http://127.0.0.1:PORT", "username": "alice", "name": "Alice", "email": "a@example.invalid", "default_credential_type": "token"},
    "bak": {"provider": "gitea", "url": "http://127.0.0.1:PORT", "username": "bob", "name": "Bob", "email": "b@example.invalid", "default_credential_type": "token"},
    "switch": {"provider": "gitea", "url": "http://127.0.0.1:PORT", "username": "alice", "name": "Alice", "email": "a@example.invalid"}
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

// TestTokenCanary follows tokens typed in a remote window over a real SSH
// session. Each reaches the host once, in the designated request parameter
// for storing a token or changing credential types. Each lands in a 0600
// file in a 0700 folder. Neither token appears in any answer, event, log,
// or error from clone, fetch, pull, mirror setup, credential check, type
// change, refused GCM setup, or credential deletion, or on the
// command line of no process of the host (sampled every 50 ms) nor of the
// ssh this side starts. A refused GCM setup starts no git credential, and
// its refusal names the store GCM would use on the host's OS. It runs on a
// session the host serves itself and on one through the daemon (#247),
// whose events.log must not hold the token either.
func TestTokenCanary(t *testing.T) {
	for _, mode := range []string{"serve", "daemon"} {
		t.Run(mode, func(t *testing.T) { tokenCanary(t, mode == "daemon") })
	}
}

func tokenCanary(t *testing.T, daemon bool) {
	forgeBin := os.Getenv("GITBOX_IT_FAKEFORGE")
	if forgeBin == "" {
		t.Fatal("GITBOX_IT_FAKEFORGE is required; run scripts/remote-it.sh")
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	canary := "gbx-canary-" + hex.EncodeToString(raw[:])
	switchCanary := "gbx-switch-" + hex.EncodeToString(raw[:])

	c := startContainer(t, ctOpts{})
	// A port of its own: a real host may already listen on 3000.
	port := strconv.Itoa(20000 + int(raw[0])<<4 + int(raw[1]>>4))
	tmp, home := c.scratch(), c.home()
	forge, forgeLog, psLog := tmp+"/fakeforge", tmp+"/forge.log", tmp+"/ps.log"
	c.install(forgeBin, forge)
	c.writeFileAsTest(tmp+"/forge-tokens", "alice "+canary+"\nalice "+switchCanary+"\nbob bak-token-value\n")
	c.sh("mkdir -p " + tmp + "/forge/org && git init -q -b main " + tmp + "/w && git -C " + tmp + "/w -c user.name=t -c user.email=t@example.invalid commit -q --allow-empty -m one" +
		" && git clone -q --bare " + tmp + "/w " + tmp + "/forge/org/small.git")
	c.background("exec " + forge + " -listen 127.0.0.1:" + port + " -root " + tmp + "/forge -tokens " + tmp + "/forge-tokens > " + forgeLog + " 2>&1")
	if !waitFor(10*time.Second, func() bool { return c.ok("exec bash -c 'exec 3<>/dev/tcp/127.0.0.1/" + port + "'") }) {
		t.Fatalf("the fake forge did not start:\n%s", c.sh("cat "+forgeLog+" 2>/dev/null || true"))
	}
	c.sampleProcesses(psLog)
	cfgDir := home + "/.config/gitbox"
	hostConfig := strings.NewReplacer("HOME", home, "PORT", port).Replace(canaryHostConfig)
	c.sh("mkdir -p " + cfgDir + " " + home + "/git && echo " + base64.StdEncoding.EncodeToString([]byte(hostConfig)) + " | base64 -d > " + cfgDir + "/gitbox.json")

	// This side's ssh, with every command line it is given.
	s := c.client(t, "client-canary", true)
	s.opts.Daemon = daemon
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
		SecretParams: rpc.SecretParams{"CredentialStoreToken": {1}, "ChangeCredentialType": {2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if (client.Server().Daemon != nil) != daemon {
		t.Fatalf("the daemon served the session: %v, want %v (fallback %q)", client.Server().Daemon != nil, daemon, client.Server().Fallback)
	}

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
	if mode := c.mode(store); mode != "600" {
		t.Errorf("the store file has mode %s, want 600", mode)
	}
	if mode := c.mode(cfgDir + "/credentials"); mode != "700" {
		t.Errorf("the credentials folder has mode %s, want 700", mode)
	}
	if n := strings.Count(c.sh("cat "+store), canary); n != 1 {
		t.Errorf("the store file holds the token %d times, want once", n)
	}

	call("CloneRepo", "acct", "org/small", "")
	if !c.exists(home + "/git/acct/org/small/.git") {
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
		t.Errorf("the forge did not accept the stored token: %+v %v\nforge log:\n%s", status, err, c.sh("cat "+forgeLog))
	}
	if !ok(call("ChangeCredentialType", "switch", "token", switchCanary)) {
		t.Fatal("switch to token did not complete")
	}
	switchStore := cfgDir + "/credentials/switch"
	wantStore := "http://alice:" + switchCanary + "@127.0.0.1:" + port
	if c.mode(switchStore) != "600" || strings.TrimSpace(c.sh("cat "+switchStore)) != wantStore {
		t.Fatal("the verified replacement was not activated in the protected credential store")
	}
	persisted := c.sh("cat " + cfgDir + "/*.json")
	if !c.ok("for file in " + cfgDir + `/credentials/.transition-*; do test ! -e "$file" || exit 1; done`) {
		t.Error("the switch left a staged credential after activation")
	}
	call("ChangeCredentialType", "acct", "gcm", "")
	call("CredentialSetupGCM", "acct")
	store4GCM := map[string]string{"linux": "Secret Service", "darwin": "the macOS keychain"}[c.goos()]
	for _, a := range answers {
		if strings.HasPrefix(a, "CredentialSetupGCM failed") && !strings.Contains(a, store4GCM) {
			t.Errorf("the GCM refusal does not name the store: %s", a)
		}
	}
	for _, method := range []string{"ChangeCredentialType", "CredentialSetupGCM"} {
		if !strings.Contains(strings.Join(answers, "\n"), method+" failed") {
			t.Errorf("%s did not refuse GCM", method)
		}
	}
	if !ok(call("CredentialDelete", "acct")) || !ok(call("CredentialDelete", "switch")) {
		t.Errorf("the delete failed: %v", answers)
	}
	if c.exists(store) || c.exists(switchStore) {
		t.Error("CredentialDelete left the store file")
	}
	time.Sleep(200 * time.Millisecond) // a last sample of the host's processes

	processes := c.sh("cat " + psLog)
	serverLog := c.sh("cat " + s.root + "/logs/*.log 2>/dev/null || true")
	argv, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatalf("this side's ssh never ran through the wrapper: %v", err)
	}
	// Positive controls: the taps and the sampler saw the run.
	wire := toServer.String()
	assertSecretParam(t, wire, "CredentialStoreToken", []string{"acct", canary}, canary)
	assertSecretParam(t, wire, "ChangeCredentialType", []string{"switch", "token", switchCanary}, switchCanary)
	if !strings.Contains(fromServer.String(), "clone:done") || !strings.Contains(processes, "fakeforge -listen") || !strings.Contains(serverLog, "serving client") {
		t.Fatalf("a tap saw nothing: wire %d bytes, ps %d bytes, server log %d bytes", len(fromServer.String()), len(processes), len(serverLog))
	}
	journal := ""
	if daemon {
		journal = c.sh("cat " + s.root + "/*/state/" + remote.EventsLogName + " 2>/dev/null || true")
		if !strings.Contains(journal, "clone:done") {
			t.Fatalf("the daemon's events.log holds no clone:done (%d bytes)", len(journal))
		}
	}
	for where, text := range map[string]string{
		"the daemon's events.log":       journal,
		"the host's answers and events": fromServer.String(),
		"the answers and errors":        strings.Join(answers, "\n"),
		"the connection log":            strings.Join(s.log.Lines(), "\n"),
		"the server log":                serverLog,
		"the persisted config":          persisted + c.sh("cat "+cfgDir+"/*.json"),
		"the host's command lines":      processes,
		"this side's ssh command lines": string(argv),
	} {
		if strings.Contains(text, canary) || strings.Contains(text, switchCanary) {
			t.Errorf("the token leaked into %s", where)
		}
	}
	for _, gcm := range []string{"credential fill", "git-credential-manager"} {
		if strings.Contains(processes, gcm) {
			t.Errorf("a refused GCM setup ran %q on the host", gcm)
		}
	}
}

// assertSecretParam allows exactly one occurrence in the designated request,
// including a check for duplicates elsewhere in the same frame.
func assertSecretParam(t *testing.T, wire, method string, expected []string, secret string) {
	t.Helper()
	reader := bufio.NewReader(strings.NewReader(wire))
	matches := 0
	for {
		body, err := rpc.ReadFrame(reader)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal("could not decode client wire capture")
		}
		count := bytes.Count(body, []byte(secret))
		if count == 0 {
			continue
		}
		var request struct {
			Method string   `json:"method"`
			Params []string `json:"params"`
		}
		if count != 1 || json.Unmarshal(body, &request) != nil ||
			request.Method != method || !slices.Equal(request.Params, expected) {
			t.Fatalf("a secret crossed the wire outside the expected %s request parameter", method)
		}
		matches++
	}
	if matches != 1 {
		t.Errorf("the %s secret crossed the wire %d times, want once", method, matches)
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
