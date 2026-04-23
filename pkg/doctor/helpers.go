package doctor

import (
	"fmt"
	"runtime"
	"strings"
)

// CredentialType names used by the gitbox config (keep in sync with
// pkg/config). Point-of-use helpers accept a string so callers don't need to
// import pkg/config here.
const (
	CredTypeGCM   = "gcm"
	CredTypeSSH   = "ssh"
	CredTypeToken = "token"
)

// CredentialPrecheck is the shape point-of-use callers (GUI/TUI) get when
// they ask "am I ready to pick this credential type?". Missing is empty when
// everything required is installed.
type CredentialPrecheck struct {
	OK      bool     // true when nothing required is missing
	Missing []Result // tool Results for binaries that are not installed
	Summary string   // one-line description suitable for a banner
	Hint    string   // multi-line install hint(s) for the missing tool(s)
}

// PrecheckForCredentialType returns what the user needs (and is missing) to
// configure the given credential type. Unknown types resolve to an empty OK
// result — not our problem to validate config shape here.
func PrecheckForCredentialType(credType string) CredentialPrecheck {
	switch credType {
	case CredTypeGCM:
		return precheck([]Tool{toolGit(), toolGCM()}, "Git Credential Manager is required for HTTPS (GCM) credentials.")
	case CredTypeSSH:
		return precheck([]Tool{toolGit(), toolSSH(), toolSSHKeygen(), toolSSHAdd()}, "OpenSSH tools are required for SSH credentials.")
	case CredTypeToken:
		// Token credentials use git's built-in `store` credential helper
		// (see pkg/credential/repoconfig.go: configureToken) writing a
		// credential-store format file under ~/.config/gitbox/credentials/.
		// No GCM is involved — only git itself.
		return precheck([]Tool{toolGit()}, "Git is required to store tokens via the built-in credential-store helper.")
	default:
		return CredentialPrecheck{OK: true}
	}
}

// precheck runs Check on the given tools and assembles a user-facing summary.
func precheck(tools []Tool, context string) CredentialPrecheck {
	results := Check(tools)
	var missing []Result
	for _, r := range results {
		if !r.Found {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return CredentialPrecheck{OK: true}
	}
	names := make([]string, 0, len(missing))
	for _, m := range missing {
		names = append(names, m.Tool.DisplayName)
	}
	summary := fmt.Sprintf("%s Missing: %s.", context, strings.Join(names, ", "))

	var hintLines []string
	for _, m := range missing {
		hint := m.InstallHint()
		if hint == "" {
			hint = fmt.Sprintf("install %s for %s", m.Tool.DisplayName, runtime.GOOS)
		}
		hintLines = append(hintLines, fmt.Sprintf("• %s → %s", m.Tool.DisplayName, hint))
	}
	return CredentialPrecheck{
		OK:      false,
		Missing: missing,
		Summary: summary,
		Hint:    strings.Join(hintLines, "\n"),
	}
}
