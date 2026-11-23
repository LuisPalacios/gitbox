//go:build !windows

package pathkey

import (
	"os"
	"path/filepath"
	"strings"
)

// A mount point uses its parent's naming rules; children use the mounted
// filesystem's rules. Fold each component in its own directory.
func comparison(abs, existing string, unknown bool) string {
	var key strings.Builder
	key.WriteString("/")
	parent := "/"
	fold := fallbackFold
	for part := range strings.SplitSeq(strings.TrimPrefix(abs, "/"), "/") {
		if part == "" {
			continue
		}
		if !unknown && (parent == existing || len(parent) < len(existing)) {
			if info, err := os.Stat(parent); err == nil {
				fold = directoryFolds(parent, part, info)
			} else {
				fold = fallbackFold
			}
		}
		if key.Len() > 1 {
			key.WriteByte('/')
		}
		if fold {
			key.WriteString(strings.ToLower(part))
		} else {
			key.WriteString(part)
		}
		parent = filepath.Join(parent, part)
	}
	return key.String()
}
