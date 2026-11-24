package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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

// domain is the side's config domain with its folder named <dir> and the
// time of a mirror setup left out.
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
	cfg := loadConfig(t, neutral)
	// A mirror setup stamps the time it ran, to the second: the sides run
	// it a moment apart.
	for mk, m := range cfg.Mirrors {
		for rk, r := range m.Repos {
			if r.LastSync != "" {
				r.LastSync = "<when>"
			}
			m.Repos[rk] = r
		}
		cfg.Mirrors[mk] = m
	}
	return config.DomainHash(cfg)
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

// checkoutTerminal follows the same completion boundary as the window.
// A successful transfer needs the authoritative row from the same operation.
// An error, skip, or no-op finishes with its done event.
func checkoutTerminal(name string) func([]recordedEvent) bool {
	return func(events []recordedEvent) bool {
		for i, done := range events {
			if done.name != name || len(done.args) != 1 {
				continue
			}
			b, err := json.Marshal(done.args[0])
			if err != nil {
				continue
			}
			var result struct {
				Error     string     `json:"error"`
				Skipped   skipReason `json:"skipped"`
				Unchanged bool       `json:"unchanged"`
			}
			if err := json.Unmarshal(b, &result); err != nil {
				continue
			}
			skip := result.Skipped.v
			if result.Error != "" || (skip != nil && skip != false) || result.Unchanged {
				return true
			}
			if slices.ContainsFunc(events[i+1:], func(e recordedEvent) bool {
				return e.name == "status:one" && e.meta.OperationID == done.meta.OperationID
			}) {
				return true
			}
		}
		return false
	}
}

func TestCheckoutContractWaitsForAuthoritativeStatus(t *testing.T) {
	terminal := checkoutTerminal("pull:done")
	done := func(payload any) recordedEvent {
		return recordedEvent{name: "pull:done", meta: EventMetadata{OperationID: "2"}, args: []any{payload}}
	}
	success := done(pullDoneDTO{Source: "acct", Repo: "org/repo"})
	unrelated := recordedEvent{name: "status:one", meta: EventMetadata{OperationID: "1"}}
	related := recordedEvent{name: "status:one", meta: EventMetadata{OperationID: "2"}}
	if terminal([]recordedEvent{success}) || terminal([]recordedEvent{success, unrelated}) ||
		!terminal([]recordedEvent{success, unrelated, related}) {
		t.Fatal("successful checkout must wait for its own status:one")
	}
	for _, payload := range []any{
		cloneDoneDTO{Error: "failed"},
		map[string]any{"skipped": "dirty"},
		pullDoneDTO{Unchanged: true},
	} {
		if !terminal([]recordedEvent{done(payload)}) {
			t.Errorf("completion waited for a status after %+v", payload)
		}
	}
}

