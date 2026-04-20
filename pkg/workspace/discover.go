package workspace

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// Discovered describes one workspace artifact found on disk that is not yet
// recorded in cfg.Workspaces. The resolution buckets (Members / Ambiguous /
// Unmatched) split the parsed folder paths by how cleanly they map back to
// known clones. Adoption only writes Members.
type Discovered struct {
	Key      string                   // suggested workspace key (filename stem, sanitised)
	Type     string                   // config.WorkspaceTypeCode | WorkspaceTypeTmuxinator
	Layout   string                   // tmuxinator only; "" for codeWorkspace
	File     string                   // absolute file path on disk
	Members  []config.WorkspaceMember // members that resolved to a single clone
	Ambig    []DiscoveredPath         // paths that matched ≥2 clones
	NoMatch  []string                 // paths that matched no clone
	Skipped  string                   // non-empty when the file is intentionally not adopted (e.g. key clash)
}

// DiscoveredPath captures one parsed folder reference plus the clone keys that
// tied for the closest match. Used for the Ambiguous bucket so the UI can
// surface what's wrong without re-walking.
type DiscoveredPath struct {
	Path       string
	Candidates []config.WorkspaceMember
}

// DiscoverResult bundles every artifact found in one pass.
//
// New: ready to adopt — at least one resolved member, no key clash with an
// existing entry. AdoptDiscovered writes these.
//
// Ambiguous: the file parsed but at least one member tied between ≥2 clones.
// Surfaced to the UI; never auto-adopted.
//
// Skipped: parsed but cannot be adopted (key clash with an existing workspace,
// zero resolved members, etc.) — kept for telemetry / UI.
type DiscoverResult struct {
	New       []Discovered
	Ambiguous []Discovered
	Skipped   []Discovered
}

// Discover walks the gitbox-managed folder for *.code-workspace files and the
// tmuxinator YAML directory (and, on Windows with WSL available, the WSL-side
// equivalent), parsing each one into a Discovered and bucketing it by
// adoptability. The function never mutates cfg; AdoptDiscovered does that.
func Discover(cfg *config.Config) (DiscoverResult, error) {
	var result DiscoverResult

	cloneIdx := buildCloneIndex(cfg)

	codeFiles, err := findCodeWorkspaces(config.ExpandTilde(cfg.Global.Folder))
	if err != nil {
		return result, fmt.Errorf("scanning .code-workspace files: %w", err)
	}
	for _, f := range codeFiles {
		d, parseErr := parseCodeWorkspace(f, cloneIdx)
		if parseErr != nil {
			continue
		}
		bucketDiscovered(cfg, d, &result)
	}

	tmuxFiles, err := findTmuxinatorFiles()
	if err == nil {
		for _, f := range tmuxFiles {
			d, parseErr := parseTmuxinator(f, cloneIdx, false)
			if parseErr != nil {
				continue
			}
			bucketDiscovered(cfg, d, &result)
		}
	}

	if runtime.GOOS == "windows" && git.IsWSLAvailable() {
		wslFiles, _ := findWSLTmuxinatorFiles()
		for _, f := range wslFiles {
			d, parseErr := parseTmuxinator(f, cloneIdx, true)
			if parseErr != nil {
				continue
			}
			bucketDiscovered(cfg, d, &result)
		}
	}

	sort.Slice(result.New, func(i, j int) bool { return result.New[i].Key < result.New[j].Key })
	sort.Slice(result.Ambiguous, func(i, j int) bool { return result.Ambiguous[i].Key < result.Ambiguous[j].Key })
	sort.Slice(result.Skipped, func(i, j int) bool { return result.Skipped[i].Key < result.Skipped[j].Key })

	return result, nil
}

