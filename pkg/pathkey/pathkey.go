// Package pathkey compares local paths without changing their stored spelling.
package pathkey

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// AccessState describes what the filesystem could establish about a path.
type AccessState uint8

const (
	Unknown AccessState = iota
	Exists
	Missing
)

// Key separates the user's spelling from the native comparison key.
type Key struct {
	Original string
	Cmp      string
	Access   AccessState
}

// ExpandHome expands the current user's home prefix.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimLeft(p[1:], `/\`))
		}
	}
	return p
}

// Of resolves the existing prefix, including symbolic links. An absent suffix
// inherits its nearest directory's case policy. When the host cannot inspect
// that directory, lexical comparison uses the platform default.
func Of(p string) Key {
	key := Key{Original: p}
	if p == "" {
		return key
	}
	abs, err := filepath.Abs(ExpandHome(p))
	if err != nil {
		key.Cmp = filepath.Clean(p)
		return key
	}
	key.Cmp = abs
	existing := ""
	for prefix := abs; ; prefix = filepath.Dir(prefix) {
		_, err := os.Stat(prefix)
		if err == nil {
			real, err := filepath.EvalSymlinks(prefix)
			if err != nil {
				break
			}
			tail, _ := filepath.Rel(prefix, abs)
			key.Cmp = filepath.Join(real, tail)
			key.Access = Exists
			if prefix != abs {
				key.Access = Missing
			}
			existing = real
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || prefix == filepath.Dir(prefix) {
			break
		}
		// A dangling link cannot establish where this placement would land.
		if entry, e := os.Lstat(prefix); e == nil && entry.Mode()&os.ModeSymlink != 0 {
			break
		}
	}
	key.Cmp = comparison(key.Cmp, existing, key.Access == Unknown)
	return key
}

// Compare returns the native comparison key for use in indexes.
func Compare(path string) string { return Of(path).Cmp }
