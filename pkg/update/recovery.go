package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const RecoveryFlag = "--recover-update"

const retention = 7 * 24 * time.Hour

func previousPath(dst string) string {
	if name, ok := strings.CutSuffix(dst, ".app"); ok {
		return name + ".previous.app"
	}
	return dst + ".previous"
}

// artifactHash covers every relative name, type, permission and file in a
// bundle. File-only installs keep the digest format of the Windows installer.
func artifactHash(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return regularFileHash(path)
	}
	h := sha256.New()
	err = filepath.WalkDir(path, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == path {
			return nil
		}
		name, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%q %d ", filepath.ToSlash(name), info.Mode())
		switch {
		case e.IsDir():
			return nil
		case e.Type()&fs.ModeSymlink != 0:
			dest, err := os.Readlink(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%q", dest)
		case info.Mode().IsRegular():
			digest, err := regularFileHash(p)
			if err != nil {
				return err
			}
			io.WriteString(h, digest)
		default:
			return errors.New("unsupported installed bundle entry")
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

func installPrevious(staged, dst string, ops replaceOps) error {
	unlock, err := lockInstall(dst)
	if err != nil {
		return err
	}
	defer unlock()
	old := previousPath(dst)
	if _, err := os.Lstat(old); !errors.Is(err, os.ErrNotExist) {
		return errors.New("another update backup already exists")
	}
	oldHash, err := artifactHash(dst)
	if err != nil {
		return err
	}
	newHash, err := artifactHash(staged)
	if err != nil {
		return err
	}
	if oldHash == newHash {
		return nil
	}
	state := updateState{Old: filepath.Base(old), OldSHA256: oldHash, InstalledSHA256: newHash, Version: ops.previousVersion, Created: time.Now().UTC()}
	marker := dst + ".update-state.json"
	if err := writeUpdateState(marker, state); err != nil {
		return err
	}
	// Preserve the atomic rename that replaces a running Unix executable.
	if fi, err := os.Lstat(dst); err != nil {
		_ = os.Remove(marker)
		return err
	} else if fi.Mode().IsRegular() {
		if err := os.Link(dst, old); err != nil {
			_ = os.Remove(marker)
			return err
		}
		if err := ops.rename(staged, dst); err != nil {
			_ = os.Remove(old)
			_ = os.Remove(marker)
			return err
		}
		return nil
	}
	if ops.exchange != nil {
		// Before the atomic exchange, the retained path holds the new bundle.
		if err := ops.rename(staged, old); err != nil {
			_ = os.Remove(marker)
			return err
		}
		if err := ops.exchange(old, dst); err != nil {
			_ = os.RemoveAll(old)
			_ = os.Remove(marker)
			return err
		}
		return nil
	}
	if err := ops.rename(dst, old); err != nil {
		_ = os.Remove(marker)
		return err
	}
	if err := ops.rename(staged, dst); err != nil {
		if restoreErr := ops.rename(old, dst); restoreErr != nil {
			return errors.Join(err, errUpdateRollback, restoreErr)
		}
		_ = os.Remove(marker)
		return err
	}
	return nil
}

func readUpdateState(dst string) (updateState, error) {
	path := dst + ".update-state.json"
	var state updateState
	info, err := os.Lstat(path)
	if err != nil {
		return state, err
	}

	data, err := readRecordBytes(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if !validBackupName(dst, state.Old) || len(state.OldSHA256) != 64 || len(state.InstalledSHA256) != 64 {
		return state, errors.New("invalid update record")
	}
	if state.Created.IsZero() {
		state.legacy = true
		state.Created = info.ModTime()
	} // Previous Windows record format.
	return state, nil
}

func validBackupName(dst, name string) bool {
	if strings.HasSuffix(strings.ToLower(dst), ".exe") {
		base := filepath.Base(dst) + ".old"
		if name == base {
			return true
		}
		suffix, ok := strings.CutPrefix(name, base+".")
		if !ok || len(suffix) != hex.EncodedLen(windowsBackupSuffixBytes) {
			return false
		}
		_, err := hex.DecodeString(suffix)
		return err == nil
	}
	return name == filepath.Base(previousPath(dst))
}

// Recovery tracks one installation transaction. Its user-writable health
// file contains only the confirmation; it never supplies install paths.
type Recovery struct {
	dst, health string
	state       updateState
	offer       bool
	confirmed   bool
}

type launchHealth struct {
	Confirmed bool `json:"confirmed"`
}

func BeginRecovery(exe, configDir string) (*Recovery, error) {
	dst := exe
	if b := bundleOf(exe); b != "" {
		dst = b
	}
	state, err := readUpdateState(dst)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A completed cleanup or an interrupted pre-swap may have only its
	// install record left. Recheck under the same lock as installation.
	old := filepath.Join(filepath.Dir(dst), state.Old)
	backupHash, backupErr := artifactHash(old)
	activeHash, err := artifactHash(dst)
	if err != nil {
		return nil, err
	}
	if activeHash == state.OldSHA256 || errors.Is(backupErr, os.ErrNotExist) {
		unlock, err := lockInstall(dst)
		if err != nil {
			return nil, err
		}
		defer unlock()
		current, err := readUpdateState(dst)
		if err != nil || current != state {
			return nil, errors.New("update record changed")
		}
		activeHash, err = artifactHash(dst)
		if err != nil {
			return nil, err
		}
		backupHash, backupErr = artifactHash(old)
		switch {
		case errors.Is(backupErr, os.ErrNotExist) && (activeHash == state.InstalledSHA256 || activeHash == state.OldSHA256):
		case activeHash == state.OldSHA256 && backupErr == nil && (backupHash == state.OldSHA256 || backupHash == state.InstalledSHA256):
			if err := os.RemoveAll(old); err != nil {
				return nil, err
			}
		default:
			return nil, errors.New("could not reconcile update record")
		}
		return nil, os.Remove(dst + ".update-state.json")
	}
	if activeHash != state.InstalledSHA256 || backupErr != nil || backupHash != state.OldSHA256 {
		return nil, errors.New("the update contents changed")
	}
	id := sha256.Sum256([]byte(dst + "\x00" + state.OldSHA256 + "\x00" + state.InstalledSHA256 + "\x00" + state.Created.UTC().Format(time.RFC3339Nano)))
	health := filepath.Join(configDir, "updates", fmt.Sprintf("%x.json", id[:]))
	r := &Recovery{dst: dst, health: health, state: state}

	if r.expired() {
		return r, nil
	}
	if err = os.MkdirAll(filepath.Dir(health), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(health, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		data, err := readRecordBytes(health)
		if err != nil {
			return nil, err
		}
		var h launchHealth
		// An interrupted first launch can leave an incomplete marker.
		r.confirmed = json.Unmarshal(data, &h) == nil && h.Confirmed
		r.offer = !r.confirmed
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	_, err = io.WriteString(f, `{"confirmed":false}`)
	return r, errors.Join(err, f.Sync(), f.Close())
}

func (r *Recovery) NeedsCleanup() bool { return r.confirmed || r.expired() }

// Remaining is the time until the recorded backup expires.
func (r *Recovery) Remaining() time.Duration { return time.Until(r.state.Created.Add(retention)) }
func (r *Recovery) expired() bool            { return r.Remaining() <= 0 }

func (r *Recovery) validate() error {
	st, err := readUpdateState(r.dst)
	if err != nil {
		return err
	}
	if st.Old != r.state.Old || st.InstalledSHA256 != r.state.InstalledSHA256 || st.OldSHA256 != r.state.OldSHA256 || !st.Created.Equal(r.state.Created) {
		return errors.New("the retained update changed")
	}
	for path, want := range map[string]string{r.dst: st.InstalledSHA256, filepath.Join(filepath.Dir(r.dst), st.Old): st.OldSHA256} {
		hash, err := artifactHash(path)
		if err != nil {
			return err
		}
		if hash != want {
			return errors.New("the retained update no longer matches the installation")
		}
	}
	return nil
}

// OfferedVersion is empty on the first launch or once the backup expires.
func (r *Recovery) OfferedVersion() string {
	if r == nil || !r.offer || r.expired() {
		return ""
	}
	if v, err := ParseVersion(r.state.Version); err == nil {
		return "v" + v.String()
	}
	return "previous"
}

// Confirm records a healthy bridge and configuration before cleanup.
func (r *Recovery) Confirm() error {
	tmp, err := os.CreateTemp(filepath.Dir(r.health), ".startup-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := io.WriteString(tmp, `{"confirmed":true}`)
	if err := errors.Join(writeErr, tmp.Sync(), tmp.Close()); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), r.health); err != nil {
		return err
	}
	r.confirmed = true
	r.offer = false
	return nil
}

// Finish removes or restores only this transaction's verified backup.
func (r *Recovery) Finish(rollback bool) error {
	if rollback && r.OfferedVersion() == "" {
		return errors.New("no rollback is available")
	}
	if !rollback && !r.confirmed && !r.expired() {
		return errors.New("the update is not confirmed")
	}
	if err := finishRecovery(r, rollback); err != nil {
		return err
	}
	_ = os.Remove(r.health)
	r.offer = false
	return nil
}

func finishFiles(r *Recovery, rollback bool, rename func(string, string) error) error {
	unlock, err := lockInstall(r.dst)
	if err != nil {
		return err
	}
	defer unlock()
	if err := r.validate(); err != nil {
		return err
	}
	old := filepath.Join(filepath.Dir(r.dst), r.state.Old)
	if rollback && bundleExchange() != nil && strings.HasSuffix(r.dst, ".app") {
		if err := bundleExchange()(old, r.dst); err != nil {
			return err
		}
		if err := os.RemoveAll(old); err != nil {
			return err
		}
	} else if rollback && canReplaceRunningFile(r.dst) {
		// A POSIX rename restores the previous executable atomically.
		if err := rename(old, r.dst); err != nil {
			return err
		}
	} else if rollback {
		// Each temporary directory belongs only to this transaction.
		tmp, err := os.MkdirTemp(filepath.Dir(r.dst), ".gitbox-rollback-*")
		if err != nil {
			return err
		}
		replaced := filepath.Join(tmp, filepath.Base(r.dst))
		if err := rename(r.dst, replaced); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := rename(old, r.dst); err != nil {
			if restoreErr := rename(replaced, r.dst); restoreErr != nil {
				return errors.Join(err, errUpdateRollback, restoreErr)
			}
			_ = os.Remove(tmp)
			return err
		}
		// Keep a legacy-compatible cleanup record if Windows still has
		// the replaced image mapped. The restored version can remove it.
		if retained, err := removeReplaced(r, tmp, replaced); err != nil || retained {
			return err
		}
	} else if err := os.RemoveAll(old); err != nil {
		return err
	}
	return os.Remove(r.dst + ".update-state.json")
}

// readRecordBytes bounds both install and per-user records, including files
// another process changes between stat and read.
func readRecordBytes(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil, errors.New("invalid update record")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readUpdateBytes(file, 4096)
}
