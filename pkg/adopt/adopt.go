// Package adopt discovers git repos under the gitbox roots that are not
// tracked in gitbox.json and helps adopt them.
package adopt

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// OrphanRepo describes a git repo on disk not tracked in gitbox.json.
type OrphanRepo struct {
	Path                string   // absolute path on disk
	RelPath             string   // relative to the deepest scanned root
	RemoteURL           string   // origin remote URL (empty if local-only)
	Host                string   // extracted hostname from remote
	Owner               string   // extracted owner/org from remote
	Repo                string   // extracted repo name from remote
	RepoKey             string   // "owner/repo" — the key for config.AddRepo
	MatchedAccount      string   // account key if matched, empty if unknown/ambiguous
	MatchedSource       string   // source key if matched, empty if needs creation
	ExpectedPath        string   // where gitbox convention says this should live
	ExpectedRoot        string   // the root ExpectedPath sits under
	NeedsRelocate       bool     // current path != expected path
	LocalOnly           bool     // no remote — cannot adopt
	AmbiguousCandidates []string // when multiple host-matching accounts tie with no disambiguator
	Nested              bool     // found inside a container repo's working tree — adopt in place, never relocate
	MatchScore          int      // MatchAccountEx score of the matched account (0 when none)
	Confident           bool     // a unique match backed by a signal beyond the host: safe to adopt unasked (#149)
	Candidates          []string // every account whose host matches, best first
}

// FindOrphans walks every configured root and container repos' working
// trees, returning repos not tracked in config.
func FindOrphans(cfg *config.Config) ([]OrphanRepo, error) {
	return FindOrphansIn(cfg, nil)
}

