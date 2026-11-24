package remote

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// The limits of ConfigAliases. OpenSSH stops at the same Include depth.
const (
	maxIncludeDepth = 16
	maxConfigFiles  = 64
	maxConfigBytes  = 256 << 10
)

// ConfigAliases returns the concrete Host aliases of the ssh configuration
// under home (home/.ssh/config and the files it includes), in file order and
// without duplicates, to suggest them when a remote is added (#330). It
// leaves out patterns (*, ?, negations) and names a remote cannot use. It
// only reads: a missing, unreadable or oversized file contributes nothing,
// and no error reaches the caller. ssh itself still resolves the alias.
func ConfigAliases(home string) []string {
	if home == "" {
		return nil
	}
	sshDir := filepath.Join(home, ".ssh")
	r := &aliasReader{home: home, sshDir: sshDir, seen: map[string]bool{}, files: map[string]bool{}}
	r.read(filepath.Join(sshDir, "config"), 0)
	return r.out
}

type aliasReader struct {
	home, sshDir string
	out          []string
	seen, files  map[string]bool
}

func (r *aliasReader) read(path string, depth int) {
	if depth > maxIncludeDepth || len(r.files) >= maxConfigFiles {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil || r.files[abs] {
		return
	}
	r.files[abs] = true
	data, ok := readConfigFile(abs)
	if !ok {
		return
	}
	for _, line := range strings.Split(data, "\n") {
		key, args := configLine(line)
		switch strings.ToLower(key) {
		case "host":
			for _, a := range args {
				r.add(a)
			}
		case "include":
			for _, a := range args {
				r.include(a, depth)
			}
		}
	}
}

// include reads the files one Include argument names: a glob, relative to
// ~/.ssh unless it is absolute or starts with "~/".
func (r *aliasReader) include(pattern string, depth int) {
	switch {
	case strings.HasPrefix(pattern, "~/"):
		pattern = filepath.Join(r.home, pattern[2:])
	case !filepath.IsAbs(pattern):
		pattern = filepath.Join(r.sshDir, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	for _, m := range matches {
		r.read(m, depth+1)
	}
}

func (r *aliasReader) add(alias string) {
	if strings.ContainsAny(alias, "*?!") || !config.ValidRemoteAlias(alias) || r.seen[alias] {
		return
	}
	r.seen[alias] = true
	r.out = append(r.out, alias)
}

// readConfigFile returns the text of a regular file of at most
// maxConfigBytes.
func readConfigFile(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigBytes+1))
	if err != nil || len(data) > maxConfigBytes {
		return "", false
	}
	return string(data), true
}

// configLine splits one ssh_config line into its keyword and arguments. The
// keyword ends at whitespace or '='; arguments may be double-quoted. Blank
// lines and comments have no keyword.
func configLine(line string) (string, []string) {
	line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if line == "" || line[0] == '#' {
		return "", nil
	}
	end := strings.IndexAny(line, " \t=")
	if end < 0 {
		return line, nil
	}
	key, rest := line[:end], strings.TrimLeft(line[end:], " \t")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), " \t")
	var args []string
	for rest != "" {
		var tok string
		if rest[0] == '"' {
			q := strings.IndexByte(rest[1:], '"')
			if q < 0 {
				return key, args
			}
			tok, rest = rest[1:q+1], rest[q+2:]
		} else {
			sp := strings.IndexAny(rest, " \t")
			if sp < 0 {
				sp = len(rest)
			}
			tok, rest = rest[:sp], rest[sp:]
		}
		if strings.HasPrefix(tok, "#") {
			break
		}
		if tok != "" {
			args = append(args, tok)
		}
		rest = strings.TrimLeft(rest, " \t")
	}
	return key, args
}
