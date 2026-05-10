package config

import (
	"path/filepath"
	"regexp"
	"strings"
)

// MigrateLegacyTerminals converts a v2.0 GlobalConfig (flat Terminals[] of
// TerminalEntry) into the v2.1 three-array shape (TerminalApps + Shells +
// TerminalProfiles). Returns true when migration ran (caller persists), false
// when no work was needed.
//
// The migration is conservative: each legacy entry becomes one TerminalProfile
// whose Args carry the legacy argv verbatim, so launch behaviour is preserved
// byte-for-byte. The terminal app is inferred from the command basename
// (`wt.exe` → wt, `wezterm-gui.exe` → wezterm, `open` + `-a Name` → that mac
// app, otherwise a generated id from the basename). A Shell is inferred only
// when the legacy entry's command is itself a known bare shell (`pwsh.exe`,
// `bash`, …); WT-profile-derived entries leave ShellID empty and rely on Args.
//
// The first surviving Profile is marked Default. Subsequent re-syncs by the
// detection layer can refine names, attach Shell associations, and add new
// detected Profiles without disturbing the user's customisations.
func MigrateLegacyTerminals(g *GlobalConfig) bool {
	if g == nil {
		return false
	}
	if len(g.Terminals) == 0 {
		return false
	}
	if len(g.TerminalProfiles) > 0 || len(g.TerminalApps) > 0 {
		// Already in the new shape — even if a stale Terminals[] is present,
		// the new fields take precedence. Drop the legacy block silently to
		// avoid double-counting on the next save.
		g.Terminals = nil
		return true
	}

	apps := make(map[string]TerminalApp)
	shells := make(map[string]ShellEntry)
	var profiles []TerminalProfile

	for _, legacy := range g.Terminals {
		appID, app := inferTerminalApp(legacy)
		shellID, shell := inferShellFromLegacy(legacy)

		if _, ok := apps[appID]; !ok {
			apps[appID] = app
		}
		if shell.ID != "" {
			if _, ok := shells[shellID]; !ok {
				shells[shellID] = shell
			}
		}

		profiles = append(profiles, TerminalProfile{
			ID:         profileID(appID, shellID, legacy.Name, len(profiles)),
			Name:       legacy.Name,
			TerminalID: appID,
			ShellID:    shellID,
			Args:       cloneStrings(legacy.Args),
			Source:     "migrated",
		})
	}

	if len(profiles) > 0 {
		profiles[0].Default = true
	}

	g.TerminalApps = sortedApps(apps)
	g.Shells = sortedShells(shells)
	g.TerminalProfiles = profiles
	g.Terminals = nil
	return true
}

// inferTerminalApp maps a legacy TerminalEntry to a TerminalApp by inspecting
// the command basename and (for `open -a <App>` on macOS) its first args. The
// returned id is stable: the same legacy command always yields the same app id
// across re-migrations and across hosts.
func inferTerminalApp(t TerminalEntry) (string, TerminalApp) {
	base := strings.ToLower(filepath.Base(t.Command))
	switch base {
	case "wt.exe":
		return "wt", TerminalApp{
			ID:           "wt",
			Name:         "Windows Terminal",
			Command:      t.Command,
			ArgsTemplate: []string{"-d", "{path}", "{shell_command}", "{shell_args}"},
		}
	case "wezterm.exe", "wezterm-gui.exe", "wezterm", "wezterm-gui":
		return "wezterm", TerminalApp{
			ID:           "wezterm",
			Name:         "WezTerm",
			Command:      t.Command,
			ArgsTemplate: []string{"start", "--cwd", "{path}", "--", "{shell_command}", "{shell_args}"},
		}
	case "open":
		// macOS `open -a <App>` pattern.
		if len(t.Args) >= 2 && t.Args[0] == "-a" {
			appName := t.Args[1]
			id := "macapp-" + slugify(appName)
			return id, TerminalApp{
				ID:           id,
				Name:         appName,
				Command:      t.Command,
				ArgsTemplate: []string{"-a", appName},
			}
		}
	case "gnome-terminal":
		return "gnome-terminal", TerminalApp{
			ID:           "gnome-terminal",
			Name:         "GNOME Terminal",
			Command:      t.Command,
			ArgsTemplate: []string{"--working-directory={path}", "--", "{shell_command}", "{shell_args}"},
		}
	case "konsole":
		return "konsole", TerminalApp{
			ID:           "konsole",
			Name:         "Konsole",
			Command:      t.Command,
			ArgsTemplate: []string{"--workdir", "{path}", "-e", "{shell_command}", "{shell_args}"},
		}
	case "kitty":
		return "kitty", TerminalApp{
			ID:           "kitty",
			Name:         "Kitty",
			Command:      t.Command,
			ArgsTemplate: []string{"--directory={path}", "{shell_command}", "{shell_args}"},
		}
	case "alacritty":
		return "alacritty", TerminalApp{
			ID:           "alacritty",
			Name:         "Alacritty",
			Command:      t.Command,
			ArgsTemplate: []string{"--working-directory", "{path}", "-e", "{shell_command}", "{shell_args}"},
		}
	case "xfce4-terminal":
		return "xfce4-terminal", TerminalApp{
			ID:           "xfce4-terminal",
			Name:         "Xfce Terminal",
			Command:      t.Command,
			ArgsTemplate: []string{"--working-directory={path}"},
		}
	case "terminator":
		return "terminator", TerminalApp{
			ID:           "terminator",
			Name:         "Terminator",
			Command:      t.Command,
			ArgsTemplate: []string{"--working-directory={path}"},
		}
	}
	// Fallback: treat the legacy entry as a "shell-only" pseudo-app so it can
	// still be launched. This covers bare cmd.exe / pwsh.exe / git-bash.exe /
	// wsl.exe / standalone bash / zsh entries that some users hand-edit.
	id := "legacy-" + slugify(strings.TrimSuffix(base, filepath.Ext(base)))
	return id, TerminalApp{
		ID:      id,
		Name:    t.Name,
		Command: t.Command,
	}
}

