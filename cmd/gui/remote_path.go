package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// Paths of the host (#241). In a remote window every folder a method takes
// is typed on the client, which the server does not trust; the local
// methods the server reuses check it here, so a relative path, one that
// leans on the working directory, or one with a control character never
// reaches the file system or the config. Local mode gets the same checks.

const (
	// remotePathInvalid: the folder is not an absolute path of this host.
	remotePathInvalid remoteCode = "remote_path_invalid"
	// maxHostPath is the longest folder a method takes, in bytes.
	maxHostPath = 4096
)

// hostPath trims p, expands a leading ~, and returns it absolute and
// cleaned, or remote_path_invalid when it is empty, relative, longer than
// maxHostPath or holds a NUL, CR or LF.
func hostPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || len(p) > maxHostPath || strings.ContainsAny(p, "\x00\r\n") {
		return "", &remoteError{remotePathInvalid, "the folder is empty, too long or holds a control character"}
	}
	expanded := config.ExpandTilde(p)
	if !filepath.IsAbs(expanded) {
		return "", &remoteError{remotePathInvalid, "the folder is not an absolute path"}
	}
	return filepath.Clean(expanded), nil
}

// RemotePathDTO says what a folder typed for the host resolves to: its
// absolute path, whether it exists and is a folder, and whether a new file
// could be created there (in its nearest existing parent when it does not
// exist yet). Error is set when the path itself is refused.
type RemotePathDTO struct {
	Abs      string `json:"abs"`
	Exists   bool   `json:"exists"`
	IsDir    bool   `json:"isDir"`
	Writable bool   `json:"writable"`
	Error    string `json:"error,omitempty"`
}

// ValidateRemotePath resolves a folder typed in a remote window against the
// host (#241). It is a read: it creates and removes only a probe file.
func (a *App) ValidateRemotePath(p string) RemotePathDTO { return a.be().ValidateRemotePath(p) }

// validateRemotePathLocal is the local backend's ValidateRemotePath.
func validateRemotePathLocal(p string) RemotePathDTO {
	abs, err := hostPath(p)
	if err != nil {
		return RemotePathDTO{Error: userMsg(err)}
	}
	out := RemotePathDTO{Abs: abs}
	probe := abs
	if info, err := os.Stat(abs); err == nil {
		out.Exists, out.IsDir = true, info.IsDir()
		if !out.IsDir {
			return out
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		probe = nearestExisting(abs)
	} else {
		return out
	}
	out.Writable = canCreateIn(probe)
	return out
}

// nearestExisting walks up from p to the first folder that exists.
func nearestExisting(p string) string {
	for {
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
	}
}

// canCreateIn reports whether a file can be created in dir, by creating
// and removing one.
func canCreateIn(dir string) bool {
	f, err := os.CreateTemp(dir, ".gitbox-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
