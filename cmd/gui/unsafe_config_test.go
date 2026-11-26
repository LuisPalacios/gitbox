package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// A setting the clone controls stops fetch and pull with a typed failure the
// window can explain: the code, the setting and where it lives, never the
// value, and no raw repair sentence next to it (#344).
func TestUnsafeConfigStopsFetchAndPull(t *testing.T) {
	app, events := refreshTestApp(t, 1)
	defer app.stopRefresh()
	const source, repo = "source-a", "org/repo-0"
	finding := git.TrustFinding{Key: git.TrustHooksPath, Scope: git.ScopeLocal}
	const other = "identity: could not write"
	app.refreshHooks.heal = func(*config.Config, string, string) heal.Report {
		return heal.Report{Warnings: []string{finding.Warning(), other}, Trust: []git.TrustFinding{finding}}
	}
	ran := false
	app.refreshHooks.fetch = func(context.Context, string) (string, error) { ran = true; return "", nil }
	app.refreshHooks.pull = func(context.Context, string) error { ran = true; return nil }
	snapshot := app.snapshotRefreshConfig()
	path := status.RepoPath(snapshot, source, repo, snapshot.Sources[source].Repos[repo])

	if err := app.performFetch(app.newOperation(), snapshot, source, repo, path, nil); err == nil {
		t.Fatal("fetch went ahead with an unsafe setting")
	}
	done := events.named("fetch:done")
	if len(done) != 1 {
		t.Fatalf("fetch:done events = %d", len(done))
	}
	fetched := done[0].args[0].(fetchDoneDTO)
	checkUnsafePayload(t, "fetch", fetched.Code, fetched.UnsafeConfig, fetched.RepairWarnings, fetched)

	pulled := app.safePull(t.Context(), snapshot, source, repo, true, pullTarget{})
	checkUnsafePayload(t, "pull", pulled.Code, pulled.UnsafeConfig, pulled.RepairWarnings, pulled)
	if ran {
		t.Fatal("git ran on a clone with an unsafe setting")
	}
}

func checkUnsafePayload(t *testing.T, op, code string, unsafe *UnsafeConfigDTO, repair []string, payload any) {
	t.Helper()
	if code != "git.unsafe_config" {
		t.Errorf("%s code = %q, want git.unsafe_config", op, code)
	}
	if unsafe == nil || *unsafe != (UnsafeConfigDTO{Setting: "core.hooksPath", Scope: "local"}) {
		t.Errorf("%s unsafeConfig = %+v", op, unsafe)
	}
	if len(repair) != 1 || repair[0] != "identity: could not write" {
		t.Errorf("%s repair warnings = %q, want only the other warning", op, repair)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "repository maintenance") {
		t.Errorf("%s payload carries the raw maintenance text: %s", op, data)
	}
}

// A server is untrusted: only names this build knows survive decoding.
func TestUnsafeConfigDTO_FromServer(t *testing.T) {
	for raw, want := range map[string]*UnsafeConfigDTO{
		`{"setting":"core.hooksPath","scope":"worktree"}`:   {Setting: "core.hooksPath", Scope: "worktree"},
		`{"setting":"url.*.insteadOf","scope":"include"}`:   {Setting: "url.*.insteadOf", Scope: "include"},
		`{"setting":"core.hooksPath","scope":"global"}`:     nil,
		`{"setting":"/home/someone/hooks","scope":"local"}`: nil,
		`{"setting":"<img src=x>","scope":"local"}`:         nil,
	} {
		var done fetchDoneDTO
		if err := json.Unmarshal([]byte(`{"source":"a","repo":"b","unsafeConfig":`+raw+`}`), &done); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		got := keptUnsafeConfig(done.UnsafeConfig)
		if (got == nil) != (want == nil) || got != nil && *got != *want {
			t.Errorf("%s decoded to %+v, want %+v", raw, got, want)
		}
	}
}
