// Package reconcile keeps gitbox.json in step with the clones on disk
// (issue #149). The config stays the record of intent — accounts, roots,
// overrides, repos I want but haven't cloned — while which clones exist and
// where is read back from the roots: Plan scans them and decides, Apply
// writes the decisions into the config. Neither touches files on disk.
package reconcile

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// Why a found clone waits for me instead of being adopted.
const (
	ReasonHostOnly  = "host-only"       // one account matches, on the host alone
	ReasonAmbiguous = "ambiguous"       // several accounts tie
	ReasonUnknown   = "unknown-host"    // no account on that server
	ReasonNoRemote  = "no-remote"       // no origin, or one gitbox can't read
	ReasonTracked   = "already-tracked" // the repo is already tracked at another path
	ReasonWorktree  = "worktree"        // a linked worktree of another clone, not a clone of its own
)

// Unmatched is a found clone that needs a decision.
type Unmatched struct {
	adopt.OrphanRepo
	Reason string
}

// Relink points a tracked repo whose clone went missing at the same clone,
// found somewhere else under a root.
type Relink struct {
	Source, Repo, Path string
	expect             location
	host               string
	keyOwners          int
}

// location records the config values that determined a planned placement.
type location struct {
	account, root, folder          string
	path, workspace, preferred     string
	rootPin, cloneFolder, idFolder string
}

func repoLocation(cfg *config.Config, source, key string, repo config.Repo) location {
	src := cfg.Sources[source]
	return location{account: src.Account, root: cfg.RepoRoot(source, repo), folder: src.EffectiveFolder(source),
		path: status.RepoPath(cfg, source, key, repo), workspace: repo.Workspace, preferred: repo.WorkspacePreferred,
		rootPin: repo.Root, cloneFolder: repo.CloneFolder, idFolder: repo.IdFolder}
}

// Adoption records a confident match and its expected destination.
type Adoption struct {
	adopt.OrphanRepo
	expect location
}

// Incomplete is a tracked repo whose clone git stopped before it finished
// (git.IsIncompleteClone). Its folder is left as it is: the window says so,
// and I delete it and clone again.
type Incomplete struct {
	Source, Repo, Path string
}

// Result is what one pass decided.
type Result struct {
	scope      *scanScope
	Adopt      []Adoption
	Relink     []Relink
	Unmatched  []Unmatched
	Ignored    []Unmatched // with the reason they would wait for me, "" when adoptable
	Incomplete []Incomplete
}

// scanScope records which directories the scan could traverse. Source
// edits outside that scope can be validated separately for each entry.
type scanScope struct {
	roots      []string
	depth      int
	containers map[string]containerScope
}

type containerScope struct {
	nested    bool
	workspace string
}

func scopeOf(cfg *config.Config) *scanScope {
	return &scanScope{roots: slices.Clone(cfg.Global.Roots), depth: cfg.Global.NestedScanDepth, containers: containerLocations(cfg)}
}

func containerLocations(cfg *config.Config) map[string]containerScope {
	locations := map[string]containerScope{}
	for sk, src := range cfg.Sources {
		for rk, repo := range src.Repos {
			if repo.Container || repo.Workspace != "" {
				locations[status.RepoPath(cfg, sk, rk, repo)] = containerScope{repo.Container, repo.Workspace}
			}
		}
	}
	return locations
}

// SameScanScope reports whether the plan still describes the live roots,
// nested depth and workspace locations. It never touches the filesystem.
func (res Result) SameScanScope(cfg *config.Config) bool {
	if res.scope == nil || res.scope.depth != cfg.Global.NestedScanDepth {
		return false
	}
	if !slices.Equal(res.scope.roots, cfg.Global.Roots) {
		for _, root := range res.scope.roots {
			if !cfg.HasRoot(root) {
				return false
			}
		}
	}
	return maps.Equal(res.scope.containers, containerLocations(cfg))
}

