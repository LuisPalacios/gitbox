package harness

import (
	_ "embed"
	"strings"
)

//go:embed shell-directory.md
var shellDirectoryMarkdown string

// ShellSpec describes one shell candidate parsed from the shell directory.
// The executable + default args are static; PATH / Program Files / login-shell
// resolution happens at the call site (cmd/gui), which knows the current OS.
type ShellSpec struct {
	Name    string   // display name (e.g. "PowerShell 7", "Bash")
	OS      string   // "Windows" | "macOS" | "Linux"
	Command string   // binary name (e.g. "pwsh.exe", "zsh")
	Args    []string // default argv (e.g. ["-l"] for login shells)
}

// KnownShells returns the parsed shell-spec list in markdown order.
// Order is the seed priority — SyncProfiles uses it when generating the
// initial Shell list on a fresh config. Editing the markdown is how a new
// shell gets auto-detected without touching Go source.
func KnownShells() []ShellSpec {
	return parseShellDirectory(shellDirectoryMarkdown)
}

// parseShellDirectory is the testable core of KnownShells. It mirrors
// parseTerminalDirectory: same row format (name, OS, command, args), same
// helpers (splitRow, isAlignmentRow, cleanName, cleanPlainCell,
// extractCommand, extractBacktickedArgs, normalizeOS) — only the output
// type differs.
func parseShellDirectory(md string) []ShellSpec {
	var specs []ShellSpec
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if strings.Contains(line, "Name") && strings.Contains(line, "OS") {
			continue
		}
		if isAlignmentRow(line) {
			continue
		}
		cells := splitRow(line)
		if len(cells) < 4 {
			continue
		}
		name := cleanName(cells[0])
		osField := cleanPlainCell(cells[1])
		cmd := extractCommand(cells[2])
		args := extractBacktickedArgs(cells[3])
		if name == "" || cmd == "" || osField == "" {
			continue
		}
		osField = normalizeOS(osField)
		if osField == "" {
			continue
		}
		specs = append(specs, ShellSpec{Name: name, OS: osField, Command: cmd, Args: args})
	}
	return specs
}
