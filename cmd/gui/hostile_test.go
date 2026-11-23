package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// The hostile values of #240: what a compromised or buggy host might send.
var (
	hostileURLs = []string{"javascript:alert(1)", "vscode://x", "ms-settings:", "https://user:pass@host", "https://host/\r\nX"}
	hostileText = []string{`"; rm -rf ~ #`, "$(id)", "../../etc", "\x1b[31mred", strings.Repeat("x", 1<<20)}
)

// countingRunner stands in for git and counts the commands it was asked
// to run.
type countingRunner struct {
	mu   sync.Mutex
	runs []string
}

func (c *countingRunner) Run(_ context.Context, req git.Request) (git.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runs = append(c.runs, strings.Join(req.Args, " "))
	return git.Result{}, nil
}

// trapPath puts a folder first on PATH whose programs, named like every
// launcher a window may start, only record that they ran. It returns the
// record file.
func trapPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	record := filepath.Join(dir, "ran.log")
	for _, name := range []string{"open", "xdg-open", "code", "cursor", "claude", "wezterm", "alacritty", "kitty", "gnome-terminal", "konsole", "x-terminal-emulator", "bash", "zsh", "sh"} {
		script := "#!/bin/sh\necho " + name + " \"$@\" >> '" + record + "'\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

// hostileServer is a gitbox-server that answers with hostile values. The
// client side is a remote window's App on a RemoteBackend.
type hostileServer struct {
	srv    *rpc.Server
	app    *App
	remote *RemoteBackend
	events *recordingSink
	log    *lockedBuffer
	served chan error
}

func startHostile(t *testing.T, handlers map[string]rpc.Handler) *hostileServer {
	t.Helper()
	h := &hostileServer{events: &recordingSink{}, log: &lockedBuffer{}, served: make(chan error, 1)}
	h.srv = &rpc.Server{
		Handlers:    handlers,
		ToUserError: toUserError,
		Self:        rpc.Hello{Version: version, Commit: commit, OS: "linux", Arch: "amd64", PID: 1},
		Log:         io.Discard,
	}
	ctx, cancel := context.WithCancel(context.Background())
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	go func() {
		err := h.srv.Serve(ctx, c2sR, s2cW)
		_ = s2cW.Close()
		_ = c2sR.Close()
		h.served <- err
	}()
	remote, err := dialRemoteBackend(s2cR, c2sW, func() EventSink { return h.events }, h.log)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	h.remote = remote
	h.app = &App{backend: remote, prefs: newFilePrefs(seedConfig(t, "client.json", nil)), events: h.events}
	t.Cleanup(func() {
		remote.Close()
		cancel()
	})
	return h
}

func (h *hostileServer) waitEvent(t *testing.T, name string) []recordedEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(h.events.named(name)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no %s event arrived", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h.events.named(name)
}

// TestHostileServer feeds a remote window hostile links, names, paths,
// events and lists. Every one is refused, dropped, cut or kept as inert
// text, and no process starts on the client.
func TestHostileServer(t *testing.T) {
	opened := recordBrowser(t)
	runner := &countingRunner{}
	git.SetRunnerForTest(t, runner)
	record := ""
	if runtime.GOOS != "windows" {
		record = trapPath(t)
	}
	runtime.GC()
	baseGoroutines := runtime.NumGoroutine()

	accounts := map[string]config.Account{}
	for i, u := range append(append([]string{}, hostileURLs...), "https://host/ok") {
		accounts[string(rune('a'+i))] = config.Account{Provider: "gitea", URL: u, Username: "u"}
	}
	statuses := make([]StatusResult, 0, len(hostileText))
	for _, text := range hostileText {
		statuses = append(statuses, StatusResult{Source: "a", Repo: text, Path: text, State: "clean", Branch: text})
	}
	tooMany := make([]StatusResult, maxRemoteRepos+1)
	for i := range tooMany {
		tooMany[i] = StatusResult{Source: "a", Repo: "r", State: "clean"}
	}

	h := startHostile(t, map[string]rpc.Handler{
		"GetConfig": rpc.Func(func() (ConfigDTO, error) {
			return ConfigDTO{Accounts: accounts}, nil
		}),
		"GetStartupSnapshot": rpc.Func(func() (StartupSnapshot, error) {
			return StartupSnapshot{Config: ConfigDTO{Accounts: accounts}, OS: "\x1b[31mlinux\r\n"}, nil
		}),
		"OpenAccountInBrowser": rpc.Func(func(key string) (string, error) { return accounts[key].URL, nil }),
		"OpenRepoInBrowser":    rpc.Func(func(source, repo string) (string, error) { return repo, nil }),
		"GetAllStatus":         rpc.Func(func() []StatusResult { return statuses }),
		"GetPRsForRepo": rpc.Func(func(source, repo string) PRSummaryDTO {
			var prs []PullRequestDTO
			for _, u := range hostileURLs {
				prs = append(prs, PullRequestDTO{URL: u, Title: hostileText[0]})
			}
			return PRSummaryDTO{Authored: prs}
		}),
		"ListRemoteRepos": rpc.Func(func(string) []DiscoverResult {
			return make([]DiscoverResult, maxRemoteRepos+5)
		}),
	})

	// Links: every hostile one is refused, only https://host/ok opens.
	for key, acct := range accounts {
		_, err := h.app.OpenAccountInBrowser(key, true)
		if acct.URL == "https://host/ok" {
			if err != nil {
				t.Errorf("the https link was refused: %v", err)
			}
			continue
		}
		if toUserError(err).Code != "url.rejected" {
			t.Errorf("account link %q: %v, want url.rejected", acct.URL, err)
		}
	}
	for _, u := range hostileURLs {
		if _, err := h.app.OpenRepoInBrowser("a", u, true); toUserError(err).Code != "url.rejected" {
			t.Errorf("repo link %q: %v", u, err)
		}
	}
	prs := h.app.GetPRsForRepo("a", "r")
	for _, pr := range prs.Authored {
		// The window passes a PR's link to OpenInBrowser as it got it.
		if _, err := h.app.OpenInBrowser(pr.URL, true); toUserError(err).Code != "url.rejected" {
			t.Errorf("PR link %q: %v", pr.URL, err)
		}
	}
	if res, err := h.app.OpenInBrowser("http://example.com", false); err != nil || !res.NeedsConfirm {
		t.Errorf("unconfirmed http: %+v, %v", res, err)
	}
	if len(*opened) != 1 || (*opened)[0] != "https://host/ok" {
		t.Errorf("the browser opened %q, want only https://host/ok", *opened)
	}

	// Names and paths arrive as text, untouched, and nothing runs them: the
	// local launchers refuse a path that is not one of the host's, and the
	// actions with no remote equivalent, before anything starts (#243).
	spawned := recordRemoteSpawn(t)
	attachSession(t, h.app)
	got := h.app.GetAllStatus()
	if len(got) != len(hostileText) {
		t.Fatalf("GetAllStatus returned %d rows", len(got))
	}
	for i, row := range got {
		if row.Repo != hostileText[i] || row.Path != hostileText[i] {
			t.Errorf("row %d was changed on the way: %.40q", i, row.Repo)
		}
		for name, c := range map[string]struct {
			launch func() error
			code   string
		}{
			"OpenInExplorer":   {func() error { return h.app.OpenInExplorer(row.Path) }, "not_available_remote"},
			"OpenFileInEditor": {func() error { return h.app.OpenFileInEditor(row.Path) }, "not_available_remote"},
			"OpenInApp":        {func() error { return h.app.OpenInApp(row.Path, "code") }, "not_available_remote_path"},
			"OpenProfile":      {func() error { return h.app.OpenProfile(row.Path, "wezterm-default") }, "not_available_remote_path"},
			"OpenInAIHarness":  {func() error { return h.app.OpenInAIHarness(row.Path, "claude", []string{row.Repo}) }, "harness_not_allowed"},
			"OpenInAIHarness/": {func() error { return h.app.OpenInAIHarness(row.Path, "claude", nil) }, "not_available_remote_path"},
		} {
			if err := c.launch(); !codeIs(err, c.code) {
				t.Errorf("%s with the remote path %.20q = %v, want %s", name, row.Path, err, c.code)
			}
		}
	}
	if len(*spawned) != 0 {
		t.Errorf("a refused launch started %+v", *spawned)
	}
	snap, err := h.app.GetStartupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snap.ServerOS != "" || snap.OS != runtime.GOOS {
		t.Errorf("snapshot OS %q, server OS %q: a malformed host OS must be dropped and the client's own kept", snap.OS, snap.ServerOS)
	}

	// Events: an unknown name and a LOCAL one are dropped; unknown fields
	// and wrong types are dropped; an oversized list is cut. The host's
	// harness list is relayed as data, and a harness it names that is not
	// the catalog's never runs.
	h.srv.Emit("made:up", "by a hostile server")
	h.srv.Emit("update:quit", "v9")
	h.srv.Emit("harnesses:updated", map[string]any{"not": "a list"})
	h.srv.Emit("harnesses:updated", []map[string]any{{"name": "evil", "command": "/bin/sh", "evil": "<script>"}})
	h.srv.Emit("fetch:done", map[string]any{"source": 7, "repo": "r"})
	h.srv.Emit("fetch:done", map[string]any{"source": "a", "repo": "r", "error": "boom", "evil": "<script>"})
	h.srv.Emit("status:updated", tooMany)
	updates := h.waitEvent(t, "status:updated")
	if n := len(updates[0].args[0].([]StatusResult)); n != maxRemoteRepos {
		t.Errorf("status:updated carried %d rows, want %d", n, maxRemoteRepos)
	}
	harnesses := h.waitEvent(t, "harnesses:updated")
	if len(harnesses) != 1 {
		t.Errorf("harnesses:updated arrived %d times, want the well-typed one only", len(harnesses))
	} else if payload := asJSON(t, harnesses[0].args[0]); strings.Contains(payload, "<script>") || !strings.Contains(payload, `"command":"/bin/sh"`) {
		t.Errorf("harnesses:updated payload %s", payload)
	}
	if err := h.app.OpenInAIHarness("/srv/repo", "/bin/sh", nil); !codeIs(err, "harness_not_allowed") {
		t.Errorf("the host's /bin/sh harness = %v, want harness_not_allowed", err)
	}
	for _, name := range []string{"made:up", "update:quit"} {
		if len(h.events.named(name)) != 0 {
			t.Errorf("the event %s reached the window", name)
		}
		if !strings.Contains(h.log.String(), name) {
			t.Errorf("dropping %s left no log line", name)
		}
	}
	done := h.events.named("fetch:done")
	if len(done) != 1 {
		t.Fatalf("fetch:done arrived %d times, want the well-typed one only", len(done))
	}
	if payload := asJSON(t, done[0].args[0]); strings.Contains(payload, "evil") || !strings.Contains(payload, `"error":"boom"`) {
		t.Errorf("fetch:done payload %s", payload)
	}

	// Lists from methods are cut too, and the window learns about it.
	truncations := 0
	h.remote.onTruncate = func() { truncations++ }
	if n := len(h.app.ListRemoteRepos("a")); n != maxRemoteRepos {
		t.Errorf("ListRemoteRepos returned %d, want %d", n, maxRemoteRepos)
	}
	if truncations != 1 || !strings.Contains(h.log.String(), "the window shows the first 10000") {
		t.Errorf("a cut list left %d notices and the log:\n%.500s", truncations, h.log.String())
	}

	// A test build is not stamped, so the window asks git for its own
	// version; nothing else may run.
	for _, run := range runner.runs {
		if run != "describe --tags --always" && run != "rev-parse --short HEAD" {
			t.Errorf("the client ran git %.80q", run)
		}
	}
	if record != "" {
		if data, err := os.ReadFile(record); err == nil {
			t.Errorf("a client process started: %s", data)
		}
	}

	// The hostile session leaves nothing running once it ends.
	h.remote.Close()
	select {
	case <-h.served:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseGoroutines && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseGoroutines {
		t.Errorf("%d goroutines are left, %d before the test", n, baseGoroutines)
	}
}

// capConfig keeps the first accounts and repos, by key, and the sources of
// the accounts it keeps.
func TestRemoteConfigIsCapped(t *testing.T) {
	dto := ConfigDTO{Accounts: map[string]config.Account{}, Sources: map[string]SourceDTO{}}
	for i := range maxRemoteAccounts + 2 {
		key := fmt.Sprintf("acct-%04d", i)
		dto.Accounts[key] = config.Account{}
		dto.Sources[key] = SourceDTO{Account: key, Repos: map[string]config.Repo{}}
	}
	big := dto.Sources["acct-0000"]
	for i := range maxRemoteRepos + 3 {
		repo := fmt.Sprintf("o/r%05d", i)
		big.Repos[repo] = config.Repo{}
		big.RepoOrder = append(big.RepoOrder, repo)
	}
	dto.Sources["acct-0000"] = big
	notes := 0
	r := &RemoteBackend{log: io.Discard, onTruncate: func() { notes++ }}
	capConfig(r, &dto)
	if len(dto.Accounts) != maxRemoteAccounts || len(dto.Sources) != maxRemoteAccounts {
		t.Errorf("%d accounts and %d sources kept", len(dto.Accounts), len(dto.Sources))
	}
	if _, ok := dto.Accounts["acct-1001"]; ok {
		t.Error("the last account by key was kept")
	}
	total := 0
	for _, src := range dto.Sources {
		total += len(src.Repos)
		if len(src.RepoOrder) != len(src.Repos) {
			t.Errorf("a source's order lists %d repos for %d", len(src.RepoOrder), len(src.Repos))
		}
	}
	if total != maxRemoteRepos || notes != 2 {
		t.Errorf("%d repos kept, %d notices", total, notes)
	}
	raw, _ := json.Marshal(dto)
	if len(raw) == 0 {
		t.Fatal("the capped config does not encode")
	}
}

// Every P4, P5 and P6 event of eventClass is relayed, and nothing else is.
func TestRelayedEventsAreRemote(t *testing.T) {
	served := func(info eventInfo) bool {
		return info.Class == classRemote && (info.Owner == "P4" || info.Owner == "P5" || info.Owner == "P6")
	}
	for name := range relayedEvents {
		if info := eventClass[name]; !served(info) {
			t.Errorf("%s is relayed but classified %+v", name, info)
		}
	}
	for name, info := range eventClass {
		if _, ok := relayedEvents[name]; served(info) && !ok {
			t.Errorf("%s is a P4 or P6 event that the relay drops", name)
		}
	}
}
