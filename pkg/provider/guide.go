package provider

import (
	"fmt"
	"strings"
)

// TokenSetupGuide returns a user-friendly message explaining how to create
// and store a PAT for the given provider. Used by CLI and GUI when no token is found.
func TokenSetupGuide(providerName, baseURL, accountKey string) string {
	base := strings.TrimRight(baseURL, "/")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("No API token found for account %q.\n\n", accountKey))
	sb.WriteString("A Personal Access Token (PAT) is needed for discovery and working with repos.\n\n")

	switch providerName {
	case "github":
		sb.WriteString(fmt.Sprintf("  1. Visit: %s/settings/tokens/new\n", base))
		sb.WriteString(fmt.Sprintf("  2. Name:  gitbox-%s\n", accountKey))
		sb.WriteString("  3. Scopes, grouped by what you plan to do:\n")
		sb.WriteString("     - repo            — discovery, clone/fetch, create repos (required)\n")
		sb.WriteString("     - read:org        — list your orgs for dest-owner pickers\n")
		sb.WriteString("     - delete_repo     — needed ONLY if you want gitbox to delete\n")
		sb.WriteString("                         repos (move with \"delete source\" opt-in).\n")
		sb.WriteString("                         Sensitive — add it only if you need it.\n")
		sb.WriteString("     - workflow        — optional, if you want gitbox to touch GH Actions\n")
		sb.WriteString("  4. Generate and copy the token\n")

	case "gitlab":
		sb.WriteString(fmt.Sprintf("  1. Visit: %s/-/user_settings/personal_access_tokens\n", base))
		sb.WriteString(fmt.Sprintf("  2. Name:  gitbox-%s\n", accountKey))
		sb.WriteString("  3. Scopes — GitLab uses one coarse scope for everything:\n")
		sb.WriteString("     - api             — discovery, clone/fetch, create, delete,\n")
		sb.WriteString("                         and mirror remotes. Required.\n")
		sb.WriteString("  4. Create and copy the token\n")

	case "gitea", "forgejo":
		sb.WriteString(fmt.Sprintf("  1. Visit: %s/user/settings/applications\n", base))
		sb.WriteString(fmt.Sprintf("  2. Name:  gitbox-%s\n", accountKey))
		sb.WriteString("  3. Permissions, grouped by use case:\n")
		sb.WriteString("     - repository:     Read+Write — discovery, clone/fetch, create,\n")
		sb.WriteString("                       delete, mirror. Required.\n")
		sb.WriteString("     - user:           Read — resolves your username / org list.\n")
		sb.WriteString("     - organization:   Read — for dest-owner pickers targeting orgs.\n")
		sb.WriteString("  4. Generate and copy the token\n")

	case "bitbucket":
		sb.WriteString(fmt.Sprintf("  1. Visit: %s/account/settings/app-passwords/new\n", base))
		sb.WriteString(fmt.Sprintf("  2. Label: gitbox-%s\n", accountKey))
		sb.WriteString("  3. Permissions:\n")
		sb.WriteString("     - Repositories: Read, Write   — discovery, clone/fetch\n")
		sb.WriteString("     - Repositories: Admin         — required to create repos\n")
		sb.WriteString("     - Repositories: Delete        — required only for \"delete source\"\n")
		sb.WriteString("                                    in a move. Sensitive.\n")
		sb.WriteString("  4. Create and copy the app password\n")

	default:
		sb.WriteString("  This provider does not have a known token creation URL.\n")
		sb.WriteString("  Create a token with full repository permissions and store it.\n")
	}

	sb.WriteString(fmt.Sprintf("\nThen store it:\n  gitbox account credential setup %s\n", accountKey))
	return sb.String()
}

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

// DiscoveryRequiredScopes returns the scopes needed for the SSH companion PAT.
// This token is used for discovery (listing repos) AND creating new repos,
// so it needs read/write repo access plus user/org read.
func DiscoveryRequiredScopes(providerName string) string {
	switch providerName {
	case "github":
		return "repo (read/write), read:user, read:org"
	case "gitlab":
		return "api (full API access)"
	case "gitea", "forgejo":
		return "repository (Read/Write), user (Read), organization (Read)"
	case "bitbucket":
		return "Repositories — Read/Write, Account — Read"
	default:
		return "repository read/write, user read"
	}
}

// TokenRequiredScopes returns a human-readable description of the required
// token scopes/permissions for a provider.
func TokenRequiredScopes(providerName string) string {
	switch providerName {
	case "github":
		return "repo (full control of private repositories), read:user"
	case "gitlab":
		return "api (full API access, read+write repos)"
	case "gitea", "forgejo":
		return "repository (Read+Write), user (Read), organization (Read)"
	case "bitbucket":
		return "Repositories — Read, Write"
	default:
		return "full repository permissions"
	}
}