// AdoptDiscovered writes every entry in result.New to cfg.Workspaces with
// Discovered: true. Returns the keys actually adopted (may be empty if cfg
// already has matching entries by file path). Existing keys are preserved
// untouched. The caller is responsible for persisting cfg.
func AdoptDiscovered(cfg *config.Config, result DiscoverResult) []string {
	if cfg.Workspaces == nil {
		cfg.Workspaces = make(map[string]config.Workspace)
	}
	existingFiles := make(map[string]bool, len(cfg.Workspaces))
	for _, w := range cfg.Workspaces {
		if w.File != "" {
			existingFiles[normPath(w.File)] = true
		}
	}

	var adopted []string
	for _, d := range result.New {
		if _, clash := cfg.Workspaces[d.Key]; clash {
			continue
		}
		if existingFiles[normPath(d.File)] {
			continue
		}
		w := config.Workspace{
			Type:       d.Type,
			Name:       d.Key,
			File:       d.File,
			Layout:     d.Layout,
			Members:    append([]config.WorkspaceMember(nil), d.Members...),
			Discovered: true,
		}
		if err := cfg.AddWorkspace(d.Key, w); err != nil {
			continue
		}
		cfg.WorkspaceOrder = append(cfg.WorkspaceOrder, d.Key)
		adopted = append(adopted, d.Key)
	}
	return adopted
}

// ─── Internals ────────────────────────────────────────────────

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
// discovered path wins. Returns ambiguous when multiple candidates tie at the
// best depth.
func (idx cloneIndex) resolve(p string) (members []config.WorkspaceMember, ambiguous bool) {
	n := normPath(p)
	if cands, ok := idx.byPath[n]; ok {
		if len(cands) == 1 {
			return cands, false
		}
		return cands, true
	}
	// Prefix scan, longest first.
	for _, candPath := range idx.paths {
		if strings.HasPrefix(n, candPath+"/") {
			cands := idx.byPath[candPath]
			if len(cands) == 1 {
				return cands, false
			}
			return cands, true
		}
	}
	return nil, false
}

// ─── code-workspace discovery ────────────────────────────────

// findCodeWorkspaces walks root and returns every *.code-workspace file
// outside hidden directories or git repos.
func findCodeWorkspaces(root string) ([]string, error) {
	if root == "" {
		return nil, nil
	}
	if _, err := os.Stat(root); err != nil {
		return nil, nil
	}
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			// Skip the inside of any git repo — workspace files at the repo
			// root are still picked up because they're seen as siblings of
			// the .git directory before we descend.
			if _, statErr := os.Stat(filepath.Join(p, ".git")); statErr == nil {
				// Still scan this dir's immediate entries (the workspace file
				// might be at the repo root) — handled by WalkDir naturally
				// because it visits files under p before SkipDir.
				return nil
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".code-workspace") {
			abs, _ := filepath.Abs(p)
			out = append(out, abs)
		}
		return nil
	})
	if err != nil {
		return out, err
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

func parseCodeWorkspace(file string, idx cloneIndex) (Discovered, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Discovered{}, err
	}
	var raw rawCodeWorkspace
	if err := json.Unmarshal(data, &raw); err != nil {
		return Discovered{}, err
	}
	d := Discovered{
		Key:  workspaceKeyFromFile(file, ".code-workspace"),
		Type: config.WorkspaceTypeCode,
		File: file,
	}
	root := filepath.Dir(file)
	for _, f := range raw.Folders {
		abs := f.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, abs)
		}
		abs = filepath.Clean(abs)
		members, ambig := idx.resolve(abs)
		switch {
		case ambig:
			d.Ambig = append(d.Ambig, DiscoveredPath{Path: abs, Candidates: members})
		case len(members) == 1:
			d.Members = append(d.Members, members[0])
		default:
			d.NoMatch = append(d.NoMatch, abs)
		}
	}
	d.Members = dedupMembers(d.Members)
	return d, nil
}

// ─── tmuxinator discovery ────────────────────────────────────

func findTmuxinatorFiles() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".tmuxinator")
	return listYAML(dir)
}

func findWSLTmuxinatorFiles() ([]string, error) {
	wslHome, err := git.WSLHome()
	if err != nil {
		return nil, err
	}
	winDir, err := git.WSLPath(path.Join(wslHome, ".tmuxinator"))
	if err != nil {
		return nil, err
	}
	return listYAML(winDir)
}

func listYAML(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") {
			abs, _ := filepath.Abs(filepath.Join(dir, e.Name()))
			out = append(out, abs)
		}
	}
	sort.Strings(out)
	return out, nil
}

