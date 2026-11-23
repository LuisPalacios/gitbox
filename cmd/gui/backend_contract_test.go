package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// wireTap copies everything written through it, so a test can look at what
// really crossed the wire.
type wireTap struct {
	w  io.Writer
	mu sync.Mutex
	b  bytes.Buffer
}

func (t *wireTap) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.b.Write(p)
	t.mu.Unlock()
	return t.w.Write(p)
}

func (t *wireTap) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.b.String()
}

// remoteLink is a RemoteBackend talking to a real server over in-memory
// pipes, the way GitboxApp talks to a gitbox-server over its stdin and
// stdout.
type remoteLink struct {
	Remote *RemoteBackend
	// Host is the server's App: the same code gitbox-server runs.
	Host   *App
	Events *recordingSink
	// Sent is everything the server wrote to the client, Received
	// everything the client wrote to the server (#242).
	Sent     *wireTap
	Received *wireTap
	// ServerLog and ClientLog are what each end logged.
	ServerLog *wireTap
	ClientLog *wireTap
	served    chan error
}

// closingTap is a wireTap in front of a pipe the client closes.
type closingTap struct {
	*wireTap
	c io.Closer
}

func (t closingTap) Close() error { return t.c.Close() }

// serveRemote starts a server on the config at path and connects a
// RemoteBackend to it. extra adds handlers that only a test needs.
func serveRemote(t *testing.T, path string, extra map[string]rpc.Handler) *remoteLink {
	t.Helper()
	host := newServerApp(path)
	serverLog, clientLog := &wireTap{w: io.Discard}, &wireTap{w: io.Discard}
	srv := newRPCServer(host, serverLog)
	maps.Copy(srv.Handlers, extra)
	ctx, cancel := context.WithCancel(context.Background())
	host.be().Init(ctx)

	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	l := &remoteLink{Host: host, Events: &recordingSink{}, Sent: &wireTap{w: s2cW}, Received: &wireTap{w: c2sW},
		ServerLog: serverLog, ClientLog: clientLog, served: make(chan error, 1)}
	go func() {
		err := srv.Serve(ctx, c2sR, l.Sent)
		host.be().Close()
		_ = s2cW.Close()
		_ = c2sR.Close()
		l.served <- err
	}()
	remote, err := dialRemoteBackend(s2cR, closingTap{l.Received, c2sW}, func() EventSink { return l.Events }, clientLog)
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}
	l.Remote = remote
	t.Cleanup(func() {
		remote.Close()
		cancel()
	})
	return l
}

