package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/gitignore"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// mutationSide is one of the two hosts of TestBackendContractMutations: its
// own config file and its own home and global git config, on the same roots
// and clones as the other side.
type mutationSide struct {
	b       Backend
	cfgPath string
	home    string
}

// use points HOME and the global git config at the side, for the calls that
// touch them (the gitignore, the GCM fix, the identity).
func (s mutationSide) use(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", s.home)
	t.Setenv("USERPROFILE", s.home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(s.home, ".gitconfig"))
}

// mutationRow is one step of TestBackendContractMutations. project replaces
// a result by what of it does not depend on the side (paths, timestamps).
type mutationRow struct {
	name    string
	call    func(b Backend, s mutationSide) (any, error)
	project func(any) any
	// code is the error code the step must give, when it must fail.
	code string
}

func mutationRows(root, firstRoot string) []mutationRow {
	acct := func(key, url string) AddAccountRequest {
		return AddAccountRequest{Key: key, Provider: "gitea", URL: url, Username: key, Name: key, Email: key + "@e", CredentialType: "token"}
	}
	none := func(err error) (any, error) { return nil, err }
	return []mutationRow{
		{name: "AddRoot", call: func(b Backend, _ mutationSide) (any, error) { return none(b.AddRoot(root + "/r2")) }},
		{name: "AddRoot existing", call: func(b Backend, _ mutationSide) (any, error) { return none(b.AddRoot(root + "/r2")) }},
		{name: "AdoptFirstProject relative", code: "remote.remote_path_invalid", call: func(b Backend, _ mutationSide) (any, error) {
			return b.AdoptFirstProject("acct", "org/project", "relative/path")
		}},
		{name: "AddRoot relative", code: "remote.remote_path_invalid", call: func(b Backend, _ mutationSide) (any, error) { return none(b.AddRoot("relative/root")) }},
		{name: "SetDefaultRoot", call: func(b Backend, _ mutationSide) (any, error) { return none(b.SetDefaultRoot(root + "/r2")) }},
		{name: "SetDefaultRoot back", call: func(b Backend, _ mutationSide) (any, error) { return none(b.SetDefaultRoot(firstRoot)) }},
		{name: "RemoveRoot", call: func(b Backend, _ mutationSide) (any, error) { return b.RemoveRoot(root+"/r2", false) }},
		{name: "InitRoot", call: func(b Backend, _ mutationSide) (any, error) { return none(b.InitRoot(firstRoot)) }},
		{name: "SetNestedScanDepth", call: func(b Backend, _ mutationSide) (any, error) { return none(b.SetNestedScanDepth(3)) }},
		{name: "SetPRBadgesEnabled", call: func(b Backend, _ mutationSide) (any, error) { return none(b.SetPRBadgesEnabled(false)) }},
		{name: "SetPRIncludeDrafts", call: func(b Backend, _ mutationSide) (any, error) { return none(b.SetPRIncludeDrafts(true)) }},
		{name: "SetCheckGlobalGitignore", call: func(b Backend, _ mutationSide) (any, error) { return none(b.SetCheckGlobalGitignore(false)) }},
		{name: "AddAccount", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.AddAccount(acct("third", "https://third.example.invalid")))
		}},
		{name: "AddAccount duplicate", code: "config.exists", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.AddAccount(acct("third", "https://third.example.invalid")))
		}},
		{name: "UpdateAccount", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.UpdateAccount(UpdateAccountRequest{Key: "third", Provider: "gitea", URL: "https://third.example.invalid", Username: "third", Name: "Third T", Email: "third@e"}))
		}},
		{name: "RenameAccount", call: func(b Backend, _ mutationSide) (any, error) { return none(b.RenameAccount("third", "fourth")) }},
		{name: "AddMirrorGroup", call: func(b Backend, _ mutationSide) (any, error) { return none(b.AddMirrorGroup("m2", "acct", "fourth")) }},
		{name: "AddMirrorRepo", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.AddMirrorRepo("m2", "org/repo", "push", "src"))
		}},
		{name: "DeleteMirrorRepo", call: func(b Backend, _ mutationSide) (any, error) { return none(b.DeleteMirrorRepo("m2", "org/repo")) }},
		{name: "DeleteMirrorGroup", call: func(b Backend, _ mutationSide) (any, error) { return none(b.DeleteMirrorGroup("m2")) }},
		{name: "DeleteMirrorGroup missing", code: "config.not_found", call: func(b Backend, _ mutationSide) (any, error) { return none(b.DeleteMirrorGroup("m2")) }},
		{name: "DiscardAccount", call: func(b Backend, _ mutationSide) (any, error) { return none(b.DiscardAccount("fourth")) }},
		{name: "AddAccount to delete", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.AddAccount(acct("fifth", "https://fifth.example.invalid")))
		}},
		{name: "DeleteAccount", call: func(b Backend, _ mutationSide) (any, error) { return none(b.DeleteAccount("fifth")) }},
		{name: "AddDiscoveredRepos", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.AddDiscoveredRepos("acct", []string{"org/new"}))
		}},
		{name: "AddDiscoveredReposToFolder", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.AddDiscoveredReposToFolder("acct", []string{"org/new2"}, root+"/custom"))
		}},
		{name: "SetRepoContainer", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.SetRepoContainer("acct", "org/repo", true))
		}},
		{name: "SetRepoWorkspace", call: func(b Backend, _ mutationSide) (any, error) {
			return none(b.SetRepoWorkspace("acct", "org/repo", ""))
		}},
		{name: "IgnoreClone", call: func(b Backend, _ mutationSide) (any, error) { return none(b.IgnoreClone(root+"/stray", true)) }},
		{name: "RemoveGlobalIdentity", call: func(b Backend, _ mutationSide) (any, error) { return none(b.RemoveGlobalIdentity()) }},
		{name: "FixGlobalGCMConfig", call: func(b Backend, _ mutationSide) (any, error) { return none(b.FixGlobalGCMConfig()) }},
		{name: "InstallGlobalGitignore", call: func(b Backend, _ mutationSide) (any, error) { return b.InstallGlobalGitignore() },
			project: func(v any) any {
				r, _ := v.(gitignore.InstallResult)
				r.Path, r.BackupPath = "", ""
				return r
			}},
		{name: "AcknowledgeConfigError", call: func(b Backend, _ mutationSide) (any, error) { b.AcknowledgeConfigError(); return nil, nil }},
		{name: "RepairConfig", call: func(b Backend, _ mutationSide) (any, error) { return b.RepairConfig(), nil }},
		{name: "RestoreFromBackup", call: func(b Backend, s mutationSide) (any, error) {
			// The sides have different backup histories, so each restores a
			// backup of its current file: the domains match at this step.
			data, err := os.ReadFile(s.cfgPath)
			if err != nil {
				return nil, err
			}
			backup := filepath.Join(filepath.Dir(s.cfgPath), "gitbox-20000101-000000.json")
			if err := os.WriteFile(backup, data, 0o600); err != nil {
				return nil, err
			}
			return b.RestoreFromBackup(backup), nil
		}, project: func(v any) any {
			r, _ := v.(ConfigRepairResult)
			return r.Success
		}},
	}
}

