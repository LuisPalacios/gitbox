// Package adopt discovers git repos under the gitbox parent folder
// that are not tracked in gitbox.json and helps adopt them.
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
	RelPath             string   // relative to parent folder
	RemoteURL           string   // origin remote URL (empty if local-only)
	Host                string   // extracted hostname from remote
	Owner               string   // extracted owner/org from remote
	Repo                string   // extracted repo name from remote
	RepoKey             string   // "owner/repo" — the key for config.AddRepo
	MatchedAccount      string   // account key if matched, empty if unknown/ambiguous
	MatchedSource       string   // source key if matched, empty if needs creation
	ExpectedPath        string   // where gitbox convention says this should live
	NeedsRelocate       bool     // current path != expected path
	LocalOnly           bool     // no remote — cannot adopt
	AmbiguousCandidates []string // when multiple host-matching accounts tie with no disambiguator
	Nested              bool     // found inside a container repo's working tree — adopt in place, never relocate
}

// FindOrphans walks the gitbox parent folder, configured extra folders, and
// container repos' working trees, returning repos not tracked in config.
func FindOrphans(cfg *config.Config) ([]OrphanRepo, error) {
	return FindOrphansIn(cfg, nil)
}

// FindOrphansIn is FindOrphans with additional caller-supplied scan roots — used
// by the on-demand "scan this folder" picker. Each root is scanned for top-level
// clones; container repos (Repo.Container) are additionally scanned for nested
// clones up to Global.NestedScanDepth. Clones found outside the standard layout
// are reported with NeedsRelocate set, so adoption stores an absolute clone_folder.
func FindOrphansIn(cfg *config.Config, extraRoots []string) ([]OrphanRepo, error) {
	parentFolder := config.ExpandTilde(cfg.Global.Folder)

	// Build set of tracked repo paths from config.
	tracked := trackedPaths(cfg, parentFolder)

	// Pass 1 — flat scan of every root: the standard folder, configured extra
	// folders, and any caller-supplied roots. FindRepos stops at each .git
	// boundary, so this yields top-level clones only.
	roots := gatherRoots(parentFolder, cfg.Global.ExtraFolders, extraRoots)
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
	candidates := filterSubmodules(flat)

	// Pass 2 — nested clones inside container repos. These live inside another
	// clone's working tree, so they must bypass the submodule filter. Container
	// paths are also added to the root set so RelPath is computed against them.
	depth := cfg.Global.NestedScanDepthOrDefault()
	containerPaths := containerRepoPaths(cfg, parentFolder)
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
		mc := MatchContext{
			Host:         host,
			Owner:        owner,
			RemoteURL:    remoteURL,
			RepoPath:     absPath,
			ParentFolder: parentFolder,
		}
		acctKey, srcKey, ambiguous := MatchAccountEx(cfg, mc)
		o.MatchedAccount = acctKey
		o.MatchedSource = srcKey
		o.AmbiguousCandidates = ambiguous

		// Compute expected path and relocation need.
		if srcKey != "" {
			src := cfg.Sources[srcKey]
			sourceFolder := src.EffectiveFolder(srcKey)
			expected := status.ResolveRepoPath(parentFolder, sourceFolder, o.RepoKey, config.Repo{})
			o.ExpectedPath = expected
			o.NeedsRelocate = normPath(absPath) != normPath(expected)
		}

		orphans = append(orphans, o)
	}

	sort.Slice(orphans, func(i, j int) bool {
		return orphans[i].RelPath < orphans[j].RelPath
	})
	return orphans, nil
}

// filterSubmodules removes repos nested inside other repos (submodules).
// Input must be sorted. A repo whose path starts with a previous repo's path
// followed by a separator is considered a submodule and is dropped.
func filterSubmodules(sorted []string) []string {
	var result []string
	for _, p := range sorted {
		nested := false
		for _, parent := range result {
			norm := filepath.ToSlash(parent) + "/"
			if strings.HasPrefix(filepath.ToSlash(p), norm) {
				nested = true
				break
			}
		}
		if !nested {
			result = append(result, p)
		}
	}
	return result
}

