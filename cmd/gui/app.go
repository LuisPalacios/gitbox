package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"time"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails application struct. All exported methods become
// frontend bindings via window.go.main.App.<Method>().
type App struct {
	ctx               context.Context
	cfg               *config.Config
	cfgPath           string
	cfgLoaded         bool                // true if config was loaded from disk (safe to save back)
	cfgLoadError      string              // non-empty if config exists but failed to parse
	testMode          bool                // true when launched with --test-mode
	testCleanup       func()              // cleanup function for test-mode temp dir
	mu                sync.Mutex
	savedWindowPos    *config.WindowState // full-mode window state pre-loaded from config
	savedCompactPos   *config.WindowState // compact-mode window state pre-loaded from config
	savedViewMode     string              // "full" or "compact" pre-loaded from config
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// ─── Lifecycle ────────────────────────────────────────────────

// Startup is called by Wails at application start.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	if a.cfgPath == "" {
		a.cfgPath = config.DefaultV2Path()
	}
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// Config exists but failed to parse — store error for the frontend to display.
			a.cfgLoadError = fmt.Sprintf("Failed to load %s:\n%v", a.cfgPath, err)
			fmt.Fprintf(os.Stderr, "warning: %s\n", a.cfgLoadError)
		}
		// Config doesn't exist or is unreadable — start with empty config for onboarding.
		cfg = &config.Config{
			Schema:   "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/json/gitbox.schema.json",
			Version:  2,
			Accounts: make(map[string]config.Account),
			Sources:  make(map[string]config.Source),
		}
	} else {
		a.cfgLoaded = true
	}
	a.cfg = cfg
}

// Shutdown is called by Wails when the app is closing.
func (a *App) Shutdown(_ context.Context) {
	if a.testCleanup != nil {
		a.testCleanup()
	}
}

// IsTestMode returns true when the app was launched with --test-mode.
// Exposed to the frontend for UI indicator.
func (a *App) IsTestMode() bool {
	return a.testMode
}

// BeforeClose is called while the window is still alive, before it is destroyed.
// We capture and persist the window position and size to the active view mode slot.
func (a *App) BeforeClose(_ context.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfgLoaded {
		return false // don't overwrite a config we failed to load
	}
	x, y := wailsrt.WindowGetPosition(a.ctx)
	w, h := wailsrt.WindowGetSize(a.ctx)
	ws := &config.WindowState{X: x, Y: y, Width: w, Height: h}
	if a.cfg.Global.ViewMode == "compact" {
		a.cfg.Global.CompactWindow = ws
	} else {
		a.cfg.Global.Window = ws
	}
	_ = config.Save(a.cfg, a.cfgPath)
	return false // don't prevent closing
}

// DomReady is called after the frontend DOM is ready.
// We start hidden, restore saved position, then show the window to prevent flickering.
// If the saved position is off-screen (e.g. monitor disconnected), we center instead.
func (a *App) DomReady(_ context.Context) {
	// Pick the right saved position based on view mode.
	w := a.savedWindowPos
	if a.savedViewMode == "compact" && a.savedCompactPos != nil {
		w = a.savedCompactPos
	}
	if w != nil {
		if a.isPositionOnScreen(w.X, w.Y, w.Width, w.Height) {
			wailsrt.WindowSetPosition(a.ctx, w.X, w.Y)
		} else {
			wailsrt.WindowCenter(a.ctx)
		}
	}
	// Sync detected editors into config before frontend reads it.
	a.SyncEditors()

	// Check for updates in background (throttled to once per 24h).
	a.CheckForUpdate()

	// In compact mode, delay WindowShow — the frontend calls ShowWindow()
	// after fitCompactHeight() to avoid flickering during resize.
	if a.savedViewMode != "compact" {
		wailsrt.WindowShow(a.ctx)
	}
}

// ShowWindow makes the window visible. Called by the frontend after
// compact mode layout is measured and the window is correctly sized.
func (a *App) ShowWindow() {
	wailsrt.WindowShow(a.ctx)
}

// ─── Update ──────────────────────────────────────────────────

// UpdateInfo is the frontend-friendly update check result.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
}

func (a *App) updateOpts() update.Options {
	cacheDir := filepath.Join(config.ConfigRoot(), config.V2ConfigDir)
	return update.Options{
		CurrentVersion: version,
		Repo:           "LuisPalacios/gitbox",
		CacheFile:      filepath.Join(cacheDir, ".update-check"),
		ThrottleDur:    24 * time.Hour,
	}
}

// CheckForUpdate runs a background update check (throttled to 24h).
// Emits "update:available" event if a newer version exists.
func (a *App) CheckForUpdate() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		result, err := update.CheckLatest(ctx, a.updateOpts())
		if err != nil || result == nil {
			return
		}
		if !result.Available {
			return
		}

		info := UpdateInfo{
			Available: true,
			Current:   result.Current,
			Latest:    result.Latest,
		}
		if result.Release != nil {
			info.URL = result.Release.HTMLURL
		}
		wailsrt.EventsEmit(a.ctx, "update:available", info)
	}()
}

// ApplyUpdate downloads and applies the latest release.
// On Windows, if the install directory requires admin privileges (e.g.
// Program Files), it falls back to a UAC-elevated helper process.
func (a *App) ApplyUpdate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	opts := a.updateOpts()
	result, err := update.CheckLatestForce(ctx, opts)
	if err != nil {
		return fmt.Errorf("checking for update: %w", err)
	}
	if !result.Available {
		return fmt.Errorf("no update available")
	}

	wailsrt.EventsEmit(a.ctx, "update:progress", "Downloading...")

	zipPath, err := update.DownloadRelease(ctx, result.Release, opts)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(zipPath))

	wailsrt.EventsEmit(a.ctx, "update:progress", "Applying...")

	// Extract first so we can retry with elevation if the in-place
	// install fails due to permissions (e.g. Program Files on Windows).
	extractDir, installDir, err := update.ExtractUpdate(zipPath)
	if err != nil {
		return fmt.Errorf("extracting: %w", err)
	}

	if err := update.InstallExtracted(extractDir, installDir); err != nil {
		if errors.Is(err, update.ErrNeedElevation) {
			// Don't clean extractDir — the elevated script needs it.
			wailsrt.EventsEmit(a.ctx, "update:progress", "Requesting admin privileges...")
			if elevErr := update.ApplyElevated(extractDir, installDir); elevErr != nil {
				os.RemoveAll(extractDir)
				return fmt.Errorf("elevated update: %w", elevErr)
			}
			// Elevated script will copy files after we exit.
			wailsrt.EventsEmit(a.ctx, "update:quit", result.Latest)
			return nil
		}
		os.RemoveAll(extractDir)
		return fmt.Errorf("applying: %w", err)
	}

	os.RemoveAll(extractDir)
	wailsrt.EventsEmit(a.ctx, "update:done", result.Latest)
	return nil
}

// isPositionOnScreen checks whether the saved window position would be visible
// on any connected monitor. On Windows, uses the virtual desktop rectangle
// (covers all monitors). Falls back to Wails primary-screen check on other platforms.
func (a *App) isPositionOnScreen(x, y, w, h int) bool {
	const margin = 100

	// Try platform-specific virtual desktop bounds (all monitors combined).
	if vx, vy, vw, vh, ok := virtualDesktopBounds(); ok {
		// At least margin pixels of the window must overlap the virtual desktop.
		return x+w > vx+margin && y+h > vy+margin && x < vx+vw-margin && y < vy+vh-margin
	}

	// Fallback: use Wails ScreenGetAll (no position info, only sizes).
	screens, err := wailsrt.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return x >= 0 && y >= 0
	}
	var sw, sh int
	for _, s := range screens {
		if s.IsPrimary {
			sw, sh = s.Size.Width, s.Size.Height
			break
		}
	}
	if sw == 0 || sh == 0 {
		sw, sh = screens[0].Size.Width, screens[0].Size.Height
	}
	return x+w > margin && y+h > margin && x < sw-margin && y < sh-margin
}

// IsPositionOnScreen is the exported binding so the frontend can validate
// a window position before calling WindowSetPosition.
func (a *App) IsPositionOnScreen(x, y, w, h int) bool {
	return a.isPositionOnScreen(x, y, w, h)
}

// GetAppVersion returns the application version string for the frontend.
// CI builds:    "v1.0.6 (abc1234)" — version and commit set via ldflags
// Local builds: "v1.0.5-3-ga99cf17-dev (a99cf17)" — auto-detected from git
func (a *App) GetAppVersion() string {
	v, c := version, commit

	// If not set by ldflags, try to detect from git at runtime.
	if v == "dev" {
		cmd := exec.Command(git.GitBin(), "describe", "--tags", "--always")
		cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
		git.HideWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			if tag := strings.TrimSpace(string(out)); tag != "" {
				v = tag + "-dev"
			}
		}
	}
	if c == "none" {
		cmd := exec.Command(git.GitBin(), "rev-parse", "--short", "HEAD")
		cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
		git.HideWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			if sha := strings.TrimSpace(string(out)); sha != "" {
				c = sha
			}
		}
	}

	if v == "dev" {
		return fmt.Sprintf("dev-%s", c)
	}
	return fmt.Sprintf("%s (%s)", v, c[:minInt(7, len(c))])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── Config ───────────────────────────────────────────────────

// ConfigDTO is the JSON-friendly config sent to the frontend.
type ConfigDTO struct {
	Version  int                        `json:"version"`
	Global   config.GlobalConfig        `json:"global"`
	Accounts map[string]config.Account  `json:"accounts"`
	Sources  map[string]SourceDTO       `json:"sources"`
	Mirrors  map[string]MirrorDTO       `json:"mirrors"`
}