// TestBackendContractMutations runs the served config mutations (#294) in
// one order against two hosts that start from the same domain: the local
// backend, as a server exposes it, and a RemoteBackend connected to a server
// on a copy of the config. After every step the result, the error and the
// domain on disk must match.
func TestBackendContractMutations(t *testing.T) {
	base := contractHost(t)
	cfg := loadConfig(t, base)
	firstRoot := cfg.Global.Roots[0]
	root := t.TempDir()
	copyPath := filepath.Join(t.TempDir(), "gitbox.json")
	data, err := os.ReadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	local := newServerApp(base)
	local.be().Init(context.Background())
	t.Cleanup(local.closeLocal)
	left := mutationSide{b: Backend(hostBackend{localBackend{local}}), cfgPath: base, home: t.TempDir()}
	link := serveRemote(t, copyPath, nil)
	right := mutationSide{b: link.Remote, cfgPath: copyPath, home: t.TempDir()}

	for _, row := range mutationRows(root, firstRoot) {
		t.Run(row.name, func(t *testing.T) {
			left.use(t)
			want, wantErr := row.call(left.b, left)
			right.use(t)
			got, gotErr := row.call(right.b, right)
			if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && toUserError(wantErr).Code != toUserError(gotErr).Code) {
				t.Fatalf("errors differ: local %v (%s), remote %v (%s)", wantErr, toUserError(wantErr).Code, gotErr, toUserError(gotErr).Code)
			}
			if code := toUserError(gotErr).Code; row.code != code && (row.code != "" || gotErr != nil) {
				t.Errorf("error code over the wire = %q (%v), want %q", code, gotErr, row.code)
			}
			if row.project != nil {
				want, got = row.project(want), row.project(got)
			}
			if w, g := asJSON(t, want), asJSON(t, got); w != g {
				t.Errorf("results differ:\n local  %s\n remote %s", w, g)
			}
			if l, r := config.DomainHash(loadConfig(t, base)), config.DomainHash(loadConfig(t, copyPath)); l != r {
				t.Errorf("the domains on disk differ after %s", row.name)
			}
		})
	}

}

