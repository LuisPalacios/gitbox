// Package reconcile keeps gitbox.json in step with the clones on disk
// (issue #149). The config stays the record of intent — accounts, roots,
// overrides, repos I want but haven't cloned — while which clones exist and
// where is read back from the roots: Plan scans them and decides, Apply
// writes the decisions into the config. Neither touches files on disk.
package reconcile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// Why a found clone waits for me instead of being adopted.
const (
	ReasonHostOnly  = "host-only"       // one account matches, on the host alone
	ReasonAmbiguous = "ambiguous"       // several accounts tie
	ReasonUnknown   = "unknown-host"    // no account on that server
	ReasonNoRemote  = "no-remote"       // no origin, or one gitbox can't read
	ReasonTracked   = "already-tracked" // the repo is already tracked at another path
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
}

// Result is what one pass decided.
type Result struct {
	Adopt     []adopt.OrphanRepo
	Relink    []Relink
	Unmatched []Unmatched
	Ignored   []adopt.OrphanRepo
}

// Plan scans every root and decides, for each clone the config doesn't
// track: re-link it to a tracked repo whose clone went missing, adopt it
// (a confident match, see adopt.OrphanRepo.Confident), or leave it for me.
// Ignored clones are reported apart. Repos under a missing root are never
// considered missing.
func Plan(cfg *config.Config) (Result, error) {
	orphans, err := adopt.FindOrphans(cfg)
	if err != nil {
		return Result{}, err
	}

	// Tracked repos whose clone is missing while their root is online, and
	// every tracked key per source, both keyed in lower case.
	type ref struct{ source, repo string }
	missing := map[string][]ref{}
	tracked := map[string]bool{}
	for _, sk := range sortedKeys(cfg.Sources) {
		src := cfg.Sources[sk]
		for _, rk := range sortedKeys(src.Repos) {
			repo := src.Repos[rk]
			tracked[sk+"/"+strings.ToLower(rk)] = true
			if status.RepoRootOffline(cfg, sk, repo) || git.IsRepo(status.RepoPath(cfg, sk, rk, repo)) {
				continue
			}
			missing[strings.ToLower(rk)] = append(missing[strings.ToLower(rk)], ref{sk, rk})
		}
	}

	var res Result
	relinked := map[ref]bool{}
	for _, o := range orphans {
		if cfg.IsIgnoredClone(o.Path) {
			res.Ignored = append(res.Ignored, o)
			continue
		}
		if o.LocalOnly {
			res.Unmatched = append(res.Unmatched, Unmatched{o, ReasonNoRemote})
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
			res.Relink = append(res.Relink, Relink{Source: hits[0].source, Repo: hits[0].repo, Path: o.Path})
			continue
		}
		source := o.MatchedSource
		if source == "" {
			source = o.MatchedAccount
		}
		switch {
		case o.MatchedAccount == "" && len(o.AmbiguousCandidates) > 0:
			res.Unmatched = append(res.Unmatched, Unmatched{o, ReasonAmbiguous})
		case o.MatchedAccount == "":
			res.Unmatched = append(res.Unmatched, Unmatched{o, ReasonUnknown})
		case tracked[source+"/"+strings.ToLower(o.RepoKey)]:
			res.Unmatched = append(res.Unmatched, Unmatched{o, ReasonTracked})
		case !o.Confident:
			res.Unmatched = append(res.Unmatched, Unmatched{o, ReasonHostOnly})
		default:
			tracked[source+"/"+strings.ToLower(o.RepoKey)] = true
			res.Adopt = append(res.Adopt, o)
		}
	}
	return res, nil
}

// Apply writes a plan into the config and reports whether it changed.
// Clones that can't be added (a source key taken by another account) are
// skipped.
func Apply(cfg *config.Config, res Result) bool {
	changed := false
	for _, r := range res.Relink {
		src, ok := cfg.Sources[r.Source]
		if !ok {
			continue
		}
		repo, ok := src.Repos[r.Repo]
		if !ok {
			continue
		}
		src.Repos[r.Repo] = locate(cfg, r.Source, r.Repo, r.Path, repo)
		cfg.Sources[r.Source] = src
		changed = true
	}
	for _, o := range res.Adopt {
		if _, err := Adopt(cfg, o, o.MatchedAccount); err == nil {
			changed = true
		}
	}
	return changed
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