// FindOrphansIn is FindOrphans with additional caller-supplied scan roots — used
// by the on-demand "scan this folder" picker. Each root is scanned for top-level
// clones; container repos (Repo.Container) are additionally scanned for nested
// clones up to Global.NestedScanDepth. A clone at the standard layout spot of
// any configured root adopts in place under that root; one found elsewhere is
// reported with NeedsRelocate set, so adoption moves it under its source's
// root or stores an absolute clone_folder.
func FindOrphansIn(cfg *config.Config, extraRoots []string) ([]OrphanRepo, error) {
	// Build set of tracked repo paths from config.
	tracked := trackedPaths(cfg)

	// Pass 1 — flat scan of every root: the configured roots and any
	// caller-supplied ones. FindRepos stops at each .git boundary, so this
	// yields top-level clones only.
	roots := gatherRoots(cfg.Global.Roots, extraRoots)
	var flat []string
	for _, root := range roots {
		paths, err := git.FindRepos(root)
		if err != nil {
			continue // best-effort per root
		}
		flat = append(flat, paths...)
	}
	sort.Strings(flat)
	flat = dedupPaths(flat)
	// Each flat scan already stops at clone boundaries. Keep results from
	// explicitly requested nested roots instead of filtering across roots.
	candidates := flat

	// Pass 2 — nested clones inside container repos. These live inside another
	// clone's working tree and the flat scan stops at clone boundaries. Container
	// paths also join the root set so RelPath resolves against them.
	depth := cfg.Global.NestedScanDepthOrDefault()
	containerPaths := containerRepoPaths(cfg)
	nestedSet := make(map[string]bool)
	for _, cp := range containerPaths {
		for _, np := range git.FindNestedRepos(cp, depth) {
			candidates = append(candidates, np)
			nestedSet[normPath(np)] = true
		}
	}
	relRoots := append(append([]string{}, roots...), containerPaths...)

	// Dedupe by normalized path across both passes.
	candidates = dedupPaths(candidates)

	var orphans []OrphanRepo
	for _, repoPath := range candidates {
		absPath, _ := filepath.Abs(repoPath)
		if tracked[normPath(absPath)] {
			continue
		}

		o := OrphanRepo{Path: absPath, Nested: nestedSet[normPath(absPath)]}
		o.RelPath = relToRoots(relRoots, absPath)

		// Read origin remote URL.
		remoteURL, err := git.RemoteURL(absPath)
		if err != nil || remoteURL == "" {
			o.LocalOnly = true
			orphans = append(orphans, o)
			continue
		}
		o.RemoteURL = remoteURL

		// Parse remote URL.
		host, owner, repo, err := git.ParseRemoteURL(remoteURL)
		if err != nil {
			o.LocalOnly = true
			orphans = append(orphans, o)
			continue
		}
		o.Host = host
		o.Owner = owner
		o.Repo = repo
		o.RepoKey = owner + "/" + repo

		// Match against accounts using the richer identity signals.
		root := containingRoot(cfg.Global.Roots, absPath)
		mc := MatchContext{
			Host:         host,
			Owner:        owner,
			RemoteURL:    remoteURL,
			RepoPath:     absPath,
			ParentFolder: root,
		}
		m := matchAccount(cfg, mc)
		acctKey, srcKey := m.accountKey, m.sourceKey
		o.MatchedAccount = acctKey
		o.MatchedSource = srcKey
		o.AmbiguousCandidates = m.ambiguous
		o.Candidates = m.candidates
		if acctKey != "" {
			o.MatchScore = m.score
			o.Confident = m.score > hostWeight
		}

		// Compute expected path and relocation need: in place when the clone
		// sits at its standard spot under the root it was found in, else
		// under its source's root.
		if srcKey != "" {
			src := cfg.Sources[srcKey]
			sourceFolder := src.EffectiveFolder(srcKey)
			o.ExpectedRoot = cfg.SourceRoot(srcKey)
			if root != "" {
				if inPlace := status.ResolveRepoPath(root, sourceFolder, o.RepoKey, config.Repo{}); normPath(inPlace) == normPath(absPath) {
					o.ExpectedRoot = root
				}
			}
			o.ExpectedPath = status.ResolveRepoPath(o.ExpectedRoot, sourceFolder, o.RepoKey, config.Repo{})
			o.NeedsRelocate = normPath(absPath) != normPath(o.ExpectedPath)
		}

		orphans = append(orphans, o)
	}

	sort.Slice(orphans, func(i, j int) bool {
		return orphans[i].RelPath < orphans[j].RelPath
	})
	return orphans, nil
}

// gatherRoots returns the de-duplicated, tilde-expanded list of directories to
// scan for top-level clones: the configured roots first, then any
// caller-supplied ones. Empty entries are dropped; order is preserved.
func gatherRoots(configured, callerExtra []string) []string {
	var roots []string
	seen := make(map[string]bool)
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		abs := filepath.Clean(config.ExpandTilde(p))
		key := normPath(abs)
		if seen[key] {
			return
		}
		seen[key] = true
		roots = append(roots, abs)
	}
	for _, p := range configured {
		add(p)
	}
	for _, p := range callerExtra {
		add(p)
	}
	return roots
}