// contractConfig is a host config with a real clone on disk, client
// preferences and an account, so every call of the table has something to
// say.
func contractConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	// A token a test stores, or a clone repair writes, stays in the test.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	clone := filepath.Join(t.TempDir(), "clone")
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main", clone},
		{"-C", clone, "-c", "user.name=T", "-c", "user.email=t@e", "commit", "--quiet", "--allow-empty", "-m", "first"},
	} {
		if out, err := exec.Command(git.GitBin(), args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return seedConfig(t, "gitbox.json", func(c *config.Config) {
		c.Global.Theme = "dark"
		c.Global.Editors = []config.EditorEntry{{Name: "VS Code", Command: "/usr/local/bin/code"}}
		c.Global.Remotes = []config.RemoteEntry{{Alias: "build-box"}}
		src := c.Sources["acct"]
		src.Repos = map[string]config.Repo{"org/repo": {CloneFolder: clone}}
		src.RepoOrder = []string{"org/repo"}
		c.Sources["acct"] = src
	})
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// canonJSON is v as JSON with every object's keys sorted, so a struct and the
// map it becomes after crossing the wire compare equal.
func canonJSON(t *testing.T, v any) string {
	t.Helper()
	var generic any
	if err := json.Unmarshal([]byte(asJSON(t, v)), &generic); err != nil {
		t.Fatal(err)
	}
	return asJSON(t, generic)
}

// contractHost extends contractConfig with what the rest of the table needs:
// a second account, a mirror between the two, and a saved backup. Provider
// requests go to fakeProvider, so nothing leaves the process.
func contractHost(t *testing.T) string {
	t.Helper()
	path := contractConfig(t)
	cfg := loadConfig(t, path)
	cfg.Accounts["other"] = config.Account{Provider: "github", URL: "https://github.com", Username: "bob", Name: "Bob", Email: "b@e"}
	cfg.Sources["other"] = config.Source{Account: "other"}
	cfg.Mirrors = map[string]config.Mirror{"mir": {AccountSrc: "acct", AccountDst: "other",
		Repos: map[string]config.MirrorRepo{"org/repo": {Direction: "push", Origin: "src"}}}}
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	// A clone the config does not know, for ScanFolderForClones.
	orphan := filepath.Join(filepath.Dir(cfg.Sources["acct"].Repos["org/repo"].CloneFolder), "orphan")
	if out, err := exec.Command(git.GitBin(), "init", "--quiet", orphan).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	t.Setenv("GITBOX_TOKEN_ACCT", "contract-test-token-value")
	t.Setenv("GITBOX_TOKEN_OTHER", "contract-test-token-value")
	provider.SetDoerForTest(t, fakeProvider{})
	return path
}

// fakeProvider answers every provider request with a small canned GitHub
// response.
type fakeProvider struct{}

func (fakeProvider) Do(req *http.Request) (*http.Response, error) {
	body := `{}`
	switch p := req.URL.Path; {
	case p == "/user":
		body = `{"login":"alice","name":"Alice"}`
	case p == "/user/orgs":
		body = `[{"login":"org"}]`
	case p == "/user/repos" || strings.HasPrefix(p, "/orgs/"):
		body = `[]`
		if req.URL.Query().Get("page") == "1" || req.URL.Query().Get("page") == "" {
			body = `[{"full_name":"org/repo","name":"repo","owner":{"login":"org"},"private":true,"clone_url":"https://github.com/org/repo.git","description":"the repo"}]`
		}
	case strings.HasSuffix(p, "/pulls"):
		body = `[]`
	case strings.HasPrefix(p, "/search/"):
		body = `{"total_count":0,"items":[]}`
	case strings.HasPrefix(p, "/repos/"):
		body = `{"full_name":"org/repo","default_branch":"main","private":true}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

// contractRow is one call of TestBackendContract. until names the events
// that end the call's work; empty says why an empty result proves enough;
// concurrent names the events the call's work emits from goroutines of
// their own, whose place among the others is not guaranteed (see
// compareContractEvents); project replaces the events by what of them is
// deterministic.
type contractRow struct {
	name       string
	call       func(Backend) (any, error)
	until      []string
	terminal   func(recordedEvent) bool
	empty      string
	concurrent []string
	project    func(t *testing.T, events []contractEvent) any
}

// The fixture has one configured clone. Require its actual status followed by
// the final batch, both tagged with the requested pass and loaded revision.
func startupStatusContract(revision uint64) contractRow {
	const passID = 37
	return contractRow{
		name: "BeginStatus",
		call: func(b Backend) (any, error) { return nil, b.BeginStatus(passID) },
		terminal: func(e recordedEvent) bool {
			if e.name != "status:batch" || len(e.args) != 1 {
				return false
			}
			b, ok := e.args[0].(StatusBatch)
			return ok && b.Done && b.PassID == passID && b.Revision == revision
		},
		project: func(t *testing.T, events []contractEvent) any {
			t.Helper()
			if len(events) != 2 {
				t.Fatalf("expected a data batch and its completion, got %+v", events)
			}
			for i, e := range events {
				if e.Name != "status:batch" || len(e.Args) != 1 {
					t.Fatalf("unexpected status event %+v", e)
				}
				b, ok := e.Args[0].(StatusBatch)
				if !ok || b.PassID != passID || b.Revision != revision || b.Done != (i == 1) {
					t.Fatalf("invalid status batch %d: %+v", i, e)
				}
				if i == 0 && (len(b.Items) != 1 || b.Items[0].Source != "acct" || b.Items[0].Repo != "org/repo" || b.Items[0].State == "") {
					t.Fatalf("expected the configured clone's status, got %+v", b.Items)
				}
				if b.Done && len(b.Items) != 0 {
					t.Fatalf("completion carries unexpected rows: %+v", b.Items)
				}
			}
			return events
		},
	}
}

func contractRows(clone string, revision uint64) []contractRow {
	move := MoveRequestDTO{SourceSourceKey: "acct", SourceRepoKey: "org/repo", DestAccountKey: "other", DestOwner: "bob", DestRepoName: "repo"}
	return []contractRow{
		{name: "GetConfig", call: func(b Backend) (any, error) { return b.GetConfig() }},
		{name: "GetStartupSnapshot", call: func(b Backend) (any, error) { return b.GetStartupSnapshot() }},
		{name: "ReloadConfig", call: func(b Backend) (any, error) { return b.ReloadConfig() }},
		{name: "GetCollapsed", call: func(b Backend) (any, error) { return b.GetCollapsed(), nil }, empty: "nothing is collapsed on the host"},
		{name: "GetOS", call: func(b Backend) (any, error) { return b.GetOS(), nil }},
		{name: "GetConfigPath", call: func(b Backend) (any, error) { return b.GetConfigPath(), nil }},
		{name: "IsFirstRun", call: func(b Backend) (any, error) { return b.IsFirstRun(), nil }},
		{name: "GetConfigLoadError", call: func(b Backend) (any, error) { return b.GetConfigLoadError(), nil }, empty: "the host's config loads"},
		{name: "ListConfigBackups", call: func(b Backend) (any, error) { return b.ListConfigBackups(), nil }},
		{name: "PreviewRoot", call: func(b Backend) (any, error) { return b.PreviewRoot(filepath.Dir(clone)) }},
		{name: "PreviewClonePath", call: func(b Backend) (any, error) { return b.PreviewClonePath("acct", "org/repo") }},
		{name: "ValidateRemotePath", call: func(b Backend) (any, error) { return b.ValidateRemotePath(os.TempDir()), nil }},
		startupStatusContract(revision),
		{name: "GetAllStatus", call: func(b Backend) (any, error) { return b.GetAllStatus(), nil }},
		// The status pass and the upstream probe each publish the statuses.
		{name: "RefreshStatus", call: func(b Backend) (any, error) { b.RefreshStatus(); return nil, nil }, until: []string{"status:updated"},
			concurrent: []string{"status:updated"}},
		// The fetch ends with a RefreshStatus: see that row.
		{name: "FetchRepo", call: func(b Backend) (any, error) { b.FetchRepo("acct", "org/repo"); return nil, nil }, until: []string{"fetch:done"},
			concurrent: []string{"status:updated"}},
		// The credential check and the upstream probe run beside the fetches
		// (fetchCycle); everything ends before fetch:alldone.
		{name: "FetchAllRepos", call: func(b Backend) (any, error) { return nil, b.FetchAllRepos() }, until: []string{"fetch:alldone"},
			concurrent: []string{"credentials:updated", "status:updated"}},
		{name: "GetRepoDetail", call: func(b Backend) (any, error) { return b.GetRepoDetail("acct", "org/repo"), nil }},
		{name: "CredentialVerify", call: func(b Backend) (any, error) { return b.CredentialVerify("acct"), nil }},
		{name: "PreviewSweep", call: func(b Backend) (any, error) { return b.PreviewSweep("acct", "org/repo"), nil }},
		{name: "SuggestAccountKey", call: func(b Backend) (any, error) { return b.SuggestAccountKey("github", "alice"), nil }},
		{name: "SuggestAccountKey for a taken key", call: func(b Backend) (any, error) { return b.SuggestAccountKey("github", "acct"), nil }},
		{name: "AccountDeletionImpact", call: func(b Backend) (any, error) { return b.AccountDeletionImpact("acct"), nil }},
		{name: "CheckGlobalIdentity", call: func(b Backend) (any, error) { return b.CheckGlobalIdentity(), nil }},
		{name: "IsGlobalGCMConfigNeeded", call: func(b Backend) (any, error) { return b.IsGlobalGCMConfigNeeded(), nil }},
		{name: "CheckGlobalGCMConfig", call: func(b Backend) (any, error) { return b.CheckGlobalGCMConfig(), nil }},
		{name: "CheckGlobalGitignore", call: func(b Backend) (any, error) { return b.CheckGlobalGitignore() }},
		{name: "RecommendedGlobalGitignoreBody", call: func(b Backend) (any, error) { return b.RecommendedGlobalGitignoreBody(), nil }},
		{name: "GetCheckGlobalGitignore", call: func(b Backend) (any, error) { return b.GetCheckGlobalGitignore(), nil }},
		{name: "GetTokenGuide", call: func(b Backend) (any, error) { return b.GetTokenGuide("acct"), nil }},
		{name: "Discover", call: func(b Backend) (any, error) { b.Discover("acct"); return nil, nil }, until: []string{"discover:done"}},
		{name: "RunFleetCheck mirror status", call: func(b Backend) (any, error) { return nil, b.RunFleetCheck(context.Background(), taskMirrors) }, until: []string{"mirror:status"}},
		{name: "GetMirrorStatus", call: func(b Backend) (any, error) { b.GetMirrorStatus("mir"); return nil, nil }, until: []string{"mirror:status"}},
		{name: "ListRemoteRepos", call: func(b Backend) (any, error) { return b.ListRemoteRepos("acct"), nil }},
		{name: "ListAccountOrgs", call: func(b Backend) (any, error) { return b.ListAccountOrgs("acct") }},
		{name: "DiscoverMirrors", call: func(b Backend) (any, error) { b.DiscoverMirrors(); return nil, nil }, until: []string{"mirror:discover:done"},
			// Which account of a pair comes first follows map order, so
			// only the number of pairs and repos found is stable.
			project: func(t *testing.T, events []contractEvent) any {
				var done struct {
					Results []struct{ Discovered []any }
				}
				for _, e := range events {
					if e.Name == "mirror:discover:done" && len(e.Args) == 1 {
						if err := json.Unmarshal([]byte(asJSON(t, e.Args[0])), &done); err != nil {
							t.Fatal(err)
						}
					}
				}
				found := 0
				for _, r := range done.Results {
					found += len(r.Discovered)
				}
				return []int{len(done.Results), found}
			}},
		{name: "CheckMirrorCredentials", call: func(b Backend) (any, error) { return b.CheckMirrorCredentials("acct"), nil }},
		{name: "DoctorRun", call: func(b Backend) (any, error) { return b.DoctorRun(), nil }},
		{name: "DoctorPrecheck", call: func(b Backend) (any, error) { return b.DoctorPrecheck("token"), nil }},
		{name: "CheckMoveReadiness", call: func(b Backend) (any, error) { return b.CheckMoveReadiness("acct", "other", false), nil }},
		{name: "ListMoveDestinationOwners", call: func(b Backend) (any, error) { return b.ListMoveDestinationOwners("other"), nil }},
		{name: "PreflightMove", call: func(b Backend) (any, error) { return b.PreflightMove(move), nil }},
		{name: "GetPRSettings", call: func(b Backend) (any, error) { return b.GetPRSettings(), nil }},
		{name: "GetPRsForRepo", call: func(b Backend) (any, error) { return b.GetPRsForRepo("acct", "org/repo"), nil }},
		{name: "RefreshAllPRs", call: func(b Backend) (any, error) { b.RefreshAllPRs(); return nil, nil }, until: []string{"pr:refreshed"}},
		{name: "GetNestedScanDepth", call: func(b Backend) (any, error) { return b.GetNestedScanDepth(), nil }},
		{name: "RepoNesting", call: func(b Backend) (any, error) { return b.RepoNesting(), nil }, empty: "the host has one clone, so nothing nests"},
		{name: "RepoWorkspaceFiles", call: func(b Backend) (any, error) { return b.RepoWorkspaceFiles("acct", "org/repo"), nil }, empty: "the clone holds no workspace file"},
		{name: "ScanFolderForClones", call: func(b Backend) (any, error) { return b.ScanFolderForClones(filepath.Dir(clone)), nil }},
		{name: "ListRoots", call: func(b Backend) (any, error) { return b.ListRoots(), nil }},
		{name: "OpenAccountInBrowser", call: func(b Backend) (any, error) { return b.OpenAccountInBrowser("acct") }},
		{name: "OpenRepoInBrowser", call: func(b Backend) (any, error) { return b.OpenRepoInBrowser("acct", "org/repo") }},
		{name: "OpenRepoInBrowser for an unknown repo", call: func(b Backend) (any, error) { return b.OpenRepoInBrowser("acct", "no/such") }},
		{name: "AccountFolder", call: func(b Backend) (any, error) { return b.AccountFolder("acct") }},
		{name: "AccountFolder for an unknown account", call: func(b Backend) (any, error) { return b.AccountFolder("nobody") }},
		// The harness detection (#243) runs on the host and reports what it
		// found with harnesses:updated; TestBackendContract stubs the
		// lookup. The refresh comes first: it finds the stubbed harness and
		// emits, then the sync finds nothing new.
		{name: "RefreshAIHarnesses", call: func(b Backend) (any, error) { b.RefreshAIHarnesses(); return nil, nil }, until: []string{"harnesses:updated"}},
		{name: "SyncAIHarnesses", call: func(b Backend) (any, error) { return b.SyncAIHarnesses(), nil }},
	}
}

// TestBackendContract runs one table of calls against two backends: the
// local one, as a server exposes it, and a RemoteBackend connected to a
// server on the same config. Every call must give the same result and the
// same error, and emit the same events. The table covers every served
// method; P5 extends it with the mutations.
func TestBackendContract(t *testing.T) {
	if len(knownAIHarnesses) == 0 {
		t.Fatal("the harness catalog is empty")
	}
	stubHarnessLookup(t, map[string]string{knownAIHarnesses[0].Command: "/opt/bin/" + knownAIHarnesses[0].Command})
	path := contractHost(t)
	clone := loadConfig(t, path).Sources["acct"].Repos["org/repo"].CloneFolder

	localSink := &recordingSink{}
	// Both sides run read-only: this table holds the reads, and a read
	// must never save nor repair a clone. The mutations have their tables
	// (contract_mutations_test.go, contract_repo_test.go).
	local := &App{cfgPath: path, events: localSink, upstreamGone: map[string]bool{}, readOnly: true}
	local.be().Init(context.Background())
	t.Cleanup(local.closeLocal)
	direct := Backend(hostBackend{localBackend{local}})

	link := serveRemote(t, path, nil)
	link.Host.readOnly = true

	covered := make(map[string]bool)
	for _, tt := range contractRows(clone, local.configRevision) {
		covered[strings.Fields(tt.name)[0]] = true
		t.Run(tt.name, func(t *testing.T) {
			want, wantEvents, wantErr := runContractCall(t, tt, direct, localSink)
			got, gotEvents, gotErr := runContractCall(t, tt, link.Remote, link.Events)
			if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && userMsg(wantErr) != userMsg(gotErr)) {
				t.Fatalf("errors differ: local %v, remote %v", wantErr, gotErr)
			}
			if w, g := asJSON(t, want), asJSON(t, got); w != g {
				t.Errorf("results differ:\n local  %s\n remote %s", w, g)
			}
			for side, events := range map[string][]contractEvent{"local": wantEvents, "remote": gotEvents} {
				if problem := eventOrderProblem(events); problem != "" {
					t.Errorf("%s: %s", side, problem)
				}
			}
			if tt.project != nil {
				if w, g := canonJSON(t, tt.project(t, wantEvents)), canonJSON(t, tt.project(t, gotEvents)); w != g {
					t.Errorf("events differ:\n local  %s\n remote %s", w, g)
				}
			} else if diff := contractEventsDiffer(t, tt.concurrent, wantEvents, gotEvents); diff != "" {
				t.Error(diff)
			}
			w := asJSON(t, want)
			if nothing := w == "null" || w == `""` || w == "[]" || w == "{}" || w == "0"; nothing && wantErr == nil && len(wantEvents) == 0 && tt.empty == "" {
				t.Errorf("the call returned nothing (%s) and emitted nothing: the comparison proves nothing", w)
			}
			if err := link.Remote.LastError(); err != nil && wantErr == nil {
				t.Errorf("the remote call recorded a failure: %v", err)
			}
		})
	}
	for name := range mutationRowMethods() {
		covered[name] = true
	}
	for name := range repoMutationRowMethods() {
		covered[name] = true
	}
	// A host refuses GCM where a desktop runs it (#242), so the two sides
	// cannot match; TestHostRefusesGCM covers it.
	covered["CredentialSetupGCM"] = true
	for _, name := range servedMethods {
		if !covered[name] {
			t.Errorf("%s is served but neither contract table calls it", name)
		}
	}

	// What crossed the wire held no client preference of the host, and no
	// token.
	for _, leaked := range []string{"/usr/local/bin/code", "build-box", `"theme"`, "contract-test-token-value"} {
		if strings.Contains(link.Sent.String(), leaked) {
			t.Errorf("the server sent %q", leaked)
		}
	}
}

// compareContractEvents demands the same events on both sides: the same
// names, payloads and counts. Their order is compared only where the work
// guarantees it. Events of one repository (a payload with "source" and
// "repo") or of one account ("accountKey") form a chain of their own, and
// the rest form one more; inside each chain, the events not named in
// concurrent keep their order. Work that runs per repository or per
// account in parallel therefore interleaves freely, and so do the events
// that independent goroutines emit. A concurrent event counts once per
// payload: the status sweeps a fetch requests can coalesce, so how many
// identical status:updated arrive depends on timing, not on the backend.
// It returns what differs, or "".
func contractEventsDiffer(t *testing.T, concurrent []string, want, got []contractEvent) string {
	t.Helper()
	if w, g := eventMultiset(t, concurrent, want), eventMultiset(t, concurrent, got); w != g {
		return fmt.Sprintf("events differ:\n local  %s\n remote %s", canonJSON(t, want), canonJSON(t, got))
	}
	if w, g := orderedChains(t, concurrent, want), orderedChains(t, concurrent, got); w != g {
		return fmt.Sprintf("events arrive in another order:\n local  %s\n remote %s", w, g)
	}
	return ""
}

func eventMultiset(t *testing.T, concurrent []string, events []contractEvent) string {
	t.Helper()
	items := make([]string, 0, len(events))
	seen := map[string]bool{}
	for _, e := range events {
		item := canonJSON(t, e)
		if slices.Contains(concurrent, e.Name) {
			if seen[item] {
				continue
			}
			seen[item] = true
		}
		items = append(items, item)
	}
	slices.Sort(items)
	return strings.Join(items, "\n")
}

func orderedChains(t *testing.T, concurrent []string, events []contractEvent) string {
	t.Helper()
	chains := map[string][]string{}
	for _, e := range events {
		if slices.Contains(concurrent, e.Name) {
			continue
		}
		key := eventChain(e)
		chains[key] = append(chains[key], canonJSON(t, e))
	}
	return canonJSON(t, chains)
}

// eventChain is the repository or account an event is about, or "" for an
// event about the whole run.
func eventChain(e contractEvent) string {
	if len(e.Args) != 1 {
		return ""
	}
	payload, ok := e.Args[0].(map[string]any)
	if !ok {
		data, err := json.Marshal(e.Args[0])
		if err != nil || json.Unmarshal(data, &payload) != nil {
			return ""
		}
	}
	if repo, ok := payload["repo"].(string); ok {
		source, _ := payload["source"].(string)
		return "repo:" + source + "/" + repo
	}
	if account, ok := payload["accountKey"].(string); ok {
		return "account:" + account
	}
	return ""
}

// eventOrderProblem checks the orderings every run guarantees, on either
// side: fetch:allstart opens a Fetch all and fetch:alldone closes it, and
// each repository's fetch:start comes before its fetch:done. It returns the
// first one broken, or "".
func eventOrderProblem(events []contractEvent) string {
	started := map[string]bool{}
	for i, e := range events {
		switch e.Name {
		case "fetch:allstart":
			if i != 0 {
				return fmt.Sprintf("fetch:allstart is event %d, want the first", i)
			}
		case "fetch:alldone":
			if i != len(events)-1 {
				return fmt.Sprintf("fetch:alldone is event %d of %d, want the last", i, len(events))
			}
		case "fetch:start":
			started[eventChain(e)] = true
		case "fetch:done":
			key := eventChain(e)
			if !started[key] && slices.ContainsFunc(events, func(x contractEvent) bool { return x.Name == "fetch:start" && eventChain(x) == key }) {
				return fmt.Sprintf("fetch:done of %s came before its fetch:start", key)
			}
		}
	}
	return ""
}

// The comparison of TestBackendContract lets concurrent events and the
// chains of different repositories interleave, and catches everything else.
func TestContractEventComparison(t *testing.T) {
	ev := func(name string, payload any) contractEvent {
		if payload == nil {
			return contractEvent{name, []any{}}
		}
		return contractEvent{name, []any{payload}}
	}
	repo := func(r string) map[string]any { return map[string]any{"source": "acct", "repo": r} }
	creds := ev("credentials:updated", map[string]any{"acct": map[string]any{"status": "ok"}})
	status := ev("status:updated", []any{map[string]any{"repo": "a"}})
	run := []contractEvent{ev("fetch:allstart", nil), ev("fetch:start", repo("a")), creds, ev("fetch:done", repo("a")),
		ev("fetch:start", repo("b")), ev("fetch:done", repo("b")), status, ev("fetch:alldone", map[string]any{})}
	concurrent := []string{"credentials:updated", "status:updated"}
	if p := eventOrderProblem(run); p != "" {
		t.Fatalf("a valid run: %s", p)
	}
	moved := func(edit func([]contractEvent) []contractEvent) []contractEvent { return edit(slices.Clone(run)) }

	same := map[string][]contractEvent{
		// credentials:updated after status:updated, as on the Windows lane.
		"concurrent event moved":   {run[0], run[1], run[3], run[4], run[5], status, creds, run[7]},
		"repositories interleaved": {run[0], run[1], run[4], run[2], run[3], run[5], run[6], run[7]},
		// Two status sweeps coalesced into one, as on the Windows lane.
		"concurrent event coalesced": {run[0], run[1], run[2], status, run[3], run[4], run[5], run[6], run[7]},
	}
	for name, other := range same {
		if p := eventOrderProblem(other); p != "" {
			t.Errorf("%s: %s", name, p)
		}
		if d := contractEventsDiffer(t, concurrent, run, other); d != "" {
			t.Errorf("%s was reported: %s", name, d)
		}
	}

	differ := map[string][]contractEvent{
		"an event missing": run[:len(run)-1],
		"an event twice":   append(slices.Clone(run), run[3]),
		"a concurrent payload missing": moved(func(e []contractEvent) []contractEvent {
			return slices.DeleteFunc(e, func(x contractEvent) bool { return x.Name == "credentials:updated" })
		}),
		"another payload": moved(func(e []contractEvent) []contractEvent {
			e[3] = ev("fetch:done", map[string]any{"source": "acct", "repo": "a", "error": "x"})
			return e
		}),
		"a repo's events swapped": moved(func(e []contractEvent) []contractEvent { e[1], e[3] = e[3], e[1]; return e }),
	}
	for name, other := range differ {
		if d := contractEventsDiffer(t, concurrent, run, other); d == "" {
			t.Errorf("%s was not reported", name)
		}
	}
	// Without concurrent, a moved event is an order change.
	if d := contractEventsDiffer(t, nil, run, same["concurrent event moved"]); d == "" {
		t.Error("a moved event outside concurrent was not reported")
	}
	for name, broken := range map[string][]contractEvent{
		"fetch:done before fetch:start": {run[0], run[3], run[1], run[7]},
		"fetch:allstart not first":      {run[1], run[0], run[3], run[7]},
		"fetch:alldone not last":        {run[0], run[7], run[1], run[3]},
	} {
		if eventOrderProblem(broken) == "" {
			t.Errorf("%s was not reported", name)
		}
	}
}

type contractEvent struct {
	Name string `json:"name"`
	Args []any  `json:"args"`
}

// runContractCall makes one call and returns its result, its error and the
// events it emitted, once every event of row.until has arrived and the
// stream has been quiet for a moment, or its explicit terminal event arrives.
func runContractCall(t *testing.T, row contractRow, b Backend, sink *recordingSink) (any, []contractEvent, error) {
	t.Helper()
	sink.mu.Lock()
	start := len(sink.events)
	sink.mu.Unlock()
	result, err := row.call(b)
	since := func() []recordedEvent {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		return slices.Clone(sink.events[start:])
	}
	if row.terminal != nil {
		// Emit records and signals under the same lock. This also handles a
		// synchronous delivery before the method returns or before we subscribe.
		deadline := time.After(20 * time.Second)
		for {
			sink.mu.Lock()
			done := slices.ContainsFunc(sink.events[start:], row.terminal)
			if !done && sink.changed == nil {
				sink.changed = make(chan struct{})
			}
			changed := sink.changed
			sink.mu.Unlock()
			if done {
				break
			}
			select {
			case <-changed:
			case <-deadline:
				t.Fatalf("%s: missing terminal event, got %+v", row.name, since())
			}
		}
	} else {
		deadline := time.Now().Add(20 * time.Second)
		for {
			events := since()
			done := true
			for _, name := range row.until {
				if !slices.ContainsFunc(events, func(e recordedEvent) bool { return e.name == name }) {
					done = false
				}
			}
			if done {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: waited for %v, got %d events", row.name, row.until, len(events))
			}
			time.Sleep(10 * time.Millisecond)
		}
		// Let stragglers of the same call arrive.
		for n := -1; n != len(since()); {
			n = len(since())
			time.Sleep(60 * time.Millisecond)
		}
	}
	out := []contractEvent{}
	for _, e := range since() {
		// The wire keeps only the latest progress of an operation per
		// interval (pkg/rpc), so progress events are not compared.
		if strings.HasSuffix(e.name, ":progress") {
			continue
		}
		args := e.args
		if args == nil {
			args = []any{}
		}
		out = append(out, contractEvent{e.name, args})
	}
	return result, out, err
}

// The server's events reach the window with the same name and payload, and
// a name this build does not know is dropped.
func TestRemoteEventsAreRelayed(t *testing.T) {
	link := serveRemote(t, contractConfig(t), nil)
	statuses := []StatusResult{{Source: "acct", Repo: "org/repo", State: "clean", Branch: "main"}}
	link.Host.sink().Emit("status:updated", statuses)
	link.Host.sink().Emit("made:up", "by a hostile server")
	link.Host.sink().Emit("fetch:done", map[string]any{"source": "acct", "repo": "org/repo", "error": "git fetch: offline"})

	deadline := time.Now().Add(5 * time.Second)
	for len(link.Events.named("fetch:done")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := link.Events.named("made:up"); len(got) != 0 {
		t.Errorf("an unknown event reached the window: %v", got)
	}
	got := link.Events.named("status:updated")
	if len(got) != 1 || canonJSON(t, got[0].args) != canonJSON(t, []any{statuses}) {
		t.Errorf("status:updated arrived %d times with a payload that differs from %s", len(got), canonJSON(t, []any{statuses}))
	}
	if len(link.Events.named("fetch:done")) != 1 {
		t.Errorf("fetch:done was not relayed")
	}
}

// An error crosses the wire only after the boundary scrubbed it, and the
// client scrubs it again: nothing of the token fixture is ever sent or shown.
func TestRemoteErrorsCarryNoSecrets(t *testing.T) {
	const token = "ghp_CANARYCANARYCANARYCANARY0123456789"
	link := serveRemote(t, contractConfig(t), map[string]rpc.Handler{
		"TestFail": rpc.Func(func() error {
			return fmt.Errorf("fetching https://alice:%s@git.example.org/x.git: Authorization: Bearer %s", token, token)
		}),
		"TestGitFail": rpc.Func(func() (string, error) {
			return "", fmt.Errorf("refresh: %w", &git.GitError{Op: "fetch", Code: git.CodeAuthFailed, Hint: "error.git.auth_failed", Detail: "exit 128"})
		}),
	})

	err := link.Remote.invoke("TestFail", nil)
	if err == nil {
		t.Fatal("the call must fail")
	}
	for _, text := range []string{err.Error(), userMsg(err), fmt.Sprintf("%+v", toUserError(err)), link.Sent.String()} {
		if strings.Contains(text, token) || strings.Contains(text, "alice:") {
			t.Errorf("the token or the URL credentials survived: %s", text)
		}
	}

	// A typed error keeps its code and hint across the wire.
	err = link.Remote.invoke("TestGitFail", new(string))
	ue := toUserError(err)
	if ue.Code != "git.auth_failed" || ue.Hint != "error.git.auth_failed" || ue.Detail != "refresh: git fetch: auth_failed (exit 128)" {
		t.Errorf("the typed error arrived as %+v", ue)
	}
}

// A hostile server cannot put anything it likes in the code and the hint.
func TestToUserErrorDistrustsTheServer(t *testing.T) {
	err := &rpc.Error{Code: rpc.CodeApplication}
	err.Data.Code = "git.offline\"><script>"
	err.Data.Hint = strings.Repeat("h", 200)
	err.Data.Detail = "see https://bob:s3cr3tValue0123456789abcdef@evil.example/\x1b[31m"
	ue := toUserError(err)
	if ue.Code != "" || ue.Hint != "" {
		t.Errorf("a malformed code or hint was kept: %+v", ue)
	}
	if strings.Contains(ue.Detail, "s3cr3t") || strings.Contains(ue.Detail, "\x1b") {
		t.Errorf("the detail was not scrubbed: %q", ue.Detail)
	}
}

// A method the server does not serve yet fails as not_implemented_remote,
// and one with no error to return answers its zero value and keeps the
// failure.
func TestRemoteMethodOutsideTheClosedList(t *testing.T) {
	path := contractConfig(t)
	link := serveRemote(t, path, nil)
	// The SSH key methods wait for #244.
	before := loadConfig(t, path).Accounts["acct"]
	if res := link.Remote.CredentialSetupSSH("acct"); !reflect.DeepEqual(res, CredentialSetupResult{}) {
		t.Errorf("CredentialSetupSSH = %+v, want the zero value for a method that is not served", res)
	}
	if after := loadConfig(t, path).Accounts["acct"]; !reflect.DeepEqual(after, before) {
		t.Errorf("the refused mutation changed the host: account %+v, was %+v", after, before)
	}
	if err := link.Remote.LastError(); toUserError(err).Code != "remote.not_implemented_remote" {
		t.Errorf("LastError = %v, want the failure of the call that had no error to return", err)
	}
}

// recordedCall is one request as a fake server saw it.
type recordedCall struct {
	Method    string
	Params    string
	RequestID string
}

// answerEverything is a server that accepts any method and answers null. It
// returns the requests it saw.
func answerEverything(t *testing.T) (in io.Reader, out io.WriteCloser, calls func() []recordedCall) {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	var mu sync.Mutex
	var seen []recordedCall
	go func() {
		defer s2cW.Close()
		reader := bufio.NewReader(c2sR)
		// The handshake, by hand: magic, hello, the client's answer, verdict.
		_, _ = s2cW.Write(rpc.Magic)
		hello, _ := json.Marshal(rpc.Hello{Version: version, Commit: commit, OS: "testos", Arch: "testarch", PID: 1})
		_ = rpc.WriteFrame(s2cW, hello)
		if _, err := rpc.ReadFrame(reader); err != nil {
			return
		}
		_ = rpc.WriteFrame(s2cW, []byte(`{"ok":true}`))
		for {
			body, err := rpc.ReadFrame(reader)
			if err != nil {
				return
			}
			var req struct {
				ID     *int64          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
				Meta   struct {
					RequestID string `json:"requestId"`
				} `json:"meta"`
			}
			if json.Unmarshal(body, &req) != nil || req.ID == nil {
				continue
			}
			if !strings.HasPrefix(req.Method, "$/") {
				mu.Lock()
				seen = append(seen, recordedCall{req.Method, string(req.Params), req.Meta.RequestID})
				mu.Unlock()
			}
			_ = rpc.WriteFrame(s2cW, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":null}`, *req.ID)))
		}
	}()
	return s2cR, c2sW, func() []recordedCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedCall(nil), seen...)
	}
}

