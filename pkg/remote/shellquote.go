package remote

import (
	"strings"
)

// Family is the quoting syntax of a shell: how one word is written so the
// shell reads it back unchanged (#243). A terminal or an AI harness that
// opens a remote folder passes the folder to the remote login shell inside
// one command string, so the folder is quoted for that shell and nothing
// else.
type Family string

const (
	// FamilyPOSIX covers sh, bash, zsh, dash, ksh and mksh.
	FamilyPOSIX Family = "posix"
	// FamilyFish is fish, whose single quotes take \\ and \' as escapes.
	FamilyFish Family = "fish"
	// FamilyCmd is cmd.exe, the default shell of Win32-OpenSSH.
	FamilyCmd Family = "cmd"
	// FamilyPowerShell is Windows PowerShell and pwsh.
	FamilyPowerShell Family = "powershell"
	// FamilyOther is any other login shell (csh, tcsh, nu, xonsh, …). The
	// command hands the work to sh in single-quoted words only, which every
	// shell of that list reads the same way.
	FamilyOther Family = "other"
)

// ShellFamily maps the login shell the probe reported ($SHELL) to its
// quoting family. An empty or unknown shell is FamilyOther.
func ShellFamily(shell string) Family {
	i := strings.LastIndexAny(shell, `/\`)
	name := strings.TrimSuffix(strings.ToLower(shell[i+1:]), ".exe")
	switch name {
	case "sh", "bash", "zsh", "dash", "ksh", "mksh", "ash":
		return FamilyPOSIX
	case "fish":
		return FamilyFish
	case "cmd":
		return FamilyCmd
	case "powershell", "pwsh":
		return FamilyPowerShell
	}
	return FamilyOther
}

// QuotePOSIX quotes s as one word for a POSIX shell: single quotes, with
// each single quote written as '\”. FamilyOther uses it too: csh and tcsh
// read it the same way.
func QuotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// QuoteFish quotes s as one word for fish. Inside fish's single quotes a
// backslash escapes another backslash or a single quote, so both are
// escaped; every other character stays literal.
func QuoteFish(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// QuotePowerShell quotes s as one word for PowerShell: single quotes, with
// each single quote doubled. PowerShell also takes the typographic quotes
// ‘ ’ ‚ ‛ as single quotes, so those are doubled as well.
func QuotePowerShell(s string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'', '‘', '’', '‚', '‛':
			b.WriteRune(r)
		}
		b.WriteRune(r)
	}
	b.WriteByte('\'')
	return b.String()
}

// cmdUnsafe are the characters cmd.exe interprets even inside double
// quotes: % expands a variable, ! does with delayed expansion, and " ends
// the quotes. No escape works inside quotes, so a word that holds one is
// refused.
const cmdUnsafe = "%!\""

// QuoteCmd quotes s as one word for cmd.exe: inside double quotes, where
// & | < > ( ) ^ are literal. ok is false when s holds a character cmd
// interprets even there (cmdUnsafe).
func QuoteCmd(s string) (quoted string, ok bool) {
	if strings.ContainsAny(s, cmdUnsafe) {
		return "", false
	}
	return `"` + s + `"`, true
}