// Plan scans every root and decides, for each clone the config doesn't
// track: re-link it to a tracked repo whose clone went missing, adopt it
// (a confident match, see adopt.OrphanRepo.Confident), or leave it for me.
// Ignored clones are reported apart, and so are tracked clones that git
// never finished. Repos under a missing root are never considered missing.
func Plan(cfg *config.Config) (Result, error) {
	orphans, err := adopt.FindOrphans(cfg)
	if err != nil {
		return Result{}, err
	}

	// Tracked repos whose clone is missing while their root is online, and
	// every tracked key, both keyed in lower case.
	type ref struct{ source, repo string }
	missing := map[string][]ref{}
	tracked := map[string]int{}
	res := Result{scope: scopeOf(cfg)}
	for _, sk := range sortedKeys(cfg.Sources) {
		src := cfg.Sources[sk]
		for _, rk := range sortedKeys(src.Repos) {
			repo := src.Repos[rk]
			tracked[strings.ToLower(rk)]++
			p := status.RepoPath(cfg, sk, rk, repo)
			if status.RepoRootOffline(cfg, sk, repo) || git.IsLinkedWorktree(p) {
				continue
			}
			if git.IsRepo(p) {
				if git.IsIncompleteClone(p) {
					res.Incomplete = append(res.Incomplete, Incomplete{Source: sk, Repo: rk, Path: p})
				}
				continue
			}
			missing[strings.ToLower(rk)] = append(missing[strings.ToLower(rk)], ref{sk, rk})
		}
	}

	// reason says why a clone waits for me, or "" when it can be adopted.
	reason := func(o adopt.OrphanRepo) string {
		switch {
		case git.IsLinkedWorktree(o.Path):
			return ReasonWorktree
		case o.LocalOnly:
			return ReasonNoRemote
		case o.MatchedAccount == "" && len(o.AmbiguousCandidates) > 0:
			return ReasonAmbiguous
		case o.MatchedAccount == "":
			return ReasonUnknown
		case tracked[strings.ToLower(o.RepoKey)] > 0:
			return ReasonTracked
		case !o.Confident:
			return ReasonHostOnly
		}
		return ""
	}

	relinked := map[ref]bool{}
	for _, o := range orphans {
		// Ignored clones keep their reason, so the list can still offer
		// Adopt where it applies.
		if cfg.IsIgnoredClone(o.Path) {
			res.Ignored = append(res.Ignored, Unmatched{o, reason(o)})
			continue
		}
		if r := reason(o); r == ReasonWorktree || r == ReasonNoRemote {
			res.Unmatched = append(res.Unmatched, Unmatched{o, r})
			continue
		}
		var hits []ref
		for _, m := range missing[strings.ToLower(o.RepoKey)] {
			if !relinked[m] && adopt.HostMatches(cfg.Accounts[cfg.Sources[m.source].Account], o.Host) {
				hits = append(hits, m)
			}
		}
		if len(hits) == 1 {
			relinked[hits[0]] = true
			hit := hits[0]
			expect := repoLocation(cfg, hit.source, hit.repo, cfg.Sources[hit.source].Repos[hit.repo])
			res.Relink = append(res.Relink, Relink{Source: hit.source, Repo: hit.repo, Path: o.Path, expect: expect, host: o.Host, keyOwners: tracked[strings.ToLower(hit.repo)]})
			continue
		}
		if r := reason(o); r != "" {
			res.Unmatched = append(res.Unmatched, Unmatched{o, r})
			continue
		}
		source := o.MatchedSource
		if source == "" {
			source = o.MatchedAccount
		}
		tracked[strings.ToLower(o.RepoKey)]++
		res.Adopt = append(res.Adopt, Adoption{OrphanRepo: o, expect: repoLocation(cfg, source, o.RepoKey, config.Repo{})})
	}
	return res, nil
}

// WorkspaceCandidates scans one external directory still listed by a
// workspace. It returns rows for explicit review and never plans auto-adopts
// or re-links. The boolean tells the caller whether the root matched.
func WorkspaceCandidates(cfg *config.Config, tree workspace.Tree, root string) ([]Unmatched, bool, error) {
	for _, row := range tree.Placeholders {
		if root == "" || row.OutsidePath != root {
			continue
		}
		orphans, err := adopt.FindOrphansUnder(cfg, root)
		if err != nil {
			return nil, true, err
		}
		out := make([]Unmatched, 0, len(orphans))
		for _, o := range orphans {
			var reason string
			switch {
			case git.IsLinkedWorktree(o.Path):
				reason = ReasonWorktree
			case o.LocalOnly:
				reason = ReasonNoRemote
			}
			out = append(out, Unmatched{o, reason})
		}
		return out, true, nil
	}
	return nil, false, nil
}

// Placed is a clone Apply or Adopt added to the config.
type Placed struct {
	Account, Source, Repo, Path string
}

// Skip describes a decision whose preconditions no longer hold.
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Applied counts only changes actually written to the live config.
type Applied struct {
	Adopted  []Placed
	Relinked int
	Skipped  []Skip
}

