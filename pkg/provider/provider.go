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
}

// Provider can list repositories visible to an authenticated user.
type Provider interface {
	// ListRepos returns every repo visible to the authenticated user,
	// handling pagination internally.
	ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error)
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
