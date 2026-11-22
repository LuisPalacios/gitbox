package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// newTestConfig builds a minimal config with two sources under a known global
// folder, so path resolution is deterministic.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	return &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Roots: []string{root}},
		Accounts: map[string]config.Account{
			"github-test":  {Provider: "github", URL: "https://github.com", Username: "u", Name: "N", Email: "e@e"},
			"forgejo-test": {Provider: "forgejo", URL: "https://f.local", Username: "u", Name: "N", Email: "e@e"},
		},
		Sources: map[string]config.Source{
			"github-test": {
				Account: "github-test",
				Repos: map[string]config.Repo{
					"team/frontend": {},
					"team/backend":  {},
				},
			},
			"forgejo-test": {
				Account: "forgejo-test",
				Folder:  "work",
				Repos: map[string]config.Repo{
					"team/infra": {},
				},
			},
		},
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// makeRepoDir creates the on-disk directory ResolveRepoPath would compute.
func makeRepoDir(t *testing.T, cfg *config.Config, srcKey, repoKey string) string {
	t.Helper()
	src := cfg.Sources[srcKey]
	folder := src.EffectiveFolder(srcKey)
	parts := []string{cfg.Global.Roots[0], folder}
	if i := filepath.ToSlash(repoKey); len(i) > 0 {
		if j := indexByte(repoKey, '/'); j >= 0 {
			parts = append(parts, repoKey[:j], repoKey[j+1:])
		} else {
			parts = append(parts, repoKey)
		}
	}
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	return dir
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func writeCodeWorkspace(t *testing.T, file string, folders ...string) {
	t.Helper()
	body := struct {
		Folders []map[string]string `json:"folders"`
	}{}
	for _, f := range folders {
		body.Folders = append(body.Folders, map[string]string{"path": f})
	}
	buf, _ := json.MarshalIndent(body, "", "  ")
	writeFile(t, file, string(buf))
}

func repoOf(cfg *config.Config, src, repo string) config.Repo {
	return cfg.Sources[src].Repos[repo]
}

func TestRootFiles_ListsOnlyRootWorkspaceFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "b.code-workspace"), "{}")
	writeFile(t, filepath.Join(dir, "A.Code-Workspace"), "{}")
	writeFile(t, filepath.Join(dir, "notes.md"), "")
	writeFile(t, filepath.Join(dir, "sub", "deep.code-workspace"), "{}")
	got, err := RootFiles(dir)
	want := []string{"A.Code-Workspace", "b.code-workspace"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("RootFiles = %v, %v, want %v", got, err, want)
	}
	if got, err := RootFiles(filepath.Join(dir, "missing")); got != nil || !os.IsNotExist(err) {
		t.Errorf("RootFiles(missing) = %v, %v, want no files and not-exist error", got, err)
	}
}

func TestSync_OneFileFlagsContainer(t *testing.T) {
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	makeRepoDir(t, cfg, "github-test", "team/backend")
	writeFile(t, filepath.Join(dir, "front.code-workspace"), "{}")

	res := Sync(cfg)
	if len(res.Choices) != 0 || len(res.Changes) != 1 {
		t.Fatalf("Sync = %+v, want one change", res)
	}
	var state SyncState
	if changed, _ := state.Apply(cfg, res.Changes); !changed {
		t.Fatal("Apply reported no change")
	}
	if r := repoOf(cfg, "github-test", "team/frontend"); !r.Container || r.Workspace != "front.code-workspace" {
		t.Errorf("repo = %+v, want container with front.code-workspace", r)
	}
	if res := Sync(cfg); len(res.Changes) != 0 {
		t.Errorf("second Sync = %+v, want no changes", res)
	}
}

func TestSync_SeveralFilesAskOnceAndKeepPick(t *testing.T) {
	var state SyncState
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	writeFile(t, filepath.Join(dir, "a.code-workspace"), "{}")
	writeFile(t, filepath.Join(dir, "b.code-workspace"), "{}")

	res := Sync(cfg)
	want := []Choice{{Source: "github-test", Repo: "team/frontend", Files: []string{"a.code-workspace", "b.code-workspace"}}}
	if !reflect.DeepEqual(res.Choices, want) {
		t.Fatalf("Choices = %+v, want %+v", res.Choices, want)
	}
	state.Apply(cfg, res.Changes)
	if r := repoOf(cfg, "github-test", "team/frontend"); !r.Container || r.Workspace != "" {
		t.Errorf("repo = %+v, want container without a pick", r)
	}

	src := cfg.Sources["github-test"]
	r := src.Repos["team/frontend"]
	r.Workspace = "b.code-workspace"
	src.Repos["team/frontend"] = r
	if res := Sync(cfg); len(res.Choices) != 0 || len(res.Changes) != 0 {
		t.Errorf("Sync with a valid pick = %+v, want nothing", res)
	}

	if err := os.Remove(filepath.Join(dir, "b.code-workspace")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "c.code-workspace"), "{}")
	res = Sync(cfg)
	if len(res.Choices) != 1 {
		t.Fatalf("Sync after the pick vanished = %+v, want a choice", res)
	}
	state.Apply(cfg, res.Changes)
	if r := repoOf(cfg, "github-test", "team/frontend"); r.Workspace != "" {
		t.Errorf("stale pick kept: %+v", r)
	}
}

