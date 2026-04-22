package provider

import "context"

// RepoDeleter can permanently delete a repository on the provider.
// Implemented by GitHub, GitLab, Gitea/Forgejo, and Bitbucket.
//
// Destructive. Callers must verify ownership and user intent before
// invoking. The interface is intentionally narrow — there is no "soft
// delete" variant because no supported provider offers one through the
// same API path.
//
// Token scopes required per provider:
//   - GitHub:         delete_repo
//   - GitLab:         api
//   - Gitea/Forgejo:  write:repository (or admin on the target)
//   - Bitbucket:      repository:delete
type RepoDeleter interface {
	DeleteRepo(ctx context.Context, baseURL, token, username, owner, repoName string) error
}