// containerRepoPaths resolves the on-disk path of every config repo flagged as a
// container, keeping only those that currently exist on disk.
func containerRepoPaths(cfg *config.Config) []string {
	var paths []string
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			if !repo.Container {
				continue
			}
			p := status.RepoPath(cfg, srcKey, repoKey, repo)
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// dedupPaths removes duplicate paths by normalized form, preserving order.
func dedupPaths(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := paths[:0]
	for _, p := range paths {
		k := normPath(p)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}

// relToRoots returns absPath relative to the deepest root that contains it
// (so nested clones display relative to their container), or the absolute path
// when no root contains it.
func relToRoots(roots []string, absPath string) string {
	bestRoot := ""
	for _, r := range roots {
		if r == "" {
			continue
		}
		if (normPath(absPath) == normPath(r) || isPathUnder(absPath, r)) && len(r) > len(bestRoot) {
			bestRoot = r
		}
	}
	if bestRoot != "" {
		if rel, err := filepath.Rel(bestRoot, absPath); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(absPath)
}

// isPathUnder reports whether child is strictly inside parent.
func isPathUnder(child, parent string) bool {
	c := normPath(child)
	p := normPath(parent)
	if c == p {
		return false
	}
	return strings.HasPrefix(c, p+"/")
}

// trackedPaths builds a set of normalized absolute paths for all configured repos.
func trackedPaths(cfg *config.Config) map[string]bool {
	paths := make(map[string]bool)
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			paths[normPath(status.RepoPath(cfg, srcKey, repoKey, src.Repos[repoKey]))] = true
		}
	}
	return paths
}

// containingRoot returns the deepest configured root that holds absPath,
// tilde-expanded, or "" when none does.
func containingRoot(roots []string, absPath string) string {
	best := ""
	for _, r := range roots {
		r = filepath.Clean(config.ExpandTilde(strings.TrimSpace(r)))
		if r != "." && isPathUnder(absPath, r) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

// MatchContext carries every signal MatchAccountEx uses to pick an account.
// Host and Owner are always required; the rest are optional and, when absent,
// simply don't contribute to scoring.
type MatchContext struct {
	Host         string // hostname parsed from the remote URL (or SSH alias)
	Owner        string // owner/org parsed from the remote URL path
	RemoteURL    string // full remote URL (used to extract an embedded user)
	RepoPath     string // repo path on disk (used for credential + parent-folder signals)
	ParentFolder string // the root holding the repo (used to compute parent-folder source match)
}

// Score weights for MatchAccountEx. The thresholds are picked so that:
//   - hostWeight alone never claims a match (ambiguous when ≥2 candidates tie).
//   - Any non-host signal unambiguously beats host-only.
//   - credentialWeight and urlUserWeight are the strongest — they directly name
//     the account's Username in the repo's own state.
const (
	hostWeight        = 1
	ownerWeight       = 3
	parentFolderScore = 5
	urlUserWeight     = 10
	credentialWeight  = 10
)

// MatchAccountEx scores every host-matching account against the signals in mc
// and returns the best match. Returns empty account/source keys when no
// account matches the host or when the top score is tied across ≥2 accounts
// (the tied candidates are returned in ambiguous).
func MatchAccountEx(cfg *config.Config, mc MatchContext) (accountKey, sourceKey string, ambiguous []string) {
	m := matchAccount(cfg, mc)
	return m.accountKey, m.sourceKey, m.ambiguous
}

// match is MatchAccountEx's full answer: the winner (empty on a tie), its
// score, the tied accounts, and every host-matching account, best first.
type match struct {
	accountKey, sourceKey string
	score                 int
	ambiguous             []string
	candidates            []string
}

func matchAccount(cfg *config.Config, mc MatchContext) match {
	type candidate struct {
		accountKey string
		sourceKey  string
		score      int
	}

	// Precompute signals that do not depend on any specific account.
	embeddedUser := ""
	if mc.RemoteURL != "" {
		embeddedUser = git.RemoteURLUser(mc.RemoteURL)
	}
	var credUsers []string
	if mc.RepoPath != "" {
		credUsers = git.CredentialUsernames(mc.RepoPath)
	}
	// First path component of the repo relative to the root holding it.
	// For a canonical layout "root/sourceFolder/owner/repo" this is the
	// source folder name; for a flat layout "root/sourceFolder/repo" it is
	// still the source folder name. That's the signal we want.
	repoSourceFolder := ""
	if mc.RepoPath != "" && mc.ParentFolder != "" {
		if rel, err := filepath.Rel(mc.ParentFolder, mc.RepoPath); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) > 0 {
				repoSourceFolder = parts[0]
			}
		}
	}

	// Iterate accounts deterministically so the scoring itself is stable
	// even before the tie-breaker decides ambiguity.
	acctKeys := make([]string, 0, len(cfg.Accounts))
	for k := range cfg.Accounts {
		acctKeys = append(acctKeys, k)
	}
	sort.Strings(acctKeys)

	var candidates []candidate
	for _, acctKey := range acctKeys {
		acct := cfg.Accounts[acctKey]
		if !HostMatches(acct, mc.Host) {
			continue
		}

		score := hostWeight

		// Owner == account Username.
		if acct.Username != "" && strings.EqualFold(mc.Owner, acct.Username) {
			score += ownerWeight
		}

		// Embedded HTTPS user.
		if embeddedUser != "" && acct.Username != "" && strings.EqualFold(embeddedUser, acct.Username) {
			score += urlUserWeight
		}

		// credential.<url>.username in the repo's own config.
		if acct.Username != "" {
			for _, cu := range credUsers {
				if strings.EqualFold(cu, acct.Username) {
					score += credentialWeight
					break
				}
			}
		}

		// Repo lives under this account's source folder.
		if repoSourceFolder != "" {
			for sk, src := range cfg.Sources {
				if src.Account != acctKey {
					continue
				}
				if strings.EqualFold(repoSourceFolder, src.EffectiveFolder(sk)) {
					score += parentFolderScore
					break
				}
			}
		}

		// Pick a source linked to this account (deterministic iteration).
		srcKey := ""
		srcKeys := make([]string, 0, len(cfg.Sources))
		for sk := range cfg.Sources {
			srcKeys = append(srcKeys, sk)
		}
		sort.Strings(srcKeys)
		for _, sk := range srcKeys {
			if cfg.Sources[sk].Account == acctKey {
				srcKey = sk
				break
			}
		}

		candidates = append(candidates, candidate{acctKey, srcKey, score})
	}

	if len(candidates) == 0 {
		return match{}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].accountKey < candidates[j].accountKey
	})

	top := candidates[0]
	all := make([]string, 0, len(candidates))
	for _, c := range candidates {
		all = append(all, c.accountKey)
	}
	// Collect every candidate tied at the top score.
	tied := []string{top.accountKey}
	for _, c := range candidates[1:] {
		if c.score == top.score {
			tied = append(tied, c.accountKey)
		}
	}
	if len(tied) >= 2 {
		return match{score: top.score, ambiguous: tied, candidates: all}
	}
	return match{accountKey: top.accountKey, sourceKey: top.sourceKey, score: top.score, candidates: all}
}