// My preferred file wins whenever it is at the root: another file stands
// in while it is away, and it takes over again, without asking, when it
// comes back.
func TestSync_PreferredFileComesBack(t *testing.T) {
	var state SyncState
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	writeFile(t, filepath.Join(dir, "other.code-workspace"), "{}")
	src := cfg.Sources["github-test"]
	src.Repos["team/frontend"] = config.Repo{Container: true, Workspace: "mine.code-workspace", WorkspacePreferred: "mine.code-workspace"}

	state.Apply(cfg, Sync(cfg).Changes)
	if r := repoOf(cfg, "github-test", "team/frontend"); r.Workspace != "other.code-workspace" || r.WorkspacePreferred != "mine.code-workspace" {
		t.Fatalf("while away: repo = %+v, want other open and mine remembered", r)
	}

	writeFile(t, filepath.Join(dir, "mine.code-workspace"), "{}")
	res := Sync(cfg)
	if len(res.Choices) != 0 {
		t.Errorf("asked although the preferred file is back: %+v", res.Choices)
	}
	state.Apply(cfg, res.Changes)
	if r := repoOf(cfg, "github-test", "team/frontend"); r.Workspace != "mine.code-workspace" {
		t.Errorf("back: repo = %+v, want mine open again", r)
	}
}

func TestSync_FileGoneClearsPickKeepsContainer(t *testing.T) {
	var state SyncState
	cfg := newTestConfig(t)
	makeRepoDir(t, cfg, "github-test", "team/frontend")
	src := cfg.Sources["github-test"]
	src.Repos["team/frontend"] = config.Repo{Container: true, Workspace: "gone.code-workspace"}

	state.Apply(cfg, Sync(cfg).Changes)
	if r := repoOf(cfg, "github-test", "team/frontend"); !r.Container || r.Workspace != "" {
		t.Errorf("repo = %+v, want container without a workspace", r)
	}
}

func TestSync_SkipsClonesNotOnDisk(t *testing.T) {
	cfg := newTestConfig(t)
	src := cfg.Sources["github-test"]
	src.Repos["team/frontend"] = config.Repo{Container: true, Workspace: "w.code-workspace"}
	res := Sync(cfg)
	if len(res.Changes) != 0 || len(res.Unknown) != 1 || !os.IsNotExist(res.Unknown[0].Err) {
		t.Errorf("Sync = %+v, want a missing clone left alone and marked unknown", res)
	}
}

func TestSync_UnreadableRootPreservesWorkspaceAndResetsMiss(t *testing.T) {
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	repos := cfg.Sources["github-test"].Repos
	repos["team/frontend"] = config.Repo{Container: true, Workspace: "current.code-workspace", WorkspacePreferred: "preferred.code-workspace"}
	writeFile(t, filepath.Join(dir, "current.code-workspace"), "{}")
	var state SyncState
	state.Apply(cfg, Sync(cfg).Changes)

	offline := dir + ".offline"
	if err := os.Rename(dir, offline); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "")
	res := Sync(cfg)
	if len(res.Unknown) != 1 || res.Unknown[0].Err == nil || len(res.Changes) != 0 {
		t.Fatalf("Sync = %+v, want one failed read with no changes", res)
	}
	state.Apply(cfg, res.Changes)
	if got := repos["team/frontend"]; !got.Container || got.Workspace != "current.code-workspace" || got.WorkspacePreferred != "preferred.code-workspace" {
		t.Fatalf("unreadable root changed workspace: %+v", got)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(offline, dir); err != nil {
		t.Fatal(err)
	}
	state.Apply(cfg, Sync(cfg).Changes)
	if got := repos["team/frontend"].WorkspacePreferred; got != "preferred.code-workspace" {
		t.Fatalf("the first successful read after a failure cleared %q", got)
	}
	if changed, skipped := state.Apply(cfg, Sync(cfg).Changes); !changed || len(skipped) != 0 || repos["team/frontend"].WorkspacePreferred != "" {
		t.Errorf("second confirmed absence did not clear the preference: changed=%v skipped=%v", changed, skipped)
	}
}

