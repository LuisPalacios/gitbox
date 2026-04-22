package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// Bitbucket implements Provider for Bitbucket Cloud.
type Bitbucket struct{}

type bitbucketResponse struct {
	Values []bitbucketRepo `json:"values"`
	Next   string          `json:"next"`
}

type bitbucketRepo struct {
	FullName string `json:"full_name"`
	Desc     string `json:"description"`
	IsPriv   bool   `json:"is_private"`
	Links    struct {
		Clone []struct {
			Name string `json:"name"`
			Href string `json:"href"`
		} `json:"clone"`
	} `json:"links"`
	Parent *struct {
		FullName string `json:"full_name"`
	} `json:"parent"`
}

func (b *Bitbucket) authHeaders(token, username string) map[string]string {
	basicAuth := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
	return map[string]string{
		"Authorization": "Basic " + basicAuth,
	}
}

func (b *Bitbucket) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
	headers := b.authHeaders(token, username)

	url := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s?pagelen=100", username)
	var all []RemoteRepo

	for url != "" {
		var resp bitbucketResponse
		if _, err := doGet(ctx, url, headers, &resp); err != nil {
			return nil, fmt.Errorf("bitbucket: %w", err)
		}
		for _, r := range resp.Values {
			repo := RemoteRepo{
				FullName:    r.FullName,
				Description: r.Desc,
				Private:     r.IsPriv,
				Fork:        r.Parent != nil,
			}
			for _, c := range r.Links.Clone {
				switch c.Name {
				case "https":
					repo.CloneHTTPS = c.Href
				case "ssh":
					repo.CloneSSH = c.Href
				}
			}
			all = append(all, repo)
		}
		url = resp.Next
	}
	return all, nil
}

// --- RepoCreator ---

func (b *Bitbucket) CreateRepo(ctx context.Context, _, token, username, owner, repoName, description string, private bool) error {
	workspace := username
	if owner != "" {
		workspace = owner
	}
	apiURL := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s", workspace, repoName)
	scm := "git"
	body := fmt.Sprintf(`{"scm":%q,"name":%q,"description":%q,"is_private":%t}`, scm, repoName, description, private)
	_, err := doPost(ctx, apiURL, b.authHeaders(token, username), strings.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("bitbucket create repo: %w", err)
	}
	return nil
}

func (b *Bitbucket) RepoExists(ctx context.Context, _, token, username, owner, repoName string) (bool, error) {
	workspace := owner
	if workspace == "" {
		workspace = username
	}
	apiURL := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s", workspace, repoName)
	var repo bitbucketRepo
	if _, err := doGet(ctx, apiURL, b.authHeaders(token, username), &repo); err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("bitbucket repo exists: %w", err)
	}
	return true, nil
}

// --- RepoDeleter ---

func (b *Bitbucket) DeleteRepo(ctx context.Context, _, token, username, owner, repoName string) error {
	if repoName == "" {
		return fmt.Errorf("bitbucket delete repo: repo name required")
	}
	workspace := owner
	if workspace == "" {
		workspace = username
	}
	apiURL := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s", workspace, repoName)
	if err := doDelete(ctx, apiURL, b.authHeaders(token, username)); err != nil {
		if IsForbiddenError(err) {
			return &InsufficientScopesError{
				Provider:       "bitbucket",
				Action:         ActionDeleteRepo,
				RequiredScopes: ScopesForAction("bitbucket", ActionDeleteRepo),
				BaseURL:        "https://bitbucket.org",
				cause:          err,
			}
		}
		return fmt.Errorf("bitbucket delete repo: %w", err)
	}
	return nil
}

// --- OrgLister ---

func (b *Bitbucket) ListUserOrgs(ctx context.Context, _, token, username string) ([]string, error) {
	apiURL := "https://api.bitbucket.org/2.0/user/permissions/workspaces?pagelen=100"
	var resp struct {
		Values []struct {
			Workspace struct {
				Slug string `json:"slug"`
			} `json:"workspace"`
		} `json:"values"`
	}
	if _, err := doGet(ctx, apiURL, b.authHeaders(token, username), &resp); err != nil {
		return nil, fmt.Errorf("bitbucket list workspaces: %w", err)
	}
	var result []string
	for _, v := range resp.Values {
		if v.Workspace.Slug != username {
			result = append(result, v.Workspace.Slug)
		}
	}
	return result, nil
}
