// Package workspace ties VS Code .code-workspace files to the clones that hold
// them. A clone with a workspace file at its root is a container: Sync flags
// it and records the file in repo.workspace, asking me to choose when the
// root holds several. Placeholders lists the folders a container's workspace
// names inside it that are not cloned yet. gitbox never creates, edits or
// deletes workspace files — I own them.
package workspace

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
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

// Placeholder is a folder a container's workspace lists that is either
// missing on disk or resolves outside the container.
type Placeholder struct {
	Parent      status.RepoRef
	Name        string // the folder's name in the workspace file, else its base name
	Path        string // relative to the container, slash-separated
	OutsidePath string // resolved externally, for an explicit search only
}

// Problem codes describe why a workspace file could not be used.
const (
	TooLarge   = "too large"
	Unreadable = "unreadable"
	Invalid    = "invalid"
)

type Diagnostic struct {
	File    string
	Problem string
}

type WorkspaceDiagnostic struct {
	Parent status.RepoRef
	Diagnostic
}

// Tree describes the workspace rows and file diagnostics for the GUI.
type Tree struct {
	Placeholders []Placeholder
	Members      []status.RepoRef
	Diagnostics  []WorkspaceDiagnostic
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

// Inspect reads each selected workspace once for the tree. A missing
// configured repo already has its own row and Clone action.
func Inspect(cfg *config.Config) Tree {
	byPath := make(map[string]status.RepoRef)
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			byPath[pathkey.Compare(status.RepoPath(cfg, srcKey, repoKey, src.Repos[repoKey]))] = status.RepoRef{Source: srcKey, Repo: repoKey}
		}
	}
	var out Tree
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			if !repo.Container || repo.Workspace == "" {
				continue
			}
			parent := status.RepoRef{Source: srcKey, Repo: repoKey}
			dir := status.RepoPath(cfg, srcKey, repoKey, repo)
			members, issues := Members(dir, repo.Workspace)
			for _, issue := range issues {
				out.Diagnostics = append(out.Diagnostics, WorkspaceDiagnostic{Parent: parent, Diagnostic: issue})
			}
			for _, m := range members {
				row := Placeholder{Parent: parent, Name: m.Name, Path: m.Rel}
				if m.Escapes {
					row.OutsidePath = m.Physical
					out.Placeholders = append(out.Placeholders, row)
					continue
				}
				ref, tracked := byPath[pathkey.Compare(m.Path)]
				if !tracked && m.ScanPath != "" {
					ref, tracked = byPath[pathkey.Compare(m.ScanPath)]
				}
				if tracked {
					out.Members = append(out.Members, ref)
				} else if m.missing && !insideClone(pathkey.Compare(m.Path), pathkey.Compare(dir), byPath) {
					out.Placeholders = append(out.Placeholders, row)
				}
			}
		}
	}
	return out
}

// insideClone reports whether p lies inside a known clone nested in the
// container dir: a folder of that clone (a submodule, say) is not a clone
// gitbox could provide.
func insideClone(p, dir string, known map[string]status.RepoRef) bool {
	for parent := filepath.Dir(p); parent != dir && strings.HasPrefix(parent, dir+string(filepath.Separator)); parent = filepath.Dir(parent) {
		if _, ok := known[parent]; ok {
			return true
		}
	}
	return false
}

// Member is a folder a workspace file lists lexically inside its container.
type Member struct {
	Path     string // absolute as listed
	Rel      string // relative to the container, slash-separated
	Name     string // the folder's name in the workspace file, else its base name
	Physical string // resolved path, empty when unknown
	ScanPath string // verified in-container directory, empty when absent or unsafe
	Exists   bool
	Escapes  bool
	missing  bool
}

// Members returns the folders a workspace lists lexically inside dir, once
// each in file order. It resolves existing folders before allowing discovery.
func Members(dir, file string) ([]Member, []Diagnostic) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, []Diagnostic{{File: filepath.Base(file), Problem: Unreadable}}
	}
	defer root.Close()

	listed, problem := folders(root, file)
	if problem != "" {
		return nil, []Diagnostic{{File: filepath.Base(file), Problem: problem}}
	}
	physicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, []Diagnostic{{File: filepath.Base(file), Problem: Unreadable}}
	}
	var out []Member
	seen := make(map[string]bool)
	for _, f := range listed {
		abs := f.Path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(dir, abs)
		}
		abs = filepath.Clean(abs)
		rel, inside := relativeInside(dir, abs)
		n := pathkey.Compare(abs)
		if !inside || seen[n] {
			continue
		}
		seen[n] = true
		name := f.Name
		if name == "" {
			name = filepath.Base(abs)
		}
		m := Member{Path: abs, Rel: filepath.ToSlash(rel), Name: name}
		physical, err := filepath.EvalSymlinks(abs)
		if err != nil {
			m.missing = os.IsNotExist(err)
		} else if info, err := os.Stat(physical); err == nil && info.IsDir() {
			m.Physical, m.Exists = physical, true
			if resolvedRel, inside := relativeInside(physicalDir, physical); inside {
				// Use the resolved name under the original container path.
				// This avoids retraversing a workspace member symlink and
				// preserves the spelling of configured clone roots.
				m.ScanPath = filepath.Join(dir, resolvedRel)
			} else if physical != physicalDir {
				m.Escapes = true
			} else {
				continue
			}
		}
		out = append(out, m)
	}
	return out, nil
}

func relativeInside(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	return rel, err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type folder struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

const workspaceLimit = 1 << 20

// folders reads a root workspace file without following links out of the
// container or loading more than the size limit.
func folders(root *os.Root, file string) ([]folder, string) {
	if file != filepath.Base(file) || !strings.HasSuffix(strings.ToLower(file), Ext) {
		return nil, Invalid
	}
	info, err := root.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return nil, Unreadable
	}
	if info.Size() > workspaceLimit {
		return nil, TooLarge
	}
	in, err := root.Open(file)
	if err != nil {
		return nil, Unreadable
	}
	defer in.Close()
	info, err = in.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, Unreadable
	}
	if info.Size() > workspaceLimit {
		return nil, TooLarge
	}
	data, err := io.ReadAll(io.LimitReader(in, workspaceLimit+1))
	if err != nil {
		return nil, Unreadable
	}
	if len(data) > workspaceLimit {
		return nil, TooLarge
	}
	var raw struct {
		Folders []folder `json:"folders"`
	}
	if json.Unmarshal(stripJSONC(data), &raw) != nil {
		return nil, Invalid
	}
	return raw.Folders, ""
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
