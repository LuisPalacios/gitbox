package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"time"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/doctor"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/harness"
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

// saveConfig persists the in-memory config to disk. It refuses to save when
// the config was not successfully loaded — the guard prevents the empty
// default fabricated in Startup() from being written over a real file we
// failed to parse, which would destroy the user's data (see issue #60).
//
// Caller must hold a.mu.
func (a *App) saveConfig() error {
	if !a.cfgLoaded {
		if a.cfgLoadError != "" {
			return fmt.Errorf("refusing to save: config not loaded (%s)", a.cfgLoadError)
		}
		return fmt.Errorf("refusing to save: config not loaded")
	}
	return config.Save(a.cfg, a.cfgPath)
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
	_ = a.saveConfig()
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
	// Sync detected editors, terminals, and AI harnesses into config before frontend reads it.
	a.SyncEditors()
	a.SyncTerminals()
	a.SyncAIHarnesses()

	// Discover and auto-adopt workspace artifacts dropped on disk outside
	// gitbox. Runs off the UI thread so startup paints immediately; the
	// frontend listens for the `workspaces:discovered` event to refresh.
	go func() {
		if _, err := a.DiscoverWorkspaces(); err != nil {
			fmt.Fprintf(os.Stderr, "workspace discovery: %v\n", err)
		}
	}()

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
		CurrentVersion: update.ResolveVersion(version),
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
	Version        int                         `json:"version"`
	Global         config.GlobalConfig         `json:"global"`
	Accounts       map[string]config.Account   `json:"accounts"`
	Sources        map[string]SourceDTO        `json:"sources"`
	Mirrors        map[string]MirrorDTO        `json:"mirrors"`
	Workspaces     map[string]WorkspaceDTO     `json:"workspaces"`
	WorkspaceOrder []string                    `json:"workspaceOrder"`
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
		Version:        a.cfg.Version,
		Global:         a.cfg.Global,
		Accounts:       a.cfg.Accounts,
		Sources:        sources,
		Mirrors:        buildMirrorsDTO(a.cfg),
		Workspaces:     buildWorkspacesDTO(a.cfg),
		WorkspaceOrder: a.cfg.OrderedWorkspaceKeys(),
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
		Version:        a.cfg.Version,
		Global:         a.cfg.Global,
		Accounts:       a.cfg.Accounts,
		Sources:        sources,
		Mirrors:        buildMirrorsDTO(a.cfg),
		Workspaces:     buildWorkspacesDTO(a.cfg),
		WorkspaceOrder: a.cfg.OrderedWorkspaceKeys(),
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
// On Windows we route through `cmd /c start`; HideWindow hides the cmd.exe
// host without affecting the grandchild GUI (start → ShellExecute spawns
// the target with its own STARTUPINFO, so SW_HIDE does not cascade).
func (a *App) OpenFileInEditor(path string) error {
	var cmd *exec.Cmd
	switch {
	case isWindows():
		cmd = exec.Command("cmd", "/c", "start", "", path)
		git.HideWindow(cmd)
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

// resolveAccountFolder returns the absolute path to the account's parent
// folder: <global.folder>/<accountKey>. Returns an error if the account key
// is unknown or the folder does not exist on disk.
func (a *App) resolveAccountFolder(accountKey string) (string, error) {
	a.mu.Lock()
	_, ok := a.cfg.Accounts[accountKey]
	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
	a.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("account %q not found", accountKey)
	}
	path := filepath.Join(globalFolder, accountKey)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("account folder does not exist: %s", path)
		}
		return "", fmt.Errorf("account folder %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("account folder is not a directory: %s", path)
	}
	return path, nil
}

// OpenAccountFolder reveals the account's parent folder in the OS file
// manager. Errors if the folder does not exist yet (no repos cloned).
func (a *App) OpenAccountFolder(accountKey string) error {
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
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

// OpenAccountInApp opens the account's parent folder in a specific editor.
func (a *App) OpenAccountInApp(accountKey string, command string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
	cmd := exec.Command(command, path)
	cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	git.HideWindow(cmd)
	return cmd.Start()
}

// OpenAccountInTerminal launches a terminal emulator in the account's parent
// folder. Delegates to openTerminalAt so the Windows cmd.exe wrapper lives
// in one place.
func (a *App) OpenAccountInTerminal(accountKey string, command string, args []string) error {
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
	return openTerminalAt(path, command, args)
}

// OpenAccountInBrowser opens the provider profile page for the account.
func (a *App) OpenAccountInBrowser(accountKey string) error {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}
	url := git.AccountProfileURL(acct.URL, acct.Username)
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
	Path                string   `json:"path"`
	RelPath             string   `json:"relPath"`
	RemoteURL           string   `json:"remoteURL"`
	RepoKey             string   `json:"repoKey"`
	MatchedAccount      string   `json:"matchedAccount"`
	MatchedSource       string   `json:"matchedSource"`
	ExpectedPath        string   `json:"expectedPath"`
	NeedsRelocate       bool     `json:"needsRelocate"`
	LocalOnly           bool     `json:"localOnly"`
	AmbiguousCandidates []string `json:"ambiguousCandidates,omitempty"`
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
			Path:                o.Path,
			RelPath:             o.RelPath,
			RemoteURL:           o.RemoteURL,
			RepoKey:             o.RepoKey,
			MatchedAccount:      o.MatchedAccount,
			MatchedSource:       o.MatchedSource,
			ExpectedPath:        o.ExpectedPath,
			NeedsRelocate:       o.NeedsRelocate,
			LocalOnly:           o.LocalOnly,
			AmbiguousCandidates: o.AmbiguousCandidates,
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
		saveErr := a.saveConfig()
		a.mu.Unlock()
		if saveErr != nil {
			return AdoptResultDTO{Adopted: adopted, Relocated: relocated, Skipped: skipped, Error: saveErr.Error()}
		}
	}

	return AdoptResultDTO{Adopted: adopted, Relocated: relocated, Skipped: skipped}
}

