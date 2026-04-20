package workspace

import (
	"path/filepath"
	"strings"
)

// commonAncestor returns the longest path that is a parent of every input path.
// Returns "" if paths is empty or if the paths share no common prefix.
// Inputs are expected to be absolute and already cleaned (filepath.Clean).
func commonAncestor(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}

	prefixes := make([]string, len(paths))
	parts := make([][]string, len(paths))
	for i, p := range paths {
		prefixes[i], parts[i] = splitAbs(p)
	}

	// All paths must share the same root prefix (volume + separator on Windows,
	// or "/" on Unix) for a common ancestor to exist.
	root := prefixes[0]
	for _, pr := range prefixes[1:] {
		if pr != root {
			return ""
		}
	}

	base := parts[0]
	for _, cur := range parts[1:] {
		n := len(base)
		if len(cur) < n {
			n = len(cur)
		}
		i := 0
		for i < n && base[i] == cur[i] {
			i++
		}
		base = base[:i]
	}

	if len(base) == 0 {
		return filepath.Clean(root)
	}
	return filepath.Clean(root + strings.Join(base, string(filepath.Separator)))
}

// splitAbs separates an absolute path into its rooted prefix (e.g. "/" on
// Unix, "C:\" on Windows) and the component list. The prefix ends with a
// trailing separator; components carry no separators.
func splitAbs(p string) (string, []string) {
	sep := string(filepath.Separator)
	vol := filepath.VolumeName(p)
	rest := p[len(vol):]
	prefix := vol
	if strings.HasPrefix(rest, sep) {
		prefix += sep
		rest = strings.TrimLeft(rest, sep)
	}
	if rest == "" {
		return prefix, nil
	}
	pieces := strings.Split(rest, sep)
	out := make([]string, 0, len(pieces))
	for _, p := range pieces {
		if p != "" {
			out = append(out, p)
		}
	}
	return prefix, out
}
