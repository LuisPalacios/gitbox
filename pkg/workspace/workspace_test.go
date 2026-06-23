package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// newTestConfig builds a minimal config with two sources under a known global
// folder, so path resolution is deterministic.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	return &config.Config{
		Version: 2,
		Global:  config.GlobalConfig{Folder: root},
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
	parts := []string{cfg.Global.Folder, folder}
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

func TestDiscover_CodeWorkspaceResolvesMembers(t *testing.T) {
	cfg := newTestConfig(t)
	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")
	backend := makeRepoDir(t, cfg, "github-test", "team/backend")

	wsFile := filepath.Join(cfg.Global.Folder, "feature-x.code-workspace")
	writeCodeWorkspace(t, wsFile, frontend, backend)

	found, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("found = %d, want 1", len(found))
	}
	if found[0].Key != "feature-x" {
		t.Errorf("key = %q, want feature-x", found[0].Key)
	}
	if len(found[0].Members) != 2 {
		t.Errorf("members = %d, want 2", len(found[0].Members))
	}
}

func TestRefreshCache_PopulatesAndDetectsChange(t *testing.T) {
	cfg := newTestConfig(t)
	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")
	wsFile := filepath.Join(cfg.Global.Folder, "feat.code-workspace")
	writeCodeWorkspace(t, wsFile, frontend)

	changed, err := RefreshCache(cfg)
	if err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	if !changed {
		t.Fatal("first refresh should report changed")
	}
	w, ok := cfg.Workspaces["feat"]
	if !ok {
		t.Fatal("cache missing feat workspace")
	}
	if !w.Discovered || w.File != wsFile {
		t.Errorf("workspace = %+v, want Discovered + File=%s", w, wsFile)
	}

	// Second refresh with nothing changed must report no change.
	changed, err = RefreshCache(cfg)
	if err != nil {
		t.Fatalf("RefreshCache 2: %v", err)
	}
	if changed {
		t.Error("second refresh should report no change")
	}

	// Removing the file on disk should drop it from the cache (changed=true).
	if err := os.Remove(wsFile); err != nil {
		t.Fatalf("remove: %v", err)
	}
	changed, _ = RefreshCache(cfg)
	if !changed || len(cfg.Workspaces) != 0 {
		t.Errorf("after delete: changed=%v workspaces=%d, want true/0", changed, len(cfg.Workspaces))
	}
}

func TestDiscover_ScansExtraFolders(t *testing.T) {
	cfg := newTestConfig(t)
	extra := t.TempDir()
	cfg.Global.ExtraFolders = []string{extra}
	wsFile := filepath.Join(extra, "side.code-workspace")
	writeCodeWorkspace(t, wsFile) // no resolvable members, still discovered

	found, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(found) != 1 || found[0].Key != "side" {
		t.Fatalf("found = %+v, want one 'side' from the extra folder", found)
	}
}

func TestTentativeContainers_FlagsManagedCloneWithWorkspaceFile(t *testing.T) {
	cfg := newTestConfig(t)
	// Make the frontend clone a container candidate by dropping a
	// .code-workspace at its root.
	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")
	writeCodeWorkspace(t, filepath.Join(frontend, "team.code-workspace"))

	got := TentativeContainers(cfg)
	if len(got) != 1 || got[0].Source != "github-test" || got[0].Repo != "team/frontend" {
		t.Fatalf("tentative = %+v, want github-test/team/frontend", got)
	}

	// Once flagged a container, it's no longer tentative.
	src := cfg.Sources["github-test"]
	repo := src.Repos["team/frontend"]
	repo.Container = true
	src.Repos["team/frontend"] = repo
	cfg.Sources["github-test"] = src
	if got := TentativeContainers(cfg); len(got) != 0 {
		t.Errorf("after flagging: tentative = %+v, want none", got)
	}
}

func TestUniqueKey_DisambiguatesCollisions(t *testing.T) {
	used := map[string]bool{}
	if k := uniqueKey("proj", used); k != "proj" {
		t.Errorf("first = %q, want proj", k)
	}
	if k := uniqueKey("proj", used); k != "proj-2" {
		t.Errorf("second = %q, want proj-2", k)
	}
	if k := uniqueKey("proj", used); k != "proj-3" {
		t.Errorf("third = %q, want proj-3", k)
	}
}
