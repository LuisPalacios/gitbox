package git

import (
	"os/exec"
	"runtime"
	"strings"
)

// RepoWebURL constructs the web URL for a repository from an account base
// URL and a repo key (e.g. "org/repo").
func RepoWebURL(accountURL, repoKey string) string {
	return strings.TrimRight(accountURL, "/") + "/" + repoKey
}

// AccountProfileURL constructs the provider profile/org web URL from an
// account base URL and a username. Works across GitHub, GitLab, Gitea,
// Forgejo, and Bitbucket — all use <base>/<username> for user and org pages.
func AccountProfileURL(accountURL, username string) string {
	return strings.TrimRight(accountURL, "/") + "/" + username
}

// OpenInBrowser opens a URL in the default browser.
// Do NOT use HideWindow here — these launch GUI apps (browser windows).
func OpenInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use rundll32 instead of "cmd /c start" to avoid a console window flash.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
