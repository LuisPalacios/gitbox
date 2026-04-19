package git

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseRemoteURL extracts host, owner, and repo from any git remote URL.
//
// Supported formats:
//
//	SSH:   git@host:owner/repo.git
//	HTTPS: https://host/owner/repo.git
//	HTTPS: https://user@host/owner/repo.git
//	HTTPS: https://user:pass@host/owner/repo.git
//
// For nested paths (e.g., GitLab subgroups), owner contains the full group
// path: "group/subgroup" with repo = "project".
func ParseRemoteURL(rawURL string) (host, owner, repo string, err error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", "", "", fmt.Errorf("empty remote URL")
	}

	// SSH format: git@host:owner/repo.git (or user@host:path)
	if i := strings.Index(rawURL, "@"); i >= 0 && !strings.Contains(rawURL, "://") {
		// Everything after @ up to : is the host.
		rest := rawURL[i+1:]
		colonIdx := strings.Index(rest, ":")
		if colonIdx < 0 {
			return "", "", "", fmt.Errorf("SSH URL %q missing colon after host", rawURL)
		}
		host = rest[:colonIdx]
		path := rest[colonIdx+1:]
		path = strings.TrimSuffix(path, ".git")
		path = strings.Trim(path, "/")
		owner, repo = splitOwnerRepo(path)
		if owner == "" || repo == "" {
			return "", "", "", fmt.Errorf("SSH URL path %q does not contain owner/repo", path)
		}
		return host, owner, repo, nil
	}

	// HTTPS format: use standard URL parser.
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("parsing URL %q: %w", rawURL, err)
	}
	host = u.Hostname()
	if host == "" {
		return "", "", "", fmt.Errorf("URL %q has no hostname", rawURL)
	}
	path := strings.Trim(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	owner, repo = splitOwnerRepo(path)
	if owner == "" || repo == "" {
		return "", "", "", fmt.Errorf("URL path %q does not contain owner/repo", u.Path)
	}
	return host, owner, repo, nil
}

// RemoteURLUser extracts the user part of an HTTPS URL of the form
// "https://user@host/...". Returns "" for URLs without an embedded user,
// for SSH URLs (where the user is always "git" and carries no account signal),
// and for malformed input.
func RemoteURLUser(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || !strings.Contains(rawURL, "://") {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return ""
	}
	return u.User.Username()
}

// splitOwnerRepo splits "owner/repo" or "group/subgroup/repo" into
// (owner, repo) where owner may contain slashes for nested groups.
func splitOwnerRepo(path string) (owner, repo string) {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "", path
	}
	return path[:idx], path[idx+1:]
}
