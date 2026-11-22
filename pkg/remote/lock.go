package remote

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// The times that govern the server root.
const (
	// MarkerRefresh is how often a running server refreshes its in-use
	// marker, and MarkerLive how old a marker may be and still count.
	MarkerRefresh = 60 * time.Second
	MarkerLive    = 10 * time.Minute
	// StaleLock is the age at which a GC lock counts as abandoned.
	StaleLock = 5 * time.Minute
	// TempMaxAge is the age at which an upload's temporary file counts as
	// abandoned.
	TempMaxAge = time.Hour
)

// Names under the server root.
const (
	markerPrefix = "in-use."
	lockName     = ".gc.lock"
	lockOwner    = "owner"
	// LogsDir holds the server logs, one per build.
	LogsDir   = "logs"
	tmpPrefix = ".gitbox-server."
	tmpSuffix = ".tmp"
)

// Marker is the content of an in-use marker: one running server.
type Marker struct {
	PID       int       `json:"pid"`
	ClientID  string    `json:"clientId"`
	StartedAt time.Time `json:"startedAt"`
}

// MarkerPath is the in-use marker of a client in a build folder.
func MarkerPath(buildDir, clientID string) string {
	return filepath.Join(buildDir, markerPrefix+clientID)
}

// WriteMarker writes the in-use marker of m in buildDir and returns its
// path.
func WriteMarker(buildDir string, m Marker) (string, error) {
	if !clientIDRE.MatchString(m.ClientID) {
		return "", fmt.Errorf("invalid client id")
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	path := MarkerPath(buildDir, m.ClientID)
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// Locate finds the server root and the build of a server binary from its
// path: <root>/<build>/gitbox-server. ok is false for a binary that is not
// installed that way, such as a development build.
func Locate(exe string) (root, build string, ok bool) {
	if filepath.Base(exe) != BinaryName {
		return "", "", false
	}
	dir := filepath.Dir(exe)
	build = filepath.Base(dir)
	if !BuildNameRE.MatchString(build) {
		return "", "", false
	}
	root = filepath.Dir(dir)
	if unsafeRoot(root) {
		return "", "", false
	}
	return root, build, true
}

// unsafeRoot reports a root that is the filesystem root or the home folder:
// nothing under it belongs to the server alone.
func unsafeRoot(root string) bool {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return true
	}
	if filepath.Dir(real) == real {
		return true
	}
	if home, err := os.UserHomeDir(); err == nil {
		if h, err := filepath.EvalSymlinks(home); err == nil && h == real {
			return true
		}
	}
	return false
}

// liveMarkers counts the markers in a build folder refreshed within
// MarkerLive of now.
func liveMarkers(buildDir string, now time.Time) int {
	entries, err := os.ReadDir(buildDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || len(e.Name()) <= len(markerPrefix) || e.Name()[:len(markerPrefix)] != markerPrefix {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) < MarkerLive {
			n++
		}
	}
	return n
}

// gcLock is the lock of one garbage collection: a folder, because mkdir is
// atomic even on NFS, where flock is not.
type gcLock struct{ path string }

// errLocked means another collection holds a fresh lock.
var errLocked = errors.New("another garbage collection holds the lock")

// takeLock creates the lock, breaking one older than StaleLock first.
func takeLock(root string, owner Marker, now time.Time, logf func(string, ...any)) (*gcLock, error) {
	path := filepath.Join(root, lockName)
	for attempt := 0; attempt < 2; attempt++ {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			data, _ := json.Marshal(struct {
				PID      int       `json:"pid"`
				ClientID string    `json:"clientId"`
				Time     time.Time `json:"time"`
			}{owner.PID, owner.ClientID, now})
			_ = os.WriteFile(filepath.Join(path, lockOwner), data, 0o600)
			return &gcLock{path}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		info, serr := os.Stat(path)
		if serr != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		if age < StaleLock {
			return nil, errLocked
		}
		logf("gc: breaking a stale lock, %s old", age.Round(time.Second))
		if err := os.RemoveAll(path); err != nil {
			return nil, err
		}
	}
	return nil, errLocked
}

func (l *gcLock) release() { _ = os.RemoveAll(l.path) }
