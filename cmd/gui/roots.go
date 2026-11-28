package main

// Wails bindings for the clone roots (issue #149): onboarding's first root,
// then listing, adding, removing and picking the default. The roots
// themselves live in pkg/config (roots.go); these only validate, persist and
// shape the data for the frontend.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// RootDTO is one clone root as Settings lists it.
type RootDTO struct {
	Path      string `json:"path"`
	IsDefault bool   `json:"isDefault"`
	Exists    bool   `json:"exists"`
	Repos     int    `json:"repos"` // tracked repos that resolve under it
}

// RemoveRootDTO answers RemoveRoot: InUse > 0 means nothing changed because
// that many repos still resolve under the root; Forgotten lists the repo
// keys dropped from the config when forget was set.
type RemoveRootDTO struct {
	InUse     int      `json:"inUse"`
	Forgotten []string `json:"forgotten"`
}

// ensureRootDir creates a validated, expanded host path when missing.
func ensureRootDir(expanded string) error {
	if err := os.MkdirAll(expanded, 0o755); err != nil {
		return fmt.Errorf("creating folder %s: %w", expanded, err)
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return fmt.Errorf("checking folder %s: %w", expanded, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", expanded)
	}
	return nil
}

// ensureListedRoot validates the placement before creating a directory.
// The caller holds a.mu; a failed mkdir leaves the list untouched.
func ensureListedRoot(cfg *config.Config, path string) (bool, error) {
	expanded, err := hostPath(path)
	if err != nil {
		return false, err
	}
	roots := cfg.Global.Roots
	changed, err := cfg.AddRootChecked(path)
	if err != nil {
		return false, err
	}
	if err := ensureRootDir(expanded); err != nil {
		cfg.Global.Roots = roots
		return false, err
	}
	return changed, nil
}

// InitRoot sets the first clone root and saves the config; onboarding
// finishes here. A new install starts in Simple mode. On a config that
// already has roots, the folder becomes the default one.
//
// It is MIXED (#241): the backend creates and records the root, and in a
// remote window the client half starts this computer's preferences in
// Simple mode, which belong to the client and not to the host.
func (a *App) InitRoot(folder string) error {
	if err := a.be().InitRoot(folder); err != nil {
		return err
	}
	if !a.remoteWindow() {
		return nil
	}
	return a.clientPrefs().Update(func(p *config.ClientPrefs) {
		if p.UIMode == "" {
			p.UIMode = uiModeSimple
		}
	})
}

// initRootLocal is the local backend's InitRoot. save and merge are
// App.saveConfig and App.mergeDiskPrefs, called under s.mu.
func (s *localSession) initRootLocal(folder string, save func() error, merge func(*config.Config) error) error {
	folder = strings.TrimSpace(folder)
	s.mu.Lock()
	defer s.mu.Unlock()

	// A new install (no root yet) starts in Simple mode. A config that
	// already chose a mode keeps it, also one another window saved.
	if onDisk, err := config.Load(s.cfgPath); err == nil {
		_ = merge(onDisk)
	}
	first := len(s.cfg.Global.Roots) == 0
	if _, err := ensureListedRoot(s.cfg, folder); err != nil {
		return err
	}
	if first && s.cfg.Global.UIMode == "" {
		s.cfg.Global.UIMode = uiModeSimple
	}
	s.cfg.SetDefaultRoot(folder)
	// Onboarding completes here: promote the in-memory config to "loaded"
	// before the first save so saveConfig() accepts it. Only reachable on
	// the os.ErrNotExist branch of startup — if the config file existed but
	// failed to parse, cfgLoadError is non-empty and onboarding is blocked
	// by the frontend until the user repairs or replaces it.
	if s.cfgLoadError != "" {
		return fmt.Errorf("cannot complete onboarding while a config load error is pending: %s", s.cfgLoadError)
	}
	s.cfgLoaded = true
	if err := save(); err != nil {
		if !isConfigChanged(err) {
			s.cfgLoaded = false
		}
		return err
	}
	return nil
}

// ListRoots returns the clone roots, the default first. Always a non-nil
// slice so it serializes to a JSON array.
func (a *App) ListRoots() []RootDTO { return a.be().ListRoots() }

// listRootsLocal is the local backend's ListRoots.
func (s *localSession) listRootsLocal() []RootDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RootDTO, 0, len(s.cfg.Global.Roots))
	for i, r := range s.cfg.Global.Roots {
		info, err := os.Stat(config.ExpandTilde(r))
		out = append(out, RootDTO{Path: r, IsDefault: i == 0, Exists: err == nil && info.IsDir(), Repos: len(s.cfg.RootRefs(r))})
	}
	return out
}

