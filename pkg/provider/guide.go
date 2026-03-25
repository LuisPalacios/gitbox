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
		sb.WriteString("  3. Scopes:\n")
		sb.WriteString("     - repo (full control of private repositories)\n")
		sb.WriteString("     - read:user\n")
		sb.WriteString("  4. Generate and copy the token\n")

	case "gitlab":
		sb.WriteString(fmt.Sprintf("  1. Visit: %s/-/user_settings/personal_access_tokens\n", base))
		sb.WriteString(fmt.Sprintf("  2. Name:  gitbox-%s\n", accountKey))
		sb.WriteString("  3. Scopes: api (full API access, read+write repos)\n")
		sb.WriteString("  4. Create and copy the token\n")

	case "gitea", "forgejo":
		sb.WriteString(fmt.Sprintf("  1. Visit: %s/user/settings/applications\n", base))
		sb.WriteString(fmt.Sprintf("  2. Name:  gitbox-%s\n", accountKey))
		sb.WriteString("  3. Permissions:\n")
		sb.WriteString("     - repository: Read and Write\n")
		sb.WriteString("     - user: Read\n")
		sb.WriteString("     - organization: Read\n")
		sb.WriteString("  4. Generate and copy the token\n")

	case "bitbucket":
		sb.WriteString(fmt.Sprintf("  1. Visit: %s/account/settings/app-passwords/new\n", base))
		sb.WriteString(fmt.Sprintf("  2. Label: gitbox-%s\n", accountKey))
		sb.WriteString("  3. Permissions: Repositories — Read, Write\n")
		sb.WriteString("  4. Create and copy the app password\n")

	default:
		sb.WriteString("  This provider does not have a known token creation URL.\n")
		sb.WriteString("  Create a token with full repository permissions and store it.\n")
	}

	sb.WriteString(fmt.Sprintf("\nThen store it:\n  gitboxcmd account credential setup %s\n", accountKey))
	return sb.String()
}

// TokenCreationURL returns the provider-specific URL where the user can create a PAT.
func TokenCreationURL(providerName, baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	switch providerName {
	case "github":
		return fmt.Sprintf("URL to create the token: %s/settings/tokens/new\nRequired scopes: repo (full control of private repositories), read:user", base)
	case "gitlab":
		return fmt.Sprintf("URL to create the token: %s/-/user_settings/personal_access_tokens\nRequired scopes: api (full API access, read+write repos)", base)
	case "gitea", "forgejo":
		return fmt.Sprintf("URL to create the token: %s/user/settings/applications\nRequired permissions: repository (Read+Write), user (Read), organization (Read)", base)
	case "bitbucket":
		return fmt.Sprintf("URL to create the token: %s/account/settings/app-passwords/new\nRequired permissions: Repositories — Read, Write", base)
	default:
		return "Create a token with full repository permissions at your provider."
	}
}
