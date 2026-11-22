package remote

import (
	"os/exec"
	"path"
	"strings"
)

// The local launches of a remote window (#243): an editor that opens a
// folder of the host, and a local terminal that runs ssh -t into it. The
// folder comes from the server, which the client does not trust, so it
// never reaches a shell by concatenation: it is validated, quoted for the
// remote login shell (shellquote.go), and every local layer it then crosses
// escapes it again, from the inside out.

const (
	// CodeNotAvailablePath: the folder cannot be opened from this computer:
	// it is not a valid path of the host, or one of the layers it would
	// cross on this computer cannot carry it safely.
	CodeNotAvailablePath Code = "not_available_remote_path"
	// CodeHarnessNotAllowed: the AI harness the window asked for is not one
	// of the catalog's.
	CodeHarnessNotAllowed Code = "harness_not_allowed"
)

// MaxRemotePath is the longest folder a remote launch takes, in bytes.
const MaxRemotePath = 4096

// ValidAlias reports whether alias can name a remote: an ssh_config alias
// of 1 to 64 of [A-Za-z0-9._-] that does not start with "-".
func ValidAlias(alias string) bool {
	return aliasRE.MatchString(alias) && !strings.HasPrefix(alias, "-")
}

// hasControl reports whether s holds a NUL, a CR or a LF: none survives a
// command line intact, and a newline would end the remote command.
func hasControl(s string) bool { return strings.ContainsAny(s, "\x00\r\n") }

// ValidateRemotePath checks a folder of a host before anything starts: it
// must be absolute for hostOS ("windows", or any POSIX system), hold no NUL,
// CR or LF, and be at most MaxRemotePath bytes.
func ValidateRemotePath(hostOS, p string) error {
	switch {
	case p == "", len(p) > MaxRemotePath:
		return newError(CodeNotAvailablePath, "the folder is empty or longer than %d bytes", MaxRemotePath)
	case hasControl(p):
		return newError(CodeNotAvailablePath, "the folder holds a control character")
	case !isAbsFor(hostOS, p):
		return newError(CodeNotAvailablePath, "the folder is not an absolute path of the host")
	}
	return nil
}

