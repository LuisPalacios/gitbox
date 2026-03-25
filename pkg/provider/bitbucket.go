package provider

import (
	"context"
	"encoding/base64"
	"fmt"
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

func (b *Bitbucket) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
	// Bitbucket Cloud uses HTTP Basic with username:app_password.
	basicAuth := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
	headers := map[string]string{
		"Authorization": "Basic " + basicAuth,
	}

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
