package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// Gitea implements Provider for Gitea and Forgejo instances (same API).
type Gitea struct{}

type giteaRepo struct {
	FullName string `json:"full_name"`
	Desc     string `json:"description"`
	CloneURL string `json:"clone_url"`
	SSHURL   string `json:"ssh_url"`
	Private  bool   `json:"private"`
	Fork     bool   `json:"fork"`
	Archived bool   `json:"archived"`
}

func (g *Gitea) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
	base := strings.TrimRight(baseURL, "/")

	// Try PAT auth first ("token <PAT>").
	// If it fails with 401, retry with Basic auth ("username:password") which
	// works when the credential comes from GCM (stores password, not PAT).
	headers := map[string]string{
		"Authorization": "token " + token,
	}

	testURL := fmt.Sprintf("%s/api/v1/user/repos?limit=1&page=1", base)
	var testBatch []giteaRepo
	if _, err := doGet(ctx, testURL, headers, &testBatch); err != nil {
		// PAT auth failed — try Basic auth.
		basic := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
		headers["Authorization"] = "Basic " + basic
		testBatch = nil
		if _, err2 := doGet(ctx, testURL, headers, &testBatch); err2 != nil {
			return nil, fmt.Errorf("gitea: %w", err)
		}
	}

	var all []RemoteRepo
	page := 1
	for {
		url := fmt.Sprintf("%s/api/v1/user/repos?limit=50&page=%d", base, page)
		var batch []giteaRepo
		if _, err := doGet(ctx, url, headers, &batch); err != nil {
			return nil, fmt.Errorf("gitea: %w", err)
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
		if len(batch) < 50 {
			break
		}
		page++
	}
	return all, nil
}
