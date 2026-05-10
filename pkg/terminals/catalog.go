// Package terminals owns the supported-terminal/shell vocabulary plus the
// host-detection, OS-aware Profile composition, and merge logic that keeps
// gitbox.json's terminal_apps[] / shells[] / terminal_profiles[] in sync
// with what is actually installed.
//
// This package replaces the markdown-driven detection that previously lived
// across cmd/gui/profiles.go, cmd/gui/app.go, and pkg/harness/{terminal,shell}-
// directory.md. The catalog is now a typed compiled-in list per OS — gitbox
// owns the supported-vocabulary explicitly and adds new entries via code
// changes, not by editing markdown.
//
// Public surface:
//
//	Sync(cfg, goos)              — top-level entry point: detect → compose → merge
//	MissingModernTerminal(cfg)   — drives the "install Windows Terminal" banner
//
// Everything else is an implementation detail consumed by Sync.
package terminals

import (
	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// CatalogTerminal describes one terminal emulator gitbox knows how to detect
// and launch. Probe returns the resolved binary path (or "open" on macOS for
// app-bundle entries) when the terminal is installed.
type CatalogTerminal struct {
	ID           string
	Name         string
	OS           string                  // "windows" | "darwin" | "linux"
	Probe        func() (string, bool)   // resolved command path + installed?
	ProbeArgs    func() []string         // optional resolved args (mac `open -a <name>`); nil for plain binaries
	ArgsTemplate []string                // launch template — see pkg/launch tokens
}

// CatalogShell describes one command-line interpreter gitbox knows how to
// detect and launch. Args is the default flag list (e.g. ["-l"] for login
// shells); per-distro WSL discovery emits its own entries at runtime.
type CatalogShell struct {
	ID      string
	Name    string
	OS      string
	Probe   func() (string, bool) // resolved binary path + installed?
	Args    []string              // default flags
}

// modernTerminal reports whether a terminal entry is "modern" — i.e. it can
// host an arbitrary shell, identified by the presence of a shell token in
// its launch template. Bare-shell-as-terminal rows (Git Bash, PowerShell-as-
// terminal) have no shell tokens and are not modern.
//
// This avoids an explicit IsModern flag that would drift from the templates.
func modernTerminal(t CatalogTerminal) bool {
	return templateAcceptsShell(t.ArgsTemplate)
}

// templateAcceptsShell reports whether the args template references either
// shell token, meaning the terminal hosts a separately-resolved shell.
func templateAcceptsShell(tmpl []string) bool {
	for _, a := range tmpl {
		if a == launch.TokenShellCommand || a == launch.TokenShellArgs {
			return true
		}
	}
	return false
}

// ─── Catalogs per OS ──────────────────────────────────────────────────────

// windowsTerminals is the supported Windows terminal-emulator catalog.
// Order is the seed priority for fresh configs.
var windowsTerminals = []CatalogTerminal{
	{
		ID: "wt", Name: "Windows Terminal", OS: "windows",
		Probe:        probeWT,
		ArgsTemplate: []string{"-d", launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "wezterm", Name: "WezTerm", OS: "windows",
		Probe:        probeBinary("wezterm-gui.exe", "wezterm.exe"),
		ArgsTemplate: []string{"start", "--cwd", launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "alacritty", Name: "Alacritty", OS: "windows",
		Probe:        probeBinary("alacritty.exe"),
		ArgsTemplate: []string{"--working-directory", launch.TokenPath, "-e", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "tabby", Name: "Tabby", OS: "windows",
		Probe:        probeBinary("tabby.exe"),
		ArgsTemplate: []string{"open", "--directory", launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "conemu", Name: "ConEmu", OS: "windows",
		Probe:        probeBinary("ConEmu64.exe", "ConEmu.exe"),
		ArgsTemplate: []string{"-Dir", launch.TokenPath, "-run", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "hyper", Name: "Hyper", OS: "windows",
		Probe:        probeBinary("hyper.exe"),
		ArgsTemplate: []string{launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "mintty", Name: "Mintty", OS: "windows",
		Probe:        probeBinary("mintty.exe"),
		ArgsTemplate: []string{"-w", "max", "-d", launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "zoc", Name: "ZOC", OS: "windows",
		Probe:        probeBinary("zoc8.exe", "zoc.exe"),
		ArgsTemplate: []string{launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs},
	},
	// Git Bash is intentionally absent: it's a shell, not a modern terminal.
	// The shell catalog covers it.
}

// darwinTerminals is the supported macOS terminal-emulator catalog.
// macOS entries always launch via `open -a <App>` — Probe checks for the
// .app bundle in the conventional locations.
var darwinTerminals = []CatalogTerminal{
	macAppTerminal("iterm", "iTerm2", "iTerm"),
	macAppTerminal("terminal", "Terminal", "Terminal"),
	macAppTerminal("warp", "Warp", "Warp"),
	macAppTerminal("kitty", "Kitty", "kitty"),
	macAppTerminal("ghostty", "Ghostty", "Ghostty"),
	macAppTerminal("wezterm", "WezTerm", "WezTerm"),
	macAppTerminal("alacritty", "Alacritty", "Alacritty"),
}

// linuxTerminals is the supported Linux terminal-emulator catalog. PATH
// probes only — Linux desktops install terminals into the standard PATH.
var linuxTerminals = []CatalogTerminal{
	{
		ID: "gnome-terminal", Name: "GNOME Terminal", OS: "linux",
		Probe:        probeBinary("gnome-terminal"),
		ArgsTemplate: []string{"--working-directory=" + launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "konsole", Name: "Konsole", OS: "linux",
		Probe:        probeBinary("konsole"),
		ArgsTemplate: []string{"--workdir", launch.TokenPath, "-e", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "terminator", Name: "Terminator", OS: "linux",
		Probe:        probeBinary("terminator"),
		ArgsTemplate: []string{"--working-directory=" + launch.TokenPath, "-x", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "foot", Name: "Foot", OS: "linux",
		Probe:        probeBinary("foot"),
		ArgsTemplate: []string{"--working-directory=" + launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "alacritty", Name: "Alacritty", OS: "linux",
		Probe:        probeBinary("alacritty"),
		ArgsTemplate: []string{"--working-directory", launch.TokenPath, "-e", launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "kitty", Name: "Kitty", OS: "linux",
		Probe:        probeBinary("kitty"),
		ArgsTemplate: []string{"--directory=" + launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs},
	},
	{
		ID: "tilda", Name: "Tilda", OS: "linux",
		Probe:        probeBinary("tilda"),
		ArgsTemplate: []string{"--working-dir=" + launch.TokenPath, "--command", launch.TokenShellCommand},
	},
	{
		ID: "guake", Name: "Guake", OS: "linux",
		Probe:        probeBinary("guake"),
		ArgsTemplate: []string{"--show", "-n", launch.TokenPath, "-e", launch.TokenShellCommand},
	},
	{
		ID: "xterm", Name: "xterm", OS: "linux",
		Probe:        probeBinary("xterm"),
		ArgsTemplate: []string{"-e", launch.TokenShellCommand, launch.TokenShellArgs},
	},
}

// windowsShells is the supported Windows shell catalog.
var windowsShells = []CatalogShell{
	{ID: "pwsh", Name: "PowerShell 7", OS: "windows", Probe: probePwsh, Args: nil},
	{ID: "powershell", Name: "PowerShell 5", OS: "windows", Probe: probeBinary("powershell.exe"), Args: nil},
	{ID: "cmd", Name: "Command Prompt", OS: "windows", Probe: probeBinary("cmd.exe"), Args: nil},
	{ID: "git-bash", Name: "Git Bash", OS: "windows", Probe: probeGitBash, Args: nil},
	// WSL is special — the bare wsl.exe row is the fallback; per-distro entries
	// are emitted at runtime via DiscoverWSLDistros.
	{ID: "wsl", Name: "WSL (default distro)", OS: "windows", Probe: probeBinary("wsl.exe"), Args: nil},
}

// darwinShells is the supported macOS shell catalog.
var darwinShells = []CatalogShell{
	{ID: "zsh", Name: "Zsh", OS: "darwin", Probe: probeBinary("zsh"), Args: []string{"-l"}},
	{ID: "bash", Name: "Bash", OS: "darwin", Probe: probeBinary("bash"), Args: []string{"-l"}},
	{ID: "fish", Name: "Fish", OS: "darwin", Probe: probeBinary("fish"), Args: []string{"-l"}},
	{ID: "dash", Name: "Dash", OS: "darwin", Probe: probeBinary("dash"), Args: []string{"-l"}},
}

// linuxShells is the supported Linux shell catalog.
var linuxShells = []CatalogShell{
	{ID: "bash", Name: "Bash", OS: "linux", Probe: probeBinary("bash"), Args: []string{"-l"}},
	{ID: "zsh", Name: "Zsh", OS: "linux", Probe: probeBinary("zsh"), Args: []string{"-l"}},
	{ID: "fish", Name: "Fish", OS: "linux", Probe: probeBinary("fish"), Args: []string{"-l"}},
	{ID: "ksh", Name: "Ksh", OS: "linux", Probe: probeBinary("ksh"), Args: []string{"-l"}},
	{ID: "dash", Name: "Dash", OS: "linux", Probe: probeBinary("dash"), Args: []string{"-l"}},
}

// catalogTerminalsFor returns the terminal catalog for the given GOOS.
// Unknown values yield nil.
func catalogTerminalsFor(goos string) []CatalogTerminal {
	switch goos {
	case "windows":
		return windowsTerminals
	case "darwin":
		return darwinTerminals
	case "linux":
		return linuxTerminals
	}
	return nil
}

// catalogShellsFor returns the shell catalog for the given GOOS.
func catalogShellsFor(goos string) []CatalogShell {
	switch goos {
	case "windows":
		return windowsShells
	case "darwin":
		return darwinShells
	case "linux":
		return linuxShells
	}
	return nil
}