// gatherRoots returns the de-duplicated, tilde-expanded list of directories to
// scan for top-level clones: the primary (standard) folder first, then the
// configured extra folders, then any caller-supplied roots. Empty entries are
// dropped; order is preserved with the primary folder first.
func gatherRoots(primary string, configExtra, callerExtra []string) []string {
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
	add(primary)
	for _, p := range configExtra {
		add(p)
	}
	for _, p := range callerExtra {
		add(p)
	}
	return roots
}

// containerRepoPaths resolves the on-disk path of every config repo flagged as a
// container, keeping only those that currently exist on disk.
func containerRepoPaths(cfg *config.Config, parentFolder string) []string {
	var paths []string
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		sourceFolder := src.EffectiveFolder(srcKey)
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			if !repo.Container {
				continue
			}
			p := status.ResolveRepoPath(parentFolder, sourceFolder, repoKey, repo)
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
func trackedPaths(cfg *config.Config, parentFolder string) map[string]bool {
	paths := make(map[string]bool)
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		sourceFolder := src.EffectiveFolder(srcKey)
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			p := status.ResolveRepoPath(parentFolder, sourceFolder, repoKey, repo)
			paths[normPath(p)] = true
		}
	}
	return paths
}

// MatchContext carries every signal MatchAccountEx uses to pick an account.
// Host and Owner are always required; the rest are optional and, when absent,
// simply don't contribute to scoring.
type MatchContext struct {
	Host         string // hostname parsed from the remote URL (or SSH alias)
	Owner        string // owner/org parsed from the remote URL path
	RemoteURL    string // full remote URL (used to extract an embedded user)
	RepoPath     string // repo path on disk (used for credential + parent-folder signals)
	ParentFolder string // gitbox parent folder (used to compute parent-folder source match)
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

// MatchAccount finds the best account + source for a remote host and owner.
// Returns (accountKey, sourceKey) — both empty if no match or the match is
// ambiguous.
//
// Kept for API compatibility; new callers should use MatchAccountEx to get
// access to the full signal set and ambiguity information.
func MatchAccount(cfg *config.Config, host, owner string) (string, string) {
	acct, src, _ := MatchAccountEx(cfg, MatchContext{Host: host, Owner: owner})
	return acct, src
}

// MatchAccountEx scores every host-matching account against the signals in mc
// and returns the best match. Returns empty account/source keys when no
// account matches the host or when the top score is tied across ≥2 accounts
// (the tied candidates are returned in ambiguous).
func MatchAccountEx(cfg *config.Config, mc MatchContext) (accountKey, sourceKey string, ambiguous []string) {
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
	// First path component of the repo relative to the gitbox parent folder.
	// For a canonical layout "parent/sourceFolder/owner/repo" this is the
	// source folder name; for a flat layout "parent/sourceFolder/repo" it is
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
		acctHost := HostnameFromURL(acct.URL)

		hostMatch := strings.EqualFold(mc.Host, acctHost)
		if !hostMatch && acct.SSH != nil && acct.SSH.Host != "" {
			hostMatch = strings.EqualFold(mc.Host, acct.SSH.Host)
		}
		if !hostMatch {
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
		return "", "", nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].accountKey < candidates[j].accountKey
	})

	top := candidates[0]
	// Collect every candidate tied at the top score.
	tied := []string{top.accountKey}
	for _, c := range candidates[1:] {
		if c.score == top.score {
			tied = append(tied, c.accountKey)
		}
	}
	if len(tied) >= 2 {
		return "", "", tied
	}
	return top.accountKey, top.sourceKey, nil
}

// PlainRemoteURL builds a remote URL without embedded credentials.
// SSH: git@host:repo.git, HTTPS: https://user@host/repo.git
func PlainRemoteURL(acct config.Account, repoKey, credType string) string {
	switch credType {
	case "ssh":
		host := acct.URL
		if acct.SSH != nil && acct.SSH.Host != "" {
			host = acct.SSH.Host
		} else {
			host = HostnameFromURL(acct.URL)
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
