package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// writeFile is a tiny helper for the discover tests.
func writeFile(t *testing.T, p string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// makeRepoDir creates a directory tree that mirrors what
// status.ResolveRepoPath would compute for the given source/repo, so that
// discovery's clone index can find a real on-disk path.
func makeRepoDir(t *testing.T, cfg *config.Config, srcKey, repoKey string) string {
	t.Helper()
	src := cfg.Sources[srcKey]
	folder := src.EffectiveFolder(srcKey)
	parts := []string{cfg.Global.Folder, folder}
	if i := indexByte(repoKey, '/'); i >= 0 {
		parts = append(parts, repoKey[:i], repoKey[i+1:])
	} else {
		parts = append(parts, repoKey)
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

func TestDiscover_CodeWorkspaceHappyPath(t *testing.T) {
	cfg := newTestConfig(t)

	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")
	backend := makeRepoDir(t, cfg, "github-test", "team/backend")

	wsFile := filepath.Join(cfg.Global.Folder, "feature-x.code-workspace")
	body := struct {
		Folders []map[string]string `json:"folders"`
	}{
		Folders: []map[string]string{
			{"path": frontend},
			{"path": backend},
		},
	}
	buf, _ := json.MarshalIndent(body, "", "  ")
	writeFile(t, wsFile, string(buf))

	result, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.New) != 1 {
		t.Fatalf("New = %d, want 1 (skipped=%d ambig=%d)", len(result.New), len(result.Skipped), len(result.Ambiguous))
	}
	d := result.New[0]
	if d.Key != "feature-x" || d.Type != config.WorkspaceTypeCode {
		t.Errorf("got Key=%q Type=%q, want feature-x/codeWorkspace", d.Key, d.Type)
	}
	if len(d.Members) != 2 {
		t.Errorf("members = %d, want 2", len(d.Members))
	}

	if adopted := AdoptDiscovered(cfg, result); len(adopted) != 1 || adopted[0] != "feature-x" {
		t.Errorf("adopted = %v, want [feature-x]", adopted)
	}
	if w, ok := cfg.Workspaces["feature-x"]; !ok {
		t.Fatal("workspace not added to cfg")
	} else {
		if !w.Discovered {
			t.Error("Discovered should be true on adopted workspace")
		}
		if len(w.Members) != 2 {
			t.Errorf("adopted members = %d, want 2", len(w.Members))
		}
	}
}

func TestDiscover_KeyClashSkipped(t *testing.T) {
	cfg := newTestConfig(t)
	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")

	wsFile := filepath.Join(cfg.Global.Folder, "existing.code-workspace")
	writeFile(t, wsFile, `{"folders":[{"path":"`+filepath.ToSlash(frontend)+`"}]}`)

	cfg.Workspaces = map[string]config.Workspace{
		"existing": {
			Type:    config.WorkspaceTypeCode,
			Members: []config.WorkspaceMember{{Source: "github-test", Repo: "team/frontend"}},
			File:    "/some/other/path/existing.code-workspace",
		},
	}

	result, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.New) != 0 {
		t.Errorf("New = %d, want 0", len(result.New))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("Skipped = %d, want 1", len(result.Skipped))
	}
	if result.Skipped[0].Skipped == "" {
		t.Error("Skipped reason not set")
	}
}

func TestDiscover_NoMembersSkipped(t *testing.T) {
	cfg := newTestConfig(t)

	wsFile := filepath.Join(cfg.Global.Folder, "stranger.code-workspace")
	writeFile(t, wsFile, `{"folders":[{"path":"/nonexistent/path/repo"}]}`)

	result, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.New) != 0 {
		t.Errorf("New = %d, want 0", len(result.New))
	}
	if len(result.Skipped) != 1 {
		t.Fatalf("Skipped = %d, want 1", len(result.Skipped))
	}
	if len(result.Skipped[0].NoMatch) == 0 {
		t.Error("NoMatch should list the unresolved path")
	}
}

