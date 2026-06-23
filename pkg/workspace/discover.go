package workspace

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// Discover scans the gitbox standard folder and configured extra folders for
// *.code-workspace files and resolves each file's folder references back to
// known clones. It never mutates cfg. Keys are the filename stem, de-duplicated
// deterministically (sorted by file path) so two same-named files don't clash.
func Discover(cfg *config.Config) ([]Found, error) {
	cloneIdx := buildCloneIndex(cfg)

	files, err := findCodeWorkspaces(scanRoots(cfg))
	if err != nil {
		return nil, fmt.Errorf("scanning .code-workspace files: %w", err)
	}

	usedKeys := make(map[string]bool)
	out := make([]Found, 0, len(files))
	for _, f := range files {
		members, parseErr := parseCodeWorkspaceMembers(f, cloneIdx)
		if parseErr != nil {
			continue
		}
		stem := workspaceKeyFromFile(f, ".code-workspace")
		key := uniqueKey(stem, usedKeys)
		out = append(out, Found{
			Key:     key,
			Name:    stem,
			File:    f,
			Members: members,
		})
	}
	return out, nil
}

// scanRoots returns the directories scanned for .code-workspace files: the
// standard folder plus any configured extra folders, tilde-expanded.
func scanRoots(cfg *config.Config) []string {
	roots := []string{config.ExpandTilde(cfg.Global.Folder)}
	for _, p := range cfg.Global.ExtraFolders {
		if p = strings.TrimSpace(p); p != "" {
			roots = append(roots, config.ExpandTilde(p))
		}
	}
	return roots
}

// uniqueKey returns stem if unused, otherwise stem-2, stem-3, … recording the
// chosen key as used.
func uniqueKey(stem string, used map[string]bool) string {
	key := stem
	for i := 2; used[key]; i++ {
		key = fmt.Sprintf("%s-%d", stem, i)
	}
	used[key] = true
	return key
}

// ─── code-workspace scanning ─────────────────────────────────

// findCodeWorkspaces walks each root and returns every *.code-workspace file
// outside hidden directories. Results are de-duplicated and sorted by path.
func findCodeWorkspaces(roots []string) ([]string, error) {
	seen := make(map[string]bool)
	var out []string
	for _, root := range roots {
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if strings.HasPrefix(name, ".") && name != "." {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(strings.ToLower(d.Name()), ".code-workspace") {
				abs, _ := filepath.Abs(p)
				if !seen[normPath(abs)] {
					seen[normPath(abs)] = true
					out = append(out, abs)
				}
			}
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	sort.Strings(out)
	return out, nil
}

type rawCodeWorkspace struct {
	Folders []struct {
		Path string `json:"path"`
		Name string `json:"name"`
	} `json:"folders"`
}

// parseCodeWorkspaceMembers reads a .code-workspace file and resolves each
// folder reference to a known clone. Unresolved or ambiguous folders are
// silently dropped — the cache only lists members it can map confidently.
func parseCodeWorkspaceMembers(file string, idx cloneIndex) ([]config.WorkspaceMember, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var raw rawCodeWorkspace
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	root := filepath.Dir(file)
	var members []config.WorkspaceMember
	for _, f := range raw.Folders {
		abs := f.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		abs = filepath.Clean(abs)
		m, ambig := idx.resolve(abs)
		if !ambig && len(m) == 1 {
			members = append(members, m[0])
		}
	}
	return dedupMembers(members), nil
}

// ─── clone index ─────────────────────────────────────────────

// cloneIndex maps normalized clone paths to their (source, repo) keys.
type cloneIndex struct {
	byPath map[string][]config.WorkspaceMember
	paths  []string // normalized paths, sorted longest-first for prefix scans
}

func buildCloneIndex(cfg *config.Config) cloneIndex {
	idx := cloneIndex{byPath: make(map[string][]config.WorkspaceMember)}
	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		sourceFolder := src.EffectiveFolder(srcKey)
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			p := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			n := normPath(p)
			idx.byPath[n] = append(idx.byPath[n], config.WorkspaceMember{Source: srcKey, Repo: repoKey})
		}
	}
	for k := range idx.byPath {
		idx.paths = append(idx.paths, k)
	}
	sort.Slice(idx.paths, func(i, j int) bool { return len(idx.paths[i]) > len(idx.paths[j]) })
	return idx
}

// resolve picks the best (source, repo) match for a discovered path. Exact
// match wins; otherwise the deepest configured clone path that contains the
// discovered path wins. Returns ambiguous when multiple candidates tie.
func (idx cloneIndex) resolve(p string) (members []config.WorkspaceMember, ambiguous bool) {
	n := normPath(p)
	if cands, ok := idx.byPath[n]; ok {
		return cands, len(cands) != 1
	}
	for _, candPath := range idx.paths {
		if strings.HasPrefix(n, candPath+"/") {
			cands := idx.byPath[candPath]
			return cands, len(cands) != 1
		}
	}
	return nil, false
}

// ─── helpers ─────────────────────────────────────────────────

// dedupMembers preserves member order while collapsing duplicates by
// (source, repo). Hand-edited workspace files frequently list the same folder
// twice; without this the cache would carry the duplicate.
func dedupMembers(members []config.WorkspaceMember) []config.WorkspaceMember {
	if len(members) < 2 {
		return members
	}
	seen := make(map[config.WorkspaceMember]bool, len(members))
	out := members[:0]
	for _, m := range members {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// workspaceKeyFromFile derives a stable workspace key from a file name by
// stripping the supplied suffix(es) (case-insensitive) and turning whitespace
// into dashes.
func workspaceKeyFromFile(file string, suffixes ...string) string {
	base := filepath.Base(file)
	low := strings.ToLower(base)
	for _, s := range suffixes {
		if strings.HasSuffix(low, s) {
			base = base[:len(base)-len(s)]
			break
		}
	}
	base = strings.TrimSpace(base)
	base = strings.ReplaceAll(base, " ", "-")
	return base
}

// parentDir returns the directory containing file.
func parentDir(file string) string {
	return filepath.Dir(file)
}

// normPath normalises an absolute path for comparison: clean, lowercase, slash
// separator. Mirrors pkg/adopt.normPath so case-insensitive filesystems work.
func normPath(p string) string {
	p = filepath.Clean(p)
	return strings.ToLower(filepath.ToSlash(p))
}
