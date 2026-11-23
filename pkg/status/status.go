// Package status checks the sync status of local Git clones.
package status

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// State represents the sync state of a repository clone.
type State int

const (
	Clean        State = iota // Up to date, no changes
	Dirty                     // Has modified/untracked files
	Behind                    // Behind upstream (needs pull)
	Ahead                     // Ahead of upstream (needs push)
	Diverged                  // Both ahead and behind
	Conflict                  // Has merge conflicts
	NotCloned                 // Directory does not exist
	NoUpstream                // No upstream tracking branch
	Error                     // Could not determine status
	UpstreamGone              // Remote repo no longer exists (404 / deleted / lost access)
	RootOffline               // Its clone root is missing (unmounted drive, missing share)
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
	case UpstreamGone:
		return "upstream gone"
	case RootOffline:
		return "root offline"
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
	case UpstreamGone:
		return "⊗"
	case RootOffline:
		return "◌"
	default:
		return "?"
	}
}

// RepoStatus holds the full status of a single repository.
type RepoStatus struct {
	Account      string `json:"account"`
	Source       string `json:"source"`
	Repo         string `json:"repo"`
	Path         string `json:"path"`
	State        State  `json:"state"`
	Ahead        int    `json:"ahead,omitempty"`
	Behind       int    `json:"behind,omitempty"`
	Modified     int    `json:"modified,omitempty"`
	Untracked    int    `json:"untracked,omitempty"`
	Conflicts    int    `json:"conflicts,omitempty"`
	ErrorMsg     string `json:"error,omitempty"`
	Branch       string `json:"branch,omitempty"`     // Current branch name (or "(detached)")
	IsDefault    bool   `json:"is_default,omitempty"` // True when current branch is the repo's default branch
	DefaultKnown bool   `json:"-"`
}

// Check determines the sync status of a single repo at the given path.
func Check(repoPath string) RepoStatus {
	return CheckContext(context.Background(), repoPath)
}

// CheckContext is Check that stops when ctx is cancelled. A check that was
// cancelled reports the Error state; callers that cancelled it drop it.
func CheckContext(ctx context.Context, repoPath string) RepoStatus {
	rs := RepoStatus{Path: repoPath}

	if !git.IsRepo(repoPath) {
		rs.State = NotCloned
		return rs
	}

	st, err := git.StatusContext(ctx, repoPath)
	if err != nil {
		rs.State = Error
		rs.ErrorMsg = err.Error()
		return rs
	}

	rs.Ahead = st.Ahead
	rs.Behind = st.Behind
	rs.Modified = st.Changed
	rs.Untracked = st.Untracked
	rs.Conflicts = st.Conflicts
	rs.Branch = st.Branch

	// Determine if current branch is the default.
	if st.Branch != "" && st.Branch != "(detached)" {
		if def, err := git.DefaultBranchContext(ctx, repoPath); err == nil {
			rs.IsDefault = st.Branch == def
			rs.DefaultKnown = true
		}
	}

	rs.State = Classify(st)
	return rs
}

// Classify determines the presentation state of a successful Git status.
func Classify(st git.RepoStatus) State {
	switch {
	case st.Conflicts > 0:
		return Conflict
	case st.Changed > 0 || st.Untracked > 0:
		return Dirty
	case st.Ahead > 0 && st.Behind > 0:
		return Diverged
	case st.Behind > 0:
		return Behind
	case st.Ahead > 0:
		return Ahead
	case st.Upstream == "":
		return NoUpstream
	default:
		return Clean
	}
}

// checkWithin runs CheckContext with its own deadline when perRepo is
// positive.
func checkWithin(ctx context.Context, repoPath string, perRepo time.Duration) RepoStatus {
	if perRepo <= 0 {
		return CheckContext(ctx, repoPath)
	}
	ctx, cancel := context.WithTimeout(ctx, perRepo)
	defer cancel()
	return CheckContext(ctx, repoPath)
}

// checkAllConcurrency bounds the number of in-flight checks in CheckAllContext.
// Each Check shells out to git, so on Windows (where subprocess spawn is
// expensive) a cap of 8 keeps a useful amount of work in flight without
// overwhelming the OS. A tight serial loop also starves Wails' IPC layer
// on WebView2 and prevents the GUI from rendering intermediate status
// updates — see issue #57.
const checkAllConcurrency = 8

// CheckAllContext checks the status of all repos in the configuration.
// Up to checkAllConcurrency repos are checked in parallel; the returned
// slice is ordered as cfg.OrderedSourceKeys() × Source.OrderedRepoKeys().
//
// It stops when ctx is cancelled: a check waiting for its turn does not
// start, and the ones in flight are stopped. It then returns ctx's error,
// and the results are incomplete. perRepo, when positive, bounds the check
// of each repo.
func CheckAllContext(ctx context.Context, cfg *config.Config, perRepo time.Duration) ([]RepoStatus, error) {
	type job struct {
		index      int
		sourceName string
		sourceAcct string
		repoName   string
		path       string
		offline    bool
	}
	var jobs []job
	for _, sourceName := range cfg.OrderedSourceKeys() {
		source := cfg.Sources[sourceName]
		for _, repoName := range source.OrderedRepoKeys() {
			repo := source.Repos[repoName]
			jobs = append(jobs, job{
				index:      len(jobs),
				sourceName: sourceName,
				sourceAcct: source.Account,
				repoName:   repoName,
				path:       RepoPath(cfg, sourceName, repoName, repo),
				offline:    RepoRootOffline(cfg, sourceName, repo),
			})
		}
	}

	results := make([]RepoStatus, len(jobs))
	sem := make(chan struct{}, checkAllConcurrency)
	var wg sync.WaitGroup
dispatch:
	for _, j := range jobs {
		// A cancelled sweep starts nothing more, even with a free slot.
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break dispatch
		}
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			rs := RepoStatus{Path: j.path, State: RootOffline}
			if !j.offline {
				rs = checkWithin(ctx, j.path, perRepo)
			}
			rs.Source = j.sourceName
			rs.Repo = j.repoName
			rs.Account = j.sourceAcct
			results[j.index] = rs
		}(j)
	}
	wg.Wait()
	return results, ctx.Err()
}

// RootOnline reports whether a clone root folder exists.
func RootOnline(root string) bool {
	info, err := os.Stat(root)
	return err == nil && info.IsDir()
}

// RepoRootOffline reports whether a repo's clone root is missing (#149): its
// path then says nothing about whether it was cloned. A repo with an
// absolute clone_folder doesn't depend on its root.
func RepoRootOffline(cfg *config.Config, sourceKey string, repo config.Repo) bool {
	if repo.CloneFolder != "" && (filepath.IsAbs(config.ExpandTilde(repo.CloneFolder)) || strings.HasPrefix(repo.CloneFolder, "~")) {
		return false
	}
	root := cfg.RepoRoot(sourceKey, repo)
	return root != "" && !RootOnline(root)
}

// ResolveRepoPath resolves the configured clone layout and folder overrides.
func ResolveRepoPath(root, sourceFolder, repoName string, repo config.Repo) string {
	return config.ResolveRepoPath(root, sourceFolder, repoName, repo)
}

// RepoPath returns where a tracked clone lives on disk.
func RepoPath(cfg *config.Config, sourceKey, repoKey string, repo config.Repo) string {
	return config.RepoPath(cfg, sourceKey, repoKey, repo)
}
