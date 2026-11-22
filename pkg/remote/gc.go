package remote

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GC describes one garbage collection of a server root. gitbox-server runs
// it right after it starts serving and has written its own marker, so it
// costs the client no extra exec.
type GC struct {
	// Root is the server root and Current the build that is running.
	Root, Current string
	// Owner is written into the lock.
	Owner Marker
	// Now is the time the ages are measured against.
	Now time.Time
	// Logf receives one line per decision.
	Logf func(format string, args ...any)
}

// CollectGarbage deletes what nobody uses any more: every build folder
// other than the current one with no live marker and no upload in
// progress, temporary files older than TempMaxAge, and the logs of builds
// that are gone. It touches only entries whose names gitbox gives them, and
// never runs on a root that is the home or the filesystem root. When
// another collection holds a fresh lock, it does nothing.
func CollectGarbage(g GC) error {
	logf := g.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if unsafeRoot(g.Root) {
		logf("gc: skipped, the server root is the home or root folder")
		return nil
	}
	lock, err := takeLock(g.Root, g.Owner, g.Now, logf)
	if errors.Is(err, errLocked) {
		logf("gc: skipped, another collection holds the lock")
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.release()

	entries, err := os.ReadDir(g.Root)
	if err != nil {
		return err
	}
	builds := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !BuildNameRE.MatchString(name) {
			continue
		}
		dir := filepath.Join(g.Root, name)
		recentTemp := sweepTemps(dir, g.Now, logf)
		switch {
		case name == g.Current:
		case liveMarkers(dir, g.Now) > 0:
			logf("gc: keeping %s, in use", name)
		case recentTemp:
			logf("gc: keeping %s, an upload is in progress", name)
		default:
			if err := os.RemoveAll(dir); err != nil {
				logf("gc: could not remove %s", name)
				builds[name] = true
				continue
			}
			logf("gc: removed %s", name)
			continue
		}
		builds[name] = true
	}
	sweepLogs(filepath.Join(g.Root, LogsDir), builds, logf)
	return nil
}

// sweepTemps deletes the abandoned upload files of a build folder and
// reports whether a recent one remains.
func sweepTemps(dir string, now time.Time, logf func(string, ...any)) (recent bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, tmpPrefix) || !strings.HasSuffix(name, tmpSuffix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) < TempMaxAge {
			recent = true
			continue
		}
		if os.Remove(filepath.Join(dir, name)) == nil {
			logf("gc: removed an abandoned upload in %s", filepath.Base(dir))
		}
	}
	return recent
}

// sweepLogs deletes the logs, rotated ones included, of builds that are no
// longer installed.
func sweepLogs(dir string, builds map[string]bool, logf func(string, ...any)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		i := strings.Index(name, ".log")
		if e.IsDir() || i < 0 {
			continue
		}
		build := name[:i]
		if !BuildNameRE.MatchString(build) || builds[build] {
			continue
		}
		if rest := name[i+len(".log"):]; rest != "" && rest != ".1" && rest != ".2" && rest != ".3" {
			continue
		}
		if os.Remove(filepath.Join(dir, name)) == nil {
			logf("gc: removed log %s", name)
		}
	}
}