// SourceDTO mirrors config.Source but exposes repos as a map.
type SourceDTO struct {
	Account   string                 `json:"account"`
	Folder    string                 `json:"folder,omitempty"`
	Repos     map[string]config.Repo `json:"repos"`
	RepoOrder []string               `json:"repoOrder"`
}

// MirrorDTO exposes a mirror group to the frontend.
type MirrorDTO struct {
	AccountSrc string                        `json:"account_src"`
	AccountDst string                        `json:"account_dst"`
	Repos      map[string]config.MirrorRepo  `json:"repos"`
	RepoOrder  []string                      `json:"repoOrder"`
}

// MirrorStatusResult is the per-repo live mirror status sent to the frontend.
type MirrorStatusResult struct {
	MirrorKey  string `json:"mirrorKey"`
	RepoKey    string `json:"repoKey"`
	Direction  string `json:"direction"`
	OriginAcct string `json:"originAcct"`
	BackupAcct string `json:"backupAcct"`
	SyncStatus string `json:"syncStatus"`
	HeadCommit string `json:"headCommit"`
	OriginHead string `json:"originHead"`
	BackupHead string `json:"backupHead"`
	Warning    string `json:"warning"`
	Error      string `json:"error"`
}

// MirrorSetupResult is sent to the frontend after a mirror setup attempt.
type MirrorSetupResult struct {
	RepoKey      string `json:"repoKey"`
	Created      bool   `json:"created"`
	Mirrored     bool   `json:"mirrored"`
	Method       string `json:"method"`
	Instructions string `json:"instructions"`
	Error        string `json:"error"`
}

// MirrorCredentialCheck reports whether an account has tokens needed for mirrors.
type MirrorCredentialCheck struct {
	AccountKey     string `json:"accountKey"`
	HasMirrorToken bool   `json:"hasMirrorToken"`
	NeedsPAT       bool   `json:"needsPat"`
	Message        string `json:"message"`
}

// GetConfig returns the full configuration for the frontend.
func (a *App) GetConfig() ConfigDTO {
	a.mu.Lock()
	defer a.mu.Unlock()

	sources := make(map[string]SourceDTO, len(a.cfg.Sources))
	for key, src := range a.cfg.Sources {
		sources[key] = SourceDTO{
			Account:   src.Account,
			Folder:    src.Folder,
			Repos:     src.Repos,
			RepoOrder: src.OrderedRepoKeys(),
		}
	}
	return ConfigDTO{
		Version:  a.cfg.Version,
		Global:   a.cfg.Global,
		Accounts: a.cfg.Accounts,
		Sources:  sources,
		Mirrors:  buildMirrorsDTO(a.cfg),
	}
}

// ReloadConfig re-reads the config from disk. Call after external edits
// or when the window regains focus.
func (a *App) ReloadConfig() (ConfigDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		return ConfigDTO{}, fmt.Errorf("reloading config: %w", err)
	}
	a.cfg = cfg

	sources := make(map[string]SourceDTO, len(a.cfg.Sources))
	for key, src := range a.cfg.Sources {
		sources[key] = SourceDTO{
			Account:   src.Account,
			Folder:    src.Folder,
			Repos:     src.Repos,
			RepoOrder: src.OrderedRepoKeys(),
		}
	}
	return ConfigDTO{
		Version:  a.cfg.Version,
		Global:   a.cfg.Global,
		Accounts: a.cfg.Accounts,
		Sources:  sources,
		Mirrors:  buildMirrorsDTO(a.cfg),
	}, nil
}

func buildMirrorsDTO(cfg *config.Config) map[string]MirrorDTO {
	mirrors := make(map[string]MirrorDTO, len(cfg.Mirrors))
	for key, m := range cfg.Mirrors {
		mirrors[key] = MirrorDTO{
			AccountSrc: m.AccountSrc,
			AccountDst: m.AccountDst,
			Repos:      m.Repos,
			RepoOrder:  m.OrderedRepoKeys(),
		}
	}
	return mirrors
}

// GetConfigPath returns the path to the config file.
func (a *App) GetConfigPath() string {
	return a.cfgPath
}

// GetGlobalFolder returns the expanded clone base folder.
func (a *App) GetGlobalFolder() string {
	return config.ExpandTilde(a.cfg.Global.Folder)
}

// OpenFileInEditor opens a file with the OS default application.
// Do NOT use git.HideWindow here — these launch GUI apps (explorer, open,
// xdg-open). HideWindow sets SW_HIDE in STARTUPINFO, which prevents GUI
// windows from becoming visible.
func (a *App) OpenFileInEditor(path string) error {
	var cmd *exec.Cmd
	switch {
	case isWindows():
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case isDarwin():
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// OpenInExplorer reveals a folder in the OS file manager.
func (a *App) OpenInExplorer(path string) error {
	var cmd *exec.Cmd
	switch {
	case isWindows():
		native := filepath.FromSlash(path)
		cmd = exec.Command("explorer", native)
	case isDarwin():
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// OpenInBrowser opens a URL in the default browser.
// Do NOT use git.HideWindow here — same reason as OpenFileInEditor.
func (a *App) OpenInBrowser(url string) error {
	return git.OpenInBrowser(url)
}

// SweepPreviewDTO holds the read-only preview of stale branches.
type SweepPreviewDTO struct {
	Merged   []string `json:"merged"`
	Gone     []string `json:"gone"`
	Squashed []string `json:"squashed"`
	Error    string   `json:"error,omitempty"`
}

// SweepDeleteDTO holds the result of actually deleting branches.
type SweepDeleteDTO struct {
	Deleted []string `json:"deleted"`
	Error   string   `json:"error,omitempty"`
}

// resolveRepoPath looks up a repo's local path from config. Returns ("", error) if not found.
func (a *App) resolveRepoPath(sourceKey, repoKey string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		return "", fmt.Errorf("source %q not found", sourceKey)
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		return "", fmt.Errorf("repo %q not found", repoKey)
	}
	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
	sourceFolder := src.EffectiveFolder(sourceKey)
	return status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo), nil
}

// PreviewSweep scans for stale branches without deleting anything.
func (a *App) PreviewSweep(sourceKey, repoKey string) SweepPreviewDTO {
	path, err := a.resolveRepoPath(sourceKey, repoKey)
	if err != nil {
		return SweepPreviewDTO{Error: err.Error()}
	}
	result, err := git.SweepBranches(path)
	if err != nil {
		return SweepPreviewDTO{Error: err.Error()}
	}
	return SweepPreviewDTO{Merged: result.Merged, Gone: result.Gone, Squashed: result.Squashed}
}

// ConfirmSweep deletes the stale branches found by PreviewSweep.
func (a *App) ConfirmSweep(sourceKey, repoKey string) SweepDeleteDTO {
	path, err := a.resolveRepoPath(sourceKey, repoKey)
	if err != nil {
		return SweepDeleteDTO{Error: err.Error()}
	}
	result, err := git.SweepBranches(path)
	if err != nil {
		return SweepDeleteDTO{Error: err.Error()}
	}
	deleted, errs := git.DeleteStaleBranches(path, result)
	if len(errs) > 0 {
		return SweepDeleteDTO{Deleted: deleted, Error: errs[0].Error()}
	}
	return SweepDeleteDTO{Deleted: deleted}
}

// ── Orphan repo adoption ──────────────────────────────────────────────────

// OrphanRepoDTO describes an orphan repo for the frontend.
type OrphanRepoDTO struct {
	Path           string `json:"path"`
	RelPath        string `json:"relPath"`
	RemoteURL      string `json:"remoteURL"`
	RepoKey        string `json:"repoKey"`
	MatchedAccount string `json:"matchedAccount"`
	MatchedSource  string `json:"matchedSource"`
	ExpectedPath   string `json:"expectedPath"`
	NeedsRelocate  bool   `json:"needsRelocate"`
	LocalOnly      bool   `json:"localOnly"`
}

// AdoptResultDTO holds the result of adopting orphan repos.
type AdoptResultDTO struct {
	Adopted   int    `json:"adopted"`
	Relocated int    `json:"relocated"`
	Skipped   int    `json:"skipped"`
	Error     string `json:"error,omitempty"`
}

// FindOrphans returns orphan repos under the parent folder.
func (a *App) FindOrphans() []OrphanRepoDTO {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	orphans, err := adopt.FindOrphans(cfg)
	if err != nil {
		return nil
	}

	dtos := make([]OrphanRepoDTO, len(orphans))
	for i, o := range orphans {
		dtos[i] = OrphanRepoDTO{
			Path:           o.Path,
			RelPath:        o.RelPath,
			RemoteURL:      o.RemoteURL,
			RepoKey:        o.RepoKey,
			MatchedAccount: o.MatchedAccount,
			MatchedSource:  o.MatchedSource,
			ExpectedPath:   o.ExpectedPath,
			NeedsRelocate:  o.NeedsRelocate,
			LocalOnly:      o.LocalOnly,
		}
	}
	return dtos
}

// AdoptOrphans adopts the specified orphans by repoKey.
func (a *App) AdoptOrphans(repoKeys []string) AdoptResultDTO {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	// Build lookup of requested keys.
	requested := make(map[string]bool, len(repoKeys))
	for _, k := range repoKeys {
		requested[k] = true
	}

	orphans, err := adopt.FindOrphans(cfg)
	if err != nil {
		return AdoptResultDTO{Error: err.Error()}
	}

	adopted := 0
	relocated := 0
	skipped := 0

	for _, o := range orphans {
		if !requested[o.RepoKey] || o.MatchedAccount == "" || o.MatchedSource == "" || o.LocalOnly {
			continue
		}

		repoPath := o.Path

		// Relocate if needed.
		if o.NeedsRelocate && o.ExpectedPath != "" {
			if _, statErr := os.Stat(o.ExpectedPath); statErr != nil {
				if mkErr := os.MkdirAll(filepath.Dir(o.ExpectedPath), 0o755); mkErr == nil {
					if mvErr := os.Rename(o.Path, o.ExpectedPath); mvErr == nil {
						repoPath = o.ExpectedPath
						relocated++
					}
				}
			}
		}

		// Add to config.
		repo := config.Repo{}
		if repoPath == o.Path && o.NeedsRelocate {
			repo.CloneFolder = repoPath
		}
		if err := cfg.AddRepo(o.MatchedSource, o.RepoKey, repo); err != nil {
			skipped++
			continue
		}

		// Sanitize .git/config.
		acct := cfg.Accounts[o.MatchedAccount]
		credType := repo.EffectiveCredentialType(&acct)
		newURL := adopt.PlainRemoteURL(acct, o.RepoKey, credType)
		_ = git.SetRemoteURL(repoPath, "origin", newURL)
		_ = credential.ConfigureRepoCredential(repoPath, acct, o.MatchedAccount, credType, cfg.Global)
		_ = git.ConfigSet(repoPath, "user.name", acct.Name)
		_ = git.ConfigSet(repoPath, "user.email", acct.Email)

		adopted++
	}

	if adopted > 0 {
		a.mu.Lock()
		cfgPath := filepath.Join(config.ConfigRoot(), config.V2ConfigDir, config.V2ConfigFile)
		saveErr := config.Save(cfg, cfgPath)
		a.mu.Unlock()
		if saveErr != nil {
			return AdoptResultDTO{Adopted: adopted, Relocated: relocated, Skipped: skipped, Error: saveErr.Error()}
		}
	}

	return AdoptResultDTO{Adopted: adopted, Relocated: relocated, Skipped: skipped}
}

// OpenInApp opens a folder in a specific application (e.g. VS Code).
func (a *App) OpenInApp(path string, command string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	cmd := exec.Command(command, path)
	cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	return cmd.Start()
}

// lookPathWithBrewPATH resolves a command using the Homebrew-augmented PATH.
// On macOS, GUI apps inherit a minimal PATH that excludes /opt/homebrew/bin
// and /usr/local/bin, so editors installed via Homebrew (code, cursor, zed)
// are invisible to exec.LookPath. This helper sets the augmented env for the
// lookup, falling back to the standard LookPath on non-macOS platforms.
func lookPathWithBrewPATH(command string) (string, error) {
	env := git.Environ() // no-op on non-macOS
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			origPath := os.Getenv("PATH")
			os.Setenv("PATH", strings.TrimPrefix(e, "PATH="))
			fullPath, err := exec.LookPath(command)
			os.Setenv("PATH", origPath)
			return fullPath, err
		}
	}
	return exec.LookPath(command)
}