func isAbsFor(hostOS, p string) bool {
	if hostOS != "windows" {
		return path.IsAbs(p)
	}
	if strings.HasPrefix(p, `\\`) {
		return len(p) > 2
	}
	return len(p) >= 3 && isLetter(p[0]) && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

func isLetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// RemoteCommand returns the one string ssh hands to the remote login shell:
// change into dir, then run program, or the login shell when program is
// empty. Every word is quoted for family. The caller has checked dir with
// ValidateRemotePath; a word with a control character, or one family
// cannot carry, is refused.
//
//   - POSIX: cd -- '<dir>' && exec "$SHELL" -l
//   - fish: cd '<dir>'; and exec "$SHELL" -l    (also before fish 3.0)
//   - other: exec sh -c 'cd -- "$1" && exec "${SHELL:-/bin/sh}" -l' sh '<dir>'
//   - cmd: cd /d "<dir>" && cmd.exe /k
//   - PowerShell: Set-Location -LiteralPath '<dir>'; if ($?) { & (Get-Process -Id $PID).Path -NoLogo }
//
// With a program, the POSIX and fish forms end in exec '<cmd>' '<arg>'…,
// the other form in exec "$@", cmd runs "<cmd>" "<arg>"… and PowerShell
// & '<cmd>' '<arg>'….
func RemoteCommand(family Family, dir string, program []string) (string, error) {
	if hasControl(dir) {
		return "", newError(CodeNotAvailablePath, "the folder holds a control character")
	}
	for _, w := range program {
		if w == "" || hasControl(w) {
			return "", newError(CodeHarnessNotAllowed, "the command holds an empty word or a control character")
		}
	}
	words := func(q func(string) string) string {
		out := make([]string, len(program))
		for i, w := range program {
			out[i] = q(w)
		}
		return strings.Join(out, " ")
	}
	switch family {
	case FamilyPOSIX:
		then := `"$SHELL" -l`
		if len(program) > 0 {
			then = words(QuotePOSIX)
		}
		return "cd -- " + QuotePOSIX(dir) + " && exec " + then, nil
	case FamilyFish:
		then := `"$SHELL" -l`
		if len(program) > 0 {
			then = words(QuoteFish)
		}
		return "cd " + QuoteFish(dir) + "; and exec " + then, nil
	case FamilyCmd:
		qdir, ok := QuoteCmd(dir)
		if !ok {
			return "", newError(CodeNotAvailablePath, "cmd.exe cannot carry this folder safely")
		}
		then := "cmd.exe /k"
		if len(program) > 0 {
			quoted := make([]string, len(program))
			for i, w := range program {
				if quoted[i], ok = QuoteCmd(w); !ok {
					return "", newError(CodeHarnessNotAllowed, "cmd.exe cannot carry this command safely")
				}
			}
			then = strings.Join(quoted, " ")
		}
		return "cd /d " + qdir + " && " + then, nil
	case FamilyPowerShell:
		then := "& (Get-Process -Id $PID).Path -NoLogo"
		if len(program) > 0 {
			then = "& " + words(QuotePowerShell)
		}
		return "Set-Location -LiteralPath " + QuotePowerShell(dir) + "; if ($?) { " + then + " }", nil
	}
	if len(program) > 0 {
		return `exec sh -c 'cd -- "$1" && shift && exec "$@"' sh ` + QuotePOSIX(dir) + " " + words(QuotePOSIX), nil
	}
	return `exec sh -c 'cd -- "$1" && exec "${SHELL:-/bin/sh}" -l' sh ` + QuotePOSIX(dir), nil
}

// SSHPath returns the system ssh, or the one a test set in o.
func SSHPath(o Options) (string, error) {
	if o.sshPath != "" {
		return o.sshPath, nil
	}
	p, err := exec.LookPath("ssh")
	if err != nil {
		return "", newError(CodeSSHMissing, "no ssh command was found on this computer")
	}
	return p, nil
}

// TerminalArgv is the local command of a remote terminal: ssh -t, the
// alias, and the remote command as one argument. "--" ends ssh's options,
// so nothing after the alias is read as one. It adds no -o option: the
// user's ssh_config decides, and a prompt shows in the terminal.
func TerminalArgv(ssh, alias, remoteCommand string) ([]string, error) {
	if !ValidAlias(alias) {
		return nil, newError(CodeSSHConfig, "the alias is not a valid ssh_config alias")
	}
	if hasControl(remoteCommand) {
		return nil, newError(CodeNotAvailablePath, "the remote command holds a control character")
	}
	return []string{ssh, "-t", "--", alias, remoteCommand}, nil
}

// RemoteEditor is an editor that opens a folder of a host itself, over the
// user's ssh, with --remote ssh-remote+<alias> <folder>.
type RemoteEditor struct {
	// Command is the name of its command, without folder or extension.
	Command string
	Name    string
	// Verified records how the entry was checked (#243).
	Verified string
}

// RemoteEditors is the one list of editors that open remote folders. Any
// other editor shows disabled in a remote window.
var RemoteEditors = []RemoteEditor{
	{Command: "code", Name: "VS Code", Verified: "the --remote ssh-remote+<alias> <folder> form of the Remote-SSH documentation; remote smoke from macOS and Windows clients"},
	{Command: "code-insiders", Name: "VS Code Insiders", Verified: "the same command line as code, from the Remote-SSH documentation; no smoke of its own"},
}

// EditorName is the name of an editor command: its last path element,
// lowercased, without .exe, .cmd or .bat.
func EditorName(command string) string {
	i := strings.LastIndexAny(command, `/\`)
	name := strings.ToLower(command[i+1:])
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

// EditorSupportsRemote reports whether command is one of RemoteEditors.
func EditorSupportsRemote(command string) bool {
	name := EditorName(command)
	for _, e := range RemoteEditors {
		if e.Command == name {
			return true
		}
	}
	return false
}

// RemoteEditorCommands lists the command names of RemoteEditors.
func RemoteEditorCommands() []string {
	out := make([]string, len(RemoteEditors))
	for i, e := range RemoteEditors {
		out[i] = e.Command
	}
	return out
}

// EditorArgv is the argv that opens dir of alias in the editor command:
// every part its own element, and no shell in between. The caller has
// checked dir with ValidateRemotePath.
func EditorArgv(command, alias, dir string) ([]string, error) {
	if !EditorSupportsRemote(command) {
		return nil, newError(CodeNotAvailablePath, "this editor cannot open folders on remote hosts")
	}
	if !ValidAlias(alias) {
		return nil, newError(CodeSSHConfig, "the alias is not a valid ssh_config alias")
	}
	if hasControl(dir) {
		return nil, newError(CodeNotAvailablePath, "the folder holds a control character")
	}
	return []string{command, "--remote", "ssh-remote+" + alias, dir}, nil
}

// batchUnsafe are the characters cmd.exe interprets when a batch file
// expands its arguments with %*, whether Go quoted the argument or not.
const batchUnsafe = "%!^&|<>\""

// IsBatchFile reports whether a Windows command is a batch file, which
// cmd.exe runs and parses: VS Code's code.cmd is one.
func IsBatchFile(command string) bool {
	l := strings.ToLower(command)
	return strings.HasSuffix(l, ".cmd") || strings.HasSuffix(l, ".bat")
}

// CheckBatchArgs refuses an argument that a batch file cannot receive
// safely: on a Windows client, VS Code's code.cmd re-parses the folder.
func CheckBatchArgs(args []string) error {
	for _, a := range args {
		if strings.ContainsAny(a, batchUnsafe) || hasControl(a) {
			return newError(CodeNotAvailablePath, "the editor's batch launcher cannot carry this folder safely on Windows")
		}
	}
	return nil
}

// LocalShellLine is the command line a macOS Terminal.app or iTerm window
// runs through my local login shell: exec, then argv quoted for that shell
// (fish, or POSIX for every other).
func LocalShellLine(localShell string, argv []string) string {
	quote := QuotePOSIX
	if ShellFamily(localShell) == FamilyFish {
		quote = QuoteFish
	}
	parts := make([]string, 0, 1+len(argv))
	parts = append(parts, "exec")
	for _, a := range argv {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}
