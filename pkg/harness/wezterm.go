package harness

import (
	"errors"
	"regexp"
	"strings"
)

// WeztermLaunchMenuEntry represents one entry of the user's `config.launch_menu`
// table from `wezterm.lua`. WezTerm itself defines this as a SpawnCommand
// object (see https://wezfurlong.org/wezterm/config/lua/SpawnCommand.html);
// the fields gitbox cares about are Label, Args, and Env (the env-var splice
// the EXECUTION pillar in #72 needs to reproduce the user's WezTerm-tuned
// shell environment when launching outside WezTerm's own menu).
type WeztermLaunchMenuEntry struct {
	Label string            // display name (may be empty — WezTerm derives one from Args)
	Args  []string          // argv to spawn (e.g. ["bash", "-l"])
	Env   map[string]string // set_environment_variables — nil when absent
}

// ParseWeztermLaunchMenu extracts every entry of `config.launch_menu` from
// the raw bytes of a `wezterm.lua` file. The parser is intentionally a
// best-effort regex scanner — Lua isn't JSON, but the launch_menu shape is
// rigid enough that a heuristic parse covers the canonical case shown in
// WezTerm's own docs and the variations seen in the wild.
//
// What it handles:
//   - `config.launch_menu = { ... }` and `M.config.launch_menu = { ... }`
//   - per-entry `label = "..."` (single or double quotes; optional)
//   - per-entry `args = { "...", "..." }` (single or double quotes; required)
//   - line comments (`--`) inside the launch_menu block (stripped before parse)
//
// What it DOESN'T handle (returns whatever it can parse, ignores the rest):
//   - dynamically-built tables (loops, function calls, table.concat)
//   - args derived from variables or string concatenation
//   - block comments (`--[[ ... ]]`)
//
// Returns an error when the launch_menu block isn't found or the file is
// malformed. Returns an empty slice (no error) when the block exists but has
// no entries — that's a valid user config.
func ParseWeztermLaunchMenu(data []byte) ([]WeztermLaunchMenuEntry, error) {
	src := string(data)
	src = stripWeztermLineComments(src)

	block, err := extractWeztermLaunchMenuBlock(src)
	if err != nil {
		return nil, err
	}

	return parseWeztermLaunchMenuEntries(block), nil
}

// stripWeztermLineComments removes `-- ...` line comments while leaving the
// rest of the line intact. Block comments (`--[[ ... ]]`) are not handled
// today — they are rare in launch_menu definitions and parsing them
// correctly requires tracking nesting that's beyond the scope of this
// best-effort parser.
func stripWeztermLineComments(src string) string {
	var out strings.Builder
	for _, line := range strings.Split(src, "\n") {
		stripped := stripLuaLineComment(line)
		out.WriteString(stripped)
		out.WriteByte('\n')
	}
	return out.String()
}

// stripLuaLineComment removes a `--` comment from a Lua source line, taking
// care not to truncate inside a string literal. The state machine tracks
// single- and double-quoted strings; `--` inside either is preserved.
func stripLuaLineComment(line string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(line)-1; i++ {
		c := line[i]
		switch {
		case c == '\\' && i+1 < len(line) && (inSingle || inDouble):
			i++ // skip escaped char
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '-' && line[i+1] == '-' && !inSingle && !inDouble:
			return line[:i]
		}
	}
	return line
}

// launchMenuOpenRe matches the start of a launch_menu assignment. We accept
// both bare `config.launch_menu = {` and module-prefixed forms like
// `M.config.launch_menu = {` or `wezterm_config.launch_menu = {`.
var launchMenuOpenRe = regexp.MustCompile(`(?m)(?:[\w.]+\.)?config\.launch_menu\s*=\s*\{`)

// extractWeztermLaunchMenuBlock returns the Lua source between the matching
// braces of the launch_menu assignment. Brace tracking is naive — it counts
// `{` and `}` while ignoring those inside quoted strings — and is sufficient
// for the launch_menu shape (no functions, no metatables, no nested closures).
func extractWeztermLaunchMenuBlock(src string) (string, error) {
	loc := launchMenuOpenRe.FindStringIndex(src)
	if loc == nil {
		return "", errors.New("config.launch_menu = { ... } not found")
	}
	// Position cursor just past the opening `{`.
	cursor := loc[1]
	depth := 1
	inSingle := false
	inDouble := false
	for cursor < len(src) && depth > 0 {
		c := src[cursor]
		switch {
		case c == '\\' && cursor+1 < len(src) && (inSingle || inDouble):
			cursor++ // skip escaped char
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '{' && !inSingle && !inDouble:
			depth++
		case c == '}' && !inSingle && !inDouble:
			depth--
			if depth == 0 {
				return src[loc[1]:cursor], nil
			}
		}
		cursor++
	}
	return "", errors.New("unterminated launch_menu block: missing closing '}'")
}