// EditorInfo describes an available code editor.
type EditorInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// knownEditors lists editors to auto-detect on PATH.
var knownEditors = []EditorInfo{
	{ID: "vscode", Name: "VS Code", Command: "code"},
	{ID: "cursor", Name: "Cursor", Command: "cursor"},
	{ID: "zed", Name: "Zed", Command: "zed"},
}

// DetectEditors returns code editors available on the system.
// It auto-detects known editors on PATH and merges any user-configured editors.
func (a *App) DetectEditors() []EditorInfo {
	var editors []EditorInfo
	for _, e := range knownEditors {
		if _, err := lookPathWithBrewPATH(e.Command); err == nil {
			editors = append(editors, e)
		}
	}
	// Merge user-configured editors from config.
	if a.cfg != nil {
		for _, e := range a.cfg.Global.Editors {
			editors = append(editors, EditorInfo{
				ID:      e.Command,
				Name:    e.Name,
				Command: e.Command,
			})
		}
	}
	return editors
}

// SyncEditors merges detected editors into the config's global.editors array.
// - Deduplicates existing entries by name (keeps the first occurrence)
// - Appends any detected editor not already present (matched by name)
// - Writes the full resolved path so users see a concrete example
// - Saves config only if changes were made
func (a *App) SyncEditors() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Step 1: deduplicate existing entries by name (keep first occurrence).
	seenName := make(map[string]bool)
	var deduped []config.EditorEntry
	for _, e := range a.cfg.Global.Editors {
		if seenName[e.Name] {
			continue
		}
		seenName[e.Name] = true
		deduped = append(deduped, e)
	}

	changed := len(deduped) != len(a.cfg.Global.Editors)

	// Step 2: append any detected editor not already present.
	for _, known := range knownEditors {
		if seenName[known.Name] {
			continue
		}
		fullPath, err := lookPathWithBrewPATH(known.Command)
		if err != nil {
			continue
		}
		deduped = append(deduped, config.EditorEntry{
			Name:    known.Name,
			Command: fullPath,
		})
		seenName[known.Name] = true
		changed = true
	}

	if changed {
		a.cfg.Global.Editors = deduped
		_ = config.Save(a.cfg, a.cfgPath)
	}
}

func isWindows() bool {
	return os.PathSeparator == '\\' || strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

func isDarwin() bool {
	if strings.Contains(strings.ToLower(os.Getenv("OSTYPE")), "darwin") {
		return true
	}
	cmd := exec.Command("uname")
	git.HideWindow(cmd)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "Darwin"
}

// ─── Global Folder ────────────────────────────────────────────

// IsFirstRun returns true if no global folder is configured (fresh install).
func (a *App) IsFirstRun() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Global.Folder == ""
}

// GetConfigLoadError returns a non-empty string if the config file existed but
// failed to parse. The frontend should show this to the user and ask whether
// to reinitialize. Returns "" if config loaded successfully or didn't exist.
func (a *App) GetConfigLoadError() string {
	return a.cfgLoadError
}

// PickFolder opens the native OS directory picker and returns the selected path.
// Returns empty string if the user cancels.
func (a *App) PickFolder(title string) string {
	dir, err := wailsrt.OpenDirectoryDialog(a.ctx, wailsrt.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return ""
	}
	return dir
}

// SetGlobalFolder validates and sets the global clone folder, then saves config.
// Creates the directory if it doesn't exist.
func (a *App) SetGlobalFolder(folder string) error {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return fmt.Errorf("folder path cannot be empty")
	}

	expanded := config.ExpandTilde(folder)

	// Create the directory if it doesn't exist.
	if err := os.MkdirAll(expanded, 0o755); err != nil {
		return fmt.Errorf("creating folder %s: %w", expanded, err)
	}

	// Verify it's actually a directory.
	info, err := os.Stat(expanded)
	if err != nil {
		return fmt.Errorf("checking folder %s: %w", expanded, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", expanded)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.cfg.Global.Folder = folder
	if err := config.Save(a.cfg, a.cfgPath); err != nil {
		return err
	}
	a.cfgLoaded = true // onboarding complete — safe to save on close
	return nil
}

// GetPeriodicSync returns the current periodic sync interval from config.
func (a *App) GetPeriodicSync() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Global.PeriodicSync == "" {
		return "off"
	}
	return a.cfg.Global.PeriodicSync
}

// SetPeriodicSync saves the periodic sync interval to config.
func (a *App) SetPeriodicSync(interval string) error {
	switch interval {
	case "off", "5m", "15m", "30m":
	default:
		return fmt.Errorf("invalid periodic sync interval: %s", interval)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if interval == "off" {
		a.cfg.Global.PeriodicSync = ""
	} else {
		a.cfg.Global.PeriodicSync = interval
	}
	return config.Save(a.cfg, a.cfgPath)
}

// GetViewMode returns the persisted view mode ("full" or "compact").
func (a *App) GetViewMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Global.ViewMode == "compact" {
		return "compact"
	}
	return "full"
}

// WindowStateDTO is the frontend-friendly window geometry.
type WindowStateDTO struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// SetViewMode saves the view mode and current window position/size to the
// appropriate slot, then switches to the target mode. Returns the target
// mode's previously saved window state (or nil if none was persisted).
func (a *App) SetViewMode(mode string) *WindowStateDTO {
	// Save current window position to the slot we're leaving.
	x, y := wailsrt.WindowGetPosition(a.ctx)
	w, h := wailsrt.WindowGetSize(a.ctx)
	ws := &config.WindowState{X: x, Y: y, Width: w, Height: h}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cfg.Global.ViewMode == "compact" {
		a.cfg.Global.CompactWindow = ws
	} else {
		a.cfg.Global.Window = ws
	}

	if mode == "compact" {
		a.cfg.Global.ViewMode = "compact"
	} else {
		a.cfg.Global.ViewMode = ""
	}
	_ = config.Save(a.cfg, a.cfgPath)

	// Return the target mode's saved window state.
	var target *config.WindowState
	if mode == "compact" {
		target = a.cfg.Global.CompactWindow
	} else {
		target = a.cfg.Global.Window
	}
	if target == nil {
		return nil
	}
	return &WindowStateDTO{X: target.X, Y: target.Y, Width: target.Width, Height: target.Height}
}

// ─── Status ───────────────────────────────────────────────────