// Apply checks each decision against current intent. It indexes the live
// inventory once and checks adoptions before any of them creates a source.
func Apply(cfg *config.Config, res Result) Applied {
	var done Applied
	if len(res.Adopt)+len(res.Relink) == 0 {
		return done
	}
	if !res.SameScanScope(cfg) {
		for _, r := range res.Relink {
			done.Skipped = append(done.Skipped, Skip{Path: r.Path, Reason: "moved"})
		}
		for _, o := range res.Adopt {
			done.Skipped = append(done.Skipped, Skip{Path: o.Path, Reason: "moved"})
		}
		return done
	}
	keys, paths := map[string]int{}, map[string]bool{}
	for sk, source := range cfg.Sources {
		for key, repo := range source.Repos {
			keys[strings.ToLower(key)]++
			paths[pathKey(status.RepoPath(cfg, sk, key, repo))] = true
		}
	}
	for _, r := range res.Relink {
		src, exists := cfg.Sources[r.Source]
		repo, tracked := src.Repos[r.Repo]
		acct, accountExists := cfg.Accounts[src.Account]
		reason := ""
		switch {
		case !exists || !accountExists || src.Account != r.expect.account || !adopt.HostMatches(acct, r.host):
			reason = "sourceGone"
		case !tracked:
			reason = "renamed"
		case cfg.IsIgnoredClone(r.Path) || repoLocation(cfg, r.Source, r.Repo, repo) != r.expect:
			reason = "moved"
		case paths[pathKey(r.Path)] || keys[strings.ToLower(r.Repo)] != r.keyOwners:
			reason = "alreadyTracked"
		}
		if reason != "" {
			done.Skipped = append(done.Skipped, Skip{Path: r.Path, Reason: reason})
			continue
		}
		src.Repos[r.Repo] = locate(cfg, r.Source, r.Repo, r.Path, repo)
		paths[pathKey(r.Path)] = true
		done.Relinked++
	}
	var pending []Adoption
	for _, o := range res.Adopt {
		source := o.MatchedSource
		if source == "" {
			source = o.MatchedAccount
		}
		reason := ""
		switch {
		case keys[strings.ToLower(o.RepoKey)] != 0 || paths[pathKey(o.Path)]:
			reason = "alreadyTracked"
		case !o.StillMatches(cfg):
			reason = "sourceGone"
		case cfg.IsIgnoredClone(o.Path) || repoLocation(cfg, source, o.RepoKey, config.Repo{}) != o.expect:
			reason = "moved"
		}
		if reason != "" {
			done.Skipped = append(done.Skipped, Skip{Path: o.Path, Reason: reason})
			continue
		}
		keys[strings.ToLower(o.RepoKey)]++
		paths[pathKey(o.Path)] = true
		pending = append(pending, o)
	}
	for _, o := range pending {
		source, err := Adopt(cfg, o.OrphanRepo, o.MatchedAccount)
		if err != nil {
			done.Skipped = append(done.Skipped, Skip{Path: o.Path, Reason: "sourceGone"})
			continue
		}
		done.Adopted = append(done.Adopted, Placed{Account: o.MatchedAccount, Source: source, Repo: o.RepoKey, Path: o.Path})
	}
	return done
}

func pathKey(path string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
}

// Adopt adds a found clone to the config under accountKey, in place, and
// returns the source it went to: the matched one when accountKey is the
// matched account, else the account's first source, else a new source
// keyed like the account.
func Adopt(cfg *config.Config, o adopt.OrphanRepo, accountKey string) (string, error) {
	if _, ok := cfg.Accounts[accountKey]; !ok {
		return "", fmt.Errorf("account %q not found", accountKey)
	}
	if o.RepoKey == "" {
		return "", fmt.Errorf("%s has no remote to name it", o.Path)
	}
	source := ""
	if accountKey == o.MatchedAccount {
		source = o.MatchedSource
	}
	if source == "" {
		for _, sk := range sortedKeys(cfg.Sources) {
			if cfg.Sources[sk].Account == accountKey {
				source = sk
				break
			}
		}
	}
	if source == "" {
		if _, taken := cfg.Sources[accountKey]; taken {
			return "", fmt.Errorf("source %q belongs to another account", accountKey)
		}
		if cfg.Sources == nil {
			cfg.Sources = map[string]config.Source{}
		}
		cfg.Sources[accountKey] = config.Source{Account: accountKey, Repos: map[string]config.Repo{}}
		cfg.SourceOrder = append(cfg.SourceOrder, accountKey)
		source = accountKey
	}
	if err := cfg.AddRepo(source, o.RepoKey, locate(cfg, source, o.RepoKey, o.Path, config.Repo{})); err != nil {
		return "", err
	}
	return source, nil
}

// locate returns repo pointed at path: the root it sits in when path is its
// standard spot there (no pin when that's the source's root), else an
// absolute clone_folder.
func locate(cfg *config.Config, source, repoKey, path string, repo config.Repo) config.Repo {
	repo.Root = ""
	// An old whole-path clone_folder names the old location; relative
	// overrides (id_folder, a plain clone_folder) still apply.
	if cf := repo.CloneFolder; cf != "" && (filepath.IsAbs(config.ExpandTilde(cf)) || strings.HasPrefix(cf, "~") || strings.HasPrefix(cf, "../")) {
		repo.CloneFolder = ""
	}
	src := cfg.Sources[source]
	for _, r := range cfg.Global.Roots {
		root := config.ExpandTilde(r)
		if config.SamePath(status.ResolveRepoPath(root, src.EffectiveFolder(source), repoKey, repo), path) {
			if !config.SameRoot(root, cfg.SourceRoot(source)) {
				repo.Root = r
			}
			return repo
		}
	}
	repo.CloneFolder = path
	return repo
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
