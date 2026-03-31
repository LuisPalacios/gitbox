package provider

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Gitea implements Provider, RepoCreator, PushMirrorProvider, and PullMirrorProvider
// for Gitea and Forgejo instances (same API).
type Gitea struct{}

type giteaRepo struct {
	FullName string `json:"full_name"`
	Desc     string `json:"description"`
	CloneURL string `json:"clone_url"`
	SSHURL   string `json:"ssh_url"`
	Private  bool   `json:"private"`
	Fork     bool   `json:"fork"`
	Archived bool   `json:"archived"`
	Mirror   bool   `json:"mirror"`
}

// resolveAuth tries PAT auth ("token <PAT>"), falling back to Basic auth
// ("username:password") which works when the credential comes from GCM.
// Returns the working headers map.
func (g *Gitea) resolveAuth(ctx context.Context, baseURL, token, username string) (map[string]string, error) {
	base := strings.TrimRight(baseURL, "/")
	headers := map[string]string{
		"Authorization": "token " + token,
	}

	testURL := fmt.Sprintf("%s/api/v1/user/repos?limit=1&page=1", base)
	var testBatch []giteaRepo
	if _, err := doGet(ctx, testURL, headers, &testBatch); err != nil {
		basic := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
		headers["Authorization"] = "Basic " + basic
		testBatch = nil
		if _, err2 := doGet(ctx, testURL, headers, &testBatch); err2 != nil {
			return nil, fmt.Errorf("gitea: %w", err)
		}
	}
	return headers, nil
}

func (g *Gitea) ListRepos(ctx context.Context, baseURL, token, username string) ([]RemoteRepo, error) {
	base := strings.TrimRight(baseURL, "/")

	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return nil, err
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
				Mirror:      r.Mirror,
			})
		}
		if len(batch) < 50 {
			break
		}
		page++
	}
	return all, nil
}

// --- RepoInfoProvider ---

func (g *Gitea) GetRepoInfo(ctx context.Context, baseURL, token, username, owner, repo string) (RepoInfo, error) {
	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return RepoInfo{}, fmt.Errorf("gitea auth: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s", base, owner, repo)
	var result struct {
		DefaultBranch string `json:"default_branch"`
		Private       bool   `json:"private"`
	}
	if _, err := doGet(ctx, url, headers, &result); err != nil {
		return RepoInfo{}, fmt.Errorf("gitea get repo: %w", err)
	}
	info := RepoInfo{DefaultBranch: result.DefaultBranch, Private: result.Private}
	// Get HEAD commit from default branch.
	branchURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches/%s", base, owner, repo, result.DefaultBranch)
	var branch struct {
		Commit struct {
			ID        string `json:"id"`
			Timestamp string `json:"timestamp"` // ISO8601
		} `json:"commit"`
	}
	if _, err := doGet(ctx, branchURL, headers, &branch); err != nil {
		return info, nil
	}
	info.HeadCommit = branch.Commit.ID
	if t, err := time.Parse(time.RFC3339, branch.Commit.Timestamp); err == nil {
		info.CommitTime = t.Unix()
	}
	return info, nil
}

// --- RepoCreator ---

