package provider

import "net/url"

// InjectTokenInURL embeds a token into a clone URL for authenticated push.
// "https://github.com/user/repo.git" → "https://token:<token>@github.com/user/repo.git"
//
// Used by mirror setup (GitLab remote_mirrors) and by the move flow
// (git push --mirror <authenticated-url>) where the caller needs a URL
// that carries the destination credential inline. Moved to its own file
// so pkg/move can reuse it without a provider-specific import.
func InjectTokenInURL(cloneURL, token string) string {
	u, err := url.Parse(cloneURL)
	if err != nil {
		return cloneURL
	}
	u.User = url.UserPassword("token", token)
	return u.String()
}