func TestApply_SkipsChangedWorkspaceAndClonePath(t *testing.T) {
	cfg := newTestConfig(t)
	makeRepoDir(t, cfg, "github-test", "team/frontend")
	repos := cfg.Sources["github-test"].Repos
	saved := config.Repo{Container: true, Workspace: "gone.code-workspace", WorkspacePreferred: "gone.code-workspace"}
	repos["team/frontend"] = saved
	plan := Sync(cfg)
	for name, current := range map[string]config.Repo{
		"new pick": {Container: true, Workspace: "new.code-workspace", WorkspacePreferred: "new.code-workspace"},
		"new path": {Container: true, Workspace: saved.Workspace, WorkspacePreferred: saved.WorkspacePreferred, CloneFolder: t.TempDir()},
	} {
		t.Run(name, func(t *testing.T) {
			repos["team/frontend"] = current
			var state SyncState
			changed, skipped := state.Apply(cfg, plan.Changes)
			if changed || !reflect.DeepEqual(skipped, []status.RepoRef{{Source: "github-test", Repo: "team/frontend"}}) ||
				repos["team/frontend"] != current {
				t.Errorf("stale Apply reported %v / %v, repo now %+v", changed, skipped, repos["team/frontend"])
			}
		})
	}
}

func TestApply_SkipsRemovedRepos(t *testing.T) {
	cfg := newTestConfig(t)
	var state SyncState
	changed, skipped := state.Apply(cfg, []Change{{Source: "github-test", Repo: "gone/repo", Container: true}, {Source: "nope", Repo: "x"}})
	if changed || len(skipped) != 2 {
		t.Errorf("Apply = %v / %v, want both removed repos skipped", changed, skipped)
	}
}

func TestPlaceholders_ListsMissingFoldersInside(t *testing.T) {
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	if err := os.MkdirAll(filepath.Join(dir, "libs", "present"), 0o755); err != nil {
		t.Fatal(err)
	}
	// team/backend resolves inside the container through an absolute
	// clone_folder: it nests there, so it is no placeholder.
	src := cfg.Sources["github-test"]
	src.Repos["team/backend"] = config.Repo{CloneFolder: filepath.Join(dir, "libs", "backend")}
	src.Repos["team/frontend"] = config.Repo{Container: true, Workspace: "w.code-workspace"}
	body := `{
  // the root and its parts
  "folders": [
    {"path": "."},
    {"path": "libs/present"},
    {"path": "libs/backend"},
    {"path": "libs/missing", "name": "Missing lib"},
    {"path": "libs/other/"},
    {"path": "libs/missing"},
    {"path": "../outside"}, /* skipped */
  ],
}`
	writeFile(t, filepath.Join(dir, "w.code-workspace"), body)

	got := Inspect(cfg).Placeholders
	parent := status.RepoRef{Source: "github-test", Repo: "team/frontend"}
	want := []Placeholder{
		{Parent: parent, Name: "Missing lib", Path: "libs/missing"},
		{Parent: parent, Name: "other", Path: "libs/other"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Placeholders = %+v, want %+v", got, want)
	}
}

// A folder inside a nested clone, like a submodule not checked out, is
// that clone's content, not a clone to make.
func TestPlaceholders_SkipsFoldersInsideNestedClones(t *testing.T) {
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	src := cfg.Sources["github-test"]
	src.Repos["team/backend"] = config.Repo{CloneFolder: filepath.Join(dir, "Browser")}
	src.Repos["team/frontend"] = config.Repo{Container: true, Workspace: "w.code-workspace"}
	writeCodeWorkspace(t, filepath.Join(dir, "w.code-workspace"), "Browser/ext/common/lib", "Missing")

	got := Inspect(cfg).Placeholders
	want := []Placeholder{{Parent: status.RepoRef{Source: "github-test", Repo: "team/frontend"}, Name: "Missing", Path: "Missing"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Placeholders = %+v, want %+v", got, want)
	}
}

func TestMemberRepos_ListsClonesTheWorkspaceNames(t *testing.T) {
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	src := cfg.Sources["github-test"]
	src.Repos["team/backend"] = config.Repo{CloneFolder: filepath.Join(dir, "Backend")}
	src.Repos["team/frontend"] = config.Repo{Container: true, Workspace: "w.code-workspace"}
	cfg.Sources["forgejo-test"].Repos["team/infra"] = config.Repo{CloneFolder: filepath.Join(dir, "infra")}
	writeCodeWorkspace(t, filepath.Join(dir, "w.code-workspace"), ".", "Backend", "missing")

	want := []status.RepoRef{{Source: "github-test", Repo: "team/backend"}}
	if got := Inspect(cfg).Members; !reflect.DeepEqual(got, want) {
		t.Errorf("MemberRepos = %+v, want %+v (infra is nested but not listed)", got, want)
	}
}

func TestPlaceholders_IgnoresContainersWithoutWorkspace(t *testing.T) {
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	writeCodeWorkspace(t, filepath.Join(dir, "w.code-workspace"), "missing")
	src := cfg.Sources["github-test"]
	src.Repos["team/frontend"] = config.Repo{Container: true}
	if got := Inspect(cfg).Placeholders; len(got) != 0 {
		t.Errorf("Placeholders = %+v, want none without a picked workspace", got)
	}
}

func TestMembers_WorkspaceSizeLimit(t *testing.T) {
	dir := t.TempDir()
	const name = "fixture.code-workspace"
	body := `{"folders":[{"path":"missing"}]}`
	writeFile(t, filepath.Join(dir, name), body+strings.Repeat(" ", workspaceLimit-len(body)))
	members, issues := Members(dir, name)
	if len(issues) != 0 || len(members) != 1 || members[0].Exists || !members[0].missing || members[0].Physical != "" {
		t.Fatalf("members = %+v, diagnostics = %+v at the size limit", members, issues)
	}
	writeFile(t, filepath.Join(dir, name), body+strings.Repeat(" ", 2*workspaceLimit-len(body)))
	members, issues = Members(dir, name)
	if len(members) != 0 || !reflect.DeepEqual(issues, []Diagnostic{{File: name, Problem: TooLarge}}) {
		t.Fatalf("members = %+v, diagnostics = %+v for an oversized file", members, issues)
	}
}

func symlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" && (errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(1314))) {
			t.Skipf("creating symlinks requires privileges: %v", err)
		}
		t.Fatal(err)
	}
}