// inferShellFromLegacy attempts to associate a known shell with a legacy
// TerminalEntry. Today we only attach a shell when the legacy command IS a
// bare shell (the entry was a standalone shell launch, not a terminal hosting
// a shell). WT-profile-derived entries return ("", zero) — they keep their
// argv verbatim via TerminalProfile.Args and do not need a Shell.
func inferShellFromLegacy(t TerminalEntry) (string, ShellEntry) {
	base := strings.ToLower(filepath.Base(t.Command))
	switch base {
	case "cmd.exe":
		return "cmd", ShellEntry{ID: "cmd", Name: "Command Prompt", Command: t.Command}
	case "powershell.exe":
		return "powershell", ShellEntry{ID: "powershell", Name: "PowerShell 5", Command: t.Command}
	case "pwsh.exe", "pwsh":
		return "pwsh", ShellEntry{ID: "pwsh", Name: "PowerShell 7", Command: t.Command}
	case "git-bash.exe":
		return "git-bash", ShellEntry{ID: "git-bash", Name: "Git Bash", Command: t.Command}
	case "wsl.exe":
		// Distinguish per-distro WSL launches via the -d <name> flag if present.
		if dist := wslDistroFromArgs(t.Args); dist != "" {
			id := "wsl-" + slugify(dist)
			return id, ShellEntry{
				ID:      id,
				Name:    "WSL — " + dist,
				Command: t.Command,
				Args:    []string{"-d", dist},
			}
		}
		return "wsl", ShellEntry{ID: "wsl", Name: "WSL (default distro)", Command: t.Command}
	case "bash":
		return "bash", ShellEntry{ID: "bash", Name: "Bash", Command: t.Command}
	case "zsh":
		return "zsh", ShellEntry{ID: "zsh", Name: "Zsh", Command: t.Command}
	case "fish":
		return "fish", ShellEntry{ID: "fish", Name: "Fish", Command: t.Command}
	}
	return "", ShellEntry{}
}

// wslDistroFromArgs scans a legacy argv for `-d <name>` or `--distribution <name>`
// and returns the distro name when present. Used during migration of standalone
// `wsl.exe -d Ubuntu-24.04` entries.
func wslDistroFromArgs(args []string) string {
	for i, a := range args {
		if (a == "-d" || a == "--distribution") && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "--distribution=") {
			return strings.TrimPrefix(a, "--distribution=")
		}
	}
	return ""
}

// profileID generates a stable identifier for a migrated Profile.
// Deterministic across re-migrations of the same legacy config.
func profileID(appID, shellID, name string, fallbackIdx int) string {
	base := slugify(name)
	if base == "" {
		if shellID != "" {
			base = appID + "-" + shellID
		} else {
			base = appID
		}
	}
	if base == "" {
		base = "profile-" + intToString(fallbackIdx)
	}
	return base
}

// slugify lower-cases a string and replaces every non-[a-z0-9] run with a
// single hyphen, trimming leading/trailing hyphens. Output is stable across
// runs and OS-locale-independent.
var slugifyRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	if s == "" {
		return ""
	}
	out := slugifyRe.ReplaceAllString(strings.ToLower(s), "-")
	return strings.Trim(out, "-")
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	negative := false
	if n < 0 {
		negative = true
		n = -n
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// sortedApps returns a slice of TerminalApps in deterministic id order. The
// migrator emits one app per unique inferred id, but the iteration order over
// a Go map is random; sorting keeps round-trips stable for tests and JSON
// diffs.
func sortedApps(m map[string]TerminalApp) []TerminalApp {
	keys := sortedKeys(m)
	out := make([]TerminalApp, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func sortedShells(m map[string]ShellEntry) []ShellEntry {
	keys := sortedKeys(m)
	out := make([]ShellEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Tiny lexicographic sort; the migrator handles at most ~20 entries so
	// avoiding a sort.Strings import keeps this file dependency-light.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
