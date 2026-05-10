package main

// Terminal Profiles bridge (issue #69 v2.1 schema, simplified by issue #71).
//
// Detection, OS-aware composition, and merge logic now live in
// pkg/terminals — this file only exposes the Wails bridge surface that
// the Svelte frontend talks to: DTOs, list/save/open methods, and the
// SyncProfiles + RedetectProfiles + MissingModernTerminal entry points.
//
// The legacy v2.0 OpenInTerminal path in app.go is unchanged. The two
// pathways coexist until the legacy Terminals[] field is removed in a
// future release.

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/launch"
	"github.com/LuisPalacios/gitbox/pkg/terminals"
)

// ─── DTOs (mirrored in cmd/gui/frontend/src/lib/types.ts) ─────────────────

// TerminalAppInfo is the wire shape for a TerminalApp shipped to the Svelte
// frontend.
type TerminalAppInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Command      string   `json:"command"`
	ArgsTemplate []string `json:"args_template"`
}

// ShellInfo is the wire shape for a Shell shipped to the Svelte frontend.
type ShellInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// TerminalProfileInfo is the wire shape for a TerminalProfile shipped to the
// Svelte frontend. The Source field is internal to the engine — it gates
// the delete-vs-hide UX rule (only Source="user" rows are deletable) and is
// never displayed in the Manager.
type TerminalProfileInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	TerminalID string   `json:"terminal"`
	ShellID    string   `json:"shell"`
	Args       []string `json:"args"`
	Default    bool     `json:"default"`
	Preferred  bool     `json:"preferred"`
	Hidden     bool     `json:"hidden"`
	Source     string   `json:"source"` // internal — never display
}

// ─── List bridges (read snapshots) ────────────────────────────────────────

// ListTerminalApps returns the persisted TerminalApp list as a DTO slice.
func (a *App) ListTerminalApps() []TerminalAppInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]TerminalAppInfo, 0, len(a.cfg.Global.TerminalApps))
	for _, app := range a.cfg.Global.TerminalApps {
		out = append(out, TerminalAppInfo{
			ID:           app.ID,
			Name:         app.Name,
			Command:      app.Command,
			ArgsTemplate: append([]string(nil), app.ArgsTemplate...),
		})
	}
	return out
}

// ListShells returns the persisted Shell list as a DTO slice.
func (a *App) ListShells() []ShellInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]ShellInfo, 0, len(a.cfg.Global.Shells))
	for _, s := range a.cfg.Global.Shells {
		out = append(out, ShellInfo{
			ID:      s.ID,
			Name:    s.Name,
			Command: s.Command,
			Args:    append([]string(nil), s.Args...),
		})
	}
	return out
}

// ListTerminalProfiles returns the persisted TerminalProfile list as a DTO
// slice in on-disk order.
func (a *App) ListTerminalProfiles() []TerminalProfileInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]TerminalProfileInfo, 0, len(a.cfg.Global.TerminalProfiles))
	for _, p := range a.cfg.Global.TerminalProfiles {
		out = append(out, TerminalProfileInfo{
			ID:         p.ID,
			Name:       p.Name,
			TerminalID: p.TerminalID,
			ShellID:    p.ShellID,
			Args:       append([]string(nil), p.Args...),
			Default:    p.Default,
			Preferred:  p.Preferred,
			Hidden:     p.Hidden,
			Source:     p.Source,
		})
	}
	return out
}

// ─── Save bridges ─────────────────────────────────────────────────────────

// SaveTerminalProfilesDTO is the bridge-friendly counterpart to
// SaveTerminalProfiles — accepts the DTO shape the frontend already speaks
// (JSON-tagged structs, no nil slices) and converts to config types before
// persisting.
func (a *App) SaveTerminalProfilesDTO(apps []TerminalAppInfo, shells []ShellInfo, profiles []TerminalProfileInfo) error {
	cfgApps := make([]config.TerminalApp, 0, len(apps))
	for _, app := range apps {
		cfgApps = append(cfgApps, config.TerminalApp{
			ID:           app.ID,
			Name:         app.Name,
			Command:      app.Command,
			ArgsTemplate: append([]string(nil), app.ArgsTemplate...),
		})
	}
	cfgShells := make([]config.ShellEntry, 0, len(shells))
	for _, s := range shells {
		cfgShells = append(cfgShells, config.ShellEntry{
			ID:      s.ID,
			Name:    s.Name,
			Command: s.Command,
			Args:    append([]string(nil), s.Args...),
		})
	}
	cfgProfiles := make([]config.TerminalProfile, 0, len(profiles))
	for _, p := range profiles {
		cfgProfiles = append(cfgProfiles, config.TerminalProfile{
			ID:         p.ID,
			Name:       p.Name,
			TerminalID: p.TerminalID,
			ShellID:    p.ShellID,
			Args:       append([]string(nil), p.Args...),
			Default:    p.Default,
			Preferred:  p.Preferred,
			Hidden:     p.Hidden,
			Source:     p.Source,
		})
	}
	return a.SaveTerminalProfiles(cfgApps, cfgShells, cfgProfiles)
}

