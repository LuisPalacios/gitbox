package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// GitLab implements Provider, RepoCreator, and PushMirrorProvider
// for GitLab.com and self-hosted GitLab instances.
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

func (g *GitLab) authHeaders(token string) map[string]string {
	return map[string]string{
		"PRIVATE-TOKEN": token,
	}
}

// --- RepoInfoProvider ---

func (g *GitLab) GetRepoInfo(ctx context.Context, baseURL, token, _, owner, repo string) (RepoInfo, error) {
	base := strings.TrimRight(baseURL, "/")
	encoded := url.PathEscape(owner + "/" + repo)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s", base, encoded)
	var proj struct {
		DefaultBranch string `json:"default_branch"`
		Visibility    string `json:"visibility"`
	}
	if _, err := doGet(ctx, apiURL, g.authHeaders(token), &proj); err != nil {
		return RepoInfo{}, fmt.Errorf("gitlab get repo: %w", err)
	}
	info := RepoInfo{DefaultBranch: proj.DefaultBranch, Private: proj.Visibility != "public"}
	branchURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/branches/%s", base, encoded, proj.DefaultBranch)
	var branch struct {
		Commit struct {
			ID          string `json:"id"`
			AuthoredDate string `json:"authored_date"` // ISO8601
		} `json:"commit"`
	}
	if _, err := doGet(ctx, branchURL, g.authHeaders(token), &branch); err != nil {
		return info, nil
	}
	info.HeadCommit = branch.Commit.ID
	if t, err := time.Parse(time.RFC3339, branch.Commit.AuthoredDate); err == nil {
		info.CommitTime = t.Unix()
	}
	return info, nil
}

// --- RepoCreator ---

func (g *GitLab) CreateRepo(ctx context.Context, baseURL, token, _, owner, repoName, description string, private bool) error {
	base := strings.TrimRight(baseURL, "/")
	apiURL := fmt.Sprintf("%s/api/v4/projects", base)
	visibility := "private"
	if !private {
		visibility = "public"
	}
	body := fmt.Sprintf(`{"name":%q,"description":%q,"visibility":%q`, repoName, description, visibility)
	if owner != "" {
		// Resolve namespace ID for the group.
		nsURL := fmt.Sprintf("%s/api/v4/namespaces?search=%s", base, url.QueryEscape(owner))
		var namespaces []struct {
			ID       int64  `json:"id"`
			FullPath string `json:"full_path"`
		}
		if _, err := doGet(ctx, nsURL, g.authHeaders(token), &namespaces); err == nil {
			for _, ns := range namespaces {
				if strings.EqualFold(ns.FullPath, owner) {
					body += fmt.Sprintf(`,"namespace_id":%d`, ns.ID)
					break
				}
			}
		}
	}
	body += "}"
	_, err := doPost(ctx, apiURL, g.authHeaders(token), strings.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("gitlab create repo: %w", err)
	}
	return nil
}

// --- OrgLister ---

func (g *GitLab) ListUserOrgs(ctx context.Context, baseURL, token, _ string) ([]string, error) {
	base := strings.TrimRight(baseURL, "/")
	// min_access_level=30 = Developer or higher (can create projects).
	apiURL := fmt.Sprintf("%s/api/v4/groups?min_access_level=30&per_page=100", base)
	var groups []struct {
		FullPath string `json:"full_path"`
	}
	if _, err := doGet(ctx, apiURL, g.authHeaders(token), &groups); err != nil {
		return nil, fmt.Errorf("gitlab list groups: %w", err)
	}
	result := make([]string, len(groups))
	for i, gr := range groups {
		result[i] = gr.FullPath
	}
	return result, nil
}

func (g *GitLab) RepoExists(ctx context.Context, baseURL, token, _, owner, repoName string) (bool, error) {
	base := strings.TrimRight(baseURL, "/")
	// GitLab uses URL-encoded "owner/repo" as the project ID.
	encoded := url.PathEscape(owner + "/" + repoName)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s", base, encoded)
	var proj gitlabProject
	if _, err := doGet(ctx, apiURL, g.authHeaders(token), &proj); err != nil {
		if strings.Contains(err.Error(), "404") {
			return false, nil
		}
		return false, fmt.Errorf("gitlab repo exists: %w", err)
	}
	return true, nil
}

// --- PRLister ---

type gitlabMR struct {
	IID       int    `json:"iid"`
	Title     string `json:"title"`
	WebURL    string `json:"web_url"`
	UpdatedAt string `json:"updated_at"`
	Draft     bool   `json:"draft"`
	Author    struct {
		Username string `json:"username"`
	} `json:"author"`
	References struct {
		Full string `json:"full"` // "group/proj!123"
	} `json:"references"`
}