// entryOpenRe matches a likely launch_menu entry start: a bare `{` that
// follows a comma, the opening of the outer table, or whitespace-only at
// line start. This is purely a hint; the real boundary tracking is done
// with brace counting in extractEntries.
var entryOpenRe = regexp.MustCompile(`(?m)(?:^|,)\s*\{`)

// labelRe captures the value of a `label = "..."` or `label = '...'` field.
// The greedy `.*?` keeps matching narrow so we don't slurp across entries.
var labelRe = regexp.MustCompile(`label\s*=\s*(?:"([^"]*)"|'([^']*)')`)

// argsBlockRe captures the contents between the braces of `args = { ... }`.
// Brace nesting inside args is rare (no nested tables in SpawnCommand.args),
// so a non-greedy match between the matched braces works well enough.
var argsBlockRe = regexp.MustCompile(`args\s*=\s*\{([^}]*)\}`)

// argTokenRe captures each quoted string inside an args = { ... } block,
// supporting both single and double quotes. Escaped quotes inside the string
// are kept intact (we don't unescape — WezTerm doesn't either).
var argTokenRe = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)'`)

// envBlockRe captures the contents between the braces of
// `set_environment_variables = { ... }`. Same simplifying assumption as
// argsBlockRe — entries inside set_environment_variables are flat KEY = "value"
// pairs, no nested tables.
var envBlockRe = regexp.MustCompile(`set_environment_variables\s*=\s*\{([^}]*)\}`)

// envPairRe captures a single `KEY = "value"` (or single-quoted) pair inside
// the set_environment_variables block. Keys are bareword identifiers in the
// canonical WezTerm form; the bracketed-string form (`["KEY"] = ...`) is also
// accepted via the second alternative because it shows up in some user
// configs.
var envPairRe = regexp.MustCompile(`(?:([A-Za-z_][A-Za-z0-9_]*)|\[\s*"([^"]*)"\s*\])\s*=\s*(?:"((?:[^"\\]|\\.)*)"|'((?:[^'\\]|\\.)*)')`)

// parseWeztermLaunchMenuEntries scans the body of the launch_menu table
// (the source between the outer braces) and emits one entry per top-level
// `{ ... }` block. Entries with neither a `label` nor an `args` block are
// dropped — they are usually structural placeholders or partially-deleted
// definitions that shouldn't surface in the menu.
func parseWeztermLaunchMenuEntries(block string) []WeztermLaunchMenuEntry {
	var entries []WeztermLaunchMenuEntry

	starts := entryOpenRe.FindAllStringIndex(block, -1)
	for _, start := range starts {
		// start[1] sits one past the matched `{` — find the matching `}`.
		end, ok := findMatchingBrace(block, start[1])
		if !ok {
			continue
		}
		body := block[start[1]:end]

		var entry WeztermLaunchMenuEntry
		if m := labelRe.FindStringSubmatch(body); m != nil {
			if m[1] != "" {
				entry.Label = m[1]
			} else {
				entry.Label = m[2]
			}
		}
		if m := argsBlockRe.FindStringSubmatch(body); m != nil {
			tokens := argTokenRe.FindAllStringSubmatch(m[1], -1)
			for _, tok := range tokens {
				if tok[1] != "" {
					entry.Args = append(entry.Args, tok[1])
				} else {
					entry.Args = append(entry.Args, tok[2])
				}
			}
		}
		if m := envBlockRe.FindStringSubmatch(body); m != nil {
			pairs := envPairRe.FindAllStringSubmatch(m[1], -1)
			for _, p := range pairs {
				key := p[1]
				if key == "" {
					key = p[2]
				}
				val := p[3]
				if val == "" {
					val = p[4]
				}
				if key == "" {
					continue
				}
				if entry.Env == nil {
					entry.Env = make(map[string]string, len(pairs))
				}
				entry.Env[key] = val
			}
		}
		if entry.Label == "" && len(entry.Args) == 0 {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// findMatchingBrace returns the index of the `}` that closes the `{`
// immediately preceding `from`, or (-1, false) if no match is found.
// Counts depth while ignoring braces inside quoted strings.
func findMatchingBrace(src string, from int) (int, bool) {
	depth := 1
	inSingle := false
	inDouble := false
	for i := from; i < len(src); i++ {
		c := src[i]
		switch {
		case c == '\\' && i+1 < len(src) && (inSingle || inDouble):
			i++ // skip escaped char
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '{' && !inSingle && !inDouble:
			depth++
		case c == '}' && !inSingle && !inDouble:
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return -1, false
}
