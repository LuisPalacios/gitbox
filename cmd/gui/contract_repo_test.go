package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// repoSide is one host of TestBackendContractRepoMutations: a folder of
// its own with the config, the clone root, the upstream repositories that
// its account's URL leads to (through an insteadOf of its global git
// config) and its home.
type repoSide struct {
	b      Backend
	events *recordingSink
	dir    string
	path   string
}

// use points HOME and the global git config at the side. Each side's work
// runs to its done event before the other side starts, so the git
// processes of one never see the other's environment.
func (s repoSide) use(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", filepath.Join(s.dir, "home"))
	t.Setenv("USERPROFILE", filepath.Join(s.dir, "home"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(s.dir, "home", ".gitconfig"))
}

// neutral is v as canonical JSON with the side's folder named <dir>, so two
// sides compare.
func (s repoSide) neutral(t *testing.T, v any) string {
	t.Helper()
	return s.unfold(canonJSON(t, v))
}

// unfold names the side's folder <dir> in text, in every spelling it takes
// there: as is, escaped in JSON, with forward slashes (a file:// URL) and,
// on Windows, with no volume (a repo key derived from such a URL).
func (s repoSide) unfold(text string) string {
	slashed := filepath.ToSlash(s.dir)
	for _, form := range []string{strings.ReplaceAll(s.dir, `\`, `\\`), s.dir, slashed, strings.TrimPrefix(slashed, filepath.VolumeName(s.dir)+"/")} {
		text = strings.ReplaceAll(text, form, "<dir>")
	}
	return text
}

// domain is the side's config domain with its folder named <dir>.
func (s repoSide) domain(t *testing.T) config.Hash {
	t.Helper()
	data, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	neutral := filepath.Join(t.TempDir(), "gitbox.json")
	// The same absolute folder for both sides, as it reads inside JSON.
	same := strings.ReplaceAll(filepath.Join(filepath.VolumeName(s.dir)+string(filepath.Separator), "dir"), `\`, `\\`)
	if err := os.WriteFile(neutral, []byte(strings.ReplaceAll(s.unfold(string(data)), "<dir>", same)), 0o600); err != nil {
		t.Fatal(err)
	}
	return config.DomainHash(loadConfig(t, neutral))
}

// repoHost builds a side: upstreams org/repo, org/fresh and org/orphan with
// one commit each, the clone of org/repo, a stray clone of org/orphan that
// the config does not track, and the account acct at serverURL, whose
// expected origins lead to the upstreams.
func repoHost(t *testing.T, serverURL string) repoSide {
	t.Helper()
	s := repoSide{dir: t.TempDir(), events: &recordingSink{}}
	s.path = filepath.Join(s.dir, "gitbox.json")
	home := filepath.Join(s.dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	s.use(t)
	run := func(dir string, args ...string) {
		t.Helper()
		if _, err := git.Default.Run(t.Context(), git.Request{Dir: dir, Args: args}); err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
	}
	up := filepath.Join(s.dir, "up")
	work := filepath.Join(s.dir, "work")
	for _, name := range []string{"repo", "fresh", "orphan"} {
		w := filepath.Join(work, name)
		run(s.dir, "init", "-q", "-b", "main", w)
		run(w, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "-q", "--allow-empty", "-m", "first")
		run(s.dir, "clone", "-q", "--bare", w, filepath.Join(up, "org", name+".git"))
	}
	base := strings.Replace(serverURL, "://", "://alice@", 1) + "/"
	run(s.dir, "config", "--global", "url.file://"+filepath.ToSlash(up)+"/.insteadOf", base)

	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Roots: []string{filepath.Join(s.dir, "git")}},
		Accounts: map[string]config.Account{
			"acct": {Provider: "gitea", URL: serverURL, Username: "alice", Name: "Alice", Email: "a@e", DefaultCredentialType: "token"},
			"bak":  {Provider: "gitea", URL: serverURL, Username: "bob", Name: "Bob", Email: "b@e", DefaultCredentialType: "token"},
		},
		Sources: map[string]config.Source{
			"acct": {Account: "acct", Repos: map[string]config.Repo{"org/repo": {}, "org/fresh": {}}},
			"bak":  {Account: "bak"},
		},
		Mirrors: map[string]config.Mirror{"mir": {AccountSrc: "acct", AccountDst: "bak",
			Repos: map[string]config.MirrorRepo{"org/repo": {Direction: "push", Origin: "src"}}}},
	}
	clone := status.RepoPath(cfg, "acct", "org/repo", cfg.Sources["acct"].Repos["org/repo"])
	run(s.dir, "clone", "-q", base+"org/repo.git", clone)
	run(s.dir, "clone", "-q", base+"org/orphan.git", filepath.Join(filepath.Dir(clone), "orphan"))
	if err := config.Save(cfg, s.path); err != nil {
		t.Fatal(err)
	}
	return s
}

// repoProvider answers the provider calls of the table: no repository
// exists yet, and every create succeeds.
type repoProvider struct{}

func (repoProvider) Do(req *http.Request) (*http.Response, error) {
	code, body := http.StatusOK, `{"login":"alice","username":"alice"}`
	switch {
	case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/repos/"):
		code, body = http.StatusNotFound, `{"message":"not found"}`
	case req.Method == http.MethodGet && strings.HasSuffix(req.URL.Path, "/repos"):
		body = `[]`
	case req.Method == http.MethodPost:
		code, body = http.StatusCreated, `{"full_name":"alice/made"}`
	}
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// repoMutationRows are the steps of TestBackendContractRepoMutations, in
// order, failures included. Each waits for the events in until.
func repoMutationRows() []contractRow {
	none := func(err error) (any, error) { return nil, err }
	return []contractRow{
		{name: "CloneRepo", until: []string{"clone:done"}, call: func(b Backend) (any, error) { b.CloneRepo("acct", "org/fresh", ""); return nil, nil }},
		{name: "CloneRepo again", until: []string{"clone:done"}, call: func(b Backend) (any, error) { b.CloneRepo("acct", "org/fresh", ""); return nil, nil }},
		{name: "CloneRepo unknown repo", until: []string{"clone:done"}, call: func(b Backend) (any, error) { b.CloneRepo("acct", "org/none", ""); return nil, nil }},
		{name: "PullRepo", until: []string{"pull:done"}, call: func(b Backend) (any, error) { b.PullRepo("acct", "org/repo"); return nil, nil }},
		{name: "PullRepo unknown repo", until: []string{"pull:done"}, call: func(b Backend) (any, error) { b.PullRepo("acct", "org/none"); return nil, nil }},
		{name: "ConfirmSweep", call: func(b Backend) (any, error) { return b.ConfirmSweep("acct", "org/repo"), nil }},
		{name: "ConfirmSweep unknown repo", call: func(b Backend) (any, error) { return b.ConfirmSweep("acct", "org/none"), nil }},
		{name: "Reconcile", call: func(b Backend) (any, error) { return b.Reconcile(), nil }},
		{name: "AdoptClones", call: func(b Backend) (any, error) { return b.AdoptClones(nil), nil }},
		{name: "AdoptClones unknown account", call: func(b Backend) (any, error) {
			return b.AdoptClones([]AdoptRequestDTO{{Path: "/nowhere/clone", Account: "nobody"}}), nil
		}},
		{name: "SetupMirrorRepo", until: []string{"mirror:setup:done"}, call: func(b Backend) (any, error) { b.SetupMirrorRepo("mir", "org/repo"); return nil, nil }},
		{name: "SetupMirrorRepo unknown group", until: []string{"mirror:setup:done"}, call: func(b Backend) (any, error) { b.SetupMirrorRepo("none", "org/repo"); return nil, nil }},
		{name: "ApplyDiscoveredMirrors", call: func(b Backend) (any, error) { return none(b.ApplyDiscoveredMirrors()) }},
		{name: "CreateNewRepo", call: func(b Backend) (any, error) { return none(b.CreateNewRepo("acct", "", "made", "", true, false, "")) }},
		{name: "CreateNewRepo unknown account", call: func(b Backend) (any, error) { return none(b.CreateNewRepo("nobody", "", "made", "", true, false, "")) }},
		{name: "MoveRepo unknown destination", until: []string{"move:done"}, call: func(b Backend) (any, error) {
			b.MoveRepo(MoveRequestDTO{SourceSourceKey: "acct", SourceRepoKey: "org/repo", DestAccountKey: "nobody", DestOwner: "alice", DestRepoName: "moved"})
			return nil, nil
		}},
		{name: "MoveRepo unknown source", until: []string{"move:done"}, call: func(b Backend) (any, error) {
			b.MoveRepo(MoveRequestDTO{SourceSourceKey: "none", SourceRepoKey: "org/repo", DestAccountKey: "acct", DestOwner: "alice", DestRepoName: "moved"})
			return nil, nil
		}},
		{name: "DeleteRepo", call: func(b Backend) (any, error) { return none(b.DeleteRepo("acct", "org/fresh")) }},
		{name: "DeleteRepo missing", call: func(b Backend) (any, error) { return none(b.DeleteRepo("acct", "org/fresh")) }},
	}
}

// repoMutationRowMethods names the methods repoMutationRows calls, for the
// coverage check of TestBackendContract.
func repoMutationRowMethods() map[string]bool {
	m := make(map[string]bool)
	for _, row := range repoMutationRows() {
		m[strings.Fields(row.name)[0]] = true
	}
	return m
}

// TestBackendContractRepoMutations runs the repository mutations (#295) in
// one order against two hosts built alike: the local backend, as a server
// exposes it, and a RemoteBackend connected to a server on the other. Each
// clones from its own upstreams, so their folders differ and are compared
// as <dir>. After every step the result, the error, the events and the
// domain on disk must match.
func TestBackendContractRepoMutations(t *testing.T) {
	// Every server counts as reachable: the account's URL is this listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	serverURL := "http://" + ln.Addr().String()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GITBOX_TOKEN_ACCT", "contract-test-token-value")
	t.Setenv("GITBOX_TOKEN_BAK", "contract-test-token-value")
	provider.SetDoerForTest(t, repoProvider{})

	left := repoHost(t, serverURL)
	local := newServerApp(left.path)
	local.events = left.events
	local.be().Init(context.Background())
	t.Cleanup(local.closeLocal)
	left.b = Backend(hostBackend{localBackend{local}})

	right := repoHost(t, serverURL)
	link := serveRemote(t, right.path, nil)
	right.b, right.events = link.Remote, link.Events

	for _, row := range repoMutationRows() {
		t.Run(row.name, func(t *testing.T) {
			left.use(t)
			want, wantEvents, wantErr := runContractCall(t, row, left.b, left.events)
			right.use(t)
			got, gotEvents, gotErr := runContractCall(t, row, right.b, right.events)
			if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && toUserError(wantErr).Code != toUserError(gotErr).Code) {
				t.Fatalf("errors differ: local %v, remote %v", wantErr, gotErr)
			}
			if w, g := left.neutral(t, want), right.neutral(t, got); w != g {
				t.Errorf("results differ:\n local  %s\n remote %s", w, g)
			}
			if w, g := left.neutral(t, wantEvents), right.neutral(t, gotEvents); w != g {
				t.Errorf("events differ:\n local  %s\n remote %s", w, g)
			}
			if left.domain(t) != right.domain(t) {
				t.Errorf("the domains on disk differ after %s", row.name)
			}
		})
	}
	// The clone the table made, and then deleted, is gone on both sides;
	// the one it started from is still there.
	for _, s := range []repoSide{left, right} {
		cfg := loadConfig(t, s.path)
		if _, ok := cfg.Sources["acct"].Repos["org/fresh"]; ok {
			t.Errorf("%s: DeleteRepo kept org/fresh", s.dir)
		}
		if !git.IsRepo(status.RepoPath(cfg, "acct", "org/repo", cfg.Sources["acct"].Repos["org/repo"])) {
			t.Errorf("%s: the clone of org/repo is gone", s.dir)
		}
	}
}