var (
	tmuxRootLine = regexp.MustCompile(`(?m)^\s+root:\s*(.+?)\s*$`)
	tmuxCdLine   = regexp.MustCompile(`(?m)^\s*-\s+cd\s+(.+?)\s*$`)
)

// parseTmuxinator extracts member paths from a tmuxinator YAML. fromWSL
// signals that paths inside the YAML are Linux-style and need translating
// back to Windows for cross-referencing against the cloneIndex.
func parseTmuxinator(file string, idx cloneIndex, fromWSL bool) (Discovered, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Discovered{}, err
	}
	d := Discovered{
		Key:    workspaceKeyFromFile(file, ".yml", ".yaml"),
		Type:   config.WorkspaceTypeTmuxinator,
		Layout: config.WorkspaceLayoutWindows,
		File:   file,
	}

	text := string(data)
	var paths []string
	seen := map[string]bool{}

	for _, m := range tmuxRootLine.FindAllStringSubmatch(text, -1) {
		raw := stripYAMLScalar(m[1])
		// Skip the workspace-level `root:` (typically `~/`); we only want
		// per-window roots.
		if raw == "" || raw == "~/" || raw == "~" {
			continue
		}
		paths = appendUnique(paths, seen, raw)
	}
	if len(paths) == 0 {
		// splitPanes layout: no per-window root, only `- cd <path>` lines.
		d.Layout = config.WorkspaceLayoutSplit
		for _, m := range tmuxCdLine.FindAllStringSubmatch(text, -1) {
			raw := stripYAMLScalar(m[1])
			if raw == "" {
				continue
			}
			paths = appendUnique(paths, seen, raw)
		}
	}

	for _, p := range paths {
		abs := expandHomeForTmuxinator(p, fromWSL)
		if fromWSL && runtime.GOOS == "windows" {
			if win, err := git.WSLPath(abs); err == nil {
				abs = win
			}
		}
		abs = filepath.Clean(abs)
		members, ambig := idx.resolve(abs)
		switch {
		case ambig:
			d.Ambig = append(d.Ambig, DiscoveredPath{Path: abs, Candidates: members})
		case len(members) == 1:
			d.Members = append(d.Members, members[0])
		default:
			d.NoMatch = append(d.NoMatch, abs)
		}
	}
	d.Members = dedupMembers(d.Members)

	return d, nil
}

func expandHomeForTmuxinator(p string, fromWSL bool) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	if fromWSL {
		if home, err := git.WSLHome(); err == nil {
			return path.Join(home, strings.TrimPrefix(p, "~"))
		}
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// ─── shared helpers ──────────────────────────────────────────

func bucketDiscovered(cfg *config.Config, d Discovered, into *DiscoverResult) {
	if _, clash := cfg.Workspaces[d.Key]; clash {
		d.Skipped = "key already exists in config"
		into.Skipped = append(into.Skipped, d)
		return
	}
	if len(d.Ambig) > 0 {
		into.Ambiguous = append(into.Ambiguous, d)
		return
	}
	if len(d.Members) == 0 {
		d.Skipped = "no members resolved to known clones"
		into.Skipped = append(into.Skipped, d)
		return
	}
	into.New = append(into.New, d)
}

// dedupMembers preserves member order while collapsing duplicates by
// (source, repo). Hand-edited workspace files frequently list the same
// folder twice; without this, adoption would propagate the duplicate.
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

func appendUnique(out []string, seen map[string]bool, s string) []string {
	if seen[s] {
		return out
	}
	seen[s] = true
	return append(out, s)
}

// stripYAMLScalar removes surrounding quotes from a parsed YAML scalar.
func stripYAMLScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	return s
}

// workspaceKeyFromFile derives a stable workspace key from a file name by
// stripping the supplied suffix(es) (case-insensitive) and sanitising
// whitespace into dashes.
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

// normPath normalises an absolute path for comparison: clean, lowercase, slash
// separator. Mirrors pkg/adopt.normPath behaviour so case-insensitive
// filesystems work transparently.
func normPath(p string) string {
	p = filepath.Clean(p)
	return strings.ToLower(filepath.ToSlash(p))
}
