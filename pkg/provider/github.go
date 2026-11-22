package provider

import (
	"context"
	"fmt"
	"net/url"
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

// checkAuth verifies the token and reads its expiry in a single request.
func (g *GitHub) checkAuth(ctx context.Context, baseURL, token, _ string) (time.Time, error) {
	var user struct{}
	headers, err := doGet(ctx, g.apiBase(baseURL)+"/user", g.authHeaders(token), &user)
	if err != nil {
		return time.Time{}, fmt.Errorf("github: %w", err)
	}
	return parseGitHubExpiry(headers.Get("GitHub-Authentication-Token-Expiration")), nil
}

// parseGitHubExpiry parses the GitHub-Authentication-Token-Expiration header:
// "2025-09-10 02:30:13 +0200" for fine-grained PATs, "2027-09-06 12:00:00 UTC"
// for classic ones. Tokens without an expiry don't send it; empty or
// unparsable values return the zero time.
func parseGitHubExpiry(v string) time.Time {
	v = strings.TrimSpace(v)
	for _, layout := range []string{"2006-01-02 15:04:05 -0700", "2006-01-02 15:04:05 MST"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (g *GitHub) ListRepos(ctx context.Context, baseURL, token, _ string) ([]RemoteRepo, error) {
	ctx, cancel := context.WithTimeout(ctx, listingTimeout)
	defer cancel()
	apiBase := g.apiBase(baseURL)
	headers := g.authHeaders(token)

	var all []RemoteRepo
	page := 1
	for {
		url := fmt.Sprintf("%s/user/repos?per_page=100&type=all&page=%d", apiBase, page)
		var batch []githubRepo
		_, err := doListGet(ctx, url, headers, &batch)
		if err != nil {
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

func (g *GitHub) ListUserOrgs(ctx context.Context, baseURL, token, _ string) (OrgList, error) {
	apiURL := g.apiBase(baseURL) + "/user/orgs?per_page=100"
	type organization struct {
		Login string `json:"login"`
	}
	orgs, complete, err := listNumberedPages[organization](ctx, apiURL, g.authHeaders(token), 10, nextFullPage(100))
	if err != nil {
		return OrgList{}, fmt.Errorf("github list orgs: %w", err)
	}
	result := OrgList{Names: make([]string, len(orgs)), Complete: complete}
	for i, org := range orgs {
		result.Names[i] = org.Login
	}
	return result, nil
}

// --- PRLister ---

// githubSearchItem is the shape returned by /search/issues for PRs.
type githubSearchItem struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	HTMLURL   string `json:"html_url"`
	RepoURL   string `json:"repository_url"` // "<api>/repos/owner/repo"
	UpdatedAt string `json:"updated_at"`
	Draft     bool   `json:"draft"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	PullRequest *struct{} `json:"pull_request"` // present only for PRs
}

type githubSearchResponse struct {
	IncompleteResults bool               `json:"incomplete_results"`
	TotalCount        int                `json:"total_count"`
	Items             []githubSearchItem `json:"items"`
}

// ListAccountPRs finds open PRs authored by the user and PRs where the user
// is a requested reviewer using two paginated GitHub search queries.
// Results are grouped by repo full name ("owner/repo").
func (g *GitHub) ListAccountPRs(ctx context.Context, baseURL, token, username string, includeDrafts bool) (AccountPRs, error) {
	if username == "" {
		return AccountPRs{}, fmt.Errorf("github list prs: empty username")
	}
	apiBase := g.apiBase(baseURL)
	headers := g.authHeaders(token)
	headers["Accept"] = "application/vnd.github+json"

	result := AccountPRs{ByRepo: map[string]PRSummary{}}

	authoredQ := fmt.Sprintf("is:open is:pr author:%s archived:false", username)
	if !includeDrafts {
		authoredQ += " draft:false"
	}
	authored, authoredComplete, err := g.searchPRs(ctx, apiBase, headers, authoredQ)
	if err != nil {
		return AccountPRs{}, fmt.Errorf("github authored PRs: %w", err)
	}
	for _, pr := range authored {
		sum := result.ByRepo[pr.RepoFull]
		sum.Authored = append(sum.Authored, pr)
		result.ByRepo[pr.RepoFull] = sum
	}

	reviewQ := fmt.Sprintf("is:open is:pr review-requested:%s archived:false draft:false", username)
	reviewRequested, reviewComplete, err := g.searchPRs(ctx, apiBase, headers, reviewQ)
	if err != nil {
		return AccountPRs{}, fmt.Errorf("github review-requested PRs: %w", err)
	}
	for _, pr := range reviewRequested {
		sum := result.ByRepo[pr.RepoFull]
		sum.ReviewRequested = append(sum.ReviewRequested, pr)
		result.ByRepo[pr.RepoFull] = sum
	}

	result.Complete = authoredComplete && reviewComplete
	return result, nil
}

// searchPRs runs /search/issues with pagination (up to 300 results) and
// returns normalized PullRequest values.
func (g *GitHub) searchPRs(ctx context.Context, apiBase string, headers map[string]string, query string) ([]PullRequest, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, listingTimeout)
	defer cancel()
	var out []PullRequest
	complete := true
	seen := 0
	for page := 1; page <= 3; page++ {
		reqURL := fmt.Sprintf("%s/search/issues?q=%s&per_page=100&page=%d", apiBase, url.QueryEscape(query), page)
		var resp githubSearchResponse
		pageHeaders, err := doListGet(ctx, reqURL, headers, &resp)
		if err != nil {
			return nil, false, err
		}
		complete = complete && !resp.IncompleteResults
		seen += len(resp.Items)
		for _, it := range resp.Items {
			if it.PullRequest == nil {
				continue
			}
			repoFull := repoFullFromAPI(it.RepoURL)
			pr := PullRequest{
				Number:   it.Number,
				Title:    it.Title,
				URL:      it.HTMLURL,
				Author:   it.User.Login,
				IsDraft:  it.Draft,
				RepoFull: repoFull,
			}
			if t, err := time.Parse(time.RFC3339, it.UpdatedAt); err == nil {
				pr.Updated = t
			}
			out = append(out, pr)
		}
		if len(resp.Items) < 100 && !hasNextPage(pageHeaders) && resp.TotalCount <= seen {
			return out, complete, nil
		}
	}
	return out, false, nil
}

// repoFullFromAPI extracts "owner/repo" from a repository_url like
// "https://api.github.com/repos/owner/repo".
func repoFullFromAPI(repoURL string) string {
	_, after, ok := strings.Cut(repoURL, "/repos/")
	if !ok {
		return ""
	}
	return strings.TrimSpace(after)
}

// --- RepoDeleter ---

func (g *GitHub) DeleteRepo(ctx context.Context, baseURL, token, _, owner, repoName string) error {
	if owner == "" || repoName == "" {
		return fmt.Errorf("github delete repo: owner and repo name required")
	}
	url := fmt.Sprintf("%s/repos/%s/%s", g.apiBase(baseURL), owner, repoName)
	if err := doDelete(ctx, url, g.authHeaders(token)); err != nil {
		if IsScopeDenied(err) {
			return &InsufficientScopesError{
				Provider:       "github",
				Action:         ActionDeleteRepo,
				RequiredScopes: ScopesForAction("github", ActionDeleteRepo),
				BaseURL:        baseURL,
				cause:          err,
			}
		}
		return fmt.Errorf("github delete repo: %w", err)
	}
	return nil
}

func (g *GitHub) RepoExists(ctx context.Context, baseURL, token, _, owner, repoName string) (bool, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", g.apiBase(baseURL), owner, repoName)
	var repo githubRepo
	if _, err := doGet(ctx, url, g.authHeaders(token), &repo); err != nil {
		if IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("github repo exists: %w", err)
	}
	return true, nil
}