// ListAccountPRs implements PRLister for GitLab using the `merge_requests` API.
// Two scoped calls: authored (`scope=created_by_me`) and reviewer-requested.
func (g *GitLab) ListAccountPRs(ctx context.Context, baseURL, token, username string, includeDrafts bool) (AccountPRs, error) {
	if username == "" {
		return AccountPRs{}, fmt.Errorf("gitlab list prs: empty username")
	}
	base := strings.TrimRight(baseURL, "/")
	headers := g.authHeaders(token)

	result := AccountPRs{ByRepo: map[string]PRSummary{}}

	authoredURL := fmt.Sprintf("%s/api/v4/merge_requests?scope=created_by_me&state=opened&per_page=100", base)
	authored, err := g.listMRs(ctx, authoredURL, headers, includeDrafts)
	if err != nil {
		return AccountPRs{}, fmt.Errorf("gitlab authored MRs: %w", err)
	}
	for _, pr := range authored {
		sum := result.ByRepo[pr.RepoFull]
		sum.Authored = append(sum.Authored, pr)
		result.ByRepo[pr.RepoFull] = sum
	}

	reviewerURL := fmt.Sprintf("%s/api/v4/merge_requests?scope=all&state=opened&reviewer_username=%s&per_page=100",
		base, url.QueryEscape(username))
	reviewer, err := g.listMRs(ctx, reviewerURL, headers, true) // reviewer list ignores draft filter
	if err != nil {
		return AccountPRs{}, fmt.Errorf("gitlab reviewer MRs: %w", err)
	}
	for _, pr := range reviewer {
		if pr.IsDraft {
			continue
		}
		sum := result.ByRepo[pr.RepoFull]
		sum.ReviewRequested = append(sum.ReviewRequested, pr)
		result.ByRepo[pr.RepoFull] = sum
	}

	return result, nil
}

func (g *GitLab) listMRs(ctx context.Context, reqURL string, headers map[string]string, includeDrafts bool) ([]PullRequest, error) {
	var mrs []gitlabMR
	if _, err := doGet(ctx, reqURL, headers, &mrs); err != nil {
		return nil, err
	}
	out := make([]PullRequest, 0, len(mrs))
	for _, mr := range mrs {
		if mr.Draft && !includeDrafts {
			continue
		}
		full := mr.References.Full
		if idx := strings.Index(full, "!"); idx > 0 {
			full = full[:idx]
		}
		pr := PullRequest{
			Number:   mr.IID,
			Title:    mr.Title,
			URL:      mr.WebURL,
			Author:   mr.Author.Username,
			IsDraft:  mr.Draft,
			RepoFull: full,
		}
		if t, err := time.Parse(time.RFC3339, mr.UpdatedAt); err == nil {
			pr.Updated = t
		}
		out = append(out, pr)
	}
	return out, nil
}

// --- PushMirrorProvider ---

type gitlabRemoteMirror struct {
	ID      int64  `json:"id"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
}

// resolveProjectID looks up the numeric GitLab project ID for owner/repo.
func (g *GitLab) resolveProjectID(ctx context.Context, baseURL, token, owner, repo string) (int64, error) {
	base := strings.TrimRight(baseURL, "/")
	encoded := url.PathEscape(owner + "/" + repo)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s", base, encoded)
	var proj struct {
		ID int64 `json:"id"`
	}
	if _, err := doGet(ctx, apiURL, g.authHeaders(token), &proj); err != nil {
		return 0, fmt.Errorf("gitlab resolve project: %w", err)
	}
	return proj.ID, nil
}

func (g *GitLab) CreatePushMirror(ctx context.Context, baseURL, token, _, owner, repo, targetURL, targetToken string) error {
	projectID, err := g.resolveProjectID(ctx, baseURL, token, owner, repo)
	if err != nil {
		return err
	}
	base := strings.TrimRight(baseURL, "/")
	apiURL := fmt.Sprintf("%s/api/v4/projects/%d/remote_mirrors", base, projectID)
	body := fmt.Sprintf(`{"url":%q,"enabled":true,"only_protected_branches":false}`,
		injectTokenInURL(targetURL, targetToken))
	_, err = doPost(ctx, apiURL, g.authHeaders(token), strings.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("gitlab create push mirror: %w", err)
	}
	return nil
}

func (g *GitLab) ListPushMirrors(ctx context.Context, baseURL, token, _, owner, repo string) ([]PushMirrorInfo, error) {
	projectID, err := g.resolveProjectID(ctx, baseURL, token, owner, repo)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(baseURL, "/")
	apiURL := fmt.Sprintf("%s/api/v4/projects/%d/remote_mirrors", base, projectID)
	var mirrors []gitlabRemoteMirror
	if _, err := doGet(ctx, apiURL, g.authHeaders(token), &mirrors); err != nil {
		return nil, fmt.Errorf("gitlab list push mirrors: %w", err)
	}
	result := make([]PushMirrorInfo, len(mirrors))
	for i, m := range mirrors {
		result[i] = PushMirrorInfo{
			ID:        m.ID,
			RemoteURL: m.URL,
		}
	}
	return result, nil
}

func (g *GitLab) DeletePushMirror(ctx context.Context, baseURL, token, _, owner, repo string, mirrorID int64) error {
	projectID, err := g.resolveProjectID(ctx, baseURL, token, owner, repo)
	if err != nil {
		return err
	}
	base := strings.TrimRight(baseURL, "/")
	apiURL := fmt.Sprintf("%s/api/v4/projects/%d/remote_mirrors/%d", base, projectID, mirrorID)
	if err := doDelete(ctx, apiURL, g.authHeaders(token)); err != nil {
		return fmt.Errorf("gitlab delete push mirror: %w", err)
	}
	return nil
}

// injectTokenInURL embeds a token into a clone URL for authenticated push.
// "https://github.com/user/repo.git" → "https://token:<token>@github.com/user/repo.git"
func injectTokenInURL(cloneURL, token string) string {
	u, err := url.Parse(cloneURL)
	if err != nil {
		return cloneURL
	}
	u.User = url.UserPassword("token", token)
	return u.String()
}
