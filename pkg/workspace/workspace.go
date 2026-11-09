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

// Change is one repo update Sync decided: its new container flag and
// workspace file.
type Change struct {
	Source, Repo string
	Container    bool
	Workspace    string
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
func RootFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), Ext) {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out
}

// Sync compares every clone on disk with the workspace files at its root.
// It reads disk but never changes cfg; Apply writes the changes.
//   - One file: the repo becomes a container that opens it.
//   - Several: the repo becomes a container; its pick stays while the file
//     exists, otherwise it is cleared and a Choice is reported.
//   - None: a stale pick is cleared. The container flag stays, so a
//     container I flagged by hand keeps working.
//
// Clones that are not on disk (not cloned, root offline) are left alone.
func Sync(cfg *config.Config) Result {
	var res Result
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			dir := status.RepoPath(cfg, srcKey, repoKey, repo)
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				continue
			}
			files := RootFiles(dir)
			container, ws := repo.Container, repo.Workspace
			switch {
			case len(files) == 1:
				container, ws = true, files[0]
			case len(files) > 1:
				container = true
				if !slices.Contains(files, ws) {
					ws = ""
					res.Choices = append(res.Choices, Choice{Source: srcKey, Repo: repoKey, Files: files})
				}
			default:
				ws = ""
			}
			if container != repo.Container || ws != repo.Workspace {
				res.Changes = append(res.Changes, Change{Source: srcKey, Repo: repoKey, Container: container, Workspace: ws})
			}
		}
	}
	return res
}

// Apply writes Sync's changes into cfg, skipping repos removed meanwhile,
// and reports whether anything changed. The caller saves.
func Apply(cfg *config.Config, changes []Change) bool {
	changed := false
	for _, c := range changes {
		src, ok := cfg.Sources[c.Source]
		if !ok {
			continue
		}
		repo, ok := src.Repos[c.Repo]
		if !ok || (repo.Container == c.Container && repo.Workspace == c.Workspace) {
			continue
		}
		repo.Container, repo.Workspace = c.Container, c.Workspace
		src.Repos[c.Repo] = repo
		changed = true
	}
	return changed
}

// Placeholders returns, for every container with a workspace file, the
// folders that file lists inside the container that are missing on disk
// and that no configured repo resolves to. A configured repo there already
// nests under the container (status.ComputeNesting) with its Clone action.
// Folders outside the container are skipped: the tree stays path-based.
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
			for _, m := range Members(status.RepoPath(cfg, srcKey, repoKey, repo), repo.Workspace) {
				if known[normPath(m.Path)] {
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