// mutationRowMethods names the methods mutationRows calls, for the coverage
// check of TestBackendContract.
func mutationRowMethods() map[string]bool {
	m := make(map[string]bool)
	for _, row := range mutationRows("/r", "/f") {
		m[strings.Fields(row.name)[0]] = true
	}
	return m
}

// TestReplayRequestID: through the real server, AddMirrorGroup sent twice
// with one request id adds one group and answers both calls alike: the
// second is replayed, not run (it would fail with config.exists). The same
// id on another method runs (#294).
func TestReplayRequestID(t *testing.T) {
	path := contractHost(t)
	link := serveRemote(t, path, nil)
	client := link.Remote.current()
	call := func(method string, params []any) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return client.Call(ctx, method, params, nil, rpc.WithRequestID("11111111-1111-4111-8111-111111111111"))
	}
	for i := range 2 {
		if err := call("AddMirrorGroup", []any{"m3", "acct", "other"}); err != nil {
			t.Fatalf("AddMirrorGroup #%d with one id = %v, want both answered alike", i+1, err)
		}
	}
	if n := len(loadConfig(t, path).Mirrors); n != 2 {
		t.Errorf("%d mirror groups after a replayed add, want the fixture's one plus m3", n)
	}
	if err := call("DeleteMirrorGroup", []any{"m3"}); err != nil {
		t.Fatalf("the same id on DeleteMirrorGroup = %v, want it to run", err)
	}
	if _, ok := loadConfig(t, path).Mirrors["m3"]; ok {
		t.Error("DeleteMirrorGroup with a reused id did not run")
	}
}

// TestUnservedMutationsRefused: a remote window refuses, before the wire,
// the mutations the server does not serve yet, the credential methods
// (#242, #244); a served one goes through.
func TestUnservedMutationsRefused(t *testing.T) {
	path := contractHost(t)
	// The test server answers ChangeCredentialType, so a call that reached
	// it would show.
	var reached atomic.Bool
	link := serveRemote(t, path, map[string]rpc.Handler{
		"ChangeCredentialType": rpc.Func(func(string, string) error { reached.Store(true); return nil }),
	})
	link.Remote.refuseUnserved = true
	if err := link.Remote.ChangeCredentialType("acct", "ssh"); toUserError(err).Code != "remote.read_only" {
		t.Errorf("ChangeCredentialType = %v, want remote.read_only", err)
	}
	if res := link.Remote.CredentialDelete("acct"); res.OK || toUserError(link.Remote.LastError()).Code != "remote.read_only" {
		t.Errorf("CredentialDelete = %+v (%v), want refused with remote.read_only", res, link.Remote.LastError())
	}
	if reached.Load() {
		t.Error("a refused mutation reached the server")
	}
	if err := link.Remote.SetNestedScanDepth(2); err != nil {
		t.Errorf("a served mutation was refused: %v", err)
	}
}
