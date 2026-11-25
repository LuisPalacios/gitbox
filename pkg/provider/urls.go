package provider

import "net/url"

// InjectTokenInURL embeds a token into a clone URL for authenticated push.
// "https://github.com/user/repo.git" → "https://token:<token>@github.com/user/repo.git"
//
// Used by server-side mirror setup.
func InjectTokenInURL(cloneURL, token string) string {
	u, err := url.Parse(cloneURL)
	if err != nil {
		return cloneURL
	}
	u.User = url.UserPassword("token", token)
	return u.String()
}
