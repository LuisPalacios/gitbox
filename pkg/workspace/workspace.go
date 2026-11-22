// Package workspace ties VS Code .code-workspace files to the clones that hold
// them. A clone with a workspace file at its root is a container: Sync flags
// it and records the file in repo.workspace, asking me to choose when the
// root holds several. Placeholders lists the folders a container's workspace
// names inside it that are not cloned yet. gitbox never creates, edits or
// deletes workspace files — I own them.
package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// Ext is the VS Code workspace file extension.
const Ext = ".code-workspace"

// Change records what the scan read and the workspace values it proposes.
type Change struct {
	Source, Repo            string
	Path                    string
	OldContainer, Container bool
	OldWorkspace, Workspace string
	OldPreferred            string
	preferredMissing        bool
}

// Unknown records a clone whose root could not be read.
type Unknown struct {
	Source, Repo string
	Path         string
	Err          error
}

// Choice is a container whose root holds several workspace files and no
// valid pick: I have to choose one.
type Choice struct {
	Source, Repo string
	Files        []string
}

// Result is what one Sync pass found.
type Result struct {
	Changes []Change
	Choices []Choice
	Unknown []Unknown
}

// Placeholder is a folder a container's workspace file lists inside the
// container that is not on disk and that no configured repo resolves to.
type Placeholder struct {
	Parent status.RepoRef
	Name   string // the folder's name in the workspace file, else its base name
	Path   string // relative to the container, slash-separated
}

// RootFiles returns the workspace file names directly in dir, sorted. It
// does not recurse.
func RootFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), Ext) {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out, nil
}

// Sync compares every clone on disk with the workspace files at its root.
// It reads disk but never changes cfg; Apply writes the changes.
//   - One file: the repo becomes a container that opens it.
//   - Several: the repo becomes a container that opens my preferred file
//     (repo.workspace_preferred) when it is there, else keeps its current
//     file while it exists, else clears it and reports a Choice.
//   - None: a stale file is cleared. The container flag stays, so a
//     container I flagged by hand keeps working.
//
// Failed reads preserve the current file and preference, including when
// the root of a clone with a recorded workspace is offline.
func Sync(cfg *config.Config) Result {
	var res Result
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			dir := status.RepoPath(cfg, srcKey, repoKey, repo)
			files, err := RootFiles(dir)
			if err != nil {
				if repo.Workspace != "" || repo.WorkspacePreferred != "" {
					res.Unknown = append(res.Unknown, Unknown{srcKey, repoKey, dir, err})
				}
				continue
			}
			container, ws := repo.Container, repo.Workspace
			choose := false
			switch {
			case len(files) == 1:
				container, ws = true, files[0]
			case len(files) > 1:
				container = true
				switch {
				case slices.Contains(files, repo.WorkspacePreferred):
					ws = repo.WorkspacePreferred
				case !slices.Contains(files, ws):
					ws, choose = "", true
					res.Choices = append(res.Choices, Choice{Source: srcKey, Repo: repoKey, Files: files})
				}
			default:
				ws = ""
			}
			preferredMissing := repo.WorkspacePreferred != "" && !slices.Contains(files, repo.WorkspacePreferred)
			if container != repo.Container || ws != repo.Workspace || preferredMissing || choose {
				res.Changes = append(res.Changes, Change{
					Source: srcKey, Repo: repoKey, Path: dir,
					OldContainer: repo.Container, Container: container,
					OldWorkspace: repo.Workspace, Workspace: ws,
					OldPreferred: repo.WorkspacePreferred, preferredMissing: preferredMissing,
				})
			}
		}
	}
	return res
}

type absence struct {
	path string
	file string
}

// SyncState remembers first confirmed absences across passes in this session.
// Its zero value is ready to use. The caller serializes calls to Apply.
type SyncState struct {
	absent map[status.RepoRef]absence
}

// Apply writes a change only while the clone and workspace values it read
// still match. A second consecutive successful read of the same root can
// clear an absent preference. The caller saves changes and retries skips.
func (s *SyncState) Apply(cfg *config.Config, changes []Change) (changed bool, skipped []status.RepoRef) {
	var next map[status.RepoRef]absence
	for _, c := range changes {
		src := cfg.Sources[c.Source]
		repo, ok := src.Repos[c.Repo]
		ref := status.RepoRef{Source: c.Source, Repo: c.Repo}
		if !ok || repo.Container != c.OldContainer || repo.Workspace != c.OldWorkspace ||
			repo.WorkspacePreferred != c.OldPreferred || status.RepoPath(cfg, c.Source, c.Repo, repo) != c.Path {
			skipped = append(skipped, ref)
			continue
		}
		preferred := repo.WorkspacePreferred
		if c.preferredMissing {
			miss := absence{path: c.Path, file: preferred}
			if s.absent[ref] == miss {
				preferred = ""
			} else {
				if next == nil {
					next = make(map[status.RepoRef]absence)
				}
				next[ref] = miss
			}
		}
		if repo.Container == c.Container && repo.Workspace == c.Workspace && repo.WorkspacePreferred == preferred {
			continue
		}
		repo.Container, repo.Workspace, repo.WorkspacePreferred = c.Container, c.Workspace, preferred
		src.Repos[c.Repo] = repo
		changed = true
	}
	s.absent = next
	return changed, skipped
}