// OpenInApp opens a folder in a specific application (e.g. VS Code).
// On Windows, editor launchers like code.cmd/cursor.cmd are batch wrappers;
// HideWindow prevents the hosting cmd.exe from flashing before the GUI spawns.
func (a *App) OpenInApp(path string, command string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	cmd := exec.Command(command, path)
	cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	git.HideWindow(cmd)
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
//
// Cursor is deliberately absent — it's detected as an AI harness via
// pkg/harness/tools-directory.md so it surfaces under the AI tools menu
// instead of the editor menu. Same rationale for Windsurf and Cline.
// Editor entries whose name collides with an AI harness name are pruned
// on every SyncEditors run (see the claimedByHarness set below).
var knownEditors = []EditorInfo{
	{ID: "vscode", Name: "VS Code", Command: "code"},
	{ID: "zed", Name: "Zed", Command: "zed"},
}

// DetectEditors returns code editors available on the system.
// It auto-detects known editors on PATH and merges any user-configured
// editors, filtering out any entry whose name is claimed by an AI harness
// (so Cursor doesn't show up as both an editor and a harness).
func (a *App) DetectEditors() []EditorInfo {
	claimedByHarness := make(map[string]bool, len(knownAIHarnesses))
	for _, h := range knownAIHarnesses {
		claimedByHarness[h.Name] = true
	}

	var editors []EditorInfo
	for _, e := range knownEditors {
		if claimedByHarness[e.Name] {
			continue
		}
		if _, err := lookPathWithBrewPATH(e.Command); err == nil {
			editors = append(editors, e)
		}
	}
	if a.cfg != nil {
		for _, e := range a.cfg.Global.Editors {
			if claimedByHarness[e.Name] {
				continue
			}
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

	// Names claimed by AI harnesses (Cursor, Windsurf, Cline, …). Any editor
	// entry with one of these names is pruned — those tools surface under
	// the AI harness menu instead, and duplicating them as editors was
	// confusing (see #23 feedback).
	claimedByHarness := make(map[string]bool, len(knownAIHarnesses))
	for _, h := range knownAIHarnesses {
		claimedByHarness[h.Name] = true
	}

	// Step 1: deduplicate existing entries by name (keep first occurrence),
	// dropping any entry whose name is claimed by the harness list.
	seenName := make(map[string]bool)
	var deduped []config.EditorEntry
	for _, e := range a.cfg.Global.Editors {
		if claimedByHarness[e.Name] {
			continue
		}
		if seenName[e.Name] {
			continue
		}
		seenName[e.Name] = true
		deduped = append(deduped, e)
	}

	changed := len(deduped) != len(a.cfg.Global.Editors)

	// Step 2: append any detected editor not already present. Skip any
	// candidate whose name overlaps the harness list — defence in depth;
	// knownEditors already excludes them, but new contributors editing the
	// directory shouldn't have to remember to also edit knownEditors.
	for _, known := range knownEditors {
		if claimedByHarness[known.Name] {
			continue
		}
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
		_ = a.saveConfig()
	}
}

// TerminalInfo describes an available terminal emulator for the frontend.
type TerminalInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// knownTerminalCandidate is an internal record used to seed auto-detection.
// Resolve is called lazily — a nil return means "not installed on this host".
type knownTerminalCandidate struct {
	Name    string
	Resolve func() (command string, args []string, ok bool)
}

// knownTerminals lists terminals to auto-detect per platform.
// Args use the literal token "{path}" to mark where the repo path is injected;
// if a candidate has no token, the path is appended as the final argv.
var knownTerminals = platformTerminalCandidates()

// wtSettingsCandidates returns the locations gitbox checks for Windows
// Terminal's settings.json, in priority order: Store install, Preview install,
// then unpackaged install. Empty paths (e.g. when LOCALAPPDATA is unset on
// non-Windows hosts) are filtered out.
func wtSettingsCandidates() []string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return nil
	}
	return []string{
		filepath.Join(local, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState", "settings.json"),
		filepath.Join(local, "Packages", "Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe", "LocalState", "settings.json"),
		filepath.Join(local, "Microsoft", "Windows Terminal", "settings.json"),
	}
}

// wtExePath resolves the absolute path to wt.exe, preferring the App Execution
// Alias under %LOCALAPPDATA%\Microsoft\WindowsApps so the config doesn't depend
// on PATH. Falls back to exec.LookPath when the alias is missing.
func wtExePath() (string, bool) {
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		alias := filepath.Join(local, "Microsoft", "WindowsApps", "wt.exe")
		if _, err := os.Stat(alias); err == nil {
			return alias, true
		}
	}
	if p, err := exec.LookPath("wt.exe"); err == nil {
		return p, true
	}
	return "", false
}

// stripJSONComments removes JSONC-style // line and /* block */ comments while
// preserving string literals (including escaped quotes). Trailing commas are
// left to the JSON decoder — Microsoft's settings.json doesn't emit them.
func stripJSONComments(in []byte) []byte {
	out := make([]byte, 0, len(in))
	const (
		stCode = iota
		stString
		stStringEscape
		stLineComment
		stBlockComment
		stBlockCommentStar
	)
	state := stCode
	for i := 0; i < len(in); i++ {
		c := in[i]
		switch state {
		case stCode:
			if c == '/' && i+1 < len(in) && in[i+1] == '/' {
				state = stLineComment
				i++
				continue
			}
			if c == '/' && i+1 < len(in) && in[i+1] == '*' {
				state = stBlockComment
				i++
				continue
			}
			out = append(out, c)
			if c == '"' {
				state = stString
			}
		case stString:
			out = append(out, c)
			if c == '\\' {
				state = stStringEscape
			} else if c == '"' {
				state = stCode
			}
		case stStringEscape:
			out = append(out, c)
			state = stString
		case stLineComment:
			if c == '\n' {
				out = append(out, c)
				state = stCode
			}
		case stBlockComment:
			if c == '*' {
				state = stBlockCommentStar
			}
		case stBlockCommentStar:
			if c == '/' {
				state = stCode
			} else if c != '*' {
				state = stBlockComment
			}
		}
	}
	return out
}

// parseWTProfiles reads a JSONC-encoded settings.json blob and returns one
// TerminalEntry per visible profile, in `profiles.list` order. A profile is
// excluded when `hidden: true` is set, or when its `source` appears in the
// top-level `disabledProfileSources` array — both criteria match WT's own
// menu-rendering rules. The wtCmd is used as the entry's Command so tests can
// inject a deterministic path.
func parseWTProfiles(data []byte, wtCmd string) ([]config.TerminalEntry, error) {
	clean := stripJSONComments(data)
	var doc struct {
		DisabledProfileSources []string `json:"disabledProfileSources"`
		Profiles               struct {
			List []struct {
				Name   string `json:"name"`
				Hidden *bool  `json:"hidden,omitempty"`
				Source string `json:"source,omitempty"`
			} `json:"list"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(clean, &doc); err != nil {
		return nil, err
	}
	if len(doc.Profiles.List) == 0 {
		return nil, errors.New("no profiles in settings.json")
	}
	disabled := make(map[string]bool, len(doc.DisabledProfileSources))
	for _, s := range doc.DisabledProfileSources {
		disabled[s] = true
	}
	var out []config.TerminalEntry
	for _, p := range doc.Profiles.List {
		if p.Name == "" {
			continue
		}
		if p.Hidden != nil && *p.Hidden {
			continue
		}
		if p.Source != "" && disabled[p.Source] {
			continue
		}
		out = append(out, config.TerminalEntry{
			Name:    p.Name,
			Command: wtCmd,
			Args:    []string{"--profile", p.Name, "-d", "{path}", "{command}"},
		})
	}
	if len(out) == 0 {
		return nil, errors.New("no visible WT profiles")
	}
	return out, nil
}

// discoverWTProfiles locates Windows Terminal's settings.json, parses the
// profile list, and returns one TerminalEntry per visible profile. Returns an
// error if wt.exe is missing, no settings.json is found, or the file can't be
// parsed — the caller is expected to fall back to bare-binary candidates.
func discoverWTProfiles() ([]config.TerminalEntry, error) {
	wtCmd, ok := wtExePath()
	if !ok {
		return nil, errors.New("wt.exe not found")
	}
	for _, path := range wtSettingsCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		profiles, err := parseWTProfiles(data, wtCmd)
		if err != nil {
			return nil, err
		}
		return profiles, nil
	}
	return nil, errors.New("settings.json not found")
}

// platformTerminalCandidates returns the terminal candidates eligible on the
// current host, in the order declared in pkg/harness/terminal-directory.md.
// Each candidate's Resolve func delegates to a platform-specific lookup
// (PATH / Homebrew PATH / Applications bundle / Windows App Alias / Program
// Files fallback) — the data stays in the markdown, the how-to-find-it stays
// in Go because it depends on OS APIs.
func platformTerminalCandidates() []knownTerminalCandidate {
	specs := harness.KnownTerminals()
	var out []knownTerminalCandidate
	for i := range specs {
		s := specs[i]
		if !terminalSpecMatchesCurrentOS(s.OS) {
			continue
		}
		out = append(out, knownTerminalCandidate{
			Name: s.Name,
			Resolve: func() (string, []string, bool) {
				return resolveTerminalSpec(s)
			},
		})
	}
	return out
}

// terminalSpecMatchesCurrentOS reports whether a spec's OS column applies to
// this host. "Linux" matches any non-Windows-non-macOS Unix to keep the
// detector useful on *BSD hosts (which ship the same terminals).
func terminalSpecMatchesCurrentOS(os string) bool {
	switch os {
	case "Windows":
		return isWindows()
	case "macOS":
		return isDarwin()
	case "Linux":
		return !isWindows() && !isDarwin()
	}
	return false
}

// resolveTerminalSpec resolves a TerminalSpec to a (command, args, ok)
// triple via the platform's detection path. Callers (DetectTerminals /
// SyncTerminals) treat ok=false as "not installed, skip this candidate".
func resolveTerminalSpec(s harness.TerminalSpec) (string, []string, bool) {
	if isWindows() {
		return resolveWindowsTerminalSpec(s)
	}
	if isDarwin() {
		return resolveDarwinTerminalSpec(s)
	}
	return resolveLinuxTerminalSpec(s)
}

func resolveWindowsTerminalSpec(s harness.TerminalSpec) (string, []string, bool) {
	// Try PATH first — covers pwsh.exe, powershell.exe, cmd.exe, wsl.exe, wt.exe.
	if p, err := exec.LookPath(s.Command); err == nil {
		return p, appendCopy(s.Args), true
	}
	// Per-command fallbacks for launchers that aren't reliably on PATH.
	switch s.Command {
	case "wt.exe":
		if p, ok := wtExePath(); ok {
			return p, appendCopy(s.Args), true
		}
	case "git-bash.exe":
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if root == "" {
				continue
			}
			cand := filepath.Join(root, "Git", "git-bash.exe")
			if _, err := os.Stat(cand); err == nil {
				return cand, appendCopy(s.Args), true
			}
		}
	}
	return "", nil, false
}

func resolveDarwinTerminalSpec(s harness.TerminalSpec) (string, []string, bool) {
	// Launching via `open -a <App>` is the mac pattern for Terminal-family
	// apps — resolve by checking for the app bundle rather than by looking
	// up "open" on PATH, since PATH always has it.
	if s.Command == "open" && len(s.Args) >= 2 && s.Args[0] == "-a" {
		appName := s.Args[1]
		bundle := appName + ".app"
		for _, root := range []string{"/Applications", "/System/Applications/Utilities", "/Applications/Utilities"} {
			if _, err := os.Stat(filepath.Join(root, bundle)); err == nil {
				return "open", appendCopy(s.Args), true
			}
		}
		return "", nil, false
	}
	p, err := lookPathWithBrewPATH(s.Command)
	if err != nil {
		return "", nil, false
	}
	return p, appendCopy(s.Args), true
}

func resolveLinuxTerminalSpec(s harness.TerminalSpec) (string, []string, bool) {
	p, err := lookPathWithBrewPATH(s.Command)
	if err != nil {
		return "", nil, false
	}
	return p, appendCopy(s.Args), true
}

// appendCopy returns a defensive copy of the slice so resolvers can't
// accidentally hand out an aliased reference to the spec's underlying args.
func appendCopy(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	return append([]string(nil), src...)
}

// DetectTerminals returns terminal emulators available on the system.
// On Windows, when Windows Terminal's settings.json is parseable, the result
// is the visible WT profiles in WT order — bare-binary candidates and any
// non-matching config entries are intentionally excluded so the menu mirrors
// WT itself. On other platforms, or when WT discovery fails, falls back to
// the platform's bare-binary candidates merged with user-configured entries.
func (a *App) DetectTerminals() []TerminalInfo {
	if isWindows() {
		if profiles, err := discoverWTProfiles(); err == nil && len(profiles) > 0 {
			merged := mergeWTProfilesWithConfig(profiles, a.cfg)
			out := make([]TerminalInfo, 0, len(merged))
			for _, t := range merged {
				out = append(out, TerminalInfo{
					ID:      terminalID(t.Name),
					Name:    t.Name,
					Command: t.Command,
					Args:    append([]string(nil), t.Args...),
				})
			}
			return out
		}
	}
	var terminals []TerminalInfo
	for _, cand := range knownTerminals {
		cmd, args, ok := cand.Resolve()
		if !ok {
			continue
		}
		terminals = append(terminals, TerminalInfo{
			ID:      terminalID(cand.Name),
			Name:    cand.Name,
			Command: cmd,
			Args:    append([]string(nil), args...),
		})
	}
	if a.cfg != nil {
		for _, t := range a.cfg.Global.Terminals {
			terminals = append(terminals, TerminalInfo{
				ID:      terminalID(t.Name),
				Name:    t.Name,
				Command: t.Command,
				Args:    append([]string(nil), t.Args...),
			})
		}
	}
	return terminals
}

// mergeWTProfilesWithConfig returns the WT-discovered profiles in WT order,
// substituting any existing config entry whose Name matches a profile so the
// user's Command/Args customizations survive sync. Config entries whose Name
// doesn't match any visible WT profile are dropped — by design — so the menu
// stays aligned with WT itself.
func mergeWTProfilesWithConfig(profiles []config.TerminalEntry, cfg *config.Config) []config.TerminalEntry {
	existing := make(map[string]config.TerminalEntry)
	if cfg != nil {
		for _, e := range cfg.Global.Terminals {
			existing[e.Name] = e
		}
	}
	out := make([]config.TerminalEntry, 0, len(profiles))
	for _, p := range profiles {
		if prior, ok := existing[p.Name]; ok && prior.Command != "" {
			// Seamless upgrade for entries emitted by the pre-{command} version
			// of this template: if the user's args are exactly the legacy
			// shape, rewrite them to the new template so harness launches work
			// without manual edits. Customizations (different flags, reorder,
			// extra args) are left untouched.
			if argsEqual(prior.Args, []string{"--profile", p.Name, "-d", "{path}"}) {
				prior.Args = []string{"--profile", p.Name, "-d", "{path}", "{command}"}
			}
			out = append(out, prior)
			continue
		}
		out = append(out, p)
	}
	return out
}

// argsEqual reports whether two argv slices are element-wise equal.
func argsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// terminalsEqual reports whether two terminal slices are byte-identical so
// SyncTerminals can skip a config write when nothing actually changed.
func terminalsEqual(a, b []config.TerminalEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Command != b[i].Command {
			return false
		}
		if len(a[i].Args) != len(b[i].Args) {
			return false
		}
		for j := range a[i].Args {
			if a[i].Args[j] != b[i].Args[j] {
				return false
			}
		}
	}
	return true
}

// SyncTerminals reconciles config's global.terminals array with the platform.
// On Windows, when WT discovery succeeds, the array is rebuilt from scratch
// using the visible WT profiles in WT order — stale legacy entries (bare
// pwsh.exe, git-bash.exe, etc.) and entries for profiles that became hidden
// or had their source disabled are dropped. User-customized Command/Args are
// preserved when the entry's Name matches a current visible profile.
// On other platforms (or when WT is absent / unparseable), falls back to the
// historical dedup-and-append behaviour that mirrors SyncEditors.
func (a *App) SyncTerminals() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if isWindows() {
		if profiles, err := discoverWTProfiles(); err == nil && len(profiles) > 0 {
			rebuilt := mergeWTProfilesWithConfig(profiles, a.cfg)
			if !terminalsEqual(a.cfg.Global.Terminals, rebuilt) {
				a.cfg.Global.Terminals = rebuilt
				_ = a.saveConfig()
			}
			return
		}
	}

	knownIndex := make(map[string]int, len(knownTerminals))
	for i, k := range knownTerminals {
		knownIndex[k.Name] = i
	}

	// Dedup existing entries by name; bucket known vs. user-custom.
	seenName := make(map[string]bool)
	existingByName := make(map[string]config.TerminalEntry)
	var customOrder []config.TerminalEntry // not in knownTerminals
	for _, t := range a.cfg.Global.Terminals {
		if seenName[t.Name] {
			continue
		}
		seenName[t.Name] = true
		if _, ok := knownIndex[t.Name]; ok {
			existingByName[t.Name] = t
		} else {
			customOrder = append(customOrder, t)
		}
	}

	// Emit the known block in markdown order. For entries the user already
	// had, upgrade exact-legacy args to the current candidate template so
	// harness launches work without manual edits; otherwise carry the
	// user's command/args through unchanged. Missing known terminals that
	// resolve on PATH are added at their markdown position.
	var result []config.TerminalEntry
	for _, cand := range knownTerminals {
		cmd, candArgs, ok := cand.Resolve()
		if existing, have := existingByName[cand.Name]; have {
			if ok && cmd == existing.Command {
				legacy := stripCommandTokens(candArgs)
				if argsEqual(existing.Args, legacy) && !argsEqual(existing.Args, candArgs) {
					existing.Args = append([]string(nil), candArgs...)
				}
			}
			result = append(result, existing)
			continue
		}
		if !ok {
			continue
		}
		result = append(result, config.TerminalEntry{
			Name:    cand.Name,
			Command: cmd,
			Args:    append([]string(nil), candArgs...),
		})
	}
	result = append(result, customOrder...)

	if !terminalsEqual(result, a.cfg.Global.Terminals) {
		a.cfg.Global.Terminals = result
		_ = a.saveConfig()
	}
}

// stripCommandTokens returns args with every "{command}" entry removed, so
// callers can compare a user's legacy (pre-harness) args against the current
// candidate template and upgrade on exact match.
func stripCommandTokens(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "{command}" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// OpenInTerminal launches a terminal emulator in the given folder.
// Args may contain the token "{path}"; if present it is substituted with
// path, otherwise path is appended as the final argv.
//
// Windows specifics: the Wails GUI is a /SUBSYSTEM:WINDOWS process with no
// console. Go's exec always passes the parent's stdio handles to the child
// with STARTF_USESTDHANDLES set, so plain console apps (cmd.exe, pwsh.exe,
// powershell.exe, wsl.exe) see closed stdin and exit immediately — even if
// we set CREATE_NEW_CONSOLE. Wrapping the launch in `cmd.exe /C start ""`
// is the canonical workaround: `start` creates the terminal as a fresh
// console process with properly connected console handles. The transient
// wrapper `cmd.exe` itself is hidden via HideWindow so the user never sees
// a flash. Editors (OpenInApp) don't need this dance because their GUI
// window detaches from the launcher's console.
//
// On macOS/Linux we launch directly with cmd.Dir = path — no console
// plumbing needed since those terminals spawn their own windows.
func (a *App) OpenInTerminal(path string, command string, args []string) error {
	return openTerminalAt(path, command, args)
}

// openTerminalAt is the shared core for launching a terminal emulator in a
// folder. Callers (OpenInTerminal, OpenAccountInTerminal) resolve the path
// first and delegate here so the Windows console-flash workaround lives in
// one place.
func openTerminalAt(path string, command string, args []string) error {
	return openTerminalWithHarnessAt(path, command, args, nil)
}

// openTerminalWithHarnessAt launches a terminal in a folder, optionally
// splicing a harness argv into a "{command}" token in the terminal's args.
// When harnessArgv is nil it behaves exactly like openTerminalAt. The Windows
// cmd.exe /C start workaround lives here so both harness and terminal-only
// launches go through the same path.
func openTerminalWithHarnessAt(path string, command string, args []string, harnessArgv []string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	resolved := resolveTerminalArgsWithCommand(args, path, harnessArgv)

	var cmd *exec.Cmd
	if isWindows() {
		// cmd.exe /C start "" /D <path> <command> <args...>
		// "" = empty title (required placeholder; first quoted arg to `start`
		// is otherwise interpreted as the window title).
		startArgs := make([]string, 0, 6+len(resolved))
		startArgs = append(startArgs, "/C", "start", "", "/D", path, command)
		startArgs = append(startArgs, resolved...)
		cmd = exec.Command("cmd.exe", startArgs...)
		git.HideWindow(cmd) // hide the transient wrapper, not the terminal
		cmd.Env = sanitizeWindowsTerminalEnv(git.Environ())
	} else {
		cmd = exec.Command(command, resolved...)
		cmd.Dir = path
		cmd.Env = git.Environ()
	}
	return cmd.Start()
}

// sanitizeWindowsTerminalEnv scrubs MSYS / Git-Bash artefacts from the env
// before handing it to a child terminal. When the GUI itself is launched
// from Git Bash (dev workflow), env vars like LOCALAPPDATA, APPDATA, TEMP
// etc. are inherited in posix form ("/c/Users/..."), which breaks Windows-
// native tools like oh-my-posh in the spawned shell. Production launches
// (from Explorer / Start Menu) are unaffected — sanitisation is a no-op on
// a clean env.
func sanitizeWindowsTerminalEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		i := strings.IndexByte(e, '=')
		if i < 0 {
			out = append(out, e)
			continue
		}
		key, val := e[:i], e[i+1:]
		switch strings.ToUpper(key) {
		case "MSYSTEM", "MSYS", "MSYS2_PATH_TYPE", "MSYS_NO_PATHCONV":
			// Drop — their presence triggers MSYS path translation in children.
			continue
		case "LOCALAPPDATA", "APPDATA", "USERPROFILE", "HOMEPATH",
			"TEMP", "TMP", "HOME", "PROGRAMFILES", "PROGRAMDATA":
			val = msysToWindowsPath(val)
		}
		out = append(out, key+"="+val)
	}
	return out
}

// msysToWindowsPath converts a single MSYS path ("/c/Users/foo") to Windows
// form ("C:\\Users\\foo"). Values already in Windows form or not matching
// the /<drive>/... shape are returned unchanged.
func msysToWindowsPath(p string) string {
	if len(p) < 2 || p[0] != '/' {
		return p
	}
	c := p[1]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return p
	}
	if len(p) > 2 && p[2] != '/' {
		return p
	}
	rest := ""
	if len(p) > 2 {
		rest = p[2:]
	}
	return strings.ToUpper(string(c)) + ":" + strings.ReplaceAll(rest, "/", `\`)
}

// resolveTerminalArgs substitutes "{path}" tokens in args with the repo path.
// If no token is found AND args is non-empty, path is appended as a final argv
// entry (covers patterns like `open -a Terminal <path>`). Empty args is
// preserved as empty — the caller sets cmd.Dir so shells start in the repo.
//
// This is a thin wrapper over resolveTerminalArgsWithCommand that passes a nil
// harness argv, so every "{command}" token expands to zero items (safe no-op
// for terminal-only launches).
func resolveTerminalArgs(args []string, path string) []string {
	return resolveTerminalArgsWithCommand(args, path, nil)
}

// resolveTerminalArgsWithCommand substitutes "{path}" in args (string replace)
// and splices harnessArgv in place of each literal "{command}" entry. The
// splice is an argv-level insertion, not a string replace — splicing through
// strings.ReplaceAll would require shell-quoting harnessArgv back to a single
// string and risks injection.
//
// Rules:
//   - An arg equal to "{command}" is replaced in-place by harnessArgv items
//     (0..N). Multiple "{command}" tokens each splice the same argv.
//   - Other occurrences of "{path}" anywhere inside an arg are substituted
//     as a plain string replace.
//   - When no "{path}" token is present AND harnessArgv is nil AND args is
//     non-empty, path is appended as the final argv (legacy behavior for
//     launchers like `open -a Terminal <path>`). Harness launches never
//     append the path — the terminal is responsible for opening the folder
//     via its own working-directory flag.
//   - Empty args is preserved as empty.
func resolveTerminalArgsWithCommand(args []string, path string, harnessArgv []string) []string {
	if len(args) == 0 {
		return nil
	}
	pathSubstituted := false
	out := make([]string, 0, len(args)+len(harnessArgv))
	for _, a := range args {
		if a == "{command}" {
			out = append(out, harnessArgv...)
			continue
		}
		if strings.Contains(a, "{path}") {
			out = append(out, strings.ReplaceAll(a, "{path}", path))
			pathSubstituted = true
			continue
		}
		out = append(out, a)
	}
	if !pathSubstituted && harnessArgv == nil {
		out = append(out, path)
	}
	return out
}

// terminalID returns a stable, lowercase slug used as the TerminalInfo.ID.
// Unlike editors (where ID is the command binary), terminals often share a
// launcher binary on macOS (`open`), so we slugify the display name instead.
func terminalID(name string) string {
	s := strings.ToLower(name)
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ─── AI Harnesses ──────────────────────────────────────────────────────────

// AIHarnessInfo describes an available AI CLI harness for the frontend.
type AIHarnessInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// knownHarnessCandidate is an internal record used to seed auto-detection.
type knownHarnessCandidate struct {
	Name    string
	Command string // binary name to look up on PATH (e.g. "claude", "codex")
}

// knownAIHarnesses is the auto-detection seed. It's built once at process
// start from the embedded agentic tools directory in pkg/harness. The order
// is the order in which Sync appends missing entries — users reorder
// global.ai_harnesses freely and their order wins on subsequent syncs.
//
// To add or remove an auto-detected harness, edit
// pkg/harness/tools-directory.md rather than this file.
var knownAIHarnesses = buildKnownAIHarnesses()

// buildKnownAIHarnesses reads the embedded tool directory and returns the
// subset whose Executable cell looks like a single PATH binary — i.e. the
// rows pkg/harness.KnownTools already filtered for us.
func buildKnownAIHarnesses() []knownHarnessCandidate {
	tools := harness.KnownTools()
	out := make([]knownHarnessCandidate, 0, len(tools))
	for _, t := range tools {
		out = append(out, knownHarnessCandidate{Name: t.Name, Command: t.Command})
	}
	return out
}

// harnessID returns a stable, lowercase slug used as the AIHarnessInfo.ID.
// Shares the terminalID slugifier — both fields share the same UI contract.
func harnessID(name string) string {
	return terminalID(name)
}

// DetectAIHarnesses returns AI CLI harnesses available on the system.
// It auto-detects known binaries on PATH (using the Homebrew-augmented PATH
// so Homebrew-installed harnesses on macOS are visible to the Wails GUI)
// and merges any user-configured entries from global.ai_harnesses.
func (a *App) DetectAIHarnesses() []AIHarnessInfo {
	var out []AIHarnessInfo
	seen := make(map[string]bool)
	for _, cand := range knownAIHarnesses {
		fullPath, err := lookPathWithBrewPATH(cand.Command)
		if err != nil {
			continue
		}
		id := harnessID(cand.Name)
		seen[cand.Name] = true
		out = append(out, AIHarnessInfo{
			ID:      id,
			Name:    cand.Name,
			Command: fullPath,
		})
	}
	if a.cfg != nil {
		for _, h := range a.cfg.Global.AIHarnesses {
			if seen[h.Name] {
				continue
			}
			out = append(out, AIHarnessInfo{
				ID:      harnessID(h.Name),
				Name:    h.Name,
				Command: h.Command,
				Args:    append([]string(nil), h.Args...),
			})
			seen[h.Name] = true
		}
	}
	return out
}

// SyncAIHarnesses reconciles config's global.ai_harnesses array with the
// embedded tools-directory. The final order follows
// pkg/harness/tools-directory.md — users curate the menu order by editing
// that file, and every sync re-applies it:
//   - Entries whose Name matches a known harness are ordered by their
//     position in knownAIHarnesses (= the markdown table order). User-
//     customized Command/Args on those entries are preserved verbatim.
//   - User-added entries not in the directory stay after the known block,
//     in their original relative order.
//   - Duplicates by Name collapse to the first occurrence.
//   - Detected known harnesses missing from config are appended (in
//     directory order), with the resolved binary path.
//   - Config is saved only when something actually changed.
func (a *App) SyncAIHarnesses() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		return
	}

	knownIndex := make(map[string]int, len(knownAIHarnesses))
	for i, k := range knownAIHarnesses {
		knownIndex[k.Name] = i
	}

	// Dedup existing entries by name, preserving the first occurrence.
	seenName := make(map[string]bool)
	existingByName := make(map[string]config.AIHarnessEntry)
	var customOrder []config.AIHarnessEntry // not in knownAIHarnesses
	for _, h := range a.cfg.Global.AIHarnesses {
		if seenName[h.Name] {
			continue
		}
		seenName[h.Name] = true
		if _, known := knownIndex[h.Name]; known {
			existingByName[h.Name] = h
		} else {
			customOrder = append(customOrder, h)
		}
	}

	// Build the known-harness block in directory order. If the user already
	// had an entry, reuse it (preserving command/args customizations);
	// otherwise detect on PATH and add with the resolved path.
	var result []config.AIHarnessEntry
	for _, k := range knownAIHarnesses {
		if e, ok := existingByName[k.Name]; ok {
			result = append(result, e)
			continue
		}
		fullPath, err := lookPathWithBrewPATH(k.Command)
		if err != nil {
			continue
		}
		result = append(result, config.AIHarnessEntry{
			Name:    k.Name,
			Command: fullPath,
		})
	}
	result = append(result, customOrder...)

	// Only save if the serialization actually differs.
	if !aiHarnessesEqual(result, a.cfg.Global.AIHarnesses) {
		a.cfg.Global.AIHarnesses = result
		_ = a.saveConfig()
	}
}

// aiHarnessesEqual compares two harness slices element-wise so
// SyncAIHarnesses can skip a config write when nothing changed.
func aiHarnessesEqual(a, b []config.AIHarnessEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Command != b[i].Command {
			return false
		}
		if len(a[i].Args) != len(b[i].Args) {
			return false
		}
		for j := range a[i].Args {
			if a[i].Args[j] != b[i].Args[j] {
				return false
			}
		}
	}
	return true
}

// ShowErrorDialog pops a native error dialog from the frontend. Used for
// flows where the WebView-level window.alert() isn't reliable (notably
// macOS WKWebView) — the Wails runtime's MessageDialog routes through the
// host OS's native dialog APIs, which work everywhere.
func (a *App) ShowErrorDialog(title, message string) {
	if a.ctx == nil {
		return
	}
	_, _ = wailsrt.MessageDialog(a.ctx, wailsrt.MessageDialogOptions{
		Type:    wailsrt.ErrorDialog,
		Title:   title,
		Message: message,
	})
}

// resolveFirstHarnessTerminal returns global.terminals[0] and an actionable
// error when (a) no terminal is configured or (b) the terminal can't accept a
// harness command. Eligibility: either the args contain the "{command}" token
// (the generic splice path for WT profiles, gnome-terminal, etc.) OR the
// entry is a macOS Terminal.app / iTerm `open -a <App>` pair (handled via
// osascript — see macAppleScriptTerminalApp).
func (a *App) resolveFirstHarnessTerminal() (config.TerminalEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil || len(a.cfg.Global.Terminals) == 0 {
		return config.TerminalEntry{}, fmt.Errorf("Configure a terminal first (global.terminals is empty)")
	}
	t := a.cfg.Global.Terminals[0]
	for _, arg := range t.Args {
		if arg == "{command}" {
			return t, nil
		}
	}
	if _, ok := macAppleScriptTerminalApp(t); ok {
		return t, nil
	}
	return config.TerminalEntry{}, fmt.Errorf("%s in global.terminals[0] doesn't support launching a command. Add {command} to its args, or reorder global.terminals so a compatible entry is first.", t.Name)
}

// macAppleScriptTerminalApp detects a macOS `open -a Terminal|iTerm` entry
// and returns the app name to talk to via osascript. Returns ok=false for
// anything else (including Warp — which has a more complex AppleScript
// dialect not yet supported). Matching is purely on the entry's shape, so
// user-renamed entries still work as long as the command/args are intact.
func macAppleScriptTerminalApp(t config.TerminalEntry) (appName string, ok bool) {
	if !isDarwin() || t.Command != "open" {
		return "", false
	}
	// Args must contain "-a <App>" with App in the supported set.
	for i := 0; i < len(t.Args)-1; i++ {
		if t.Args[i] != "-a" {
			continue
		}
		switch t.Args[i+1] {
		case "Terminal", "iTerm":
			return t.Args[i+1], true
		}
	}
	return "", false
}

// shellQuotePOSIX wraps s in single quotes for POSIX shells, escaping any
// embedded single quotes. Suitable for building a shell command line that
// gets passed to `sh -c` or an AppleScript `do script`.
func shellQuotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleScriptEscape escapes a string for embedding in an AppleScript
// double-quoted string literal. Backslashes and double quotes get prefixed
// with a backslash.
func appleScriptEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// buildMacHarnessShellLine returns the shell command line that osascript
// will `do script` (Terminal.app) or `write text` (iTerm). It cd's into the
// target path and then execs the harness argv — everything POSIX-quoted so
// paths with spaces and unicode round-trip safely.
func buildMacHarnessShellLine(path string, harnessArgv []string) string {
	parts := make([]string, 0, 1+len(harnessArgv))
	parts = append(parts, "cd "+shellQuotePOSIX(path))
	if len(harnessArgv) > 0 {
		quoted := make([]string, len(harnessArgv))
		for i, a := range harnessArgv {
			quoted[i] = shellQuotePOSIX(a)
		}
		parts = append(parts, strings.Join(quoted, " "))
	}
	return strings.Join(parts, " && ")
}

// buildMacAppleScript returns the AppleScript source that launches the
// harness inside the named Terminal-family app. For Terminal.app a plain
// `do script` (which opens a new window with the command running); for
// iTerm, create a fresh window with the default profile and write the
// command to its current session.
func buildMacAppleScript(appName, path string, harnessArgv []string) string {
	shell := buildMacHarnessShellLine(path, harnessArgv)
	escaped := appleScriptEscape(shell)
	switch appName {
	case "Terminal":
		return fmt.Sprintf(`tell application "Terminal"
    activate
    do script "%s"
end tell`, escaped)
	case "iTerm":
		// iTerm's `create window with default profile` returns before the
		// session is ready to accept `write text` — with no delay the window
		// opens but the command silently drops. A small delay gives iTerm
		// time to finish initializing the session; `current session of
		// current window` then resolves to the just-created session. This
		// is the pattern that works across iTerm 3.x versions; the
		// apparently-cleaner "nested tell (create window …)" form races.
		return fmt.Sprintf(`tell application "iTerm"
    activate
    create window with default profile
    delay 0.3
    tell current session of current window
        write text "%s"
    end tell
end tell`, escaped)
	}
	return ""
}

// launchMacHarnessViaAppleScript spawns osascript with the built AppleScript.
// First invocation may prompt the user for Automation permission (macOS
// Privacy → Automation) to let gitbox control Terminal.app / iTerm — that's
// a one-time OS-level consent, not a gitbox prompt.
func launchMacHarnessViaAppleScript(appName, path string, harnessArgv []string) error {
	script := buildMacAppleScript(appName, path, harnessArgv)
	if script == "" {
		return fmt.Errorf("unsupported macOS terminal app: %q", appName)
	}
	cmd := exec.Command("osascript", "-e", script)
	cmd.Env = git.Environ()
	return cmd.Start()
}

// launchHarnessInTerminal dispatches between the generic {command}-splice
// launcher and the macOS AppleScript launcher based on the terminal entry's
// shape. Central point for harness-side launch routing.
func launchHarnessInTerminal(path string, term config.TerminalEntry, harnessArgv []string) error {
	if appName, ok := macAppleScriptTerminalApp(term); ok {
		return launchMacHarnessViaAppleScript(appName, path, harnessArgv)
	}
	return openTerminalWithHarnessAt(path, term.Command, term.Args, harnessArgv)
}

// buildHarnessArgv returns the harness argv to splice into the terminal's
// "{command}" slot: the harness binary followed by its args. An empty command
// returns nil so the splice expands to zero items (matches the terminal-only
// launch contract and prevents silent misfires).
func buildHarnessArgv(command string, args []string) []string {
	if command == "" {
		return nil
	}
	argv := make([]string, 0, 1+len(args))
	argv = append(argv, command)
	argv = append(argv, args...)
	return argv
}

// OpenInAIHarness launches the given AI harness in the clone folder, using
// global.terminals[0] as the host terminal. Takes the harness command + args
// directly (rather than looking up by ID) so both auto-detected and
// config-stored entries call the same contract — mirrors OpenInTerminal's
// signature. Errors are actionable strings the frontend can surface verbatim.
func (a *App) OpenInAIHarness(path string, command string, args []string) error {
	if command == "" {
		return fmt.Errorf("AI harness command is required")
	}
	term, err := a.resolveFirstHarnessTerminal()
	if err != nil {
		return err
	}
	return launchHarnessInTerminal(path, term, buildHarnessArgv(command, args))
}

// OpenAccountInAIHarness launches the given AI harness in the account's
// parent folder (<global.folder>/<accountKey>), using global.terminals[0] as
// the host terminal. Errors out when the account is unknown or the folder is
// missing, matching the pattern from OpenAccountInTerminal.
func (a *App) OpenAccountInAIHarness(accountKey string, command string, args []string) error {
	if command == "" {
		return fmt.Errorf("AI harness command is required")
	}
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
	term, err := a.resolveFirstHarnessTerminal()
	if err != nil {
		return err
	}
	return launchHarnessInTerminal(path, term, buildHarnessArgv(command, args))
}

// ─── Platform helpers ─────────────────────────────────────────────────────

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
// failed to parse. The frontend should show this to the user and offer
// Repair / Start fresh / Exit. Returns "" if config loaded successfully or
// didn't exist.
func (a *App) GetConfigLoadError() string {
	return a.cfgLoadError
}

// ConfigRepairResult is the frontend-friendly outcome of a RepairConfig call.
// On success, the in-memory config has been replaced with the repaired copy
// and saved back to disk (the existing Save() path writes a dated backup
// first, so the pre-repair file is preserved).
type ConfigRepairResult struct {
	Success bool     `json:"success"`
	Error   string   `json:"error,omitempty"`
	Repairs []string `json:"repairs,omitempty"`
}

// RepairConfig runs LoadWithRepair against the on-disk config, adopts the
// repaired copy into the live App state, and saves it back. Intended as the
// user-confirmed "Repair" action when Startup detected cfgLoadError.
//
// The normal Save() backup sweep covers the pre-repair file, so users who
// regret the repair can roll back manually from ~/.config/gitbox/gitbox-*.json.
func (a *App) RepairConfig() ConfigRepairResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, repairs, err := config.LoadWithRepair(a.cfgPath)
	if err != nil {
		return ConfigRepairResult{Success: false, Error: err.Error()}
	}
	a.cfg = cfg
	a.cfgLoaded = true
	a.cfgLoadError = ""

	if err := config.Save(a.cfg, a.cfgPath); err != nil {
		return ConfigRepairResult{Success: false, Error: fmt.Sprintf("saving repaired config: %v", err)}
	}

	details := make([]string, 0, len(repairs))
	for _, r := range repairs {
		details = append(details, r.Detail)
	}
	return ConfigRepairResult{Success: true, Repairs: details}
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
	return a.saveConfig()
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
	_ = a.saveConfig()

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

	return a.saveConfig()
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
	if err := a.saveConfig(); err != nil {
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
	saveErr := a.saveConfig()
	a.mu.Unlock()
	return saveErr
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
		_ = a.saveConfig()
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
		_ = a.saveConfig()
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
	return a.saveConfig()
}

// AccountDeletionImpactDTO describes what a DeleteAccount call would remove.
// Sources and Mirrors are sorted for stable rendering.
type AccountDeletionImpactDTO struct {
	Account    string   `json:"account"`
	Sources    []string `json:"sources"`
	Mirrors    []string `json:"mirrors"`
	RepoCount  int      `json:"repo_count"`
	CloneCount int      `json:"clone_count"`
}

// AccountDeletionImpact reports the cascade effect of deleting the given
// account — every source and mirror that references it, plus the total repo
// count and how many are currently cloned on disk. Read-only.
func (a *App) AccountDeletionImpact(accountKey string) AccountDeletionImpactDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	sources, mirrors, repoCount := a.cfg.AccountDeletionImpact(accountKey)
	if sources == nil {
		sources = []string{}
	}
	if mirrors == nil {
		mirrors = []string{}
	}
	return AccountDeletionImpactDTO{
		Account:    accountKey,
		Sources:    sources,
		Mirrors:    mirrors,
		RepoCount:  repoCount,
		CloneCount: a.countClonedRepos(accountKey),
	}
}

// DeleteAccount removes an account, every source that references it, every
// mirror that references it, and all local clone folders. Cascade semantics:
// leaving a dangling mirror reference behind would corrupt the config and
// cause data loss on the next launch (see issue #60).
func (a *App) DeleteAccount(accountKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.cfg.Accounts[accountKey]; !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)

	// Delete all clone folders and source directories for this account. We
	// walk the sources map before CascadeDeleteAccount mutates it.
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
		// Remove the entire source directory (e.g. ~/00.git/github-acme/).
		sourcePath := filepath.Join(globalFolder, sourceFolder)
		_ = os.RemoveAll(sourcePath)
	}

	if _, _, err := a.cfg.CascadeDeleteAccount(accountKey); err != nil {
		return err
	}

	return a.saveConfig()
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
	if err := a.saveConfig(); err != nil {
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

	if err := a.saveConfig(); err != nil {
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
	return a.saveConfig()
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
	return a.saveConfig()
}

// DeleteMirrorGroup removes a mirror group and all its repos.
func (a *App) DeleteMirrorGroup(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.DeleteMirror(key); err != nil {
		return err
	}
	return a.saveConfig()
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
	return a.saveConfig()
}

// DeleteMirrorRepo removes a repo from a mirror group.
func (a *App) DeleteMirrorRepo(mirrorKey, repoKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.DeleteMirrorRepo(mirrorKey, repoKey); err != nil {
		return err
	}
	return a.saveConfig()
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
		saveErr := a.saveConfig()
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
		return a.saveConfig()
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

// ── Doctor: external-tool detection ────────────────────────────────────────

// DoctorToolDTO mirrors one doctor.Result in a JSON-friendly shape for the
// Svelte frontend. Includes a "required" flag derived from the current
// config so the UI can highlight missing required tools.
type DoctorToolDTO struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Purpose     string `json:"purpose"`
	Found       bool   `json:"found"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	Required    bool   `json:"required"`
	RequiredFor string `json:"requiredFor,omitempty"`
	InstallHint string `json:"installHint,omitempty"`
}

// DoctorReport is the full "System check" result: the tool list plus a
// rolled-up boolean saying "any required tool missing?".
type DoctorReport struct {
	Tools       []DoctorToolDTO `json:"tools"`
	AllOK       bool            `json:"allOk"`        // true when nothing required is missing
	MissingReq  int             `json:"missingReq"`   // count of required tools that are missing
	MissingOpt  int             `json:"missingOpt"`   // count of optional tools that are missing
}

// DoctorPrecheckDTO is the point-of-use answer for "is credential type X
// installable on this host?".
type DoctorPrecheckDTO struct {
	OK      bool            `json:"ok"`
	Summary string          `json:"summary,omitempty"`
	Hint    string          `json:"hint,omitempty"`
	Missing []DoctorToolDTO `json:"missing,omitempty"`
}

// DoctorRun probes every tool gitbox knows about and annotates each with
// whether it's required for the current config. Used by the GUI Settings
// "System check" row.
func (a *App) DoctorRun() DoctorReport {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	results := doctor.Check(doctor.StandardTools())
	required := gatherRequiredTools(cfg)

	report := DoctorReport{Tools: make([]DoctorToolDTO, 0, len(results)), AllOK: true}
	for _, r := range results {
		reason, isRequired := required[r.Tool.Name]
		dto := DoctorToolDTO{
			Name:        r.Tool.Name,
			DisplayName: r.Tool.DisplayName,
			Purpose:     r.Tool.Purpose,
			Found:       r.Found,
			Path:        r.Path,
			Version:     r.Version,
			Required:    isRequired,
			RequiredFor: reason,
			InstallHint: r.InstallHint(),
		}
		report.Tools = append(report.Tools, dto)
		if !r.Found {
			if isRequired {
				report.AllOK = false
				report.MissingReq++
			} else {
				report.MissingOpt++
			}
		}
	}
	return report
}

// DoctorPrecheck runs a point-of-use check for a single credential type.
// Called by the GUI add-account / change-credential-type flows BEFORE the
// user commits, so they learn about a missing dependency with an install
// command instead of hitting a cryptic error at auth time.
func (a *App) DoctorPrecheck(credentialType string) DoctorPrecheckDTO {
	pc := doctor.PrecheckForCredentialType(credentialType)
	out := DoctorPrecheckDTO{OK: pc.OK, Summary: pc.Summary, Hint: pc.Hint}
	for _, m := range pc.Missing {
		out.Missing = append(out.Missing, DoctorToolDTO{
			Name:        m.Tool.Name,
			DisplayName: m.Tool.DisplayName,
			Purpose:     m.Tool.Purpose,
			Found:       false,
			InstallHint: m.InstallHint(),
		})
	}
	return out
}

// gatherRequiredTools reads the config and returns, for each tool name,
// the reason it is required (or not in the map at all if purely optional).
func gatherRequiredTools(cfg *config.Config) map[string]string {
	required := map[string]string{
		"git": "always — core dependency",
	}
	if cfg == nil {
		return required
	}
	var anyGCM, anySSH, anyToken bool
	for _, acct := range cfg.Accounts {
		switch acct.DefaultCredentialType {
		case "gcm":
			anyGCM = true
		case "ssh":
			anySSH = true
		case "token":
			anyToken = true
		}
	}
	if anyGCM || anyToken {
		required["git-credential-manager"] = "you have accounts using the gcm/token credential type"
	}
	if anySSH {
		required["ssh"] = "you have accounts using the ssh credential type"
		required["ssh-keygen"] = "you have accounts using the ssh credential type"
		required["ssh-add"] = "you have accounts using the ssh credential type"
	}
	var anyTmuxinator bool
	for _, ws := range cfg.Workspaces {
		if ws.Type == "tmuxinator" {
			anyTmuxinator = true
			break
		}
	}
	if anyTmuxinator {
		required["tmux"] = "you have tmuxinator workspaces"
		required["tmuxinator"] = "you have tmuxinator workspaces"
		if runtime.GOOS == "windows" {
			required["wsl"] = "tmuxinator runs inside WSL on Windows"
		}
	}
	return required
}