// SaveTerminalProfiles overwrites the persisted terminal-profile arrays with
// the caller-supplied set, normalises the Default flag (exactly one), and
// persists.
func (a *App) SaveTerminalProfiles(apps []config.TerminalApp, shells []config.ShellEntry, profiles []config.TerminalProfile) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	terminals.EnforceSingleDefault(profiles)
	a.cfg.Global.TerminalApps = append([]config.TerminalApp(nil), apps...)
	a.cfg.Global.Shells = append([]config.ShellEntry(nil), shells...)
	a.cfg.Global.TerminalProfiles = append([]config.TerminalProfile(nil), profiles...)
	return a.saveConfig()
}

// ─── Sync / re-detect / missing-modern bridges ────────────────────────────

// SyncProfiles reconciles config's TerminalApps + Shells + TerminalProfiles
// arrays with what the host catalog reports as installed. Idempotent — only
// writes when the resulting payload differs from what's already persisted.
func (a *App) SyncProfiles() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if terminals.Sync(a.cfg, runtime.GOOS) {
		_ = a.saveConfig()
	}
}

// RedetectProfiles re-runs SyncProfiles and returns the refreshed config DTO.
func (a *App) RedetectProfiles() ConfigDTO {
	a.SyncProfiles()
	return a.GetConfig()
}

// MissingModernTerminal reports whether the GUI/TUI banner ("install Windows
// Terminal for the best experience") should fire. True only on Windows when
// no shell-token-aware Terminal is installed.
func (a *App) MissingModernTerminal() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return terminals.MissingModernTerminal(a.cfg, runtime.GOOS)
}

// ─── Profile launch (resolves a profile id and routes through the existing
// openTerminal* path so the Windows console-flash workaround stays in one
// place). ─────────────────────────────────────────────────────────────────

// OpenProfile launches the given profile in the given folder.
func (a *App) OpenProfile(path, profileID string) error {
	if profileID == "" {
		return fmt.Errorf("profile id is required")
	}
	a.mu.Lock()
	profile, app, shell, err := a.lookupProfileLocked(profileID)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	template := profile.Args
	if len(template) == 0 {
		template = app.ArgsTemplate
	}
	args := launch.ResolveArgs(launch.ProfileArgs{
		Template:     template,
		Path:         path,
		ShellCommand: shell.Command,
		ShellArgs:    shell.Args,
	})
	// A bare-shell DIRECT Profile (TerminalID=="") points at a console-
	// subsystem .exe (pwsh, cmd, bash, …) that needs the cmd.exe-start
	// wrapper to get a fresh console. Modern terminal apps are GUI-
	// subsystem and launch directly (no wrapper, no console flash).
	isConsole := profile.TerminalID == ""
	return openTerminalRawAt(path, app.Command, args, isConsole)
}

// OpenAccountProfile launches the given profile in the account's parent
// folder.
func (a *App) OpenAccountProfile(accountKey, profileID string) error {
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
	return a.OpenProfile(path, profileID)
}

