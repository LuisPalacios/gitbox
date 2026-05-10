package terminals

import (
	"github.com/LuisPalacios/gitbox/pkg/config"
)

// ComposeProfiles produces the auto-derived TerminalProfile set from the
// detected apps + shells, applying the OS-aware composition rules from
// issue #71.
//
// Rules per goos:
//
//	"windows" — A Profile is a Terminal × Shell pair. Bare-shell auto-
//	            Profiles (a row whose Terminal is itself the shell) are
//	            NOT emitted when at least one modern Terminal is installed.
//	            When no modern Terminal is installed, fall back to one
//	            bare-shell Profile per shell so the user isn't stranded.
//	"darwin" / "linux"
//	          — A Profile is Terminal-only (ShellID == ""). The login
//	            shell is implicit; the GUI/TUI render it as a dim badge
//	            next to the Terminal name. No Terminal × Shell auto-
//	            combinations on Unix.
//
// All emitted Profiles carry Source = "detected". Default flag is NOT set
// here — the caller (Sync) owns Default placement after merge so user's
// existing Default is preserved.
func ComposeProfiles(apps []config.TerminalApp, shells []config.ShellEntry, goos string) []config.TerminalProfile {
	if goos == "windows" {
		return composeWindows(apps, shells)
	}
	return composeUnix(apps)
}

// composeWindows produces Terminal × Shell pairs plus Hidden bare-shell
// "DIRECT" Profiles (issue #71).
//
// Visible auto-Profiles: every modern Terminal × every Shell.
// Hidden DIRECT Profiles: one per shell that Windows can launch without a
// terminal wrapper (pwsh, powershell, cmd, wsl). They're in the config so
// the user can un-hide them from the Manager when they want a quick "open
// the shell directly" shortcut, but stay out of the launcher menu by
// default since the modern Terminal × Shell rows cover the same shells in
// a richer wrapper.
//
// When no modern Terminal is installed the bare-shell Profiles become
// VISIBLE for every shell — that's the "no-modern-Terminal" fallback so
// the user isn't stranded.
func composeWindows(apps []config.TerminalApp, shells []config.ShellEntry) []config.TerminalProfile {
	var modern []config.TerminalApp
	for _, a := range apps {
		if templateAcceptsShell(a.ArgsTemplate) {
			modern = append(modern, a)
		}
	}
	if len(modern) == 0 {
		// Fallback: visible bare-shell Profile per shell so the user has
		// something to launch.
		out := make([]config.TerminalProfile, 0, len(shells))
		for _, sh := range shells {
			out = append(out, config.TerminalProfile{
				ID:         "bare-" + sh.ID,
				Name:       sh.Name,
				TerminalID: "",
				ShellID:    sh.ID,
				Source:     "detected",
			})
		}
		return out
	}
	out := make([]config.TerminalProfile, 0, len(modern)*len(shells)+len(windowsDirectShellIDs))
	for _, app := range modern {
		for _, sh := range shells {
			out = append(out, config.TerminalProfile{
				ID:         app.ID + "+" + sh.ID,
				Name:       app.Name + " — " + sh.Name,
				TerminalID: app.ID,
				ShellID:    sh.ID,
				Source:     "detected",
			})
		}
	}
	out = append(out, composeWindowsDirectShells(shells)...)
	return out
}

// windowsDirectShellIDs lists the shell ids that Windows can launch as a
// standalone process — they ship their own console host so they don't need
// a terminal wrapper. Order matches the issue #71 spec.
var windowsDirectShellIDs = []string{"pwsh", "powershell", "cmd", "wsl"}

// composeWindowsDirectShells returns the Hidden-by-default bare-shell
// Profiles for the 4 directly-launchable Windows shells. Each Profile has
// no Terminal — the launcher takes the shell's Command verbatim. The user
// un-hides via the Manager's eye toggle when they want the shortcut to
// surface in the launcher menu.
func composeWindowsDirectShells(shells []config.ShellEntry) []config.TerminalProfile {
	byID := make(map[string]config.ShellEntry, len(shells))
	for _, s := range shells {
		byID[s.ID] = s
	}
	out := make([]config.TerminalProfile, 0, len(windowsDirectShellIDs))
	for _, id := range windowsDirectShellIDs {
		sh, ok := byID[id]
		if !ok {
			continue
		}
		out = append(out, config.TerminalProfile{
			ID:         "bare-" + sh.ID,
			Name:       sh.Name + " (direct)",
			TerminalID: "",
			ShellID:    sh.ID,
			Source:     "detected",
			Hidden:     true,
		})
	}
	return out
}

// composeUnix produces Terminal-only Profiles. The implicit shell is the
// host's login shell; the GUI/TUI render its name as a dim metadata badge.
// Power users override the shell on a per-Profile basis via Add Profile.
func composeUnix(apps []config.TerminalApp) []config.TerminalProfile {
	out := make([]config.TerminalProfile, 0, len(apps))
	for _, app := range apps {
		out = append(out, config.TerminalProfile{
			ID:         app.ID,
			Name:       app.Name,
			TerminalID: app.ID,
			ShellID:    "", // implicit — login shell
			Source:     "detected",
		})
	}
	return out
}

// HasModernTerminal reports whether at least one TerminalApp on the host has
// shell-token-aware ArgsTemplate (i.e. is a modern terminal that can host a
// shell). Used by MissingModernTerminal — if false on Windows we surface the
// install banner.
func HasModernTerminal(apps []config.TerminalApp) bool {
	for _, a := range apps {
		if templateAcceptsShell(a.ArgsTemplate) {
			return true
		}
	}
	return false
}