// HostMatches reports whether host, parsed from a remote URL, names the
// account's server: its URL host or its SSH alias.
func HostMatches(acct config.Account, host string) bool {
	if strings.EqualFold(host, HostnameFromURL(acct.URL)) {
		return true
	}
	return acct.SSH != nil && acct.SSH.Host != "" && strings.EqualFold(host, acct.SSH.Host)
}

// PlainRemoteURL builds a remote URL without embedded credentials.
// SSH: git@host:repo.git, HTTPS: https://user@host/repo.git
func PlainRemoteURL(acct config.Account, repoKey, credType string) string {
	switch credType {
	case "ssh":
		host := HostnameFromURL(acct.URL)
		if acct.SSH != nil && acct.SSH.Host != "" {
			host = acct.SSH.Host
		}
		return fmt.Sprintf("git@%s:%s.git", host, repoKey)
	default:
		hostname := HostnameFromURL(acct.URL)
		return fmt.Sprintf("https://%s@%s/%s.git", acct.Username, hostname, repoKey)
	}
}

// HostnameFromURL extracts the hostname from a URL like "https://github.com".
func HostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if h := u.Hostname(); h != "" {
		return h
	}
	return rawURL
}

// normPath normalizes a path for comparison (lowercase on Windows, clean).
func normPath(p string) string {
	p = filepath.Clean(p)
	// Case-insensitive comparison on Windows.
	return strings.ToLower(filepath.ToSlash(p))
}
