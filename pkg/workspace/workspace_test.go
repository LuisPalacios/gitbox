package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
	if got, want := RootFiles(dir), []string{"A.Code-Workspace", "b.code-workspace"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RootFiles = %v, want %v", got, want)
	}
	if got := RootFiles(filepath.Join(dir, "missing")); got != nil {
		t.Errorf("RootFiles(missing) = %v, want nil", got)
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
	if !Apply(cfg, res.Changes) {
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
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	writeFile(t, filepath.Join(dir, "a.code-workspace"), "{}")
	writeFile(t, filepath.Join(dir, "b.code-workspace"), "{}")

	res := Sync(cfg)
	want := []Choice{{Source: "github-test", Repo: "team/frontend", Files: []string{"a.code-workspace", "b.code-workspace"}}}
	if !reflect.DeepEqual(res.Choices, want) {
		t.Fatalf("Choices = %+v, want %+v", res.Choices, want)
	}
	Apply(cfg, res.Changes)
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
	Apply(cfg, res.Changes)
	if r := repoOf(cfg, "github-test", "team/frontend"); r.Workspace != "" {
		t.Errorf("stale pick kept: %+v", r)
	}
}

func TestSync_FileGoneClearsPickKeepsContainer(t *testing.T) {
	cfg := newTestConfig(t)
	makeRepoDir(t, cfg, "github-test", "team/frontend")
	src := cfg.Sources["github-test"]
	src.Repos["team/frontend"] = config.Repo{Container: true, Workspace: "gone.code-workspace"}

	Apply(cfg, Sync(cfg).Changes)
	if r := repoOf(cfg, "github-test", "team/frontend"); !r.Container || r.Workspace != "" {
		t.Errorf("repo = %+v, want container without a workspace", r)
	}
}

func TestSync_SkipsClonesNotOnDisk(t *testing.T) {
	cfg := newTestConfig(t)
	src := cfg.Sources["github-test"]
	src.Repos["team/frontend"] = config.Repo{Container: true, Workspace: "w.code-workspace"}
	if res := Sync(cfg); len(res.Changes) != 0 {
		t.Errorf("Sync = %+v, want a missing clone left alone", res)
	}
}

func TestApply_SkipsRemovedRepos(t *testing.T) {
	cfg := newTestConfig(t)
	if Apply(cfg, []Change{{Source: "github-test", Repo: "gone/repo", Container: true}, {Source: "nope", Repo: "x"}}) {
		t.Error("Apply changed a repo that is not in the config")
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

	got := Placeholders(cfg)
	parent := status.RepoRef{Source: "github-test", Repo: "team/frontend"}
	want := []Placeholder{
		{Parent: parent, Name: "Missing lib", Path: "libs/missing"},
		{Parent: parent, Name: "other", Path: "libs/other"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Placeholders = %+v, want %+v", got, want)
	}
}

func TestPlaceholders_IgnoresContainersWithoutWorkspace(t *testing.T) {
	cfg := newTestConfig(t)
	dir := makeRepoDir(t, cfg, "github-test", "team/frontend")
	writeCodeWorkspace(t, filepath.Join(dir, "w.code-workspace"), "missing")
	src := cfg.Sources["github-test"]
	src.Repos["team/frontend"] = config.Repo{Container: true}
	if got := Placeholders(cfg); len(got) != 0 {
		t.Errorf("Placeholders = %+v, want none without a picked workspace", got)
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
