// Package harness reads the embedded Agentic Ecosystem Directory (an
// opinionated markdown table at tools-directory.md) and returns the subset
// of tools that the GUI can auto-detect as "Open in AI harness" targets.
//
// The embedded file is the authoritative source of known tools — to add a
// new harness, add a row to the markdown rather than changing Go code.
package harness

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:embed tools-directory.md
var directoryMarkdown string

// Tool describes one harness entry parsed from the directory.
type Tool struct {
	Name     string // display name (e.g. "Claude Code") — markdown bold stripped
	Category string // e.g. "Agentic CLI"
	Command  string // PATH binary name (e.g. "claude")
}

// eligibleCategories enumerates the categories gitbox treats as AI harnesses
// for auto-detection. Frameworks, orchestrators, and cloud platforms live
// in the directory for reference but don't get menu entries. Agentic IDEs
// (Cursor, Windsurf) and hybrid IDE/CLI tools (Cline) ARE included — they
// launch from a terminal in a folder, which is exactly the menu's contract.
var eligibleCategories = map[string]bool{
	"Agentic CLI":       true,
	"AI Harness":        true,
	"Headless Harness":  true,
	"Agentic IDE":       true,
	"Agentic IDE / CLI": true,
}

// cmdTokenRE matches a single identifier-shaped binary name. Entries with
// paths, arguments, or helper words (e.g. "python devika.py", "docker-compose")
// are rejected so we don't try to PATH-resolve strings that aren't binaries.
var cmdTokenRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*$`)

// boldMarkerRE strips the surrounding ** from markdown bold runs.
var boldMarkerRE = regexp.MustCompile(`\*\*(.+?)\*\*`)

// KnownTools parses the embedded directory and returns the filtered tool
// list. The output is deterministic: rows appear in markdown order.
func KnownTools() []Tool {
	return parseDirectory(directoryMarkdown)
}

// parseDirectory is the testable core of KnownTools. Exported via KnownTools
// for production and called directly by tests with synthetic markdown.
func parseDirectory(md string) []Tool {
	var tools []Tool
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		// Skip the header row and the alignment row. The header has literal
		// "Tool Name"; the alignment row is all pipes and colons/dashes.
		if strings.Contains(line, "Tool Name") {
			continue
		}
		if isAlignmentRow(line) {
			continue
		}
		cells := splitRow(line)
		if len(cells) < 5 {
			continue
		}
		name := cleanName(cells[0])
		category := cleanPlainCell(cells[2])
		cmd := extractCommand(cells[4])
		if name == "" || cmd == "" {
			continue
		}
		if !eligibleCategories[category] {
			continue
		}
		tools = append(tools, Tool{Name: name, Category: category, Command: cmd})
	}
	return tools
}

// splitRow splits a pipe-delimited markdown row into cell slices. Leading and
// trailing pipes are stripped; empty sentinel cells are preserved so callers
// can address columns by index. Pipes inside inline code spans (`...`) are
// respected as content, not separators, so a command cell like
// "`foo | bar`" wouldn't be mistakenly split — none of the current rows
// exercise this but the guard keeps future rows safe.
func splitRow(line string) []string {
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	var cells []string
	var cur strings.Builder
	inCode := false
	for _, r := range line {
		switch r {
		case '`':
			inCode = !inCode
			cur.WriteRune(r)
		case '|':
			if inCode {
				cur.WriteRune(r)
			} else {
				cells = append(cells, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	cells = append(cells, cur.String())
	return cells
}

// isAlignmentRow reports whether a pipe row is the markdown alignment row
// (e.g. "| :--- | :--- |"). Such rows contain only pipes, colons, dashes,
// and whitespace.
func isAlignmentRow(line string) bool {
	for _, r := range line {
		switch r {
		case '|', ':', '-', ' ', '\t':
			continue
		default:
			return false
		}
	}
	return true
}

// cleanName returns the display name from the first cell of a row, stripping
// markdown bold markers and trimming whitespace.
func cleanName(cell string) string {
	s := strings.TrimSpace(cell)
	m := boldMarkerRE.FindStringSubmatch(s)
	if len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}

// cleanPlainCell returns the trimmed plain text of a cell (no markdown
// transforms). Used for Category.
func cleanPlainCell(cell string) string {
	return strings.TrimSpace(cell)
}

// extractCommand pulls a valid single-binary command name out of the
// Executable column. The cell may contain trailing annotations like
// "`Claude` *(App Executable)*" or "`openhands` *(or Docker)*" — only the
// first backticked token is considered, and only when it matches the strict
// identifier shape. Cells whose backticked content contains a path, spaces,
// or is literally "N/A" return an empty string so the row is skipped.
func extractCommand(cell string) string {
	s := strings.TrimSpace(cell)
	// Find the first backticked run.
	i := strings.IndexByte(s, '`')
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(s[i+1:], '`')
	if j < 0 {
		return ""
	}
	candidate := strings.TrimSpace(s[i+1 : i+1+j])
	if candidate == "" {
		return ""
	}
	if !cmdTokenRE.MatchString(candidate) {
		return ""
	}
	return candidate
}
