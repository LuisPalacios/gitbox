package main

import (
	"errors"
	"os"
	"runtime"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/harness"
	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/terminals"
)

// The local launches of a remote window (#243). The folder belongs to the
// host and comes from its server, which this window does not trust; the
// launch runs on this computer. An editor that opens remote folders gets
// --remote ssh-remote+<alias> <folder>; a terminal profile or an AI harness
// gets a local terminal that runs ssh -t <alias> into the folder. Nothing
// of the host reaches a shell unquoted: pkg/remote validates the folder,
// quotes it for the host's login shell and builds every local layer.
//
// What has no local equivalent (the file manager, the account folder, the
// host's config file) is disabled with not_available_remote.

const (
	// remoteNotAvailable: the action has no remote equivalent.
	remoteNotAvailable remoteCode = "not_available_remote"
)

var (
	errProfileRequired   = errors.New("profile id is required")
	errNoTerminalProfile = errors.New("configure a terminal profile first (Settings → Terminals → Manager)")
)

// refuseInRemote stops an action that has no remote equivalent.
func (a *App) refuseInRemote() error {
	if a.remoteWindow() {
		return &remoteError{remoteNotAvailable, "not available for remote hosts"}
	}
	return nil
}

// remoteTarget is where a remote window's launches go: the alias of this
// window, which is the local remotes list's and never the server's, the
// host's system and the quoting family of its login shell.
type remoteTarget struct {
	alias  string
	hostOS string
	family remote.Family
}

func (a *App) remoteTarget() (remoteTarget, error) {
	s := a.remote
	backend, ok := a.be().(*RemoteBackend)
	if s == nil || !ok || backend.current() == nil {
		return remoteTarget{}, &remoteError{remoteNotConnected, "not connected to the remote"}
	}
	if _, ok := findRemote(a.clientPrefs().Get().Remotes, s.alias); !ok || !remote.ValidAlias(s.alias) {
		return remoteTarget{}, &remoteError{remoteUnknownAlias, "this remote is no longer configured on this computer"}
	}
	s.mu.Lock()
	probe := s.probe
	s.mu.Unlock()
	t := remoteTarget{alias: s.alias, hostOS: probe.OS, family: remote.ShellFamily(probe.Shell)}
	if t.hostOS == "" {
		t.hostOS = backend.Hello().OS
	}
	return t, nil
}

// remoteClientOS is the system of this computer as the remote terminal
// launch sees it. Tests set it to take the Windows branch on any host.
var remoteClientOS = runtime.GOOS

// remoteSpawn starts what a remote launch built. Tests replace it to see
// the launch without starting anything.
var remoteSpawn = spawnRemote

// remoteSpawnSpec is one remote launch, in one of three shapes.
type remoteSpawnSpec struct {
	// Console is ssh's argv on a Windows client: a console of its own.
	Console []string
	// Editor is an editor's argv.
	Editor []string
	// Terminal is a terminal emulator on macOS or Linux, or an argv-safe
	// one on Windows (terminals.ProgramArgvSafe).
	Terminal terminals.Launch
	// Dir is the local working folder of the terminal.
	Dir string
}

func spawnRemote(s remoteSpawnSpec) error {
	switch {
	case len(s.Console) > 0:
		return remote.StartConsole(s.Console, s.Dir, sanitizeWindowsTerminalEnv(git.Environ()))
	case len(s.Editor) > 0:
		return startEditor(s.Editor)
	}
	return runLaunch(s.Dir, s.Terminal)
}

// prefsGlobal is the global block of this computer's client preferences:
// its editors, terminal apps, shells and profiles.
func (a *App) prefsGlobal() config.GlobalConfig {
	scratch := &config.Config{}
	config.ApplyPrefs(scratch, a.clientPrefs().Get())
	return scratch.Global
}

// openRemoteEditor opens dir of the host in an editor that supports remote
// folders.
func (a *App) openRemoteEditor(dir, command string) error {
	t, err := a.remoteTarget()
	if err != nil {
		return err
	}
	if err := remote.ValidateRemotePath(t.hostOS, dir); err != nil {
		return err
	}
	argv, err := remote.EditorArgv(command, t.alias, dir)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// VS Code's code.cmd is a batch file: cmd.exe parses the folder.
		if resolved, err := lookPathWithBrewPATH(command); err == nil {
			argv[0] = resolved
		}
		if remote.IsBatchFile(argv[0]) {
			if err := remote.CheckBatchArgs(argv[1:]); err != nil {
				return err
			}
		}
	}
	return remoteSpawn(remoteSpawnSpec{Editor: argv})
}

// openRemoteTerminal opens a local terminal that runs ssh -t into dir of
// the host, then program there, or the login shell when program is empty.
//
// On macOS and Linux the terminal profile's app runs ssh as its own
// program (terminals.ResolveProgramLaunch). On a Windows client only a
// terminal that takes its program as argv does (WezTerm, Alacritty with
// their catalog template: terminals.ProgramArgvSafe, #286); every other
// profile starts ssh.exe in a console of its own (remote.StartConsole),
// because Windows Terminal, cmd.exe and the other launchers re-parse a
// command line.
func (a *App) openRemoteTerminal(dir, profileID string, program []string) error {
	t, err := a.remoteTarget()
	if err != nil {
		return err
	}
	if err := remote.ValidateRemotePath(t.hostOS, dir); err != nil {
		return err
	}
	cmdline, err := remote.RemoteCommand(t.family, dir, program)
	if err != nil {
		return err
	}
	ssh, err := remote.SSHPath(remote.Options{})
	if err != nil {
		return err
	}
	argv, err := remote.TerminalArgv(ssh, t.alias, cmdline)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	global := a.prefsGlobal()
	if remoteClientOS == "windows" && (profileID == "" || !terminals.ProgramArgvSafe(global, profileID, remoteClientOS)) {
		return remoteSpawn(remoteSpawnSpec{Console: argv, Dir: home})
	}
	if profileID == "" {
		return errProfileRequired
	}
	l, err := terminals.ResolveProgramLaunch(terminals.LaunchRequest{
		Global:    global,
		ProfileID: profileID,
		Path:      home,
		GOOS:      remoteClientOS,
	}, argv, remote.LocalShellLine(os.Getenv("SHELL"), argv))
	if err != nil {
		return err
	}
	return remoteSpawn(remoteSpawnSpec{Terminal: l, Dir: home})
}

// openRemoteHarness runs an AI harness of the catalog in dir of the host,
// in a terminal of the default profile.
func (a *App) openRemoteHarness(dir, command string, args []string) error {
	cmd, ok := harness.RemoteCommand(command, args)
	if !ok {
		return &remote.Error{Code: remote.CodeHarnessNotAllowed, Detail: "only an AI harness of the catalog runs on a remote host"}
	}
	profileID := ""
	if p, ok := terminals.DefaultLaunchProfile(a.prefsGlobal()); ok {
		profileID = p.ID
	} else if remoteClientOS != "windows" {
		// A Windows client falls back to a console of its own.
		return errNoTerminalProfile
	}
	return a.openRemoteTerminal(dir, profileID, []string{cmd})
}

// remoteAccountFolder asks the host for an account's folder.
func (a *App) remoteAccountFolder(accountKey string) (string, error) {
	return a.be().AccountFolder(accountKey)
}