// Each of RemoteBackend's methods must call the server method of its own
// name, pass every argument in order, and carry a request id exactly when
// the method changes state on the host. The lines are written by hand, one
// per method: this is what catches a wrong name or a dropped argument.
func TestRemoteBackendCallsEachMethodByName(t *testing.T) {
	in, out, calls := answerEverything(t)
	remote, err := dialRemoteBackend(in, out, func() EventSink { return nopSink{} }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()

	iface := reflect.TypeFor[Backend]()
	value := reflect.ValueOf(remote)
	checked := 0
	for m := range iface.Methods() {
		if m.Name == "Init" || m.Name == "Close" {
			continue
		}
		// One distinct, recognisable value per argument.
		args := make([]reflect.Value, m.Type.NumIn())
		wantParams := make([]any, 0, m.Type.NumIn())
		for a := range m.Type.NumIn() {
			if a == 0 && m.Type.In(a) == reflect.TypeFor[context.Context]() {
				// RPC supplies the request context on the host, outside the wire parameters.
				args[a] = reflect.ValueOf(t.Context())
				continue
			}
			args[a] = distinctValue(m.Type.In(a), a)
			wantParams = append(wantParams, args[a].Interface())
		}
		before := len(calls())
		value.MethodByName(m.Name).Call(args)
		got := calls()
		// A long call returns at once and reaches the wire in the
		// background (goLong).
		for deadline := time.Now().Add(5 * time.Second); len(got) == before && time.Now().Before(deadline); got = calls() {
			time.Sleep(time.Millisecond)
		}
		if len(got) != before+1 {
			t.Errorf("%s made %d calls, want 1", m.Name, len(got)-before)
			continue
		}
		call := got[len(got)-1]
		if call.Method != m.Name {
			t.Errorf("%s called the server method %q", m.Name, call.Method)
		}
		if want := asJSON(t, wantParams); call.Params != want {
			t.Errorf("%s sent params %s, want %s", m.Name, call.Params, want)
		}
		if mutating := methodClass[m.Name].Mutating; (call.RequestID != "") != mutating {
			t.Errorf("%s: request id %q, but the method is classified mutating=%v", m.Name, call.RequestID, mutating)
		}
		checked++
	}
	if checked < 100 {
		t.Errorf("only %d methods were checked", checked)
	}
	ids := make(map[string]bool)
	for _, c := range calls() {
		if c.RequestID != "" {
			if ids[c.RequestID] {
				t.Errorf("request id %s was used twice", c.RequestID)
			}
			ids[c.RequestID] = true
		}
	}
}

// distinctValue returns a value of type t that differs by argument position,
// so swapped or dropped arguments show.
func distinctValue(t reflect.Type, position int) reflect.Value {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprintf("arg-%d", position))
	case reflect.Bool:
		v.SetBool(position%2 == 0)
	case reflect.Int, reflect.Int64:
		v.SetInt(int64(100 + position))
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			v.Set(reflect.ValueOf([]string{fmt.Sprintf("item-%d", position)}))
		} else {
			v.Set(reflect.MakeSlice(t, 0, 0))
		}
	}
	return v
}

