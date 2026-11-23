package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(s.home, ".config"))
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
		// The token credentials (#242), each side in its own credentials
		// folder. GCM is not symmetric: a host refuses it (TestHostRefusesGCM).
		{name: "CredentialStoreToken", call: func(b Backend, _ mutationSide) (any, error) {
			return b.CredentialStoreToken("acct", "contract-store-token"), nil
		}, project: withoutFindingPaths},
		{name: "CredentialStoreToken again", call: func(b Backend, _ mutationSide) (any, error) {
			return b.CredentialStoreToken("acct", "contract-store-token"), nil
		}, project: withoutFindingPaths},
		{name: "CredentialStoreToken empty", call: func(b Backend, _ mutationSide) (any, error) {
			return b.CredentialStoreToken("acct", "  "), nil
		}},
		{name: "CredentialStoreToken missing account", call: func(b Backend, _ mutationSide) (any, error) {
			return b.CredentialStoreToken("nobody", "contract-store-token"), nil
		}},
		{name: "ChangeCredentialType", call: func(b Backend, _ mutationSide) (any, error) { return none(b.ChangeCredentialType("acct", "token")) }},
		{name: "CredentialDelete", call: func(b Backend, _ mutationSide) (any, error) { return b.CredentialDelete("acct"), nil }},
		{name: "CredentialDelete again", call: func(b Backend, _ mutationSide) (any, error) { return b.CredentialDelete("acct"), nil }},
		// The SSH keys (#244), on an account whose host does not resolve, so
		// the connection test fails fast. Each side makes its own key in its
		// own home.
		{name: "AddAccount ssh", call: func(b Backend, _ mutationSide) (any, error) {
			req := acct("sshacct", "https://forge.example.invalid")
			req.CredentialType = "ssh"
			return none(b.AddAccount(req))
		}},
		{name: "CredentialSetupSSH", call: func(b Backend, _ mutationSide) (any, error) { return b.CredentialSetupSSH("sshacct"), nil }, project: withoutSSHKey},
		{name: "CredentialSetupSSH again", call: func(b Backend, _ mutationSide) (any, error) { return b.CredentialSetupSSH("sshacct"), nil }, project: withoutSSHKey},
		{name: "CredentialSSHAgentReport", call: func(b Backend, _ mutationSide) (any, error) { return b.CredentialSSHAgentReport("sshacct"), nil }, project: withoutKeyFile},
		{name: "CredentialRegenerateSSH", call: func(b Backend, _ mutationSide) (any, error) { return b.CredentialRegenerateSSH("sshacct"), nil }, project: withoutSSHKey},
		{name: "CredentialSetupSSH missing account", call: func(b Backend, _ mutationSide) (any, error) { return b.CredentialSetupSSH("nobody"), nil }},
		{name: "CredentialSSHAgentReport missing account", call: func(b Backend, _ mutationSide) (any, error) { return b.CredentialSSHAgentReport("nobody"), nil }},
		{name: "DeleteAccount ssh", call: func(b Backend, s mutationSide) (any, error) {
			if err := b.DeleteAccount("sshacct"); err != nil {
				return nil, err
			}
			// The key went with the account (#244).
			_, err := os.Stat(filepath.Join(s.home, ".ssh", "gitbox-sshacct-sshkey"))
			return os.IsNotExist(err), nil
		}},
	}
}

// withoutSSHKey keeps an SSH setup result but not its public key, which
// each side made on its own; it must be there.
func withoutSSHKey(v any) any {
	r, _ := v.(CredentialSetupResult)
	if strings.HasPrefix(r.SSHPublicKey, "ssh-ed25519 ") {
		r.SSHPublicKey = "<key>"
	}
	return r
}

// withoutKeyFile keeps an agent report but not its key file and that key's
// fingerprint, which name each side's own key; both must be there.
func withoutKeyFile(v any) any {
	r, _ := v.(SSHAgentReportDTO)
	if strings.HasSuffix(r.KeyFile, "gitbox-sshacct-sshkey") && strings.HasPrefix(r.KeyFileFingerprint, "SHA256:") {
		r.KeyFile, r.KeyFileFingerprint = "<key file>", "<fingerprint>"
	}
	return r
}

// withoutFindingPaths keeps the credential findings of a setup result but
// not their paths, which name each side's own folder.
func withoutFindingPaths(v any) any {
	r, _ := v.(CredentialSetupResult)
	for i := range r.Findings {
		r.Findings[i].Path = ""
	}
	return r
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

// unserve takes method off the served list for the test, as a method is
// before its phase serves it: a server built after the call does not answer
// it, and a remote window refuses it if it is a mutation. Every Backend
// mutation is served since #244, so the tests of that gate make their own
// unserved method.
func unserve(t *testing.T, method string) {
	t.Helper()
	before := slices.Clone(servedMethods)
	servedMethods = slices.DeleteFunc(slices.Clone(servedMethods), func(m string) bool { return m == method })
	delete(servedSet, method)
	t.Cleanup(func() {
		servedMethods = before
		servedSet[method] = true
	})
}

// TestUnservedMutationsRefused: a remote window refuses, before the wire,
// a mutation the server does not serve; a served one goes through.
func TestUnservedMutationsRefused(t *testing.T) {
	path := contractHost(t)
	unserve(t, "CredentialSetupSSH")
	unserve(t, "CredentialRegenerateSSH")
	// The test server answers CredentialSetupSSH, so a call that reached it
	// would show.
	var reached atomic.Bool
	link := serveRemote(t, path, map[string]rpc.Handler{
		"CredentialSetupSSH": rpc.Func(func(string) CredentialSetupResult { reached.Store(true); return CredentialSetupResult{OK: true} }),
	})
	link.Remote.refuseUnserved = true
	if res := link.Remote.CredentialSetupSSH("acct"); res.OK || toUserError(link.Remote.LastError()).Code != "remote.read_only" {
		t.Errorf("CredentialSetupSSH = %+v (%v), want refused with remote.read_only", res, link.Remote.LastError())
	}
	if res := link.Remote.CredentialRegenerateSSH("acct"); res.OK || toUserError(link.Remote.LastError()).Code != "remote.read_only" {
		t.Errorf("CredentialRegenerateSSH = %+v (%v), want refused with remote.read_only", res, link.Remote.LastError())
	}
	if reached.Load() {
		t.Error("a refused mutation reached the server")
	}
	if err := link.Remote.SetNestedScanDepth(2); err != nil {
		t.Errorf("a served mutation was refused: %v", err)
	}
}
