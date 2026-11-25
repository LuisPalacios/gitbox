package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Window presence (#330): each remote window holds a lock on a file of its
// own while it runs, and writes its connection state next to it. Other
// processes on this machine (the local window, the other remote windows, the
// Remote hosts window) read them to say which remotes have a window open.
// The operating system drops the lock when the process ends, however it
// ends, so a crashed window never counts as open.

// WindowsDir is the presence folder for the client config at cfgPath.
func WindowsDir(cfgPath string) string {
	return filepath.Join(filepath.Dir(cfgPath), "windows")
}

// WindowPresence is the presence a remote window holds.
type WindowPresence struct {
	f         *os.File
	statePath string
}

// PresenceState is what a remote window last recorded: its connection state
// and, while it connects, what the login waits for ("agent", "1password").
type PresenceState struct {
	State   string `json:"state"`
	Waiting string `json:"waiting,omitempty"`
}

// windowState is the content of a presence state file.
type windowState struct {
	PresenceState
	UpdatedAt time.Time `json:"updatedAt"`
}

func presencePaths(dir, alias string) (lock, state string, err error) {
	if !ValidRemoteAlias(alias) {
		return "", "", fmt.Errorf("invalid remote alias")
	}
	base := filepath.Join(dir, "remote-"+alias)
	return base + ".lock", base + ".json", nil
}

// HoldWindow takes the presence of the window of alias. It fails when
// another process holds it.
func HoldWindow(dir, alias string) (*WindowPresence, error) {
	lock, state, err := presencePaths(dir, alias)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("presence folder cannot be created")
	}
	f, err := os.OpenFile(lock, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("presence file cannot be opened")
	}
	if err := tryLock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another window holds this remote")
	}
	return &WindowPresence{f: f, statePath: state}, nil
}

// SetState records the connection state of the window.
func (p *WindowPresence) SetState(st PresenceState) error {
	if p == nil {
		return nil
	}
	data, err := json.Marshal(windowState{PresenceState: st, UpdatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	tmp := p.statePath + ".new"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("presence state cannot be written")
	}
	if err := os.Rename(tmp, p.statePath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("presence state cannot be written")
	}
	return nil
}

// Release gives the presence up. The lock file stays, so a window that takes
// it next never races a removal; the state file goes.
func (p *WindowPresence) Release() {
	if p == nil || p.f == nil {
		return
	}
	_ = os.Remove(p.statePath)
	unlock(p.f)
	_ = p.f.Close()
	p.f = nil
}

// WindowOpen reports whether a process holds the presence of alias, and the
// state it last recorded (empty when it recorded none).
func WindowOpen(dir, alias string) (bool, PresenceState) {
	lock, state, err := presencePaths(dir, alias)
	if err != nil {
		return false, PresenceState{}
	}
	f, err := os.OpenFile(lock, os.O_RDWR, 0)
	if err != nil {
		return false, PresenceState{}
	}
	defer f.Close()
	if err := tryLock(f); err == nil {
		unlock(f)
		return false, PresenceState{}
	} else if !errors.Is(err, errLocked) {
		return false, PresenceState{}
	}
	data, err := os.ReadFile(state)
	if err != nil {
		return true, PresenceState{}
	}
	var ws windowState
	if json.Unmarshal(data, &ws) != nil {
		return true, PresenceState{}
	}
	return true, ws.PresenceState
}

// errLocked is what tryLock returns when another holder has the lock.
var errLocked = errors.New("locked")
