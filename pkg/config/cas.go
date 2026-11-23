package config

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
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
	// writer that may have died. Its PID and identity are checked before removal.
	lockStale = 30 * time.Second
	// lockPoll is how often a writer retries a held lock.
	lockPoll = 20 * time.Millisecond
)

// lockWait is how long a writer waits for a held lock; tests shorten it.
var lockWait = 2 * time.Second

// lockWarn reports a broken lock; tests replace it.
var lockWarn = func(path string) {
	fmt.Fprintf(os.Stderr, "warning: removed a stale config lock %s\n", path)
}

// acquireLock takes path.lock with O_EXCL, which works the same on every
// supported system, reclaiming only a dead writer’s stale lock.
func acquireLock(path string) (release func(), err error) {
	lock := path + ".lock"
	deadline := time.Now().Add(lockWait)
	for {
		f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := fmt.Fprintf(f, "%d\n", os.Getpid())
			if err := errors.Join(writeErr, f.Close()); err != nil {
				_ = os.Remove(lock)
				return nil, err
			}
			return func() { _ = os.Remove(lock) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("locking config: %w", err)
		}
		if !time.Now().Before(deadline) {
			return nil, &ChangedError{Locked: true}
		}
		if info, err := os.Stat(lock); err == nil && time.Since(info.ModTime()) > lockStale && removeDeadLock(lock) {
			lockWarn(lock)
			continue
		}
		time.Sleep(min(lockPoll, time.Until(deadline)))
	}
}

// Lock the stale inode before examining its owner. A second reaper may
// already have removed it and a new writer may own the name.
func removeDeadLock(path string) bool {
	f, err := openReaper(path)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 32))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 || !processGone(pid) {
		return false
	}
	held, err := f.Stat()
	if err != nil || time.Since(held.ModTime()) <= lockStale {
		return false
	}
	current, err := os.Stat(path)
	return err == nil && os.SameFile(held, current) && os.Remove(path) == nil
}

// FileRevision identifies the bytes a window loaded or saved. Keeping the
// domain and presentation fingerprints lets concurrent windows merge a change
// confined to preferences or layout. A missing file also has a revision.
type FileRevision struct {
	known                                  bool
	bytes, domain, presentation, collapsed Hash
}

func jsonHash(v any) Hash {
	b, _ := json.Marshal(v) // preferences and layout contain only JSON field types
	return sha256.Sum256(b)
}

// SameDomain reports whether both reads describe the same saved domain.
func (r FileRevision) SameDomain(other FileRevision) bool { return r.domain == other.domain }

func revision(data []byte, cfg *Config) FileRevision {
	r := FileRevision{known: true}
	if data != nil {
		r.bytes = sha256.Sum256(data)
	}
	if cfg != nil {
		r.domain = DomainHash(cfg)
		r.collapsed = jsonHash(cfg.Global.Collapsed)
		r.presentation = presentationHash(cfg)
	}
	return r
}

func presentationHash(c *Config) Hash {
	return jsonHash(struct {
		Prefs     ClientPrefs
		Collapsed *CollapsedState
	}{ExtractPrefs(c), c.Global.Collapsed})
}

// LoadRevision decodes and fingerprints the same bytes. On a parse failure
// the raw revision can still guard an explicit repair or restore.
func LoadRevision(path string, repair bool) (*Config, []Repair, FileRevision, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, FileRevision{known: true}, err
	}
	c, repairs, err := decode(data, repair)
	r := revision(data, c)
	if repair {
		r.domain = Hash{} // a repair must match the original bytes exactly
	}
	return c, repairs, r, err
}

func (r FileRevision) matches(data []byte, disk *Config) bool {
	if !r.known {
		return r.domain.IsZero() || r.domain == DomainHash(disk)
	}
	var sum Hash
	if data != nil {
		sum = sha256.Sum256(data)
	}
	if r.bytes == sum {
		return true
	}
	return disk != nil && !r.domain.IsZero() && r.domain == DomainHash(disk) && r.presentation != presentationHash(disk)
}

// SaveIfUnchanged saves against a domain fingerprint for existing callers.
// A window that has read the file uses FileRevision.Save to guard its bytes.
func SaveIfUnchanged(cfg *Config, path string, base Hash, merge func(*Config) error) (Hash, error) {
	r, err := (FileRevision{domain: base}).Save(cfg, path, merge)
	return r.domain, err
}

// Save rechecks the revision under the lock, then merges the latest preferences
// and writes atomically. Keep an external collapse change when the caller did
// not change it (account renames, for example, can update the selected view).
func (r FileRevision) Save(cfg *Config, path string, merge func(*Config) error) (FileRevision, error) {
	if err := EnsureDir(path); err != nil {
		return r, fmt.Errorf("creating config directory: %w", err)
	}
	release, err := acquireLock(path)
	if err != nil {
		return r, err
	}
	defer release()

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return r, fmt.Errorf("reading config: %w", err)
	}
	var onDisk *Config
	if data != nil {
		onDisk, _ = Parse(data)
	}
	if !r.matches(data, onDisk) {
		return r, &ChangedError{}
	}
	if onDisk != nil && r.known && r.collapsed == jsonHash(cfg.Global.Collapsed) {
		cfg.Global.Collapsed = onDisk.Global.Collapsed
	}
	if merge != nil {
		if err := merge(onDisk); err != nil {
			return r, err
		}
	}
	cfg.PruneCollapsed()
	data, err = Marshal(cfg)
	if err != nil {
		return r, err
	}
	if err := saveBytes(cfg, path, data, configFiles()); err != nil {
		return r, err
	}
	return revision(data, cfg), nil
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
