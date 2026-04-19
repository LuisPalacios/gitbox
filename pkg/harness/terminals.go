package harness

import (
	_ "embed"
	"strings"
)

//go:embed terminal-directory.md
var terminalDirectoryMarkdown string

// TerminalSpec describes one terminal candidate parsed from the directory.
// The executable + default args are static; PATH / bundle / alias resolution
// happens at the call site (cmd/gui), which also knows the current OS.
type TerminalSpec struct {
	Name    string   // display name (e.g. "GNOME Terminal")
	OS      string   // "Windows" | "macOS" | "Linux"
	Command string   // binary name or "open"
	Args    []string // default argv template (may include {path} and {command})
}

// KnownTerminals returns the parsed terminal spec list in markdown order.
// Order matters: SyncTerminals seeds global.terminals using this order on a
// fresh config, so editing the markdown is how a user changes the initial
// priority without touching Go source.
func KnownTerminals() []TerminalSpec {
	return parseTerminalDirectory(terminalDirectoryMarkdown)
}

// parseTerminalDirectory is the testable core of KnownTerminals.
func parseTerminalDirectory(md string) []TerminalSpec {
	var specs []TerminalSpec
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
		// Normalize the OS string — tolerate minor variants so the markdown
		// stays forgiving of casing / punctuation.
		osField = normalizeOS(osField)
		if osField == "" {
			continue
		}
		specs = append(specs, TerminalSpec{Name: name, OS: osField, Command: cmd, Args: args})
	}
	return specs
}

// extractBacktickedArgs pulls every backticked token in cell order, trimming
// inner whitespace. Used for the Default Args column, where each argv
// element is its own backticked token. An empty cell (no backticks) yields
// a nil slice — meaning "no args", not "one empty arg".
func extractBacktickedArgs(cell string) []string {
	var out []string
	in := false
	var cur strings.Builder
	for _, r := range cell {
		if r == '`' {
			if in {
				tok := strings.TrimSpace(cur.String())
				if tok != "" {
					out = append(out, tok)
				}
				cur.Reset()
				in = false
			} else {
				in = true
			}
			continue
		}
		if in {
			cur.WriteRune(r)
		}
	}
	return out
}

// normalizeOS maps user-written variants to the canonical three strings
// ("Windows", "macOS", "Linux") that the cmd/gui dispatcher compares
// against. Empty string means "unrecognized OS — skip this row".
func normalizeOS(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "windows", "win":
		return "Windows"
	case "macos", "mac", "darwin", "osx":
		return "macOS"
	case "linux":
		return "Linux"
	}
	return ""
}
