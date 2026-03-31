// Package provider defines the interface and implementations for Git hosting provider APIs.
package provider

import (
	"context"
	"fmt"
)

// RemoteRepo is the normalized representation of a repository returned by any provider.
type RemoteRepo struct {
	FullName    string `json:"full_name"`    // "org/repo" format
	Description string `json:"description"`
	CloneHTTPS  string `json:"clone_https"`
	CloneSSH    string `json:"clone_ssh"`
	Private     bool   `json:"private"`
	Fork        bool   `json:"fork"`
	Archived    bool   `json:"archived"`
	Mirror      bool   `json:"mirror"`
}

// Provider can list repositories visible to an authenticated user.
type Provider interface {
	// ListRepos returns every repo visible to the authenticated user,
	// handling pagination internally.
	ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error)
}

// RepoCreator can create repositories on a provider and check existence.
// Implemented by GitHub, GitLab, Gitea, and Forgejo.
// The username parameter is required for Gitea/Forgejo auth probing (PAT vs Basic).
// If owner is empty, the repo is created under the authenticated user's personal namespace.
// If owner is non-empty, the repo is created under that organization/group.
type RepoCreator interface {
	CreateRepo(ctx context.Context, baseURL, token, username, owner, repoName, description string, private bool) error
	RepoExists(ctx context.Context, baseURL, token, username, owner, repoName string) (bool, error)
}

// OrgLister can list organizations/groups the authenticated user belongs to.
type OrgLister interface {
	ListUserOrgs(ctx context.Context, baseURL, token, username string) ([]string, error)
}

// PushMirrorInfo describes a server-side push mirror on a repository.
type PushMirrorInfo struct {
	ID         int64  `json:"id"`
	RemoteURL  string `json:"remote_url"`
	Interval   string `json:"interval"`
	SyncOnCommit bool `json:"sync_on_commit"`
}

// PushMirrorProvider can set up server-side push mirrors.
// Implemented by Forgejo/Gitea and GitLab.
type PushMirrorProvider interface {
	CreatePushMirror(ctx context.Context, baseURL, token, username, owner, repo, targetURL, targetToken string) error
	ListPushMirrors(ctx context.Context, baseURL, token, username, owner, repo string) ([]PushMirrorInfo, error)
	DeletePushMirror(ctx context.Context, baseURL, token, username, owner, repo string, mirrorID int64) error
}

// PullMirrorProvider can create pull mirrors (repos that sync from an external source).
// Implemented by Forgejo/Gitea.
type PullMirrorProvider interface {
	CreatePullMirror(ctx context.Context, baseURL, token, username, repoName, sourceURL, sourceToken string, private bool) error
}

// RepoInfo contains basic repository metadata for sync comparison.
type RepoInfo struct {
	DefaultBranch string `json:"default_branch"`
	HeadCommit    string `json:"head_commit"`      // SHA of latest commit on default branch
	CommitTime    int64  `json:"commit_time"`       // Unix timestamp of HEAD commit (0 if unknown)
	Private       bool   `json:"private"`
}

// RepoInfoProvider can fetch basic repo metadata. Used for mirror sync checks.
type RepoInfoProvider interface {
	GetRepoInfo(ctx context.Context, baseURL, token, username, owner, repo string) (RepoInfo, error)
}

// TestAuth makes a minimal API call to verify credentials work.
// Returns nil if authenticated successfully, error otherwise.
func TestAuth(ctx context.Context, providerName, baseURL, token, username string) error {
	prov, err := ByName(providerName)
	if err != nil {
		return err
	}
	repos, err := prov.ListRepos(ctx, baseURL, token, username)
	if err != nil {
		return err
	}
	_ = repos
	return nil
}

// ByName returns the Provider implementation for a given provider name.
func ByName(name string) (Provider, error) {
	switch name {
	case "github":
		return &GitHub{}, nil
	case "gitea", "forgejo":
		return &Gitea{}, nil
	case "gitlab":
		return &GitLab{}, nil
	case "bitbucket":
		return &Bitbucket{}, nil
	default:
		return nil, fmt.Errorf("provider %q does not support repository discovery", name)
	}
}
