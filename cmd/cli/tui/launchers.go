package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/launch"
	"github.com/LuisPalacios/gitbox/pkg/terminals"
	tea "github.com/charmbracelet/bubbletea"
)

// launchDoneMsg reports the outcome of a launch action so the origin screen
// can surface a status/error line without needing separate message types per
// kind.
type launchDoneMsg struct {
	target string // short label for status line ("VS Code", "Git Bash", ...)
	err    error
}

// launchEditorCmd opens a folder in a GUI editor. On Windows, editor CLIs
// like code.cmd are batch wrappers that exit immediately after launching the
// editor's own window, so no console-flash workaround is needed here (the TUI
// owns the console; the wrapper briefly shares it and exits).
func launchEditorCmd(path, command, name string) tea.Cmd {
	return func() tea.Msg {
		if command == "" {
			return launchDoneMsg{target: name, err: fmt.Errorf("editor command is empty")}
		}
		cmd := exec.Command(command, path)
		cmd.Env = git.Environ()
		if err := cmd.Start(); err != nil {
			return launchDoneMsg{target: name, err: err}
		}
		return launchDoneMsg{target: name}
	}
}

// launchProfileCmd spawns a TerminalProfile in the given folder, expanding
// the profile's args via pkg/launch.ResolveArgs so the GUI bridge
// (cmd/gui/profiles.go::OpenProfile) and the TUI agree byte-for-byte on the
// final argv. Used by the v2.1 launcher overlay; the legacy
// launchTerminalCmd below stays until cmd/cli/tui/screen_launcher.go is
// reshaped in the next commit so the branch keeps bisecting cleanly.
func launchProfileCmd(path string, profile config.TerminalProfile, app config.TerminalApp, shell config.ShellEntry) tea.Cmd {
	return func() tea.Msg {
		if app.Command == "" {
			return launchDoneMsg{target: profile.Name, err: fmt.Errorf("terminal command for profile %q is empty", profile.ID)}
		}
		template := profile.Args
		if len(template) == 0 {
			template = app.ArgsTemplate
		}
		// EXECUTION pillar (#72): consult the user's terminal config so
		// "WezTerm + PowerShell 7" picks up wezterm.lua launch_menu env
		// and "Windows Terminal + <Shell>" hands off to wt.exe via
		// --profile so WT applies the user's profile-tuned font/colors.
		var extraEnv map[string]string
		if profile.TerminalID != "" && profile.ShellID != "" {
			if ov, hit := terminals.LookupForLaunch(profile.TerminalID, profile.ShellID, shell.Name); hit {
				template = ov.Argv
				extraEnv = ov.Env
			}
		}
		args := launch.ResolveArgs(launch.ProfileArgs{
			Template:     template,
			Path:         path,
			ShellCommand: shell.Command,
			ShellArgs:    shell.Args,
		})
		cmd := exec.Command(app.Command, args...)
		cmd.Env = appendEnvOverlay(git.Environ(), extraEnv)
		if err := cmd.Start(); err != nil {
			return launchDoneMsg{target: profile.Name, err: err}
		}
		return launchDoneMsg{target: profile.Name}
	}
}

// appendEnvOverlay layers `overlay` on top of `base`, returning a new slice
// of "KEY=VAL" entries where overlay keys override any pre-existing
// definition. Returns base unchanged when overlay is empty. Mirrors the
// helper in cmd/gui/profiles.go — duplicated here to keep the launcher
// import surface tight (no shared launcher-helpers package today).
func appendEnvOverlay(base []string, overlay map[string]string) []string {
	if len(overlay) == 0 {
		return base
	}
	idx := make(map[string]int, len(base))
	for i, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		idx[kv[:eq]] = i
	}
	out := append([]string(nil), base...)
	for k, v := range overlay {
		entry := k + "=" + v
		if i, ok := idx[k]; ok {
			out[i] = entry
			continue
		}
		out = append(out, entry)
	}
	return out
}

// launchTerminalCmd spawns a terminal emulator in the given folder. The
// terminal command is expected to open its own window (wt.exe, open -a
// Terminal, gnome-terminal …), so the TUI's stdio is never taken over.
func launchTerminalCmd(path string, term config.TerminalEntry) tea.Cmd {
	return func() tea.Msg {
		if term.Command == "" {
			return launchDoneMsg{target: term.Name, err: fmt.Errorf("terminal command is empty")}
		}
		args := resolveTerminalArgs(term.Args, path, nil)
		cmd := exec.Command(term.Command, args...)
		cmd.Env = git.Environ()
		if err := cmd.Start(); err != nil {
			return launchDoneMsg{target: term.Name, err: err}
		}
		return launchDoneMsg{target: term.Name}
	}
}

// launchAIHarnessCmd spawns the given AI harness inside the first configured
// terminal (mirrors the GUI contract: harnesses are CLI-only and must run in
// a terminal). Returns an actionable error if no terminal is configured.
func launchAIHarnessCmd(path string, h config.AIHarnessEntry, terminals []config.TerminalEntry) tea.Cmd {
	return func() tea.Msg {
		if h.Command == "" {
			return launchDoneMsg{target: h.Name, err: fmt.Errorf("harness command is empty")}
		}
		if len(terminals) == 0 {
			return launchDoneMsg{target: h.Name, err: fmt.Errorf("configure at least one terminal in global.terminals to launch AI harnesses")}
		}
		term := terminals[0]
		harnessArgv := append([]string{h.Command}, h.Args...)
		args := resolveTerminalArgs(term.Args, path, harnessArgv)
		cmd := exec.Command(term.Command, args...)
		cmd.Env = git.Environ()
		if err := cmd.Start(); err != nil {
			return launchDoneMsg{target: h.Name, err: err}
		}
		return launchDoneMsg{target: h.Name}
	}
}

// resolveTerminalArgs substitutes {path} and splices {command} in terminal
// args. Mirrors the GUI's resolveTerminalArgsWithCommand (cmd/gui/app.go) so
// both frontends interpret config the same way. Kept local to the TUI to
// avoid introducing a public pkg surface just for two call sites.
func resolveTerminalArgs(args []string, path string, harnessArgv []string) []string {
	if len(args) == 0 {
		return nil
	}
	pathSubstituted := false
	out := make([]string, 0, len(args)+len(harnessArgv))
	for _, a := range args {
		if a == "{command}" {
			out = append(out, harnessArgv...)
			continue
		}
		if strings.Contains(a, "{path}") {
			out = append(out, strings.ReplaceAll(a, "{path}", path))
			pathSubstituted = true
			continue
		}
		out = append(out, a)
	}
	if !pathSubstituted && harnessArgv == nil {
		out = append(out, path)
	}
	return out
}
