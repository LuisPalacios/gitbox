package terminals

import (
	"fmt"
	"runtime"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// Launch is the fully resolved "what to exec" for a Terminal Profile, used
// by the GUI bridge (cmd/gui/profiles.go).
//
// Exactly one of two shapes is populated:
//
//   - Command + Args (+ Env, IsConsole): exec the terminal binary directly.
//   - AppleScript: run `osascript -e <AppleScript>` instead. Used for macOS
//     Terminal.app and iTerm harness launches, whose `open -a` templates
//     cannot carry a command.
type Launch struct {
	Command string
	Args    []string
	// Env is the #72 user-config env overlay (wezterm.lua launch_menu
	// set_environment_variables). nil means no override.
	Env map[string]string
	// IsConsole is true for bare-shell DIRECT profiles (TerminalID == "")
	// whose Command is a console-subsystem .exe. On Windows the caller must
	// wrap the launch in `cmd.exe /C start` so the shell gets a fresh
	// console; GUI-subsystem terminal apps launch directly.
	IsConsole bool
	// AppleScript, when non-empty, replaces Command/Args entirely.
	AppleScript string
}

// LaunchRequest bundles the inputs to ResolveLaunch.
type LaunchRequest struct {
	Global    config.GlobalConfig
	ProfileID string
	Path      string
	// HarnessArgv, when non-empty, runs the AI harness inside the profile's
	// shell. nil means a plain terminal launch.
	HarnessArgv []string
	// GOOS overrides runtime.GOOS (tests). Empty means the current host.
	GOOS string
	// DefaultShell is used for harness launches when the profile has no
	// shell (Unix profiles use the terminal's implicit login shell). Callers
	// pass $SHELL; empty falls back to /bin/sh.
	DefaultShell string
}

// DefaultLaunchProfile mirrors the GUI launcher's default pick
// (LauncherMenu.svelte): the first visible profile flagged Default, else the
// first visible profile. ok is false only when no visible profile exists.
func DefaultLaunchProfile(g config.GlobalConfig) (config.TerminalProfile, bool) {
	var first *config.TerminalProfile
	for i := range g.TerminalProfiles {
		p := &g.TerminalProfiles[i]
		if p.Hidden {
			continue
		}
		if p.Default {
			return *p, true
		}
		if first == nil {
			first = p
		}
	}
	if first != nil {
		return *first, true
	}
	return config.TerminalProfile{}, false
}

// LookupProfile resolves a profile id into its (profile, app, shell)
// triple. A bare-shell DIRECT profile (TerminalID == "") gets a synthesized
// pseudo-app whose Command is the shell binary so launchers can exec it
// without an indirection.
func LookupProfile(g config.GlobalConfig, profileID string) (config.TerminalProfile, config.TerminalApp, config.ShellEntry, error) {
	for _, p := range g.TerminalProfiles {
		if p.ID != profileID {
			continue
		}
		var shell config.ShellEntry
		if p.ShellID != "" {
			for _, s := range g.Shells {
				if s.ID == p.ShellID {
					shell = s
					break
				}
			}
		}
		var app config.TerminalApp
		if p.TerminalID == "" && p.ShellID != "" {
			if shell.ID != "" {
				app = config.TerminalApp{ID: "bare-" + shell.ID, Name: shell.Name, Command: shell.Command}
			}
		} else {
			for _, t := range g.TerminalApps {
				if t.ID == p.TerminalID {
					app = t
					break
				}
			}
		}
		if app.ID == "" {
			return p, app, shell, fmt.Errorf("terminal %q for profile %q not found", p.TerminalID, p.ID)
		}
		return p, app, shell, nil
	}
	return config.TerminalProfile{}, config.TerminalApp{}, config.ShellEntry{}, fmt.Errorf("profile %q not found", profileID)
}

// ResolveLaunch builds the argv for a plain profile launch or for an AI
// harness launch inside the profile's shell.
//
// Plain launches (HarnessArgv == nil) reproduce the pre-#80 behaviour: the
// profile's Args (or the app's ArgsTemplate), swapped for the user's
// wezterm.lua / WT settings.json override when one matches (#72), expanded
// through launch.ResolveArgs.
//
// Harness launches add, in order:
//
//  1. DIRECT bare-shell profile → the shell itself hosts the harness
//     (launch.HarnessInShell), IsConsole = true.
//  2. macOS Terminal.app / iTerm (`open -a <App>`) → AppleScript.
//  3. Template without a shell or {command} slot → error naming the profile.
//  4. Otherwise the wrapped shell (override shell, else profile shell, else
//     DefaultShell) is spliced into the shell tokens and {command}. Single-
//     slot templates ({shell_command} without {shell_args}) get the whole
//     invocation as one POSIX-quoted string.
func ResolveLaunch(req LaunchRequest) (Launch, error) {
	goos := req.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	profile, app, shell, err := LookupProfile(req.Global, req.ProfileID)
	if err != nil {
		return Launch{}, err
	}
	if app.Command == "" {
		return Launch{}, fmt.Errorf("terminal command for profile %q is empty", profile.ID)
	}
	harness := len(req.HarnessArgv) > 0

	// 1. DIRECT bare shell.
	if profile.TerminalID == "" {
		if !harness {
			args := launch.ResolveArgs(launch.ProfileArgs{Template: profile.Args, Path: req.Path, ShellCommand: shell.Command, ShellArgs: shell.Args})
			return Launch{Command: app.Command, Args: args, IsConsole: true}, nil
		}
		cmd, args := launch.HarnessInShell(shell.Command, shell.Args, req.HarnessArgv, goos)
		return Launch{Command: cmd, Args: args, IsConsole: true}, nil
	}

	template := profile.Args
	if len(template) == 0 {
		template = app.ArgsTemplate
	}
	shellCmd, shellArgs := shell.Command, shell.Args
	var extraEnv map[string]string
	if profile.ShellID != "" {
		if ov, hit := LookupForLaunch(profile.TerminalID, profile.ShellID, shell.Name); hit {
			template = ov.Argv
			extraEnv = ov.Env
			if ov.ShellCommand != "" {
				shellCmd, shellArgs = ov.ShellCommand, ov.ShellArgs
			}
		}
	}

	if !harness {
		args := launch.ResolveArgs(launch.ProfileArgs{Template: template, Path: req.Path, ShellCommand: shellCmd, ShellArgs: shellArgs})
		return Launch{Command: app.Command, Args: args, Env: extraEnv}, nil
	}

	// 2. macOS AppleScript terminals.
	if goos == "darwin" && app.Command == "open" {
		if name, ok := macAppleScriptApp(app.ArgsTemplate); ok {
			return Launch{AppleScript: launch.MacAppleScript(name, req.Path, req.HarnessArgv)}, nil
		}
	}

	// 3. Templates that cannot host a command.
	if !launch.TemplateAcceptsCommand(template) {
		return Launch{}, fmt.Errorf("profile %q (%s) cannot run a command; pick a profile that hosts a shell (Windows Terminal, WezTerm, GNOME Terminal, iTerm, Terminal.app, …) or set it as default", profile.Name, app.Name)
	}

	// 4. Wrap the shell around the harness and splice.
	if shellCmd == "" {
		shellCmd = req.DefaultShell
		if shellCmd == "" {
			shellCmd = "/bin/sh"
		}
		shellArgs = []string{"-l"}
	}
	cmd, args := launch.HarnessInShell(shellCmd, shellArgs, req.HarnessArgv, goos)
	if launch.TemplateHasToken(template, launch.TokenShellCommand) && !launch.TemplateHasToken(template, launch.TokenShellArgs) {
		cmd = launch.JoinPOSIX(append([]string{cmd}, args...))
		args = nil
	}
	invocation := append([]string{cmd}, args...)
	resolved := launch.ResolveArgs(launch.ProfileArgs{
		Template:     template,
		Path:         req.Path,
		ShellCommand: cmd,
		ShellArgs:    args,
		HarnessArgv:  invocation,
	})
	return Launch{Command: app.Command, Args: resolved, Env: extraEnv}, nil
}

// macAppleScriptApp recognises the persisted `open -a Terminal|iTerm`
// template shape. Matching is purely structural so user-renamed profiles
// still route through AppleScript as long as the app template is intact.
func macAppleScriptApp(tmpl []string) (string, bool) {
	for i := 0; i+1 < len(tmpl); i++ {
		if tmpl[i] != "-a" {
			continue
		}
		switch tmpl[i+1] {
		case "Terminal", "iTerm":
			return tmpl[i+1], true
		}
	}
	return "", false
}
