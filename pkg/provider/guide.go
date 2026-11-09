package provider

import (
	"fmt"
	"strings"
)

// TokenCreationURL returns the provider-specific URL where the user can create a PAT.
func TokenCreationURL(providerName, baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	switch providerName {
	case "github":
		return fmt.Sprintf("%s/settings/tokens/new", base)
	case "gitlab":
		return fmt.Sprintf("%s/-/user_settings/personal_access_tokens", base)
	case "gitea", "forgejo":
		return fmt.Sprintf("%s/user/settings/applications", base)
	case "bitbucket":
		return fmt.Sprintf("%s/account/settings/app-passwords/new", base)
	default:
		return ""
	}
}

// TokenRequiredScopes returns the scopes a gitbox PAT needs for a provider,
// in the provider's own vocabulary. Every PAT gitbox uses gets the same list:
// a Token account's PAT, an SSH companion PAT and the PAT a GCM account keeps
// for the API or mirrors. It covers discovery, clone and fetch, the owner
// pickers, repo creation and mirrors. Deleting repos needs one more scope,
// which ScopesForAction reports when a delete is refused.
//
// Keep it in step with "Token scopes by capability" in docs/credentials.md.
func TokenRequiredScopes(providerName string) string {
	switch providerName {
	case "github":
		return "repo, read:org"
	case "gitlab":
		return "api"
	case "gitea", "forgejo":
		return "repository (Read and Write), user (Read), organization (Read)"
	case "bitbucket":
		return "Repositories (Read, Write, Admin), Account (Read)"
	default:
		return "full repository permissions"
	}
}