// AddRoot adds a clone root (created when missing, deduped) and persists.
func (a *App) AddRoot(path string) error { return a.be().AddRoot(path) }

// addRootLocal is the local backend's AddRoot.
func (s *localSession) addRootLocal(path string, save func() error) error {
	path = strings.TrimSpace(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	changed, err := ensureListedRoot(s.cfg, path)
	if err != nil {
		return err
	}
	if changed {
		return save()
	}
	return nil
}

// RemoveRoot drops a clone root and persists. When repos still resolve
// under it, nothing changes unless forget is set: then those repos leave the
// config, and their files stay on disk.
func (a *App) RemoveRoot(path string, forget bool) (RemoveRootDTO, error) {
	return a.be().RemoveRoot(path, forget)
}

// removeRootLocal is the local backend's RemoveRoot.
func (s *localSession) removeRootLocal(path string, forget bool, save func() error) (RemoveRootDTO, error) {
	path, err := hostPath(path)
	if err != nil {
		return RemoveRootDTO{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	refs, err := s.cfg.RemoveRoot(path, forget)
	if errors.Is(err, config.ErrRootInUse) {
		return RemoveRootDTO{InUse: len(refs)}, nil
	}
	if err != nil {
		return RemoveRootDTO{}, err
	}
	if err := save(); err != nil {
		return RemoveRootDTO{}, err
	}
	return RemoveRootDTO{Forgotten: append([]string{}, refs...)}, nil
}

// SetDefaultRoot makes a listed root the default and persists. No clone
// moves: accounts that followed the old default stay on it.
func (a *App) SetDefaultRoot(path string) error { return a.be().SetDefaultRoot(path) }

// setDefaultRootLocal is the local backend's SetDefaultRoot.
func (s *localSession) setDefaultRootLocal(path string, save func() error) error {
	path, err := hostPath(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfg.HasRoot(path) {
		return fmt.Errorf("%s is not a clone root", path)
	}
	s.cfg.SetDefaultRoot(path)
	return save()
}

// auditClonePathsLocked returns the folder collisions shown in Needs
// attention, auditing again when the config changed since the last audit.
// A new set of collisions is logged to log, or to stderr when log is nil.
// The caller holds s.mu.
func (s *localSession) auditClonePathsLocked(log io.Writer) []config.PathConflict {
	if s.pathsAudited && s.pathAuditBase == s.cfgBase {
		return s.pathConflicts
	}
	conflicts := s.cfg.PathConflicts()
	if len(conflicts) > 0 && !slices.Equal(conflicts, s.pathConflicts) {
		if log == nil {
			log = os.Stderr
		}
		for _, conflict := range conflicts {
			fmt.Fprintf(log, "warning: folder collision between %q and %q; see Needs attention\n", userText(conflict.First), userText(conflict.Second))
		}
	}
	s.pathConflicts, s.pathAuditBase, s.pathsAudited = conflicts, s.cfgBase, true
	return s.pathConflicts
}

// accountFolder returns <root>/<source folder> for an account's source: the
// source keyed like the account, else the first one it owns; with none, the
// default root joined with the account key.
func accountFolder(cfg *config.Config, accountKey string) string {
	sourceKey := ""
	if src, ok := cfg.Sources[accountKey]; ok && src.Account == accountKey {
		sourceKey = accountKey
	} else {
		for _, sk := range cfg.OrderedSourceKeys() {
			if cfg.Sources[sk].Account == accountKey {
				sourceKey = sk
				break
			}
		}
	}
	if sourceKey == "" {
		return filepath.Join(cfg.DefaultRoot(), accountKey)
	}
	src := cfg.Sources[sourceKey]
	return filepath.Join(cfg.SourceRoot(sourceKey), src.EffectiveFolder(sourceKey))
}