// StatusResult is the frontend-friendly repo status.
type StatusResult struct {
	Source    string `json:"source"`
	Repo     string `json:"repo"`
	Account  string `json:"account"`
	Path     string `json:"path"`
	State    string `json:"state"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Modified int    `json:"modified"`
	Untracked int  `json:"untracked"`
	Conflicts int  `json:"conflicts"`
	Error    string `json:"error,omitempty"`
	Branch    string `json:"branch,omitempty"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

func toStatusResult(rs status.RepoStatus) StatusResult {
	return StatusResult{
		Source:    rs.Source,
		Repo:     rs.Repo,
		Account:  rs.Account,
		Path:     rs.Path,
		State:    rs.State.String(),
		Ahead:    rs.Ahead,
		Behind:   rs.Behind,
		Modified: rs.Modified,
		Untracked: rs.Untracked,
		Conflicts: rs.Conflicts,
		Error:    rs.ErrorMsg,
		Branch:   rs.Branch,
		IsDefault: rs.IsDefault,
	}
}

// GetAllStatus returns the sync status of every configured repo.
func (a *App) GetAllStatus() []StatusResult {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	raw := status.CheckAll(cfg)
	results := make([]StatusResult, len(raw))
	for i, rs := range raw {
		results[i] = toStatusResult(rs)
	}
	return results
}

// RefreshStatus checks all repos and emits a "status:updated" event.
func (a *App) RefreshStatus() {
	go func() {
		results := a.GetAllStatus()
		wailsrt.EventsEmit(a.ctx, "status:updated", results)
	}()
}

// ─── Clone ────────────────────────────────────────────────────

// cloneURL builds the clone URL for a repo based on credential type.
func (a *App) cloneURL(acct config.Account, repoKey string, credType string) string {
	switch credType {
	case "ssh":
		host := acct.URL
		if acct.SSH != nil && acct.SSH.Host != "" {
			host = acct.SSH.Host
		} else {
			// Strip scheme for SSH.
			host = stripScheme(acct.URL)
		}
		return fmt.Sprintf("git@%s:%s.git", host, repoKey)
	default:
		// Embed username in HTTPS URL so GCM matches the stored credential
		// (e.g. https://User@github.com/User/repo.git).
		u, err := url.Parse(acct.URL)
		if err == nil && acct.Username != "" {
			u.User = url.User(acct.Username)
			return fmt.Sprintf("%s/%s.git", u.String(), repoKey)
		}
		return fmt.Sprintf("%s/%s.git", acct.URL, repoKey)
	}
}

// CloneRepo clones a single repo. Emits clone:progress and clone:done events.
func (a *App) CloneRepo(sourceKey, repoKey string) {
	go func() {
		a.mu.Lock()
		src, ok := a.cfg.Sources[sourceKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "clone:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("source %q not found", sourceKey),
			})
			return
		}
		repo, ok := src.Repos[repoKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "clone:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("repo %q not found in source %q", repoKey, sourceKey),
			})
			return
		}
		acct := a.cfg.Accounts[src.Account]
		globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
		sourceFolder := src.EffectiveFolder(sourceKey)
		a.mu.Unlock()

		dest := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
		credType := repo.EffectiveCredentialType(&acct)
		accountKey := src.Account
		plainURL := a.cloneURL(acct, repoKey, credType)

		// For token clones, embed the token in the URL so git can authenticate.
		// After cloning, we sanitize back to the plain URL (no secret in config).
		cloneURLStr := plainURL
		if credType == "token" {
			if tok, _, err := credential.ResolveToken(acct, accountKey); err == nil && tok != "" {
				if u, err := url.Parse(plainURL); err == nil {
					u.User = url.UserPassword(acct.Username, tok)
					cloneURLStr = u.String()
				}
			}
		}

		// For token clones, cancel the global credential helper during clone
		// to prevent GCM from storing a ghost credential via "credential approve".
		cloneOpts := git.CloneOpts{Quiet: true}
		if credType == "token" {
			cloneOpts.ConfigArgs = []string{"credential.helper="}
		}

		err := git.CloneWithProgress(cloneURLStr, dest, cloneOpts,
			func(p git.CloneProgress) {
				wailsrt.EventsEmit(a.ctx, "clone:progress", map[string]interface{}{
					"source": sourceKey, "repo": repoKey,
					"phase": p.Phase, "percent": p.Percent,
				})
			})

		result := map[string]interface{}{"source": sourceKey, "repo": repoKey}
		if err != nil {
			result["error"] = err.Error()
		} else {
			// For token clones, sanitize the remote URL to remove the embedded token.
			if credType == "token" {
				_ = git.SetRemoteURL(dest, "origin", plainURL)
			}

			// Set per-repo identity (user.name, user.email).
			wantName, wantEmail := identity.ResolveIdentity(repo, acct)
			identity.EnsureRepoIdentity(dest, wantName, wantEmail)

			// Configure per-repo credential isolation (cancels global helper,
			// sets type-specific helper: store for token, manager for GCM, empty for SSH).
			_ = credential.ConfigureRepoCredential(dest, acct, accountKey, credType, a.cfg.Global)
		}
		wailsrt.EventsEmit(a.ctx, "clone:done", result)
	}()
}

// ─── Pull ─────────────────────────────────────────────────────

// PullRepo pulls a single repo. Emits pull:done event.
func (a *App) PullRepo(sourceKey, repoKey string) {
	go func() {
		a.mu.Lock()
		src, ok := a.cfg.Sources[sourceKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "pull:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("source %q not found", sourceKey),
			})
			return
		}
		repo, ok := src.Repos[repoKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "pull:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("repo %q not found in source %q", repoKey, sourceKey),
			})
			return
		}
		globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
		sourceFolder := src.EffectiveFolder(sourceKey)
		a.mu.Unlock()

		path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
		err := git.PullQuiet(path)

		result := map[string]interface{}{"source": sourceKey, "repo": repoKey}
		if err != nil {
			result["error"] = err.Error()
		}
		wailsrt.EventsEmit(a.ctx, "pull:done", result)
	}()
}

// RepoDetail holds human-readable status information for a single repo.
type RepoDetail struct {
	Branch    string           `json:"branch"`
	Ahead     int              `json:"ahead"`
	Behind    int              `json:"behind"`
	Changed   []git.FileChange `json:"changed"`
	Untracked []string         `json:"untracked"`
	Error     string           `json:"error,omitempty"`
}

// GetRepoDetail returns detailed file-level status for a repo.
func (a *App) GetRepoDetail(sourceKey, repoKey string) RepoDetail {
	a.mu.Lock()
	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		a.mu.Unlock()
		return RepoDetail{Error: fmt.Sprintf("source %q not found", sourceKey)}
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		a.mu.Unlock()
		return RepoDetail{Error: fmt.Sprintf("repo %q not found", repoKey)}
	}
	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
	sourceFolder := src.EffectiveFolder(sourceKey)
	a.mu.Unlock()

	path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
	branch, ahead, behind, changed, untracked, err := git.DetailedStatus(path)
	if err != nil {
		return RepoDetail{Error: err.Error()}
	}
	return RepoDetail{
		Branch:    branch,
		Ahead:     ahead,
		Behind:    behind,
		Changed:   changed,
		Untracked: untracked,
	}
}

// FetchRepo runs git fetch on a single repo and emits fetch:done + refreshes status.
func (a *App) FetchRepo(sourceKey, repoKey string) {
	go func() {
		a.mu.Lock()
		src, ok := a.cfg.Sources[sourceKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "fetch:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("source %q not found", sourceKey),
			})
			return
		}
		repo, ok := src.Repos[repoKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "fetch:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("repo %q not found in source %q", repoKey, sourceKey),
			})
			return
		}
		globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
		sourceFolder := src.EffectiveFolder(sourceKey)
		a.mu.Unlock()

		acct := a.cfg.Accounts[src.Account]
		path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
		err := git.Fetch(path)

		result := map[string]interface{}{"source": sourceKey, "repo": repoKey}
		if err != nil {
			result["error"] = err.Error()
		} else {
			// Verify per-repo identity after fetch.
			wantName, wantEmail := identity.ResolveIdentity(repo, acct)
			identity.EnsureRepoIdentity(path, wantName, wantEmail)
		}
		wailsrt.EventsEmit(a.ctx, "fetch:done", result)

		// Refresh status after fetch so the UI picks up behind/ahead changes.
		a.RefreshStatus()
	}()
}

// FetchAllRepos runs git fetch on every cloned repo and emits fetch:alldone when finished.
func (a *App) FetchAllRepos() {
	go func() {
		a.mu.Lock()
		globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
		type repoRef struct {
			sourceKey, repoKey, path string
			wantName, wantEmail      string
		}
		var repos []repoRef
		for _, srcKey := range a.cfg.OrderedSourceKeys() {
			src := a.cfg.Sources[srcKey]
			acct := a.cfg.Accounts[src.Account]
			sourceFolder := src.EffectiveFolder(srcKey)
			for _, rKey := range src.OrderedRepoKeys() {
				repo := src.Repos[rKey]
				p := status.ResolveRepoPath(globalFolder, sourceFolder, rKey, repo)
				if git.IsRepo(p) {
					wn, we := identity.ResolveIdentity(repo, acct)
					repos = append(repos, repoRef{srcKey, rKey, p, wn, we})
				}
			}
		}
		a.mu.Unlock()

		for _, r := range repos {
			wailsrt.EventsEmit(a.ctx, "fetch:start", map[string]string{
				"source": r.sourceKey, "repo": r.repoKey,
			})
			if err := git.Fetch(r.path); err == nil {
				identity.EnsureRepoIdentity(r.path, r.wantName, r.wantEmail)
			}
			wailsrt.EventsEmit(a.ctx, "fetch:done", map[string]interface{}{
				"source": r.sourceKey, "repo": r.repoKey,
			})
		}

		wailsrt.EventsEmit(a.ctx, "fetch:alldone", nil)
		a.RefreshStatus()
	}()
}

// ─── Account CRUD ─────────────────────────────────────────────