func TestDiscover_AmbiguousMember(t *testing.T) {
	cfg := newTestConfig(t)
	// Force two sources to point at the same effective folder to manufacture
	// an ambiguous resolve for the same repo path.
	cfg.Sources = map[string]config.Source{
		"github-test": {
			Account: "github-test",
			Folder:  "shared",
			Repos:   map[string]config.Repo{"team/frontend": {}},
		},
		"forgejo-test": {
			Account: "forgejo-test",
			Folder:  "shared",
			Repos:   map[string]config.Repo{"team/frontend": {}},
		},
	}

	dir := filepath.Join(cfg.Global.Folder, "shared", "team", "frontend")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	wsFile := filepath.Join(cfg.Global.Folder, "ambig.code-workspace")
	writeFile(t, wsFile, `{"folders":[{"path":"`+filepath.ToSlash(dir)+`"}]}`)

	result, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.Ambiguous) != 1 {
		t.Fatalf("Ambiguous = %d, want 1 (new=%d skipped=%d)", len(result.Ambiguous), len(result.New), len(result.Skipped))
	}
	if len(result.Ambiguous[0].Ambig) != 1 {
		t.Errorf("Ambig paths = %d, want 1", len(result.Ambiguous[0].Ambig))
	}
}

func TestDiscover_TmuxinatorWindowsPerRepo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local-side tmuxinator path isn't relevant on Windows")
	}
	cfg := newTestConfig(t)
	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")
	backend := makeRepoDir(t, cfg, "github-test", "team/backend")

	// Point HOME at a temp dir so we can stage a .tmuxinator file deterministically.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	yml := "name: feat-x\nroot: ~/\n\nwindows:\n" +
		"  - frontend:\n      root: " + frontend + "\n      panes:\n        - null\n" +
		"  - backend:\n      root: " + backend + "\n      panes:\n        - null\n"
	writeFile(t, filepath.Join(tmpHome, ".tmuxinator", "feat-x.yml"), yml)

	result, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.New) != 1 {
		t.Fatalf("New = %d, want 1", len(result.New))
	}
	d := result.New[0]
	if d.Type != config.WorkspaceTypeTmuxinator {
		t.Errorf("type = %q, want tmuxinator", d.Type)
	}
	if d.Layout != config.WorkspaceLayoutWindows {
		t.Errorf("layout = %q, want windowsPerRepo", d.Layout)
	}
	if len(d.Members) != 2 {
		t.Errorf("members = %d, want 2", len(d.Members))
	}
}

func TestDiscover_TmuxinatorSplitPanes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local-side tmuxinator path isn't relevant on Windows")
	}
	cfg := newTestConfig(t)
	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	yml := "name: split-x\nroot: ~/\n\nwindows:\n  - all:\n      layout: tiled\n      panes:\n        - cd " + frontend + "\n"
	writeFile(t, filepath.Join(tmpHome, ".tmuxinator", "split-x.yml"), yml)

	result, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.New) != 1 {
		t.Fatalf("New = %d (skipped=%d), want 1", len(result.New), len(result.Skipped))
	}
	d := result.New[0]
	if d.Layout != config.WorkspaceLayoutSplit {
		t.Errorf("layout = %q, want splitPanes", d.Layout)
	}
}

func TestDiscover_DuplicateFolderEntriesDeduped(t *testing.T) {
	cfg := newTestConfig(t)
	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")

	wsFile := filepath.Join(cfg.Global.Folder, "dups.code-workspace")
	body := `{"folders":[
		{"path":"` + filepath.ToSlash(frontend) + `"},
		{"path":"` + filepath.ToSlash(frontend) + `"}
	]}`
	writeFile(t, wsFile, body)

	result, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(result.New) != 1 {
		t.Fatalf("New = %d, want 1", len(result.New))
	}
	if got := len(result.New[0].Members); got != 1 {
		t.Errorf("members = %d, want 1 (duplicates should be collapsed)", got)
	}
}

func TestDiscover_DuplicateFileSkipped(t *testing.T) {
	cfg := newTestConfig(t)
	frontend := makeRepoDir(t, cfg, "github-test", "team/frontend")

	wsFile := filepath.Join(cfg.Global.Folder, "dup.code-workspace")
	writeFile(t, wsFile, `{"folders":[{"path":"`+filepath.ToSlash(frontend)+`"}]}`)

	cfg.Workspaces = map[string]config.Workspace{
		"already-known": {
			Type:    config.WorkspaceTypeCode,
			File:    wsFile, // same on-disk file, different key
			Members: []config.WorkspaceMember{{Source: "github-test", Repo: "team/frontend"}},
		},
	}

	result, err := Discover(cfg)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// File is new (different key) so it lands in New, but AdoptDiscovered
	// must skip because the file is already tracked.
	if len(result.New) != 1 {
		t.Fatalf("New = %d, want 1", len(result.New))
	}
	adopted := AdoptDiscovered(cfg, result)
	if len(adopted) != 0 {
		t.Errorf("adopted = %v, want []", adopted)
	}
}
