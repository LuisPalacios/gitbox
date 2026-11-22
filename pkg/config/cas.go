package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// The guard across processes (#241). Several processes write one
// gitbox.json: the local window, every remote window, the terminals window
// and, on a host, one gitbox-server per client. Each writer remembers the
// domain it last read or wrote (Hash) and saves only when the file still
// holds that domain; otherwise it gets ChangedError and reloads. Client
// preferences and UI state are left out of the hash: they are merged from
// disk on every save, so a window that saves its geometry never makes
// another window's domain save fail. A lock file next to the config turns
// the read, compare and write into one step.

// Hash identifies the domain content of a config: everything but the
// client preferences and the UI state. The zero Hash means "never read".
type Hash [sha256.Size]byte

// IsZero reports whether h was never set.
func (h Hash) IsZero() bool { return h == Hash{} }

// DomainHash is the Hash of cfg.
func DomainHash(cfg *Config) Hash {
	if cfg == nil {
		return Hash{}
	}
	c := *cfg
	StripClientPrefs(&c)
	c.Global.Window = nil
	c.Global.CompactWindow = nil
	c.Global.Collapsed = nil
	data, err := marshalConfig(&c)
	if err != nil {
		return Hash{}
	}
	return sha256.Sum256(data)
}

// ChangedError says the config on disk is not the one the writer read:
// another process saved a domain change meanwhile, or held the lock too
// long. The writer reloads and the user retries.
type ChangedError struct {
	// Locked is set when the lock could not be taken in time.
	Locked bool
}

func (e *ChangedError) Error() string {
	if e.Locked {
		return "the config is being saved by another process"
	}
	return "the config changed on disk since it was read"
}

const (
	// lockStale is the age after which a lock file is taken to be left by a
	// writer that died, and is broken.
	lockStale = 30 * time.Second
	// lockPoll is how often a writer retries a held lock.
	lockPoll = 20 * time.Millisecond
)

// lockWait is how long a writer waits for a held lock; tests shorten it.
var lockWait = 5 * time.Second

// lockWarn reports a broken lock; tests replace it.
var lockWarn = func(path string) {
	fmt.Fprintf(os.Stderr, "warning: removed a stale config lock %s\n", path)
}

// acquireLock takes path.lock with O_EXCL, which works the same on every
// supported system, breaking a lock older than lockStale.
func acquireLock(path string) (release func(), err error) {
	lock := path + ".lock"
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(lock) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("locking config: %w", err)
		}
		if info, serr := os.Stat(lock); serr == nil && time.Since(info.ModTime()) > lockStale {
			lockWarn(lock)
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return nil, &ChangedError{Locked: true}
		}
		time.Sleep(lockPoll)
	}
}

// SaveIfUnchanged saves cfg to path when the file still holds the domain
// base (#241). Under the lock it reads the file and, when its domain hash
// differs from base, returns ChangedError without writing. Otherwise it
// calls merge with the config on disk (nil when there is no file or it
// does not parse), so the caller can lay the
// preferences on disk over cfg, and writes cfg. It returns the domain hash
// written. A zero base means the caller never read the file: the save goes
// ahead whatever the file holds, as before this guard. A file that does not
// parse cannot be compared, so the save replaces it, as before (#240).
func SaveIfUnchanged(cfg *Config, path string, base Hash, merge func(onDisk *Config) error) (Hash, error) {
	if err := EnsureDir(path); err != nil {
		return Hash{}, fmt.Errorf("creating config directory: %w", err)
	}
	release, err := acquireLock(path)
	if err != nil {
		return Hash{}, err
	}
	defer release()

	data, rerr := os.ReadFile(path)
	switch {
	case rerr == nil:
		onDisk, perr := Parse(data)
		if perr != nil {
			// A file that does not parse cannot be compared: the save
			// replaces it, as before this guard (#240).
			onDisk = nil
		} else if !base.IsZero() && DomainHash(onDisk) != base {
			return Hash{}, &ChangedError{}
		}
		if merge != nil {
			if err := merge(onDisk); err != nil {
				return Hash{}, err
			}
		}
	case errors.Is(rerr, fs.ErrNotExist):
		if merge != nil {
			if err := merge(nil); err != nil {
				return Hash{}, err
			}
		}
	default:
		return Hash{}, fmt.Errorf("reading config: %w", rerr)
	}

	if err := saveWithFiles(cfg, path, configFiles()); err != nil {
		return Hash{}, err
	}
	return DomainHash(cfg), nil
}

// UpdateFile reads the config at path, lets change edit it, and saves the
// result, all under the lock (#241): a writer that changes only its own
// keys never interleaves with another process's save. A file that is
// missing or does not parse is returned as an error and nothing is
// written. When change fails, nothing is written either.
func UpdateFile(path string, change func(*Config) error) (*Config, error) {
	release, err := acquireLock(path)
	if err != nil {
		return nil, err
	}
	defer release()
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	if err := change(cfg); err != nil {
		return nil, err
	}
	if err := saveWithFiles(cfg, path, configFiles()); err != nil {
		return nil, err
	}
	return cfg, nil
}