// lookupProfileLocked resolves a profile id into its (profile, app, shell)
// triple under the App mutex. Caller is responsible for holding a.mu.
func (a *App) lookupProfileLocked(profileID string) (config.TerminalProfile, config.TerminalApp, config.ShellEntry, error) {
	for _, p := range a.cfg.Global.TerminalProfiles {
		if p.ID != profileID {
			continue
		}
		var app config.TerminalApp
		// A bare-shell fallback Profile has TerminalID == "" — synthesize a
		// pseudo-app whose Command is the shell binary directly so the
		// launcher path can call exec.Start without an indirection.
		if p.TerminalID == "" && p.ShellID != "" {
			for _, s := range a.cfg.Global.Shells {
				if s.ID == p.ShellID {
					app = config.TerminalApp{
						ID:      "bare-" + s.ID,
						Name:    s.Name,
						Command: s.Command,
					}
					break
				}
			}
		} else {
			for _, t := range a.cfg.Global.TerminalApps {
				if t.ID == p.TerminalID {
					app = t
					break
				}
			}
		}
		if app.ID == "" {
			return p, app, config.ShellEntry{}, fmt.Errorf("terminal %q for profile %q not found", p.TerminalID, p.ID)
		}
		var shell config.ShellEntry
		if p.ShellID != "" {
			for _, s := range a.cfg.Global.Shells {
				if s.ID == p.ShellID {
					shell = s
					break
				}
			}
		}
		return p, app, shell, nil
	}
	return config.TerminalProfile{}, config.TerminalApp{}, config.ShellEntry{}, fmt.Errorf("profile %q not found", profileID)
}

// ─── Window-mode + sub-process Manager window ─────────────────────────────

// GetWindowMode reports which UI the current process should render. Returns
// "terminals" for the dedicated Profile-editor sub-process spawned by
// OpenTerminalsManagerWindow, and "main" otherwise.
func (a *App) GetWindowMode() string {
	if a.windowMode == "" {
		return "main"
	}
	return a.windowMode
}

// OpenTerminalsManagerWindow spawns the current binary as a sub-process with
// --terminals-window so the Profile editor lives in its own OS window
// (issue #69).
func (a *App) OpenTerminalsManagerWindow() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate gitbox binary: %w", err)
	}
	args := []string{
		"--terminals-window",
		fmt.Sprintf("--parent-pid=%d", os.Getpid()),
	}
	if a.testMode {
		args = append(args, "--test-mode")
	}

	a.terminalsMu.Lock()
	defer a.terminalsMu.Unlock()

	havePrevious := a.terminalsCmd != nil

	cmd := exec.Command(exe, args...)
	// Do NOT call git.HideWindow here — see comment in original implementation:
	// the sub-process is /SUBSYSTEM:WINDOWS Wails GUI; HideWindow=SW_HIDE
	// would translate via STARTUPINFO and make the Manager window start
	// hidden, requiring a second click to reveal it.
	configureChildLifetime(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	linkChildLifetimeToParent(cmd.Process)

	if havePrevious {
		// Subprocess already alive — this spawn is just a dedupe-trigger.
		go func(c *exec.Cmd) { _, _ = c.Process.Wait() }(cmd)
		return nil
	}

	a.terminalsCmd = cmd
	go func(c *exec.Cmd) {
		_, _ = c.Process.Wait()
		a.terminalsMu.Lock()
		if a.terminalsCmd == c {
			a.terminalsCmd = nil
		}
		a.terminalsMu.Unlock()
	}(cmd)

	return nil
}

// ─── Plain-launch helper (no harness splice) ──────────────────────────────

// openTerminalRawAt launches a terminal command + args in a folder without
// any token expansion — args are passed straight through.
//
// Windows callers must pass `isConsole` correctly:
//
//   - true  — `command` is a console-subsystem .exe (pwsh, cmd, bash,
//     wsl, …). Wrap in `cmd.exe /C start "" /D <path>` so it gets a
//     fresh console; without the wrapper, console apps inherit the
//     /SUBSYSTEM:WINDOWS parent's null stdio and exit immediately.
//   - false — `command` is a GUI-subsystem terminal app (wezterm-gui.exe,
//     mintty.exe, alacritty.exe, …). Launch directly. The cmd.exe
//     wrapper is harmful here: it flashes a brief cmd.exe console
//     window before the terminal appears (issue #71 follow-up — the
//     "quick window that flashes and disappears").
func openTerminalRawAt(path, command string, args []string, isConsole bool) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	var cmd *exec.Cmd
	if isWindows() {
		if isConsole {
			startArgs := make([]string, 0, 6+len(args))
			startArgs = append(startArgs, "/C", "start", "", "/D", path, command)
			startArgs = append(startArgs, args...)
			cmd = exec.Command("cmd.exe", startArgs...)
			git.HideWindow(cmd)
			cmd.Env = sanitizeWindowsTerminalEnv(git.Environ())
		} else {
			cmd = exec.Command(command, args...)
			cmd.Dir = path
			cmd.Env = sanitizeWindowsTerminalEnv(git.Environ())
		}
	} else {
		cmd = exec.Command(command, args...)
		cmd.Dir = path
		cmd.Env = git.Environ()
	}
	return cmd.Start()
}