func TestMembers_ResolvesFoldersInsideAndOutside(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	for _, path := range []string{filepath.Join(outside, "nested"), filepath.Join(dir, "actual")} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	symlink(t, outside, filepath.Join(dir, "external-link"))
	symlink(t, filepath.Join(dir, "actual"), filepath.Join(dir, "internal-link"))
	writeCodeWorkspace(t, filepath.Join(dir, "fixture.code-workspace"), "external-link", "external-link/nested", "internal-link", "../escaped")
	members, issues := Members(dir, "fixture.code-workspace")
	if len(issues) != 0 || len(members) != 3 {
		t.Fatalf("members = %+v, diagnostics = %+v", members, issues)
	}
	for _, m := range members[:2] {
		physical, err := filepath.EvalSymlinks(m.Path)
		if err != nil || !m.Escapes || !m.Exists || m.Physical != physical || m.ScanPath != "" {
			t.Errorf("escaped member = %+v, resolve = %q / %v", m, physical, err)
		}
	}
	inner := members[2]
	if inner.Escapes || !inner.Exists || inner.ScanPath != filepath.Join(dir, "actual") {
		t.Errorf("internal link = %+v", inner)
	}
}

func TestMembers_RejectsInvalidFilePath(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.code-workspace")
	writeCodeWorkspace(t, outside, "missing")
	symlink(t, outside, filepath.Join(dir, "linked.code-workspace"))
	members, issues := Members(dir, "linked.code-workspace")
	if len(members) != 0 || !reflect.DeepEqual(issues, []Diagnostic{{File: "linked.code-workspace", Problem: Unreadable}}) {
		t.Fatalf("members = %+v, diagnostics = %+v", members, issues)
	}
}

func TestStripJSONC_KeepsStrings(t *testing.T) {
	in := `{"a": "http://x/*y*/", // c
"b": [1,2,], /* d */ "c": "q\"//"}`
	var out map[string]any
	if err := json.Unmarshal(stripJSONC([]byte(in)), &out); err != nil {
		t.Fatalf("Unmarshal: %v (%s)", err, stripJSONC([]byte(in)))
	}
	if out["a"] != "http://x/*y*/" || out["c"] != `q"//` {
		t.Errorf("strings changed: %v", out)
	}
}
