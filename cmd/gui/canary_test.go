package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/git/testutil"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// newCanary is a token no other text can hold by chance.
func newCanary(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return "gbx-canary-" + hex.EncodeToString(b[:])
}

// argvRecorder records the arguments of every git the process starts.
type argvRecorder struct {
	mu   sync.Mutex
	argv []string
}

func (r *argvRecorder) install(t *testing.T) {
	t.Helper()
	real := git.Default
	git.SetRunnerForTest(t, testutil.RunnerFunc(func(ctx context.Context, req git.Request) (git.Result, error) {
		r.mu.Lock()
		r.argv = append(r.argv, strings.Join(req.Args, " "))
		r.mu.Unlock()
		return real.Run(ctx, req)
	}))
}

func (r *argvRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.argv, "\n")
}

// TestTokenCanary is the value half of the secret canary, in process (#242;
// pkg/remote's TestTokenCanary runs it over ssh against a real host). A
// token typed in a remote window reaches the host's store once, in the
// CredentialStoreToken request, and never comes back: not in an answer, an
// event, a log line of either end, an error, a decoded result, or the
// command line of a git the host starts, through a clone, a fetch, a pull,
// a mirror setup, a type change, a refused GCM
// setup and the delete.
func TestTokenCanary(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TOKEN", "")
	t.Setenv("GITBOX_TOKEN_ACCT", "")
	t.Setenv("GITBOX_TOKEN_BAK", "bak-token-value")
	provider.SetDoerForTest(t, repoProvider{})
	side := repoHost(t, "http://"+ln.Addr().String())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(side.dir, "home", ".config"))
	argv := &argvRecorder{}
	argv.install(t)
	canary := newCanary(t)
	switchCanary := "switch-" + newCanary(t)

	link := serveRemote(t, side.path, nil)
	var results []any
	run := func(row contractRow) {
		t.Helper()
		got, _, err := runContractCall(t, row, link.Remote, link.Events)
		results = append(results, got)
		if err != nil {
			results = append(results, err.Error(), toUserError(err))
		}
		if err := link.Remote.LastError(); err != nil {
			results = append(results, err.Error(), toUserError(err))
		}
	}

	stored := link.Remote.CredentialStoreToken("acct", canary)
	if !stored.OK {
		t.Fatalf("CredentialStoreToken = %+v (%v)", stored, link.Remote.LastError())
	}
	results = append(results, stored)
	storeFile := credential.CredentialFilePath("acct")
	data, err := os.ReadFile(storeFile)
	if err != nil || strings.Count(string(data), canary) != 1 {
		t.Fatalf("the host's store file holds the token %d times (%v)", strings.Count(string(data), canary), err)
	}
	if runtime.GOOS != "windows" {
		for path, mode := range map[string]os.FileMode{storeFile: 0o600, filepath.Dir(storeFile): 0o700} {
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != mode {
				t.Errorf("%s: mode %v (%v), want %o", path, info.Mode().Perm(), err, mode)
			}
		}
	}

	for _, row := range []contractRow{
		{name: "CloneRepo", until: []string{"clone:done"}, call: func(b Backend) (any, error) { b.CloneRepo("acct", "org/fresh", ""); return nil, nil }},
		{name: "FetchRepo", until: []string{"fetch:done"}, call: func(b Backend) (any, error) { b.FetchRepo("acct", "org/repo"); return nil, nil }},
		{name: "PullRepo", until: []string{"pull:done"}, call: func(b Backend) (any, error) { b.PullRepo("acct", "org/repo"); return nil, nil }},
		{name: "SetupMirrorRepo", until: []string{"mirror:setup:done"}, call: func(b Backend) (any, error) { b.SetupMirrorRepo("mir", "org/repo"); return nil, nil }},
		{name: "ChangeCredentialType", call: func(b Backend) (any, error) { return b.ChangeCredentialType("acct", "token", switchCanary) }},
		{name: "ChangeCredentialType gcm", call: func(b Backend) (any, error) { return b.ChangeCredentialType("acct", "gcm", "") }},
		{name: "CredentialSetupGCM", call: func(b Backend) (any, error) { return b.CredentialSetupGCM("acct"), nil }},
		{name: "CredentialVerify", call: func(b Backend) (any, error) { return b.CredentialVerify("acct"), nil }},
	} {
		run(row)
	}

	// The proof must redact secrets planted in raw Git config as well as the
	// token already seen in the account's credential-store file.
	cfg := loadConfig(t, side.path)
	clone := status.RepoPath(cfg, "acct", "org/repo", cfg.Sources["acct"].Repos["org/repo"])
	gitConfig := filepath.Join(clone, ".git", "config")
	f, err := os.OpenFile(gitConfig, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(f, "\n[user]\n  email = %s\n[credential]\n  helper = !%s\n", canary, canary)
	if closeErr := f.Close(); err != nil || closeErr != nil {
		t.Fatalf("planting proof canary: %v / %v", err, closeErr)
	}
	proof, err := link.Remote.GetIdentityProof("acct", "org/repo")
	if err != nil || proof.Verdict != "unsafe" || len(proof.Overrides) < 2 {
		t.Fatalf("proof did not inspect the injected settings: verdict %q, reason %q, %d overrides, %v", proof.Verdict, proof.Reason, len(proof.Overrides), err)
	}
	results = append(results, proof)
	run(contractRow{name: "CredentialDelete", call: func(b Backend) (any, error) { return b.CredentialDelete("acct"), nil }})

	// Positive controls: the clone uses the credential store and mirror
	// setup still succeeds through the provider without pushing from Git.
	if !strings.Contains(argv.String(), "clone --progress") || !strings.Contains(argv.String(), "credential.helper=store") {
		t.Fatal("the clone did not use the account credential store")
	}
	if strings.Contains(argv.String(), "push --mirror") {
		t.Fatal("the provider mirror ran a local Git push")
	}
	done := link.Events.named("mirror:setup:done")
	if len(done) != 1 || !strings.Contains(asJSON(t, done[0].args), `"mirrored":true`) {
		t.Fatal("the server-side mirror was not set up")
	}
	if n := strings.Count(link.Received.String(), canary); n != 1 {
		t.Errorf("the token crossed the wire to the host %d times, want once", n)
	}
	if n := strings.Count(link.Received.String(), switchCanary); n != 1 {
		t.Errorf("the switch token crossed the wire to the host %d times, want once", n)
	}
	if _, err := os.Stat(storeFile); !os.IsNotExist(err) {
		t.Errorf("CredentialDelete left the store file: %v", err)
	}

	events := link.Events
	events.mu.Lock()
	var eventLines []string
	for _, e := range events.events {
		eventLines = append(eventLines, e.name+" "+asJSON(t, e.args))
	}
	events.mu.Unlock()
	eventText := strings.Join(eventLines, "\n")
	if !strings.Contains(eventText, "clone:done") {
		t.Fatalf("the recorded events miss the clone:\n%s", eventText)
	}
	decoded, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	for where, text := range map[string]string{
		"the host's answers and events": link.Sent.String(),
		"the window's events":           eventText,
		"the results and errors":        string(decoded),
		"the server log":                link.ServerLog.String(),
		"the client log":                link.ClientLog.String(),
		"the argv of git":               argv.String(),
	} {
		if strings.Contains(text, canary) || strings.Contains(text, switchCanary) {
			t.Errorf("the token leaked into %s", where)
		}
	}
}