// AddAccountRequest is the frontend payload for creating an account.
type AddAccountRequest struct {
	Key            string `json:"key"`
	Provider       string `json:"provider"`
	URL            string `json:"url"`
	Username       string `json:"username"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	CredentialType string `json:"credentialType"`
}

// AddAccount creates a new account and a matching source, then saves.
// Populates the credential sub-objects (GCM/SSH/Token) based on credential type.
func (a *App) AddAccount(req AddAccountRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acct := config.Account{
		Provider:              req.Provider,
		URL:                   req.URL,
		Username:              req.Username,
		Name:                  req.Name,
		Email:                 req.Email,
		DefaultCredentialType: req.CredentialType,
	}
	if acct.DefaultCredentialType == "" {
		acct.DefaultCredentialType = "gcm"
	}

	// Populate credential sub-objects.
	switch acct.DefaultCredentialType {
	case "gcm":
		acct.GCM = &config.GCMConfig{
			Provider:    inferGCMProvider(acct.Provider),
			UseHTTPPath: false,
		}
	case "ssh":
		hostname := hostnameFromURL(acct.URL)
		acct.SSH = &config.SSHConfig{
			Host:     credential.SSHHostAlias(req.Key),
			Hostname: hostname,
			KeyType:  "ed25519",
		}
	}

	if err := a.cfg.AddAccount(req.Key, acct); err != nil {
		return err
	}

	// Create a matching source so repos can be added immediately.
	src := config.Source{
		Account: req.Key,
		Repos:   make(map[string]config.Repo),
	}
	if err := a.cfg.AddSource(req.Key, src); err != nil {
		_ = a.cfg.DeleteAccount(req.Key)
		return err
	}

	return config.Save(a.cfg, a.cfgPath)
}

// UpdateAccountRequest is the frontend payload for editing an account.
type UpdateAccountRequest struct {
	Key      string `json:"key"`
	Provider string `json:"provider"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// UpdateAccount updates the editable fields of an existing account and saves.
func (a *App) UpdateAccount(req UpdateAccountRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acct, ok := a.cfg.Accounts[req.Key]
	if !ok {
		return fmt.Errorf("account %q not found", req.Key)
	}

	if req.Provider != "" {
		acct.Provider = req.Provider
	}
	acct.URL = req.URL
	acct.Username = req.Username
	acct.Name = req.Name
	acct.Email = req.Email

	if err := a.cfg.UpdateAccount(req.Key, acct); err != nil {
		return err
	}
	if err := config.Save(a.cfg, a.cfgPath); err != nil {
		return err
	}

	// Update user.name and user.email in all cloned repos for this account.
	a.updateCloneIdentity(req.Key)
	return nil
}

// validAccountKey matches lowercase alphanumeric keys with hyphens.
var validAccountKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

// RenameAccount renames an account key and migrates all related artifacts:
// config references, source key + folder, credential file tokens, SSH keys/config.
func (a *App) RenameAccount(oldKey, newKey string) error {
	if newKey == "" {
		return fmt.Errorf("new key cannot be empty")
	}
	if oldKey == newKey {
		return nil
	}
	if !validAccountKey.MatchString(newKey) {
		return fmt.Errorf("invalid key %q: use lowercase letters, numbers, and hyphens", newKey)
	}

	a.mu.Lock()
	acct, ok := a.cfg.Accounts[oldKey]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("account %q not found", oldKey)
	}
	if _, exists := a.cfg.Accounts[newKey]; exists {
		a.mu.Unlock()
		return fmt.Errorf("account %q already exists", newKey)
	}
	cfg := a.cfg
	a.mu.Unlock()

	// ── Credential migration ──

	switch acct.DefaultCredentialType {
	case "token":
		a.migrateKeyringToken(oldKey, newKey)

	case "ssh":
		sshFolder := credential.SSHFolder(cfg)

		oldAlias := credential.SSHHostAlias(oldKey)
		newAlias := credential.SSHHostAlias(newKey)
		oldKeyPath := credential.SSHKeyPath(sshFolder, oldKey)
		newKeyPath := credential.SSHKeyPath(sshFolder, newKey)

		// Rename key files (ignore not-found).
		_ = os.Rename(oldKeyPath, newKeyPath)
		_ = os.Rename(oldKeyPath+".pub", newKeyPath+".pub")

		// Update SSH config: remove old, write new.
		_ = credential.RemoveSSHConfigEntry(sshFolder, oldAlias)
		hostname := hostnameFromURL(acct.URL)
		if acct.SSH != nil && acct.SSH.Hostname != "" {
			hostname = acct.SSH.Hostname
		}
		_ = credential.WriteSSHConfigEntry(sshFolder, credential.SSHConfigEntryOpts{
			Host:     newAlias,
			Hostname: hostname,
			KeyFile:  newKeyPath,
			Username: acct.Username,
			Name:     acct.Name,
			Email:    acct.Email,
			URL:      acct.URL,
		})

		// Update account SSH config.
		if acct.SSH != nil {
			acct.SSH.Host = newAlias
		}

		// Also migrate keyring token (SSH accounts may store a PAT).
		a.migrateKeyringToken(oldKey, newKey)

	case "gcm":
		// GCM credentials are keyed by hostname+username, not account key.
		// Still migrate keyring token in case one was stored.
		a.migrateKeyringToken(oldKey, newKey)
	}

	// ── Source key + folder rename ──

	a.mu.Lock()
	if _, srcExists := cfg.Sources[oldKey]; srcExists {
		if _, conflict := cfg.Sources[newKey]; !conflict {
			// Rename on-disk folder if source uses default folder (source key).
			src := cfg.Sources[oldKey]
			if src.Folder == "" {
				globalFolder := config.ExpandTilde(cfg.Global.Folder)
				oldPath := filepath.Join(globalFolder, oldKey)
				newPath := filepath.Join(globalFolder, newKey)
				_ = os.Rename(oldPath, newPath)
			}
			_ = cfg.RenameSource(oldKey, newKey)
		}
	}

	// ── Config mutation ──

	// Update SSH host in the account before renaming.
	if acct.SSH != nil {
		cfg.Accounts[oldKey] = acct
	}
	if err := cfg.RenameAccount(oldKey, newKey); err != nil {
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()

	return config.Save(cfg, a.cfgPath)
}

// migrateKeyringToken moves a token from oldKey to newKey in the credential file.
func (a *App) migrateKeyringToken(oldKey, newKey string) {
	tok, err := credential.GetToken(oldKey)
	if err != nil || tok == "" {
		return
	}
	if err := credential.StoreToken(newKey, tok); err != nil {
		return
	}
	_ = credential.DeleteToken(oldKey)
}

// ─── Identity ────────────────────────────────────────────────

// CheckGlobalIdentity returns whether ~/.gitconfig has user.name/user.email.
func (a *App) CheckGlobalIdentity() identity.GlobalIdentityStatus {
	return identity.CheckGlobalIdentity()
}

// RemoveGlobalIdentity removes user.name/user.email from ~/.gitconfig.
func (a *App) RemoveGlobalIdentity() error {
	return identity.RemoveGlobalIdentity()
}

// ─── Autostart ───────────────────────────────────────────────

// GetAutostart returns whether the app is set to run at OS login.
func (a *App) GetAutostart() (bool, error) {
	return autostartEnabled()
}

// SetAutostart enables or disables run-at-login for the app.
func (a *App) SetAutostart(enable bool) error {
	return autostartSet(enable)
}

// ─── Credential Setup ─────────────────────────────────────────

// CredentialSetupResult is the outcome of a credential setup step.
type CredentialSetupResult struct {
	OK           bool   `json:"ok"`
	Message      string `json:"message"`
	NeedsPAT     bool   `json:"needsPAT,omitempty"`     // true when SSH works but discovery needs a PAT
	SSHPublicKey string `json:"sshPublicKey,omitempty"`  // public key to copy (SSH setup)
	SSHAddURL    string `json:"sshAddURL,omitempty"`     // provider URL to add key (SSH setup)
	SSHVerified  bool   `json:"sshVerified,omitempty"`   // true when SSH connection test passed
}

// CredentialSetupGCM triggers GCM browser authentication for an account.
func (a *App) CredentialSetupGCM(accountKey string) CredentialSetupResult {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}

	// Ensure global git config has the GCM credential sections BEFORE
	// any fill/approve — git needs to know the credential helper to store it.
	a.mu.Lock()
	credential.EnsureGlobalGCMConfig(a.cfg.Global)
	a.mu.Unlock()

	host := hostnameFromURL(acct.URL)

	// Check if GCM already has a stored credential.
	_, _, err := credential.ResolveGCMToken(acct.URL, acct.Username)
	if err == nil {
		return CredentialSetupResult{OK: true, Message: fmt.Sprintf("GCM credential already stored for %s@%s", acct.Username, host)}
	}

	// Trigger interactive git credential fill — GCM opens the browser.
	// Stderr MUST be connected so GCM can launch its browser auth window.
	input := fmt.Sprintf("protocol=https\nhost=%s\nusername=%s\n\n", host, acct.Username)

	// Run from home dir so repo-local .git/config credential overrides
	// don't interfere with GCM. Home has ~/.gitconfig with per-host settings.
	homeDir, _ := os.UserHomeDir()
	fillCmd := exec.Command(git.GitBin(), "credential", "fill")
	fillCmd.Dir = homeDir
	fillCmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	fillCmd.Stdin = strings.NewReader(input)
	var stderrBuf bytes.Buffer
	fillCmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	git.HideWindow(fillCmd)
	out, err := fillCmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" {
			detail = err.Error()
		}
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("GCM authentication failed for %s@%s: %s", acct.Username, host, detail)}
	}

	// Parse the fill output — extract password and the actual username GCM used.
	gotPassword := false
	realUsername := acct.Username
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "password=") {
			if strings.TrimPrefix(line, "password=") != "" {
				gotPassword = true
			}
		}
		if strings.HasPrefix(line, "username=") {
			if u := strings.TrimPrefix(line, "username="); u != "" {
				realUsername = u
			}
		}
	}
	if !gotPassword {
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("GCM returned no credential for %s@%s", acct.Username, host)}
	}

	// If GCM returned a different username casing (e.g. "Jacopalas" vs "jacopalas"),
	// update the account config so future lookups match.
	if realUsername != acct.Username {
		a.mu.Lock()
		acct.Username = realUsername
		_ = a.cfg.UpdateAccount(accountKey, acct)
		_ = config.Save(a.cfg, a.cfgPath)
		a.mu.Unlock()
	}

	// Approve so git stores it persistently. Best-effort — modern GCM may
	// already persist during fill. We verify storage below.
	approveCmd := exec.Command(git.GitBin(), "credential", "approve")
	approveCmd.Dir = homeDir
	approveCmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	approveCmd.Stdin = strings.NewReader(string(out))
	git.HideWindow(approveCmd)
	_ = approveCmd.Run()

	// Verify the credential was actually stored.
	if _, _, err := credential.ResolveGCMToken(acct.URL, realUsername); err != nil {
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("GCM authentication completed but credential was not stored for %s@%s. Try running: git credential approve manually.", realUsername, host)}
	}

	// Reconfigure existing clones to use the current credential type.
	a.mu.Lock()
	n := a.reconfigureClones(accountKey)
	a.mu.Unlock()
	msg := fmt.Sprintf("GCM credential stored for %s@%s", realUsername, host)
	if n > 0 {
		msg += fmt.Sprintf("\n%d clone(s) reconfigured", n)
	}
	return CredentialSetupResult{OK: true, Message: msg}
}

