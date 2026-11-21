package main

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// prefsStore reads and writes the client preferences: the keys of
// config.ClientPrefs, which belong to the machine that shows the window. No
// client key is read or written any other way, so the same code serves local
// mode, where they sit in the one gitbox.json, and remote mode (#233), where
// the server owns its host's file and the client keeps its own.
type prefsStore interface {
	// Get returns a detached copy of the preferences.
	Get() config.ClientPrefs
	// Update changes the preferences and saves them. It always saves; a
	// caller that only wants to save a real change checks Get first.
	Update(func(*config.ClientPrefs)) error
}

// clientPrefs returns the store. An App built without one (local mode and
// most tests) shares its config with the local backend.
func (a *App) clientPrefs() prefsStore {
	if a.prefs != nil {
		return a.prefs
	}
	return sharedPrefs{a}
}

// sharedPrefs is local mode: the preferences are the client keys of the one
// in-memory config, saved to the one file by saveConfig. A domain save can
// never revert a preference, or the other way round.
type sharedPrefs struct{ a *App }

func (s sharedPrefs) Get() config.ClientPrefs {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.a.cfg == nil {
		return config.ClientPrefs{}
	}
	return config.ExtractPrefs(s.a.cfg)
}

func (s sharedPrefs) Update(change func(*config.ClientPrefs)) error {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.a.cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	p := config.ExtractPrefs(s.a.cfg)
	change(&p)
	if err := config.ValidateRemotes(p.Remotes); err != nil {
		return err
	}
	config.ApplyPrefs(s.a.cfg, p)
	return s.a.saveConfig()
}

// filePrefs keeps the preferences in the client's own gitbox.json, apart
// from the backend's config. Non-local backends use it, and so do tests.
type filePrefs struct {
	mu      sync.Mutex
	path    string
	cfg     *config.Config
	loaded  bool   // the file parsed, so it is safe to save back (#60)
	loadErr string // why it did not, when the file exists
}

func newFilePrefs(path string) *filePrefs {
	f := &filePrefs{path: path, cfg: &config.Config{}}
	f.load()
	return f
}

// load re-reads the file. On failure it keeps the last good preferences in
// memory and blocks saving. Caller holds f.mu, except in newFilePrefs.
func (f *filePrefs) load() {
	cfg, err := config.Load(f.path)
	if err != nil {
		f.loaded = false
		f.loadErr = ""
		if !errors.Is(err, os.ErrNotExist) {
			f.loadErr = err.Error()
		}
		return
	}
	f.cfg, f.loaded, f.loadErr = cfg, true, ""
}

// Reload picks up edits made to the file outside this process.
func (f *filePrefs) Reload() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
}

func (f *filePrefs) Get() config.ClientPrefs {
	f.mu.Lock()
	defer f.mu.Unlock()
	return config.ExtractPrefs(f.cfg)
}

// Update reloads the file first and writes only the client keys back, so
// two windows that share the file overwrite as little of each other as
// possible. Two edits of the same preference are last-writer-wins. Like
// saveConfig, it refuses to save over a file it could not load (#60).
func (f *filePrefs) Update(change func(*config.ClientPrefs)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.load()
	if !f.loaded {
		if f.loadErr != "" {
			return fmt.Errorf("refusing to save preferences: %s not loaded (%s)", f.path, f.loadErr)
		}
		return fmt.Errorf("refusing to save preferences: %s not loaded", f.path)
	}
	p := config.ExtractPrefs(f.cfg)
	change(&p)
	if err := config.ValidateRemotes(p.Remotes); err != nil {
		return err
	}
	config.ApplyPrefs(f.cfg, p)
	return config.Save(f.cfg, f.path)
}

// overlayPrefs lays the client preferences over a config that came from the
// backend: the frontend sees the host's domain data with this machine's
// preferences.
func (a *App) overlayPrefs(dto *ConfigDTO) {
	tmp := config.Config{Global: dto.Global}
	config.ApplyPrefs(&tmp, a.clientPrefs().Get())
	dto.Global = tmp.Global
}