func (g *Gitea) CreateRepo(ctx context.Context, baseURL, token, username, owner, repoName, description string, private bool) error {
	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return fmt.Errorf("gitea auth: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	var apiURL string
	if owner == "" {
		apiURL = fmt.Sprintf("%s/api/v1/user/repos", base)
	} else {
		apiURL = fmt.Sprintf("%s/api/v1/orgs/%s/repos", base, owner)
	}
	body := fmt.Sprintf(`{"name":%q,"description":%q,"private":%t}`, repoName, description, private)
	_, err = doPost(ctx, apiURL, headers, strings.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("gitea create repo: %w", err)
	}
	return nil
}

// --- OrgLister ---

func (g *Gitea) ListUserOrgs(ctx context.Context, baseURL, token, username string) ([]string, error) {
	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return nil, fmt.Errorf("gitea auth: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	apiURL := fmt.Sprintf("%s/api/v1/user/orgs?limit=50", base)
	var orgs []struct {
		Username string `json:"username"`
	}
	if _, err := doGet(ctx, apiURL, headers, &orgs); err != nil {
		return nil, fmt.Errorf("gitea list orgs: %w", err)
	}
	result := make([]string, len(orgs))
	for i, o := range orgs {
		result[i] = o.Username
	}
	return result, nil
}

func (g *Gitea) RepoExists(ctx context.Context, baseURL, token, username, owner, repoName string) (bool, error) {
	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return false, fmt.Errorf("gitea auth: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s", base, owner, repoName)
	var repo giteaRepo
	if _, err := doGet(ctx, url, headers, &repo); err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("gitea repo exists: %w", err)
	}
	return true, nil
}

// --- PushMirrorProvider ---

type giteaPushMirror struct {
	ID           int64  `json:"id"`
	RemoteAddr   string `json:"remote_address"`
	Interval     string `json:"interval"`
	SyncOnCommit bool   `json:"sync_on_commit"`
}

func (g *Gitea) CreatePushMirror(ctx context.Context, baseURL, token, username, owner, repo, targetURL, targetToken string) error {
	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return fmt.Errorf("gitea auth: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/push_mirrors", base, owner, repo)
	body := fmt.Sprintf(`{"remote_address":%q,"remote_username":"token","remote_password":%q,"interval":"8h","sync_on_commit":true}`,
		targetURL, targetToken)
	_, err = doPost(ctx, url, headers, strings.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("gitea create push mirror: %w", err)
	}
	// Trigger immediate sync so the mirror doesn't wait for the first interval.
	// Note: /push_mirrors-sync is for push mirrors; /mirror-sync is for pull mirrors only.
	syncURL := fmt.Sprintf("%s/api/v1/repos/%s/%s/push_mirrors-sync", base, owner, repo)
	doPost(ctx, syncURL, headers, strings.NewReader(`{}`), nil) // best-effort, ignore errors
	return nil
}

func (g *Gitea) ListPushMirrors(ctx context.Context, baseURL, token, username, owner, repo string) ([]PushMirrorInfo, error) {
	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return nil, fmt.Errorf("gitea auth: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/push_mirrors", base, owner, repo)
	var mirrors []giteaPushMirror
	if _, err := doGet(ctx, url, headers, &mirrors); err != nil {
		return nil, fmt.Errorf("gitea list push mirrors: %w", err)
	}
	result := make([]PushMirrorInfo, len(mirrors))
	for i, m := range mirrors {
		result[i] = PushMirrorInfo{
			ID:           m.ID,
			RemoteURL:    m.RemoteAddr,
			Interval:     m.Interval,
			SyncOnCommit: m.SyncOnCommit,
		}
	}
	return result, nil
}

func (g *Gitea) DeletePushMirror(ctx context.Context, baseURL, token, username, owner, repo string, mirrorID int64) error {
	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return fmt.Errorf("gitea auth: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/push_mirrors/%d", base, owner, repo, mirrorID)
	if err := doDelete(ctx, url, headers); err != nil {
		return fmt.Errorf("gitea delete push mirror: %w", err)
	}
	return nil
}

// --- PullMirrorProvider ---

func (g *Gitea) CreatePullMirror(ctx context.Context, baseURL, token, username, repoName, sourceURL, sourceToken string, private bool) error {
	headers, err := g.resolveAuth(ctx, baseURL, token, username)
	if err != nil {
		return fmt.Errorf("gitea auth: %w", err)
	}
	base := strings.TrimRight(baseURL, "/")
	url := fmt.Sprintf("%s/api/v1/repos/migrate", base)
	// Pass source credentials via auth_token field, NOT embedded in the URL.
	body := fmt.Sprintf(`{"clone_addr":%q,"auth_token":%q,"repo_name":%q,"mirror":true,"private":%t,"service":"git"}`,
		sourceURL, sourceToken, repoName, private)
	_, err = doPost(ctx, url, headers, strings.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("gitea create pull mirror: %w", err)
	}
	return nil
}
