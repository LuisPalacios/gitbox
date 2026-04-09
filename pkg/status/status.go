// Package status checks the sync status of local Git clones.
package status

import (
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// State represents the sync state of a repository clone.
type State int

const (
	Clean     State = iota // Up to date, no changes
	Dirty                  // Has modified/untracked files
	Behind                 // Behind upstream (needs pull)
	Ahead                  // Ahead of upstream (needs push)
	Diverged               // Both ahead and behind
	Conflict               // Has merge conflicts
	NotCloned              // Directory does not exist
	NoUpstream             // No upstream tracking branch
	Error                  // Could not determine status
)

// String returns a human-readable label for the state.
func (s State) String() string {
	switch s {
	case Clean:
		return "clean"
	case Dirty:
		return "dirty"
	case Behind:
		return "behind"
	case Ahead:
		return "ahead"
	case Diverged:
		return "diverged"
	case Conflict:
		return "conflict"
	case NotCloned:
		return "not cloned"
	case NoUpstream:
		return "no upstream"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}

// Symbol returns a status indicator character.
func (s State) Symbol() string {
	switch s {
	case Clean:
		return "✓"
	case Dirty:
		return "✗"
	case Behind:
		return "↓"
	case Ahead:
		return "↑"
	case Diverged:
		return "⚠"
	case Conflict:
		return "⚡"
	case NotCloned:
		return "○"
	case NoUpstream:
		return "~"
	case Error:
		return "!"
	default:
		return "?"
	}
}

// RepoStatus holds the full status of a single repository.
type RepoStatus struct {
	Account   string `json:"account"`
	Source    string `json:"source"`
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	State    State  `json:"state"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
	Modified int    `json:"modified,omitempty"`
	Untracked int  `json:"untracked,omitempty"`
	Conflicts int  `json:"conflicts,omitempty"`
	ErrorMsg string `json:"error,omitempty"`
	Branch    string `json:"branch,omitempty"`     // Current branch name (or "(detached)")
	IsDefault bool   `json:"is_default,omitempty"` // True when current branch is the repo's default branch
}

// Check determines the sync status of a single repo at the given path.
func Check(repoPath string) RepoStatus {
	rs := RepoStatus{Path: repoPath}

	if !git.IsRepo(repoPath) {
		rs.State = NotCloned
		return rs
	}

	st, err := git.Status(repoPath)
	if err != nil {
		rs.State = Error
		rs.ErrorMsg = err.Error()
		return rs
	}

	rs.Ahead = st.Ahead
	rs.Behind = st.Behind
	rs.Modified = st.Modified
	rs.Untracked = st.Untracked
	rs.Conflicts = st.Conflicts
	rs.Branch = st.Branch

	// Determine if current branch is the default.
	if st.Branch != "" && st.Branch != "(detached)" {
		if def, err := git.DefaultBranch(repoPath); err == nil {
			rs.IsDefault = st.Branch == def
		}
	}

	// Determine state.
	switch {
	case st.Conflicts > 0:
		rs.State = Conflict
	case st.Upstream == "":
		rs.State = NoUpstream
	case st.Ahead > 0 && st.Behind > 0:
		rs.State = Diverged
	case st.Behind > 0:
		rs.State = Behind
	case st.Ahead > 0:
		rs.State = Ahead
	case st.Modified > 0 || st.Untracked > 0 || st.Added > 0 || st.Deleted > 0:
		rs.State = Dirty
	default:
		rs.State = Clean
	}

	return rs
}

// CheckAll checks the status of all repos in the configuration.
func CheckAll(cfg *config.Config) []RepoStatus {
	var results []RepoStatus
	globalFolder := config.ExpandTilde(cfg.Global.Folder)

	for _, sourceName := range cfg.OrderedSourceKeys() {
		source := cfg.Sources[sourceName]
		sourceFolder := source.EffectiveFolder(sourceName)
		for _, repoName := range source.OrderedRepoKeys() {
			repo := source.Repos[repoName]
			path := ResolveRepoPath(globalFolder, sourceFolder, repoName, repo)
			rs := Check(path)
			rs.Source = sourceName
			rs.Repo = repoName
			rs.Account = source.Account
			results = append(results, rs)
		}
	}

	return results
}

// ResolveRepoPath computes the clone path for a repository.
//
// Path structure: globalFolder / sourceFolder / idFolder / cloneFolder
//
// Repo key format: "org/repo" (e.g., "myorg/my-project")
//   - idFolder defaults to "org" (part before /)
//   - cloneFolder defaults to "repo" (part after /)
//
// Overrides:
//   - repo.IdFolder overrides the 2nd level (org) dir
//   - repo.CloneFolder overrides the 3rd level (clone) dir
//   - If cloneFolder is absolute (starts with /, ~, or ../), it replaces the entire path
func ResolveRepoPath(globalFolder, sourceFolder, repoName string, repo config.Repo) string {
	// If clone_folder is absolute, it replaces everything.
	if repo.CloneFolder != "" {
		expanded := config.ExpandTilde(repo.CloneFolder)
		if filepath.IsAbs(expanded) || strings.HasPrefix(repo.CloneFolder, "~") || strings.HasPrefix(repo.CloneFolder, "../") {
			return expanded
		}
	}

	// Split repo key into org and repo parts.
	orgPart, repoPart := splitRepoKey(repoName)

	// Apply overrides.
	if repo.IdFolder != "" {
		orgPart = repo.IdFolder
	}
	if repo.CloneFolder != "" {
		repoPart = repo.CloneFolder
	}

	if orgPart != "" {
		return filepath.Join(globalFolder, sourceFolder, orgPart, repoPart)
	}
	return filepath.Join(globalFolder, sourceFolder, repoPart)
}

// splitRepoKey splits "org/repo" into ("org", "repo").
// If no slash, returns ("", repoName).
func splitRepoKey(repoName string) (string, string) {
	if i := strings.IndexByte(repoName, '/'); i >= 0 {
		return repoName[:i], repoName[i+1:]
	}
	return "", repoName
}
