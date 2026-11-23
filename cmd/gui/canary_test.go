package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/git/testutil"
	"github.com/LuisPalacios/gitbox/pkg/provider"
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

// argvRecorder records the arguments of every git the process starts, and
// answers a push --mirror itself: its destination exists only in the test.
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
		if slices.Contains(req.Args, "--mirror") {
			return git.Result{}, nil
		}
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
// a mirror setup, a move that pushes a mirror, a type change, a refused GCM
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

	none := func(err error) (any, error) { return nil, err }
	for _, row := range []contractRow{
		{name: "CloneRepo", until: []string{"clone:done"}, call: func(b Backend) (any, error) { b.CloneRepo("acct", "org/fresh", ""); return nil, nil }},
		{name: "FetchRepo", until: []string{"fetch:done"}, call: func(b Backend) (any, error) { b.FetchRepo("acct", "org/repo"); return nil, nil }},
		{name: "PullRepo", until: []string{"pull:done"}, call: func(b Backend) (any, error) { b.PullRepo("acct", "org/repo"); return nil, nil }},
		{name: "SetupMirrorRepo", until: []string{"mirror:setup:done"}, call: func(b Backend) (any, error) { b.SetupMirrorRepo("mir", "org/repo"); return nil, nil }},
		{name: "MoveRepo", until: []string{"move:done"}, call: func(b Backend) (any, error) {
			b.MoveRepo(MoveRequestDTO{SourceSourceKey: "acct", SourceRepoKey: "org/fresh", DestAccountKey: "acct", DestOwner: "alice", DestRepoName: "moved"})
			return nil, nil
		}},
		{name: "ChangeCredentialType", call: func(b Backend) (any, error) { return none(b.ChangeCredentialType("acct", "token")) }},
		{name: "ChangeCredentialType gcm", call: func(b Backend) (any, error) { return none(b.ChangeCredentialType("acct", "gcm")) }},
		{name: "CredentialSetupGCM", call: func(b Backend) (any, error) { return b.CredentialSetupGCM("acct"), nil }},
		{name: "CredentialVerify", call: func(b Backend) (any, error) { return b.CredentialVerify("acct"), nil }},
		{name: "CredentialDelete", call: func(b Backend) (any, error) { return b.CredentialDelete("acct"), nil }},
	} {
		run(row)
	}

	// Positive controls: the work really ran with the token.
	if !strings.Contains(argv.String(), "push --mirror") || !strings.Contains(argv.String(), "clone") {
		t.Fatalf("the flows did not run git:\n%s", argv)
	}
	if done := link.Events.named("move:done"); len(done) != 1 || strings.Contains(asJSON(t, done[0].args), `"error"`) && !strings.Contains(asJSON(t, done[0].args), `"error":""`) {
		t.Errorf("the move did not finish cleanly: %s", asJSON(t, done))
	}
	if n := strings.Count(link.Received.String(), canary); n != 1 {
		t.Errorf("the token crossed the wire to the host %d times, want once", n)
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
		if strings.Contains(text, canary) {
			t.Errorf("the token leaked into %s", where)
		}
	}
}
