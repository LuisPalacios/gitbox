package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
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
