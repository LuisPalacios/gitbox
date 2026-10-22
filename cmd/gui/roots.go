package main

// Wails bindings for the clone roots (issue #149): onboarding's first root,
// then listing, adding, removing and picking the default. The roots
// themselves live in pkg/config (roots.go); these only validate, persist and
// shape the data for the frontend.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// ensureRootDir creates path (tilde-expanded) when missing and checks it is
// a directory.
func ensureRootDir(path string) error {
	if path == "" {
		return fmt.Errorf("folder path cannot be empty")
	}
	expanded := config.ExpandTilde(path)
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

// InitRoot sets the first clone root and saves the config; onboarding
// finishes here. A new install starts in Simple mode. On a config that
// already has roots, the folder becomes the default one.
func (a *App) InitRoot(folder string) error {
	folder = strings.TrimSpace(folder)
	if err := ensureRootDir(folder); err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// A new install (no root yet) starts in Simple mode. A config that
	// already chose a mode keeps it.
	if len(a.cfg.Global.Roots) == 0 && a.cfg.Global.UIMode == "" {
		a.cfg.Global.UIMode = uiModeSimple
	}
	a.cfg.AddRoot(folder)
	a.cfg.SetDefaultRoot(folder)
	// Onboarding completes here: promote the in-memory config to "loaded"
	// before the first save so saveConfig() accepts it. Only reachable on
	// the os.ErrNotExist branch of Startup — if the config file existed but
	// failed to parse, cfgLoadError is non-empty and onboarding is blocked
	// by the frontend until the user repairs or replaces it.
	if a.cfgLoadError != "" {
		return fmt.Errorf("cannot complete onboarding while a config load error is pending: %s", a.cfgLoadError)
	}
	a.cfgLoaded = true
	if err := a.saveConfig(); err != nil {
		a.cfgLoaded = false
		return err
	}
	return nil
}

// ListRoots returns the clone roots, the default first. Always a non-nil
// slice so it serializes to a JSON array.
func (a *App) ListRoots() []RootDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]RootDTO, 0, len(a.cfg.Global.Roots))
	for i, r := range a.cfg.Global.Roots {
		info, err := os.Stat(config.ExpandTilde(r))
		out = append(out, RootDTO{Path: r, IsDefault: i == 0, Exists: err == nil && info.IsDir(), Repos: len(a.cfg.RootRefs(r))})
	}
	return out
}

// AddRoot adds a clone root (created when missing, deduped) and persists.
func (a *App) AddRoot(path string) error {
	path = strings.TrimSpace(path)
	if err := ensureRootDir(path); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.AddRoot(path) {
		return a.saveConfig()
	}
	return nil
}

// RemoveRoot drops a clone root and persists. When repos still resolve
// under it, nothing changes unless forget is set: then those repos leave the
// config, and their files stay on disk.
func (a *App) RemoveRoot(path string, forget bool) (RemoveRootDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	refs, err := a.cfg.RemoveRoot(path, forget)
	if errors.Is(err, config.ErrRootInUse) {
		return RemoveRootDTO{InUse: len(refs)}, nil
	}
	if err != nil {
		return RemoveRootDTO{}, err
	}
	if err := a.saveConfig(); err != nil {
		return RemoveRootDTO{}, err
	}
	return RemoveRootDTO{Forgotten: append([]string{}, refs...)}, nil
}

// SetDefaultRoot makes a listed root the default and persists. No clone
// moves: accounts that followed the old default stay on it.
func (a *App) SetDefaultRoot(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfg.HasRoot(path) {
		return fmt.Errorf("%s is not a clone root", path)
	}
	a.cfg.SetDefaultRoot(path)
	return a.saveConfig()
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
