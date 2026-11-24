package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"
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

func (b *Bitbucket) checkAuth(ctx context.Context, _, token, username string) (time.Time, error) {
	var user struct{}
	_, err := doGet(ctx, "https://api.bitbucket.org/2.0/user", b.authHeaders(token, username), &user)
	if err != nil {
		return time.Time{}, fmt.Errorf("bitbucket: %w", err)
	}
	return time.Time{}, nil
}

func (b *Bitbucket) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
	ctx, cancel := context.WithTimeout(ctx, listingTimeout)
	defer cancel()
	headers := b.authHeaders(token, username)

	url := fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s?pagelen=100", username)
	var all []RemoteRepo
	var err error

	for url != "" {
		var resp bitbucketResponse
		if _, err := doListGet(ctx, url, headers, &resp); err != nil {
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
		url, err = bitbucketNext(resp.Next)
		if err != nil {
			return nil, err
		}
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
		if IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("bitbucket repo exists: %w", err)
	}
	return true, nil
}

// --- OrgLister ---

func (b *Bitbucket) ListUserOrgs(ctx context.Context, _, token, username string) (OrgList, error) {
	ctx, cancel := context.WithTimeout(ctx, listingTimeout)
	defer cancel()
	apiURL := "https://api.bitbucket.org/2.0/user/permissions/workspaces?pagelen=100"
	headers := b.authHeaders(token, username)
	var result OrgList
	for range 10 {
		var resp struct {
			Values []struct {
				Workspace struct {
					Slug string `json:"slug"`
				} `json:"workspace"`
			} `json:"values"`
			Next string `json:"next"`
		}
		if _, err := doListGet(ctx, apiURL, headers, &resp); err != nil {
			return OrgList{}, fmt.Errorf("bitbucket list workspaces: %w", err)
		}
		for _, v := range resp.Values {
			if v.Workspace.Slug != username {
				result.Names = append(result.Names, v.Workspace.Slug)
			}
		}
		next, err := bitbucketNext(resp.Next)
		if err != nil {
			return OrgList{}, err
		}
		if next == "" {
			result.Complete = true
			break
		}
		apiURL = next
	}
	return result, nil
}

func bitbucketNext(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	next, err := url.Parse(raw)
	if err != nil || next.Scheme != "https" || next.Host != "api.bitbucket.org" || next.User != nil {
		return "", fmt.Errorf("bitbucket: invalid pagination link")
	}
	return raw, nil
}