// repoMutationRows are the steps of TestBackendContractRepoMutations, in
// order, failures included.
func repoMutationRows() []contractRow {
	none := func(err error) (any, error) { return nil, err }
	return []contractRow{
		{name: "CloneRepo", terminal: checkoutTerminal("clone:done"), call: func(b Backend) (any, error) { b.CloneRepo("acct", "org/fresh", ""); return nil, nil }},
		{name: "CloneRepo again", terminal: checkoutTerminal("clone:done"), call: func(b Backend) (any, error) { b.CloneRepo("acct", "org/fresh", ""); return nil, nil }},
		{name: "CloneRepo unknown repo", terminal: checkoutTerminal("clone:done"), call: func(b Backend) (any, error) { b.CloneRepo("acct", "org/none", ""); return nil, nil }},
		{name: "PullAll", terminal: func(events []recordedEvent) bool {
			return slices.ContainsFunc(events, func(e recordedEvent) bool { return e.name == "pull:alldone" })
		}, call: func(b Backend) (any, error) { b.PullAll("contract"); return nil, nil }},
		{name: "PullRepo", terminal: checkoutTerminal("pull:done"), call: func(b Backend) (any, error) { b.PullRepo("acct", "org/repo"); return nil, nil }},
		{name: "PullRepo unknown repo", terminal: checkoutTerminal("pull:done"), call: func(b Backend) (any, error) { b.PullRepo("acct", "org/none"); return nil, nil }},
		{name: "RepairRepoIdentity", call: func(b Backend) (any, error) { return none(b.RepairRepoIdentity("acct", "org/repo")) }},
		{name: "RepairRepoIdentity again", call: func(b Backend) (any, error) { return none(b.RepairRepoIdentity("acct", "org/repo")) }},
		{name: "ConfirmSweep", call: func(b Backend) (any, error) {
			return b.ConfirmSweep("acct", "org/repo", b.PreviewSweep("acct", "org/repo")), nil
		}},
		{name: "ConfirmSweep unknown repo", call: func(b Backend) (any, error) { return b.ConfirmSweep("acct", "org/none", SweepPreviewDTO{}), nil }},
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
		{name: "DeleteRepoFolder", call: func(b Backend) (any, error) { return none(b.DeleteRepoFolder("acct", "org/fresh")) }},
		{name: "DeleteRepoFolder missing", call: func(b Backend) (any, error) { return none(b.DeleteRepoFolder("acct", "org/fresh")) }},
		{name: "StopTrackingRepo", call: func(b Backend) (any, error) { return none(b.StopTrackingRepo("acct", "org/repo")) }},
		{name: "Reconcile after stop tracking", call: func(b Backend) (any, error) { return b.Reconcile(), nil }},
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

	for _, host := range []struct {
		name string
		side repoSide
	}{{"local", left}, {"remote", right}} {
		t.Run("confirm the previewed branches/"+host.name, func(t *testing.T) {
			s := host.side
			s.use(t)
			cfg := loadConfig(t, s.path)
			path := status.RepoPath(cfg, "acct", "org/repo", cfg.Sources["acct"].Repos["org/repo"])
			run := func(args ...string) {
				t.Helper()
				if _, err := git.Run(t.Context(), git.Request{Dir: path, Args: args}); err != nil {
					t.Fatal(err)
				}
			}
			run("branch", "a")
			run("branch", "b")
			preview := s.b.PreviewSweep("acct", "org/repo")
			if preview.ID == "" || !slices.Equal(preview.Merged, []string{"a", "b"}) {
				t.Fatalf("preview: %+v", preview)
			}
			run("branch", "c")
			altered := preview
			altered.Merged = []string{"a", "b", "c"}
			if d := s.b.ConfirmSweep("acct", "org/repo", altered); len(d.Deleted) != 0 || len(d.Skipped) != 3 {
				t.Fatalf("altered preview: %+v", d)
			}
			if d := s.b.ConfirmSweep("acct", "org/repo", preview); d.Error != "" || len(d.Skipped) != 0 || !slices.Equal(d.Deleted, []string{"a", "b"}) {
				t.Fatalf("confirm: %+v", d)
			}
			run("show-ref", "--verify", "refs/heads/c")
			next := s.b.PreviewSweep("acct", "org/repo")
			if next.ID == "" {
				t.Fatalf("missing fresh preview: %+v", next)
			}
			s.b.PreviewSweep("acct", "org/missing")
			if d := s.b.ConfirmSweep("acct", "org/repo", next); !slices.Equal(d.Skipped, []string{"c"}) {
				t.Fatalf("expired preview: %+v", d)
			}
			run("show-ref", "--verify", "refs/heads/c")
		})
	}

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
			if row.name == "PullAll" {
				// Hosts drain independently, so compare the events without
				// assuming a shared schedule for the two servers.
				slices.SortFunc(wantEvents, func(a, b contractEvent) int {
					return strings.Compare(left.neutral(t, a), left.neutral(t, b))
				})
				slices.SortFunc(gotEvents, func(a, b contractEvent) int {
					return strings.Compare(right.neutral(t, a), right.neutral(t, b))
				})
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
			t.Errorf("%s: DeleteRepoFolder kept org/fresh", s.dir)
		}
		fresh := status.RepoPath(cfg, "acct", "org/fresh", config.Repo{})
		if _, err := os.Stat(fresh); !os.IsNotExist(err) {
			t.Errorf("%s: DeleteRepoFolder did not remove the folder: %v", s.dir, err)
		}
		repo := status.RepoPath(cfg, "acct", "org/repo", config.Repo{})
		if _, ok := cfg.Sources["acct"].Repos["org/repo"]; ok || !cfg.IsIgnoredClone(repo) || !git.IsRepo(repo) {
			t.Errorf("%s: StopTrackingRepo did not preserve and ignore the folder", s.dir)
		}
	}
}
