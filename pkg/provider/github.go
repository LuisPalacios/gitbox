package provider

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GitHub implements Provider and RepoCreator for GitHub.com and GitHub Enterprise.
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

func (g *GitHub) apiBase(baseURL string) string {
	if baseURL == "" || baseURL == "https://github.com" {
		return "https://api.github.com"
	}
	return strings.TrimRight(baseURL, "/") + "/api/v3"
}

func (g *GitHub) authHeaders(token string) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + token,
	}
}

// --- RepoInfoProvider ---

func (g *GitHub) GetRepoInfo(ctx context.Context, baseURL, token, _, owner, repo string) (RepoInfo, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s", g.apiBase(baseURL), owner, repo)
	var result struct {
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
	}
	if _, err := doGet(ctx, apiURL, g.authHeaders(token), &result); err != nil {
		return RepoInfo{}, fmt.Errorf("github get repo: %w", err)
	}
	info := RepoInfo{DefaultBranch: result.DefaultBranch, Private: result.Private}
	branchURL := fmt.Sprintf("%s/repos/%s/%s/branches/%s", g.apiBase(baseURL), owner, repo, result.DefaultBranch)
	var branch struct {
		Commit struct {
			SHA    string `json:"sha"`
			Commit struct {
				Author struct {
					Date string `json:"date"` // ISO8601
				} `json:"author"`
			} `json:"commit"`
		} `json:"commit"`
	}
	if _, err := doGet(ctx, branchURL, g.authHeaders(token), &branch); err != nil {
		return info, nil
	}
	info.HeadCommit = branch.Commit.SHA
	if t, err := time.Parse(time.RFC3339, branch.Commit.Commit.Author.Date); err == nil {
		info.CommitTime = t.Unix()
	}
	return info, nil
}

// --- RepoCreator ---

func (g *GitHub) CreateRepo(ctx context.Context, baseURL, token, _, owner, repoName, description string, private bool) error {
	var apiURL string
	if owner == "" {
		apiURL = fmt.Sprintf("%s/user/repos", g.apiBase(baseURL))
	} else {
		apiURL = fmt.Sprintf("%s/orgs/%s/repos", g.apiBase(baseURL), owner)
	}
	body := fmt.Sprintf(`{"name":%q,"description":%q,"private":%t}`, repoName, description, private)
	_, err := doPost(ctx, apiURL, g.authHeaders(token), strings.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("github create repo: %w", err)
	}
	return nil
}

// --- OrgLister ---

func (g *GitHub) ListUserOrgs(ctx context.Context, baseURL, token, _ string) ([]string, error) {
	apiURL := fmt.Sprintf("%s/user/orgs?per_page=100", g.apiBase(baseURL))
	var orgs []struct {
		Login string `json:"login"`
	}
	if _, err := doGet(ctx, apiURL, g.authHeaders(token), &orgs); err != nil {
		return nil, fmt.Errorf("github list orgs: %w", err)
	}
	result := make([]string, len(orgs))
	for i, o := range orgs {
		result[i] = o.Login
	}
	return result, nil
}

func (g *GitHub) RepoExists(ctx context.Context, baseURL, token, _, owner, repoName string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", g.apiBase(baseURL), owner, repoName)
	var repo githubRepo
	if _, err := doGet(ctx, url, g.authHeaders(token), &repo); err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("github repo exists: %w", err)
	}
	return true, nil
}
