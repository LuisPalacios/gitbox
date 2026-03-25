package provider

import (
	"context"
	"fmt"
	"strings"
)

// GitLab implements Provider for GitLab.com and self-hosted GitLab instances.
type GitLab struct{}

type gitlabProject struct {
	PathWithNS string `json:"path_with_namespace"`
	Desc       string `json:"description"`
	HTTPURL    string `json:"http_url_to_repo"`
	SSHURL     string `json:"ssh_url_to_repo"`
	Visibility string `json:"visibility"`
	Archived   bool   `json:"archived"`
	ForkedFrom *struct {
		ID int `json:"id"`
	} `json:"forked_from_project"`
}

func (g *GitLab) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
	base := strings.TrimRight(baseURL, "/")
	headers := map[string]string{
		"PRIVATE-TOKEN": token,
	}

	var all []RemoteRepo
	page := 1
	for {
		url := fmt.Sprintf("%s/api/v4/projects?membership=true&per_page=100&page=%d", base, page)
		var batch []gitlabProject
		hdrs, err := doGet(ctx, url, headers, &batch)
		if err != nil {
			return nil, fmt.Errorf("gitlab: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, p := range batch {
			all = append(all, RemoteRepo{
				FullName:    p.PathWithNS,
				Description: p.Desc,
				CloneHTTPS:  p.HTTPURL,
				CloneSSH:    p.SSHURL,
				Private:     p.Visibility != "public",
				Fork:        p.ForkedFrom != nil,
				Archived:    p.Archived,
			})
		}
		// GitLab uses x-next-page header; empty means last page.
		next := hdrs.Get("X-Next-Page")
		if next == "" || len(batch) < 100 {
			break
		}
		page++
	}
	return all, nil
}