// CredentialStoreToken stores a PAT in the credential file for an account.
func (a *App) CredentialStoreToken(accountKey, token string) CredentialSetupResult {
	a.mu.Lock()
	_, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}
	if strings.TrimSpace(token) == "" {
		return CredentialSetupResult{OK: false, Message: "Token cannot be empty"}
	}

	if err := credential.StoreToken(accountKey, strings.TrimSpace(token)); err != nil {
		return CredentialSetupResult{OK: false, Message: err.Error()}
	}

	// Reconfigure existing clones to use the current credential type.
	a.mu.Lock()
	n := a.reconfigureClones(accountKey)
	a.mu.Unlock()
	msg := "Token stored in credential file"
	if n > 0 {
		msg += fmt.Sprintf("\n%d clone(s) reconfigured", n)
	}
	return CredentialSetupResult{OK: true, Message: msg}
}

// TokenGuideInfo returns the PAT creation URL and guidance for an account.
type TokenGuideInfo struct {
	CreationURL string `json:"creationURL"`
	Scopes      string `json:"scopes"`
	Guide       string `json:"guide"`
}

// GetTokenGuide returns provider-specific PAT creation info.
func (a *App) GetTokenGuide(accountKey string) TokenGuideInfo {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return TokenGuideInfo{}
	}
	scopes := provider.TokenRequiredScopes(acct.Provider)
	if acct.DefaultCredentialType == "ssh" {
		scopes = provider.DiscoveryRequiredScopes(acct.Provider)
	}
	return TokenGuideInfo{
		CreationURL: provider.TokenCreationURL(acct.Provider, acct.URL),
		Scopes:      scopes,
		Guide:       provider.TokenSetupGuide(acct.Provider, acct.URL, accountKey),
	}
}

// CredentialSetupSSH generates an SSH key and configures ~/.ssh/config.
func (a *App) CredentialSetupSSH(accountKey string) CredentialSetupResult {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	cfg := a.cfg
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}

	hostAlias := credential.SSHHostAlias(accountKey)
	keyType := "ed25519"
	if acct.SSH != nil && acct.SSH.KeyType != "" {
		keyType = acct.SSH.KeyType
	}
	hostname := hostnameFromURL(acct.URL)
	if acct.SSH != nil && acct.SSH.Hostname != "" {
		hostname = acct.SSH.Hostname
	}

	sshFolder := credential.SSHFolder(cfg)

	// Ensure ~/.ssh exists.
	if err := os.MkdirAll(sshFolder, 0o700); err != nil {
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("Creating %s: %v", sshFolder, err)}
	}

	// Write ~/.ssh/config entry if missing.
	found, _ := credential.FindSSHConfigEntry(sshFolder, hostAlias)
	if !found {
		opts := credential.SSHConfigEntryOpts{
			Host:     hostAlias,
			Hostname: hostname,
			KeyFile:  credential.SSHKeyPath(sshFolder, accountKey),
			Username: acct.Username,
			Name:     acct.Name,
			Email:    acct.Email,
			URL:      acct.URL,
		}
		if err := credential.WriteSSHConfigEntry(sshFolder, opts); err != nil {
			return CredentialSetupResult{OK: false, Message: fmt.Sprintf("Writing SSH config: %v", err)}
		}
	}

	// Generate SSH key if missing.
	keyPath := credential.SSHKeyPath(sshFolder, accountKey)
	if _, err := os.Stat(keyPath); err != nil {
		if _, err := credential.GenerateSSHKey(sshFolder, accountKey, keyType); err != nil {
			return CredentialSetupResult{OK: false, Message: fmt.Sprintf("Generating SSH key: %v", err)}
		}
	}

	// Update account SSH config in gitbox.json.
	a.mu.Lock()
	if acct.SSH == nil || acct.SSH.Host != hostAlias {
		if acct.SSH == nil {
			acct.SSH = &config.SSHConfig{}
		}
		acct.SSH.Host = hostAlias
		acct.SSH.Hostname = hostname
		acct.SSH.KeyType = keyType
		_ = a.cfg.UpdateAccount(accountKey, acct)
		_ = config.Save(a.cfg, a.cfgPath)
	}
	a.mu.Unlock()

	// Read public key for display.
	pubKey, _ := credential.ReadPublicKey(keyPath)
	addURL := credential.SSHPublicKeyURL(acct.Provider, acct.URL)

	// Test SSH connection.
	sshVerified := false
	if _, sshErr := credential.TestSSHConnection(sshFolder, hostAlias); sshErr == nil {
		sshVerified = true
	}

	// Reconfigure existing clones to use the current credential type.
	a.mu.Lock()
	n := a.reconfigureClones(accountKey)
	a.mu.Unlock()

	keyName := fmt.Sprintf("gitbox-%s-sshkey", accountKey)
	msg := fmt.Sprintf("SSH Host %s and %s available.", hostAlias, keyName)
	if sshVerified {
		msg = fmt.Sprintf("SSH Host %s and %s available.", hostAlias, keyName)
	}
	if n > 0 {
		msg += fmt.Sprintf("\n%d clone(s) reconfigured.", n)
	}

	// Check whether a PAT exists for API discovery.
	_, _, apiErr := credential.ResolveToken(acct, accountKey)
	needsPAT := apiErr != nil

	return CredentialSetupResult{
		OK:           true,
		Message:      msg,
		NeedsPAT:     needsPAT,
		SSHPublicKey: pubKey,
		SSHAddURL:    addURL,
		SSHVerified:  sshVerified,
	}
}

// CredentialRegenerateSSH deletes the existing SSH key and config entry,
// then runs CredentialSetupSSH to create fresh ones.
func (a *App) CredentialRegenerateSSH(accountKey string) CredentialSetupResult {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	cfg := a.cfg
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}

	sshFolder := credential.SSHFolder(cfg)

	hostAlias := credential.SSHHostAlias(accountKey)
	keyPath := credential.SSHKeyPath(sshFolder, accountKey)

	// Remove old key files.
	_ = os.Remove(keyPath)
	_ = os.Remove(keyPath + ".pub")

	// Remove old ssh/config entry so it gets re-created.
	_ = credential.RemoveSSHConfigEntry(sshFolder, hostAlias)

	// Also remove from provider reminder.
	_ = acct

	// Re-run setup from scratch — creates config entry + key + tests connection.
	return a.CredentialSetupSSH(accountKey)
}

// inferGCMProvider maps a gitbox provider name to the GCM provider hint.
func inferGCMProvider(prov string) string {
	switch prov {
	case "github":
		return "github"
	case "gitlab":
		return "gitlab"
	case "bitbucket":
		return "bitbucket"
	default:
		return "generic"
	}
}

// hostnameFromURL extracts the hostname from a URL.
func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		for _, prefix := range []string{"https://", "http://"} {
			if strings.HasPrefix(rawURL, prefix) {
				return strings.TrimPrefix(rawURL, prefix)
			}
		}
		return rawURL
	}
	return u.Hostname()
}


// ─── Delete ───────────────────────────────────────────────────

// DeleteRepo removes a repo from the config and deletes its local folder.
// Returns an error message or empty string on success.
func (a *App) DeleteRepo(sourceKey, repoKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		return fmt.Errorf("source %q not found", sourceKey)
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		return fmt.Errorf("repo %q not found in source %q", repoKey, sourceKey)
	}

	// Resolve the local path and remove the folder if it exists.
	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
	sourceFolder := src.EffectiveFolder(sourceKey)
	path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)

	if git.IsRepo(path) {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("deleting folder %s: %w", path, err)
		}
	}

	// Remove from config and save.
	if err := a.cfg.DeleteRepo(sourceKey, repoKey); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// DeleteAccount removes an account, all its sources, and all local clone folders.
func (a *App) DeleteAccount(accountKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.cfg.Accounts[accountKey]; !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)

	// Delete all clone folders and source directories for this account.
	for sourceKey, src := range a.cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if git.IsRepo(path) {
				_ = os.RemoveAll(path)
			}
		}
		// Remove the entire source directory (e.g. ~/00.git/github-Jacopalas/).
		sourcePath := filepath.Join(globalFolder, sourceFolder)
		_ = os.RemoveAll(sourcePath)
		delete(a.cfg.Sources, sourceKey)
	}

	// Delete the account.
	delete(a.cfg.Accounts, accountKey)

	return config.Save(a.cfg, a.cfgPath)
}

