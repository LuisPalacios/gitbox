// Package adopt discovers git repos under the gitbox parent folder
// that are not tracked in gitbox.json and helps adopt them.
package adopt

import (
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
	Path           string // absolute path on disk
	RelPath        string // relative to parent folder
	RemoteURL      string // origin remote URL (empty if local-only)
	Host           string // extracted hostname from remote
	Owner          string // extracted owner/org from remote
	Repo           string // extracted repo name from remote
	RepoKey        string // "owner/repo" — the key for config.AddRepo
	MatchedAccount string // account key if matched, empty if unknown
	MatchedSource  string // source key if matched, empty if needs creation
	ExpectedPath   string // where gitbox convention says this should live
	NeedsRelocate  bool   // current path != expected path
	LocalOnly      bool   // no remote — cannot adopt
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

		// Match against accounts.
		acctKey, srcKey := MatchAccount(cfg, host, owner)
		o.MatchedAccount = acctKey
		o.MatchedSource = srcKey

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

// MatchAccount finds the best account + source for a remote host and owner.
// Returns (accountKey, sourceKey) — both empty if no match.
func MatchAccount(cfg *config.Config, host, owner string) (string, string) {
	type candidate struct {
		accountKey string
		sourceKey  string
		score      int // higher is better
	}
	var candidates []candidate

	for acctKey, acct := range cfg.Accounts {
		acctHost := hostnameFromURL(acct.URL)

		// Check direct hostname match.
		hostMatch := strings.EqualFold(host, acctHost)

		// Check SSH host alias match (e.g., host="gitbox-github-luis", alias="gitbox-github-luis").
		if !hostMatch && acct.SSH != nil && acct.SSH.Host != "" {
			hostMatch = strings.EqualFold(host, acct.SSH.Host)
		}

		if !hostMatch {
			continue
		}

		// Score: host match = 1, username match = +2
		score := 1
		if strings.EqualFold(owner, acct.Username) {
			score += 2
		}

		// Find a source linked to this account.
		srcKey := ""
		for sk, src := range cfg.Sources {
			if src.Account == acctKey {
				srcKey = sk
				break
			}
		}

		candidates = append(candidates, candidate{acctKey, srcKey, score})
	}

	if len(candidates) == 0 {
		return "", ""
	}

	// Pick the best match (highest score).
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	return candidates[0].accountKey, candidates[0].sourceKey
}

// hostnameFromURL extracts the hostname from a URL like "https://github.com".
func hostnameFromURL(rawURL string) string {
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
