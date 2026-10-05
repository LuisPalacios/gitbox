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
	Command  string // primary PATH binary name (e.g. "claude") — Commands[0]
	// Commands lists every binary name the Executable cell offers, in cell
	// order. Most rows have one; a renamed CLI keeps its old name as an
	// alternate ("`agent` or `cursor-agent`") so either install resolves.
	Commands []string
	// ExtraDirs are tool-specific well-known install directories from the
	// "Well-known locations" column, probed before PATH. Entries may use
	// `~`, $VAR or %VAR%; expansion happens at probe time (pkg/doctor).
	ExtraDirs []string
}

// retiredCategory marks rows whose CLI has been discontinued by its vendor.
// They are never auto-detected, and config entries carrying their name are
// pruned on sync (see cmd/gui SyncAIHarnesses).
const retiredCategory = "Retired CLI"

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

// RetiredTools returns the rows marked with the retired category, in
// markdown order. Callers use the names to prune stale config entries.
func RetiredTools() []Tool {
	return parseRetired(directoryMarkdown)
}

// parseDirectory is the testable core of KnownTools. Exported via KnownTools
// for production and called directly by tests with synthetic markdown.
func parseDirectory(md string) []Tool {
	return filterRows(md, func(category string) bool { return eligibleCategories[category] })
}

// parseRetired is the testable core of RetiredTools.
func parseRetired(md string) []Tool {
	return filterRows(md, func(category string) bool { return category == retiredCategory })
}

// filterRows walks every table row with a single-identifier command and
// keeps those whose category satisfies keep.
func filterRows(md string, keep func(category string) bool) []Tool {
	var tools []Tool
	for line := range strings.SplitSeq(md, "\n") {
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
		cmds := extractCommands(cells[4])
		if name == "" || len(cmds) == 0 {
			continue
		}
		if !keep(category) {
			continue
		}
		var dirs []string
		if len(cells) >= 8 {
			dirs = extractDirs(cells[7])
		}
		tools = append(tools, Tool{
			Name:      name,
			Category:  category,
			Command:   cmds[0],
			Commands:  cmds,
			ExtraDirs: dirs,
		})
	}
	return tools
}

// backtickTokens returns the trimmed content of every `...` run in cell,
// in order, skipping empty runs.
func backtickTokens(cell string) []string {
	var out []string
	s := cell
	for {
		i := strings.IndexByte(s, '`')
		if i < 0 {
			return out
		}
		j := strings.IndexByte(s[i+1:], '`')
		if j < 0 {
			return out
		}
		if tok := strings.TrimSpace(s[i+1 : i+1+j]); tok != "" {
			out = append(out, tok)
		}
		s = s[i+1+j+1:]
	}
}

// extractCommands returns every backticked token in the Executable column
// that has the strict identifier shape, in cell order. Tokens with paths,
// spaces or helper words are dropped, so a cell like "`python devika.py`"
// yields nothing and the row is skipped. The first token is the primary
// command; the rest are alternates (older or platform-specific names).
func extractCommands(cell string) []string {
	var out []string
	for _, tok := range backtickTokens(cell) {
		if cmdTokenRE.MatchString(tok) {
			out = append(out, tok)
		}
	}
	return out
}

// extractDirs returns every backticked token in the Well-known locations
// column. Each location must be its own backticked run; a cell that is
// blank or an italic placeholder like *N/A* yields nil.
func extractDirs(cell string) []string {
	return backtickTokens(cell)
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