// Closing the backend ends the connection and the server, and leaves no
// goroutine behind on either side.
func TestRemoteBackendCloseLeavesNoGoroutines(t *testing.T) {
	path := contractConfig(t)
	runtime.GC()
	base := runtime.NumGoroutine()
	for range 3 {
		link := serveRemote(t, path, nil)
		if roots := link.Remote.ListRoots(); len(roots) != 1 {
			t.Fatalf("ListRoots = %v", roots)
		}
		link.Remote.Close()
		select {
		case err := <-link.served:
			if err != nil {
				t.Errorf("the server ended with %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the server did not stop after the client closed")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > base {
		buf := make([]byte, 1<<16)
		t.Errorf("%d goroutines are left, %d before the test:\n%s", n, base, buf[:runtime.Stack(buf, true)])
	}
}

type waitingGitInput struct {
	io.Reader
	started chan struct{}
}

func (r waitingGitInput) Read(p []byte) (int, error) {
	// The runner starts pumping stdin only after Git starts.
	close(r.started)
	return r.Reader.Read(p)
}

// The end of the connection is the server's only shutdown signal. It must
// reach a git process that a handler started and that would never end by
// itself.
func TestServerEndOfInputStopsGit(t *testing.T) {
	path := contractConfig(t)
	repo := loadConfig(t, path).Sources["acct"].Repos["org/repo"].CloneFolder
	gitRunning := make(chan struct{})
	gitEnded := make(chan error, 1)
	link := serveRemote(t, path, map[string]rpc.Handler{
		"TestHang": rpc.Func(func(ctx context.Context) error {
			// git cat-file --batch waits for requests on an input that
			// never delivers any.
			pr, pw := io.Pipe()
			defer pw.Close()
			_, err := git.Run(ctx, git.Request{
				Dir: repo, Args: []string{"cat-file", "--batch"},
				Stdin: waitingGitInput{Reader: pr, started: gitRunning},
			})
			gitEnded <- err
			return err
		}),
	})
	pending := make(chan error, 1)
	go func() { pending <- link.Remote.invoke("TestHang", nil) }()
	select {
	case <-gitRunning:
	case <-time.After(10 * time.Second):
		t.Fatal("the handler never started")
	}

	started := time.Now()
	link.Remote.Close()
	select {
	case err := <-gitEnded:
		if err == nil {
			t.Error("the hung git ended without an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("git was still running 10 seconds after the connection ended")
	}
	select {
	case <-link.served:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop")
	}
	if spent := time.Since(started); spent > rpc.ShutdownGrace+2*time.Second {
		t.Errorf("stopping took %v", spent)
	}
	if err := <-pending; err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the pending call ended with %v", err)
	}
}

// A window on a remote host shows its own version, and learns the server's
// from the handshake.
func TestStartupSnapshotCarriesTheServerVersion(t *testing.T) {
	// The server says it runs another build's version; the window shows its
	// own.
	var link *remoteLink
	link = serveRemote(t, contractConfig(t), map[string]rpc.Handler{
		"GetStartupSnapshot": rpc.Func(func() (StartupSnapshot, error) {
			snap, err := hostBackend{localBackend{link.Host}}.GetStartupSnapshot()
			snap.AppVersion = "the-server-says-so"
			return snap, err
		}),
	})
	a := &App{backend: link.Remote, prefs: newFilePrefs(seedConfig(t, "client.json", nil))}
	if got := a.GetAppVersion(); got != a.getAppVersionLocal() {
		t.Errorf("GetAppVersion = %q, want this build's own %q", got, a.getAppVersionLocal())
	}
	remoteSnap, err := a.GetStartupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if remoteSnap.AppVersion != a.getAppVersionLocal() {
		t.Errorf("the snapshot's AppVersion = %q, want this build's own %q", remoteSnap.AppVersion, a.getAppVersionLocal())
	}
	if want := formatAppVersion(version, commit); remoteSnap.ServerVersion != want {
		t.Errorf("the snapshot's ServerVersion = %q, want %q", remoteSnap.ServerVersion, want)
	}
	if remoteSnap.OS != runtime.GOOS || remoteSnap.ServerOS != runtime.GOOS {
		t.Errorf("the snapshot's OS = %q and ServerOS = %q, want this machine's and the server's", remoteSnap.OS, remoteSnap.ServerOS)
	}
	if len(remoteSnap.Config.Accounts) != 1 {
		t.Errorf("the snapshot lost the host's config: %+v", remoteSnap.Config)
	}

	// A local window says nothing about a server.
	local := &App{cfgPath: contractConfig(t)}
	local.be().Init(context.Background())
	snap, err := local.GetStartupSnapshot()
	if snap.ServerOS != snap.OS || snap.OS != runtime.GOOS {
		t.Errorf("a local snapshot's OS %q and ServerOS %q differ", snap.OS, snap.ServerOS)
	}
	if err != nil || snap.ServerVersion != "" || snap.AppVersion == "" {
		t.Errorf("local snapshot: server %q, app %q, %v", snap.ServerVersion, snap.AppVersion, err)
	}
	if strings.Contains(asJSON(t, snap), "serverVersion") {
		t.Error("a local snapshot must not carry the serverVersion key")
	}
}

// The repository events (#295) reach the window as this build emits them,
// and a pull:done whose skipped is neither a flag nor an identifier is
// dropped.
func TestRepositoryEventsAreRelayed(t *testing.T) {
	link := serveRemote(t, contractConfig(t), nil)
	sent := []struct {
		name    string
		payload any
	}{
		{"clone:progress", map[string]any{"source": "acct", "repo": "org/repo", "phase": "Receiving objects", "percent": 42}},
		{"clone:done", map[string]any{"source": "acct", "repo": "org/repo", "path": "/h/git/org/repo", "fixes": []string{"set user.name"}}},
		{"pull:done", map[string]any{"source": "acct", "repo": "org/repo", "skipped": "dirty", "state": "dirty", "modified": 2, "untracked": 1, "ahead": 0, "behind": 3}},
		{"pull:done", map[string]any{"source": "acct", "repo": "org/other", "error": "offline", "code": "git.offline", "skipped": true, "host": "github.com"}},
		{"mirror:setup:done", MirrorSetupResult{MirrorKey: "mir", RepoKey: "org/repo", Created: true, Mirrored: true, Method: "api"}},
		{"move:progress", MoveProgressEventDTO{Phase: "fetch", Message: "Fetching"}},
		{"move:done", MoveResultDTO{NewOrigin: "https://example.invalid/o/r.git", DestRepoCreated: true, Warnings: []string{"w"}}},
		{"reconcile:done", ReconcileDTO{Adopted: 1, Unmatched: []OrphanRepoDTO{}, Ignored: []OrphanRepoDTO{}, Incomplete: []IncompleteCloneDTO{{Source: "acct", Repo: "org/x", Path: "/h/x"}}}},
	}
	for _, e := range sent {
		link.Host.sink().Emit(e.name, e.payload)
	}
	link.Host.sink().Emit("pull:done", map[string]any{"source": "acct", "repo": "org/evil", "skipped": "<img src=x>"})
	link.Host.sink().Emit("reconcile:done", map[string]any{"adopted": "many"})

	deadline := time.Now().Add(5 * time.Second)
	for len(link.Events.named("reconcile:done")) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // the hostile ones, sent last, would be in by now
	counts := map[string]int{}
	for _, e := range sent {
		got := link.Events.named(e.name)
		i := counts[e.name]
		counts[e.name]++
		if e.name == "clone:progress" || e.name == "move:progress" {
			if len(got) == 0 {
				t.Errorf("%s was not relayed", e.name)
			}
			continue
		}
		if i >= len(got) {
			t.Errorf("%s #%d was not relayed", e.name, i+1)
			continue
		}
		if w, g := canonJSON(t, []any{e.payload}), canonJSON(t, got[i].args); w != g {
			t.Errorf("%s arrived as %s, want %s", e.name, g, w)
		}
	}
	if n := len(link.Events.named("pull:done")); n != 2 {
		t.Errorf("pull:done arrived %d times, want the two valid ones", n)
	}
	if n := len(link.Events.named("reconcile:done")); n != 1 {
		t.Errorf("reconcile:done arrived %d times, want the valid one", n)
	}
}