// Placeholders returns, for every container with a workspace file, the
// folders that file lists inside the container that are missing on disk
// and that no configured repo resolves to. A configured repo there already
// nests under the container (status.ComputeNesting) with its Clone action.
// Folders inside another clone (a submodule, say) and folders outside the
// container are skipped: the tree stays path-based.
func Placeholders(cfg *config.Config) []Placeholder {
	known := make(map[string]bool)
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			known[normPath(status.RepoPath(cfg, srcKey, repoKey, src.Repos[repoKey]))] = true
		}
	}
	var out []Placeholder
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			if !repo.Container || repo.Workspace == "" {
				continue
			}
			dir := status.RepoPath(cfg, srcKey, repoKey, repo)
			for _, m := range Members(dir, repo.Workspace) {
				if known[normPath(m.Path)] || insideClone(normPath(m.Path), normPath(dir), known) {
					continue
				}
				if _, err := os.Stat(m.Path); err == nil {
					continue
				}
				out = append(out, Placeholder{
					Parent: status.RepoRef{Source: srcKey, Repo: repoKey},
					Name:   m.Name,
					Path:   m.Rel,
				})
			}
		}
	}
	return out
}

// MemberRepos returns the configured repos that a container's workspace
// file lists inside it, so the GUI can show what the workspace opens. The
// container itself is not listed.
func MemberRepos(cfg *config.Config) []status.RepoRef {
	byPath := make(map[string]status.RepoRef)
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			byPath[normPath(status.RepoPath(cfg, srcKey, repoKey, src.Repos[repoKey]))] = status.RepoRef{Source: srcKey, Repo: repoKey}
		}
	}
	var out []status.RepoRef
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			if !repo.Container || repo.Workspace == "" {
				continue
			}
			for _, m := range Members(status.RepoPath(cfg, srcKey, repoKey, repo), repo.Workspace) {
				if ref, ok := byPath[normPath(m.Path)]; ok {
					out = append(out, ref)
				}
			}
		}
	}
	return out
}

// insideClone reports whether p lies inside a known clone nested in the
// container dir: a folder of that clone (a submodule, say) is not a clone
// gitbox could provide.
func insideClone(p, dir string, known map[string]bool) bool {
	for parent := filepath.Dir(p); parent != dir && strings.HasPrefix(parent, dir+string(filepath.Separator)); parent = filepath.Dir(parent) {
		if known[parent] {
			return true
		}
	}
	return false
}

// Member is a folder a container's workspace file lists inside the
// container.
type Member struct {
	Path string // absolute
	Rel  string // relative to the container, slash-separated
	Name string // the folder's name in the workspace file, else its base name
}

// Members reads the workspace file (relative to dir) and returns the folders
// it lists strictly inside dir, once each, in file order. Folders outside
// dir and dir itself are skipped.
func Members(dir, file string) []Member {
	var out []Member
	seen := make(map[string]bool)
	for _, f := range folders(filepath.Join(dir, file)) {
		abs := f.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		abs = filepath.Clean(abs)
		rel, err := filepath.Rel(dir, abs)
		n := normPath(abs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || seen[n] {
			continue
		}
		seen[n] = true
		name := f.Name
		if name == "" {
			name = filepath.Base(abs)
		}
		out = append(out, Member{Path: abs, Rel: filepath.ToSlash(rel), Name: name})
	}
	return out
}

type folder struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// folders reads the folders a workspace file lists; nil when it can't be
// read or parsed.
func folders(file string) []folder {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var raw struct {
		Folders []folder `json:"folders"`
	}
	if json.Unmarshal(stripJSONC(data), &raw) != nil {
		return nil
	}
	return raw.Folders
}

// stripJSONC turns VS Code's JSON-with-comments into plain JSON: it drops
// // and /* */ comments and trailing commas outside strings.
func stripJSONC(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(in) && in[j] != '"' {
				if in[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j, len(in)-1)
			out = append(out, in[i:j+1]...)
			i = j
		case c == '/' && i+1 < len(in) && in[i+1] == '/':
			for i < len(in) && in[i] != '\n' {
				i++
			}
			i--
		case c == '/' && i+1 < len(in) && in[i+1] == '*':
			i += 2
			for i+1 < len(in) && !(in[i] == '*' && in[i+1] == '/') {
				i++
			}
			i++
		case c == ']' || c == '}':
			k := len(out) - 1
			for k >= 0 && strings.ContainsRune(" \t\r\n", rune(out[k])) {
				k--
			}
			if k >= 0 && out[k] == ',' {
				out = append(out[:k], out[k+1:]...)
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// normPath cleans a path for comparison, case-folded on case-insensitive
// filesystems (Windows, macOS default), like status.ComputeNesting.
func normPath(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}