// reconfigureClones updates the remote URL and credential config of every
// cloned repo belonging to the given account so they match the current
// credential type. Returns the number of repos updated.
// Caller MUST hold a.mu.
func (a *App) reconfigureClones(accountKey string) int {
	acct, ok := a.cfg.Accounts[accountKey]
	if !ok {
		return 0
	}
	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
	count := 0
	for sourceKey, src := range a.cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if !git.IsRepo(path) {
				continue
			}
			credType := repo.EffectiveCredentialType(&acct)
			newURL := a.cloneURL(acct, repoKey, credType)
			_ = git.SetRemoteURL(path, "origin", newURL)

			// Configure per-repo credential isolation.
			_ = credential.ConfigureRepoCredential(path, acct, accountKey, credType, a.cfg.Global)
			count++
		}
	}
	return count
}

// updateCloneIdentity sets user.name and user.email in all cloned repos
// belonging to the given account. Caller MUST hold a.mu.
func (a *App) updateCloneIdentity(accountKey string) {
	acct, ok := a.cfg.Accounts[accountKey]
	if !ok {
		return
	}
	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
	for sourceKey, src := range a.cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if !git.IsRepo(path) {
				continue
			}
			name := repo.Name
			if name == "" {
				name = acct.Name
			}
			email := repo.Email
			if email == "" {
				email = acct.Email
			}
			_ = git.ConfigSet(path, "user.name", name)
			_ = git.ConfigSet(path, "user.email", email)
		}
	}
}

// countClonedRepos returns the number of cloned repos for the given account.
// Caller MUST hold a.mu.
func (a *App) countClonedRepos(accountKey string) int {
	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
	count := 0
	for sourceKey, src := range a.cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if git.IsRepo(path) {
				count++
			}
		}
	}
	return count
}

// ChangeCredentialType changes an account's credential type and populates the
// appropriate sub-object. Returns the updated account key for re-rendering.
// removeCredentialArtifacts cleans up the OS-level artifacts (keyring entries,
// SSH keys, GCM cached credentials) for the account's current credential type.
// Does NOT modify the config — caller handles that.
// Caller must NOT hold a.mu (this function does its own locking for reads).
func (a *App) removeCredentialArtifacts(accountKey string) []string {
	a.mu.Lock()
	acct := a.cfg.Accounts[accountKey]
	cfg := a.cfg
	a.mu.Unlock()

	var msgs []string
	switch acct.DefaultCredentialType {
	case "token":
		if err := credential.DeleteToken(accountKey); err == nil {
			msgs = append(msgs, "Token removed from credential file")
		}
		if err := credential.RemoveCredentialFile(accountKey); err == nil {
			msgs = append(msgs, "Credential store file removed")
		}
	case "gcm":
		host := hostnameFromURL(acct.URL)
		input := fmt.Sprintf("protocol=https\nhost=%s\nusername=%s\n", host, acct.Username)
		homeDir, _ := os.UserHomeDir()
		cmd := exec.Command(git.GitBin(), "credential", "reject")
		cmd.Dir = homeDir // Avoid repo-local .git/config credential overrides.
		cmd.Env = git.Environ()
		git.HideWindow(cmd)
		cmd.Stdin = strings.NewReader(input)
		if err := cmd.Run(); err == nil {
			msgs = append(msgs, fmt.Sprintf("GCM credential removed for %s@%s", acct.Username, host))
		}
	case "ssh":
		sshFolder := credential.SSHFolder(cfg)
		hostAlias := credential.SSHHostAlias(accountKey)
		keyPath := credential.SSHKeyPath(sshFolder, accountKey)
		if err := os.Remove(keyPath); err == nil {
			msgs = append(msgs, fmt.Sprintf("Removed SSH key: %s", keyPath))
		}
		if err := os.Remove(keyPath + ".pub"); err == nil {
			msgs = append(msgs, fmt.Sprintf("Removed SSH public key: %s.pub", keyPath))
		}
		if err := credential.RemoveSSHConfigEntry(sshFolder, hostAlias); err == nil {
			msgs = append(msgs, fmt.Sprintf("Removed Host %s from ~/.ssh/config", hostAlias))
		}
		if err := credential.DeleteToken(accountKey); err == nil {
			msgs = append(msgs, "Removed discovery PAT from credential file")
		}
	}
	return msgs
}

func (a *App) ChangeCredentialType(accountKey, newType string) error {
	// Clean up old credential artifacts before switching.
	a.removeCredentialArtifacts(accountKey)

	a.mu.Lock()
	defer a.mu.Unlock()

	acct, ok := a.cfg.Accounts[accountKey]
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	acct.DefaultCredentialType = newType

	// Clear old credential sub-objects and populate the new one.
	acct.GCM = nil
	acct.SSH = nil

	switch newType {
	case "gcm":
		acct.GCM = &config.GCMConfig{
			Provider:    inferGCMProvider(acct.Provider),
			UseHTTPPath: false,
		}
	case "ssh":
		acct.SSH = &config.SSHConfig{
			Host:     credential.SSHHostAlias(accountKey),
			Hostname: hostnameFromURL(acct.URL),
			KeyType:  "ed25519",
		}
	}

	if err := a.cfg.UpdateAccount(accountKey, acct); err != nil {
		return err
	}
	if err := config.Save(a.cfg, a.cfgPath); err != nil {
		return err
	}

	// Update all existing clones to match the new credential type.
	a.reconfigureClones(accountKey)
	return nil
}

// CredentialDelete removes the credential artifacts for an account and clears
// its credential configuration so it can be set up from scratch.
func (a *App) CredentialDelete(accountKey string) CredentialSetupResult {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}

	if acct.DefaultCredentialType == "" {
		return CredentialSetupResult{OK: true, Message: "No credential configured"}
	}

	msgs := a.removeCredentialArtifacts(accountKey)

	// For SSH, remind the user to remove the public key from the provider.
	if acct.DefaultCredentialType == "ssh" {
		msgs = append(msgs, fmt.Sprintf("Remember to remove the SSH public key from your provider:\n  %s", credential.SSHPublicKeyURL(acct.Provider, acct.URL)))
	}

	// Clear credential config.
	a.mu.Lock()
	nClones := a.countClonedRepos(accountKey)
	acct.DefaultCredentialType = ""
	acct.GCM = nil
	acct.SSH = nil
	if err := a.cfg.UpdateAccount(accountKey, acct); err != nil {
		a.mu.Unlock()
		return CredentialSetupResult{OK: false, Message: err.Error()}
	}
	a.mu.Unlock()

	if err := config.Save(a.cfg, a.cfgPath); err != nil {
		return CredentialSetupResult{OK: false, Message: err.Error()}
	}

	if nClones > 0 {
		msgs = append(msgs, fmt.Sprintf("%d clone(s) will be reconfigured when a new credential is set up", nClones))
	}

	return CredentialSetupResult{OK: true, Message: strings.Join(msgs, "\n")}
}

// ─── Discovery ────────────────────────────────────────────────

