// Package adopt discovers git repos under the gitbox parent folder
// that are not tracked in gitbox.json and helps adopt them.
package adopt

import (
	"fmt"
	"net/url"
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
}

// FindOrphans walks the gitbox parent folder and returns repos not in config.
func FindOrphans(cfg *config.Config) ([]OrphanRepo, error) {
	parentFolder := config.ExpandTilde(cfg.Global.Folder)

	// Find all repos on disk.
	allPaths, err := git.FindRepos(parentFolder)
	if err != nil {
		return nil, err
	}
	sort.Strings(allPaths)

	// Build set of tracked repo paths from config.
	tracked := trackedPaths(cfg, parentFolder)

	// Filter out submodules: any repo nested inside another repo is a submodule.
	// Since paths are sorted, a nested repo always comes after its parent.
	topLevel := filterSubmodules(allPaths)

	var orphans []OrphanRepo
	for _, repoPath := range topLevel {
		absPath, _ := filepath.Abs(repoPath)
		if tracked[normPath(absPath)] {
			continue
		}

		o := OrphanRepo{Path: absPath}
		if rel, err := filepath.Rel(parentFolder, absPath); err == nil {
			o.RelPath = filepath.ToSlash(rel)
		}

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
