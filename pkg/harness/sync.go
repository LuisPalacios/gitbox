package harness

import (
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/doctor"
)

// Source values written to config.AIHarnessEntry.Source.
const (
	SourceDetected = "detected"
	SourceUser     = "user"
)

// LookupFunc resolves a command to an absolute path, or "" when the host
// doesn't have it. command is either a bare binary name or a stored path;
// extraDirs are the catalog row's well-known install directories. The GUI
// and TUI pass DefaultLookup; tests pass a map-backed fake.
type LookupFunc func(command string, extraDirs []string) string

// DefaultLookup is the production LookupFunc: pkg/doctor's Setenv-free
// resolver, safe to call from background goroutines.
func DefaultLookup(command string, extraDirs []string) string {
	return doctor.LookupIn(command, extraDirs)
}

// Sync reconciles a config's global.ai_harnesses list with the embedded
// catalog and with what is actually installed on the host. It is pure: it
// never touches disk or process state, and it returns a new slice plus
// whether that slice differs from the input (so callers can skip a save).
//
// Rules:
//   - Duplicates by Name collapse to the first occurrence; entries whose
//     Name matches a retired catalog row are dropped.
//   - Known entries (Name matches a catalog row) come first in catalog
//     order; user-added entries follow in their original relative order.
//   - A pre-existing entry with an empty Source is classified: "detected"
//     when its command's basename equals one of the row's binaries (that is
//     what an earlier sync wrote), otherwise "user".
//   - Every entry is re-probed through lookup using its stored Command. A
//     "detected" entry whose stored path is gone is retried with the row's
//     catalog binaries and well-known directories; on success its Command
//     is replaced with the new location (a reinstall moved it). Missing is
//     set when nothing resolves and cleared when something does.
//   - "user" entries are never modified beyond the Missing flag: their
//     Command and Args are the user's own.
//   - Catalog rows with no config entry are probed and, when found,
//     appended as "detected" with the resolved path.
//   - Nothing is ever deleted for being missing: hidden entries keep their
//     Args so a reinstall restores them untouched.
func Sync(entries []config.AIHarnessEntry, lookup LookupFunc) ([]config.AIHarnessEntry, bool) {
	known := KnownTools()
	knownIndex := make(map[string]int, len(known))
	for i, t := range known {
		knownIndex[t.Name] = i
	}
	retired := make(map[string]bool)
	for _, t := range RetiredTools() {
		retired[t.Name] = true
	}

	seen := make(map[string]bool, len(entries))
	existing := make(map[string]config.AIHarnessEntry, len(entries))
	var custom []config.AIHarnessEntry
	for _, h := range entries {
		if h.Name == "" || seen[h.Name] || retired[h.Name] {
			continue
		}
		seen[h.Name] = true
		if _, ok := knownIndex[h.Name]; ok {
			existing[h.Name] = h
		} else {
			custom = append(custom, h)
		}
	}

	out := make([]config.AIHarnessEntry, 0, len(entries)+len(known))
	for _, tool := range known {
		if h, ok := existing[tool.Name]; ok {
			out = append(out, probeExisting(h, &tool, lookup))
			continue
		}
		if path := probeCatalog(tool, lookup); path != "" {
			out = append(out, config.AIHarnessEntry{
				Name:    tool.Name,
				Command: path,
				Source:  SourceDetected,
			})
		}
	}
	for _, h := range custom {
		out = append(out, probeExisting(h, nil, lookup))
	}

	return out, !Equal(out, entries)
}

// probeExisting re-checks one config entry. tool is the matching catalog
// row, or nil for a user-added entry outside the catalog.
func probeExisting(h config.AIHarnessEntry, tool *Tool, lookup LookupFunc) config.AIHarnessEntry {
	h.Args = cloneArgs(h.Args)
	if h.Source == "" {
		h.Source = classify(h, tool)
	}
	var dirs []string
	if tool != nil {
		dirs = tool.ExtraDirs
	}
	found := lookup(h.Command, dirs)
	if found == "" && tool != nil && h.Source == SourceDetected {
		if path := probeCatalog(*tool, lookup); path != "" {
			h.Command = path
			found = path
		}
	}
	h.Missing = found == ""
	return h
}

// probeCatalog resolves a catalog row's binaries in order, returning the
// first path found.
func probeCatalog(tool Tool, lookup LookupFunc) string {
	for _, cmd := range tool.Commands {
		if path := lookup(cmd, tool.ExtraDirs); path != "" {
			return path
		}
	}
	return ""
}

// classify tags a pre-#81 entry. An earlier sync stored the resolved path
// of a catalog binary, so a basename match means "detected"; anything else
// is something the user typed.
func classify(h config.AIHarnessEntry, tool *Tool) string {
	if tool == nil {
		return SourceUser
	}
	base := commandBase(h.Command)
	for _, cmd := range tool.Commands {
		if strings.EqualFold(base, cmd) {
			return SourceDetected
		}
	}
	return SourceUser
}

// commandBase returns the file name of a stored command with any Windows
// launcher extension (.exe, .cmd, .bat) removed, so "C:\x\claude.cmd",
// "/opt/homebrew/bin/claude" and "claude" all compare equal.
func commandBase(command string) string {
	base := filepath.Base(strings.ReplaceAll(command, `\`, "/"))
	switch strings.ToLower(filepath.Ext(base)) {
	case ".exe", ".cmd", ".bat":
		base = base[:len(base)-len(filepath.Ext(base))]
	}
	return base
}

// Equal reports whether two harness lists are identical in every field the
// sync can change (Name, Command, Args, Source, Missing).
func Equal(a, b []config.AIHarnessEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Command != b[i].Command ||
			a[i].Source != b[i].Source || a[i].Missing != b[i].Missing {
			return false
		}
		if len(a[i].Args) != len(b[i].Args) {
			return false
		}
		for j := range a[i].Args {
			if a[i].Args[j] != b[i].Args[j] {
				return false
			}
		}
	}
	return true
}

func cloneArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	return append([]string(nil), args...)
}