// DiscoverResult is a repo found during discovery.
type DiscoverResult struct {
	FullName    string `json:"fullName"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	Fork        bool   `json:"fork"`
	Archived    bool   `json:"archived"`
}

// Discover lists remote repos for an account. Emits discover:done event.
func (a *App) Discover(accountKey string) {
	go func() {
		a.mu.Lock()
		acct, ok := a.cfg.Accounts[accountKey]
		a.mu.Unlock()

		if !ok {
			wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
				"accountKey": accountKey,
				"error":      fmt.Sprintf("account %q not found", accountKey),
			})
			return
		}

		token, _, err := credential.ResolveAPIToken(acct, accountKey)
		if err != nil {
			wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
				"accountKey": accountKey,
				"error":      fmt.Sprintf("no API token available: %v", err),
			})
			return
		}

		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
				"accountKey": accountKey,
				"error":      err.Error(),
			})
			return
		}

		repos, err := prov.ListRepos(context.Background(), acct.URL, token, acct.Username)
		if err != nil {
			wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
				"accountKey": accountKey,
				"error":      err.Error(),
			})
			return
		}

		results := make([]DiscoverResult, len(repos))
		for i, r := range repos {
			results[i] = DiscoverResult{
				FullName:    r.FullName,
				Description: r.Description,
				Private:     r.Private,
				Fork:        r.Fork,
				Archived:    r.Archived,
			}
		}

		wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
			"accountKey": accountKey,
			"repos":      results,
		})
	}()
}

// AddDiscoveredRepos adds discovered repos to the config and saves.
// The key can be either a source key or an account key — if it's an account
// key, the first source referencing that account is used.
func (a *App) AddDiscoveredRepos(key string, repoNames []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	sourceKey := key
	if _, ok := a.cfg.Sources[sourceKey]; !ok {
		// Not a direct source key — find source by account key.
		found := false
		for sk, src := range a.cfg.Sources {
			if src.Account == key {
				sourceKey = sk
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no source found for key %q", key)
		}
	}

	for _, name := range repoNames {
		if err := a.cfg.AddRepo(sourceKey, name, config.Repo{}); err != nil {
			return err
		}
	}
	return config.Save(a.cfg, a.cfgPath)
}

// ─── Credential Verification ──────────────────────────────────

// CredentialStatus is the result of verifying an account's credentials.
type CredentialStatus struct {
	Status     string `json:"status"`     // overall: "ok", "warning", "error", "none"
	Message    string `json:"message"`
	Primary    string `json:"primary"`    // primary credential status
	PrimaryMsg string `json:"primaryMsg"`
	PAT        string `json:"pat"`        // companion PAT status (for SSH/GCM)
	PATMsg     string `json:"patMsg"`
}

// CredentialVerify checks if credentials are working for an account.
// Uses the shared credential.Check logic from pkg/credential/status.go.
func (a *App) CredentialVerify(accountKey string) CredentialStatus {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	cfg := a.cfg
	a.mu.Unlock()

	if !ok {
		return CredentialStatus{Status: "error", Message: "account not found"}
	}

	result := credential.Check(acct, accountKey, cfg)
	return CredentialStatus{
		Status:     result.Overall.String(),
		Message:    result.PrimaryDetail,
		Primary:    result.Primary.String(),
		PrimaryMsg: result.PrimaryDetail,
		PAT:        result.PAT.String(),
		PATMsg:     result.PATDetail,
	}
}

// ─── Helpers ──────────────────────────────────────────────────

func stripScheme(rawURL string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if len(rawURL) > len(prefix) && rawURL[:len(prefix)] == prefix {
			return rawURL[len(prefix):]
		}
	}
	return rawURL
}

// ─── Mirror methods ──────────────────────────────────────────

// GetMirrorStatus checks live sync status for all repos in a mirror group.
// Runs async; emits "mirror:status" event with []MirrorStatusResult.
func (a *App) GetMirrorStatus(mirrorKey string) {
	go func() {
		a.mu.Lock()
		cfg := a.cfg
		a.mu.Unlock()

		m, ok := cfg.Mirrors[mirrorKey]
		if !ok {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var results []MirrorStatusResult
		for repoKey := range m.Repos {
			r := mirror.CheckStatus(ctx, cfg, mirrorKey, repoKey)
			results = append(results, MirrorStatusResult{
				MirrorKey:  mirrorKey,
				RepoKey:    r.RepoKey,
				Direction:  r.Direction,
				OriginAcct: r.OriginAcct,
				BackupAcct: r.BackupAcct,
				SyncStatus: r.SyncStatus,
				HeadCommit: r.HeadCommit,
				OriginHead: r.OriginHead,
				BackupHead: r.BackupHead,
				Warning:    r.Warning,
				Error:      r.Error,
			})
		}
		wailsrt.EventsEmit(a.ctx, "mirror:status", results)
	}()
}

// SetupMirrorRepo runs API setup for a single mirror repo.
// Runs async; emits "mirror:setup:done" event with MirrorSetupResult.
func (a *App) SetupMirrorRepo(mirrorKey, repoKey string) {
	go func() {
		a.mu.Lock()
		cfg := a.cfg
		a.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		r := mirror.SetupMirror(ctx, cfg, mirrorKey, repoKey)
		result := MirrorSetupResult{
			RepoKey:      r.RepoKey,
			Created:      r.Created,
			Mirrored:     r.Mirrored,
			Method:       r.Method,
			Instructions: r.Instructions,
			Error:        r.Error,
		}

		_ = r.Error // result is emitted via event below

		wailsrt.EventsEmit(a.ctx, "mirror:setup:done", result)
	}()
}

// AddMirrorGroup creates a new mirror group pairing two accounts.
func (a *App) AddMirrorGroup(key, accountSrc, accountDst string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	m := config.Mirror{
		AccountSrc: accountSrc,
		AccountDst: accountDst,
	}
	if err := a.cfg.AddMirror(key, m); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// DeleteMirrorGroup removes a mirror group and all its repos.
func (a *App) DeleteMirrorGroup(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.DeleteMirror(key); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// AddMirrorRepo adds a repo to a mirror group.
func (a *App) AddMirrorRepo(mirrorKey, repoKey, direction, origin string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	repo := config.MirrorRepo{
		Direction: direction,
		Origin:    origin,
	}
	if err := a.cfg.AddMirrorRepo(mirrorKey, repoKey, repo); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// DeleteMirrorRepo removes a repo from a mirror group.
func (a *App) DeleteMirrorRepo(mirrorKey, repoKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.DeleteMirrorRepo(mirrorKey, repoKey); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// ListRemoteRepos returns the repo list for an account, for use in mirror repo pickers.
func (a *App) ListRemoteRepos(accountKey string) []DiscoverResult {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()
	if !ok {
		return nil
	}

	token, _, err := credential.ResolveAPIToken(acct, accountKey)
	if err != nil {
		return nil
	}

	prov, err := provider.ByName(acct.Provider)
	if err != nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repos, err := prov.ListRepos(ctx, acct.URL, token, acct.Username)
	if err != nil {
		return nil
	}

	results := make([]DiscoverResult, len(repos))
	for i, r := range repos {
		results[i] = DiscoverResult{
			FullName:    r.FullName,
			Description: r.Description,
			Private:     r.Private,
			Fork:        r.Fork,
			Archived:    r.Archived,
		}
	}
	return results
}

// ListAccountOrgs returns the organizations/namespaces available for an account.
// The first entry is always the personal username.
func (a *App) ListAccountOrgs(accountKey string) ([]string, error) {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("account %q not found", accountKey)
	}

	token, _, err := credential.ResolveAPIToken(acct, accountKey)
	if err != nil {
		return nil, fmt.Errorf("resolving credentials: %w", err)
	}

	prov, err := provider.ByName(acct.Provider)
	if err != nil {
		return nil, err
	}

	// Start with the personal username.
	result := []string{acct.Username}

	// Try to list orgs if the provider supports it.
	if ol, ok := prov.(provider.OrgLister); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		orgs, err := ol.ListUserOrgs(ctx, acct.URL, token, acct.Username)
		if err == nil {
			result = append(result, orgs...)
		}
	}

	return result, nil
}

// CreateNewRepo creates a repository on the provider and optionally adds it
// to the config and clones it locally.
func (a *App) CreateNewRepo(accountKey, owner, repoName, description string, private, cloneAfter bool) error {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	token, _, err := credential.ResolveAPIToken(acct, accountKey)
	if err != nil {
		return fmt.Errorf("resolving credentials: %w", err)
	}

	prov, err := provider.ByName(acct.Provider)
	if err != nil {
		return err
	}

	rc, ok := prov.(provider.RepoCreator)
	if !ok {
		return fmt.Errorf("provider %q does not support repo creation", acct.Provider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// If owner matches the username, create under personal namespace (empty owner).
	apiOwner := owner
	if apiOwner == acct.Username {
		apiOwner = ""
	}

	if err := rc.CreateRepo(ctx, acct.URL, token, acct.Username, apiOwner, repoName, description, private); err != nil {
		return err
	}

	if cloneAfter {
		// Build the full repo key: owner/name.
		repoKey := owner + "/" + repoName

		// Add to config as a source and save.
		a.mu.Lock()
		sourceKey := accountKey
		src, srcOK := a.cfg.Sources[sourceKey]
		if !srcOK {
			src = config.Source{
				Account: accountKey,
				Repos:   make(map[string]config.Repo),
			}
		}
		src.Repos[repoKey] = config.Repo{}
		a.cfg.Sources[sourceKey] = src
		saveErr := config.Save(a.cfg, a.cfgPath)
		a.mu.Unlock()
		if saveErr != nil {
			return fmt.Errorf("saving config: %w", saveErr)
		}

		// Trigger clone (reuses existing async clone flow).
		a.CloneRepo(sourceKey, repoKey)
	}

	return nil
}

// DiscoverMirrors scans all account pairs to detect mirror relationships.
// Runs async; emits "mirror:discover:done" event with results.
func (a *App) DiscoverMirrors() {
	go func() {
		a.mu.Lock()
		cfg := a.cfg
		a.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		results, err := mirror.DiscoverMirrors(ctx, cfg, func(p mirror.DiscoverProgress) {
			wailsrt.EventsEmit(a.ctx, "mirror:discover:progress", map[string]interface{}{
				"phase":   p.Phase,
				"account": p.Account,
				"current": p.Current,
				"total":   p.Total,
			})
		})
		if err != nil {
			wailsrt.EventsEmit(a.ctx, "mirror:discover:done", map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		wailsrt.EventsEmit(a.ctx, "mirror:discover:done", map[string]interface{}{
			"results": results,
		})
	}()
}

// ApplyDiscoveredMirrors merges discovered mirrors into the config.
func (a *App) ApplyDiscoveredMirrors() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	results, err := mirror.DiscoverMirrors(ctx, a.cfg, nil)
	if err != nil {
		return err
	}
	added, _ := mirror.ApplyDiscovery(a.cfg, results)
	if added > 0 {
		return config.Save(a.cfg, a.cfgPath)
	}
	return nil
}

// CheckMirrorCredentials checks whether an account has the portable PAT
// needed for mirror operations (remote servers can't use GCM OAuth tokens).
func (a *App) CheckMirrorCredentials(accountKey string) MirrorCredentialCheck {
	a.mu.Lock()
	acct, ok := a.cfg.GetAccountByKey(accountKey)
	a.mu.Unlock()

	if !ok {
		return MirrorCredentialCheck{AccountKey: accountKey, Message: "account not found"}
	}

	_, _, err := credential.ResolveMirrorToken(acct, accountKey)
	if err != nil {
		needsPAT := acct.DefaultCredentialType == "gcm"
		msg := "No portable PAT found."
		if needsPAT {
			msg = "GCM accounts need a separate PAT for mirrors. Store one with the token setup."
		}
		return MirrorCredentialCheck{
			AccountKey:     accountKey,
			HasMirrorToken: false,
			NeedsPAT:       needsPAT,
			Message:        msg,
		}
	}
	return MirrorCredentialCheck{
		AccountKey:     accountKey,
		HasMirrorToken: true,
		Message:        "Mirror token available.",
	}
}
