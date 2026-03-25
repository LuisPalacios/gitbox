package provider

import (
	"context"
	"fmt"
	"strings"
)

// GitHub implements Provider for GitHub.com and GitHub Enterprise.
type GitHub struct{}

type githubRepo struct {
	FullName string `json:"full_name"`
	Desc     string `json:"description"`
	CloneURL string `json:"clone_url"`
	SSHURL   string `json:"ssh_url"`
	Private  bool   `json:"private"`
	Fork     bool   `json:"fork"`
	Archived bool   `json:"archived"`
}

func (g *GitHub) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
	apiBase := "https://api.github.com"
	if baseURL != "" && baseURL != "https://github.com" {
		// GitHub Enterprise: <baseURL>/api/v3
		apiBase = strings.TrimRight(baseURL, "/") + "/api/v3"
	}

	headers := map[string]string{
		"Authorization": "Bearer " + token,
	}

	var all []RemoteRepo
	page := 1
	for {
		url := fmt.Sprintf("%s/user/repos?per_page=100&type=all&page=%d", apiBase, page)
		var batch []githubRepo
		if _, err := doGet(ctx, url, headers, &batch); err != nil {
			return nil, fmt.Errorf("github: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			all = append(all, RemoteRepo{
				FullName:    r.FullName,
				Description: r.Desc,
				CloneHTTPS:  r.CloneURL,
				CloneSSH:    r.SSHURL,
				Private:     r.Private,
				Fork:        r.Fork,
				Archived:    r.Archived,
			})
		}
		if len(batch) < 100 {
			break
		}
		page++
	}
	return all, nil
}
