package launch

import (
	"encoding/base64"
	"slices"
	"strings"
	"unicode/utf16"
)

// TemplateAcceptsCommand reports whether a terminal args template can host a
// command: it must carry at least one shell token or the {command} token.
// Templates like macOS `open -a Warp {path}` have neither, so a harness has
// nowhere to go and the caller must refuse the launch instead of silently
// opening a plain terminal.
func TemplateAcceptsCommand(tmpl []string) bool {
	for _, a := range tmpl {
		switch a {
		case TokenShellCommand, TokenShellArgs, TokenCommand:
			return true
		}
	}
	return false
}

// TemplateHasToken reports whether tmpl contains the whole-arg token.
func TemplateHasToken(tmpl []string, token string) bool {
	return slices.Contains(tmpl, token)
}

// shellFamily classifies a shell binary by its basename so HarnessInShell
// can pick the right "run this, then stay interactive" syntax.
type shellFamily int

const (
	familyPOSIX      shellFamily = iota // bash, zsh, sh, dash, ksh, git-bash …
	familyPowerShell                    // pwsh, powershell
	familyCmd                           // cmd
	familyFish                          // fish
	familyWSL                           // wsl launcher (harness runs inside the distro)
)

// baseName returns the last path element of p, splitting on both '/' and
// '\' so Windows paths classify correctly on any host (tests run on Linux
// too), lowercased with a trailing .exe stripped.
func baseName(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	b := strings.ToLower(p[i+1:])
	return strings.TrimSuffix(b, ".exe")
}

func classifyShell(shellCommand string) shellFamily {
	switch baseName(shellCommand) {
	case "pwsh", "powershell":
		return familyPowerShell
	case "cmd":
		return familyCmd
	case "fish":
		return familyFish
	case "wsl":
		return familyWSL
	}
	return familyPOSIX
}

// HarnessInShell returns the shell command + args that run harnessArgv
// inside the given shell and leave an interactive shell behind when the
// harness exits. Running through the shell (rather than handing the harness
// binary straight to the terminal) keeps the user's rc files — nvm/npm PATH
// on macOS and Linux, Git Bash PATH on Windows — which is where most
// harness CLIs actually live.
//
// Per family:
//
//   - PowerShell: `-NoExit -EncodedCommand <base64 UTF-16LE>` of
//     `& '<cmd>' '<arg>'…`. The encoded form survives Windows Terminal's
//     argv rebuild and the cmd.exe `start` wrapper without any quoting.
//   - cmd: `/K <cmd> <args>` as separate argv items (Go quotes items with
//     spaces; cmd.exe keeps a single quoted pair intact).
//   - fish: `-C '<cmd> <args>'` — fish runs -C before going interactive.
//   - wsl: `-- sh -lc '<cmd> <args>; exec "${SHELL:-sh}" -l'` after the
//     existing distro args.
//   - everything else (bash, zsh, sh, dash, ksh, git-bash): shell args plus
//     `-i` (if absent) and `-c '<cmd> <args>; exec <shell> <shell args>'`.
//
// On Windows, POSIX-family shells cannot run an npm `.cmd` shim, so Git Bash
// receives shims by bare name (npm installs an extensionless sh shim on the
// same PATH) and other paths with forward slashes; WSL always gets the bare
// name. Native Windows shells get the command verbatim (normally the
// resolved full path). See harnessCommandFor.
func HarnessInShell(shellCommand string, shellArgs []string, harnessArgv []string, goos string) (string, []string) {
	fam := classifyShell(shellCommand)
	harness := append([]string(nil), harnessArgv...)
	if len(harness) > 0 {
		harness[0] = harnessCommandFor(harness[0], fam, goos)
	}
	base := append([]string(nil), shellArgs...)

	switch fam {
	case familyPowerShell:
		return shellCommand, append(base, "-NoExit", "-EncodedCommand", psEncode(psInvocation(harness)))
	case familyCmd:
		return shellCommand, append(append(base, "/K"), harness...)
	case familyFish:
		return shellCommand, append(base, "-C", JoinPOSIX(harness))
	case familyWSL:
		script := JoinPOSIX(harness) + `; exec "${SHELL:-sh}" -l`
		return shellCommand, append(base, "--", "sh", "-lc", script)
	}

	// POSIX default.
	interactive := false
	for _, a := range base {
		if a == "-i" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "i")) {
			interactive = true
			break
		}
	}
	args := base
	if !interactive {
		args = append(args, "-i")
	}
	relaunch := append([]string{shellCommand}, base...)
	script := JoinPOSIX(harness) + "; exec " + JoinPOSIX(relaunch)
	return shellCommand, append(args, "-c", script)
}

// harnessCommandFor adapts the stored harness command to the shell that will
// run it. Native Windows shells (pwsh, powershell, cmd) and every non-Windows
// host get it verbatim. On Windows:
//
//   - WSL always gets the bare name (`claude`): a Windows path is meaningless
//     inside the distro, and the harness is resolved on the distro's PATH.
//   - Git Bash / MSYS shells get npm `.cmd`/`.bat`/`.ps1` shims by bare name
//     (bash cannot exec them, and npm also installs an extensionless sh shim
//     on the same PATH). Any other path is kept but with forward slashes,
//     which MSYS bash accepts (`C:/Users/me/.local/bin/claude.exe`).
func harnessCommandFor(command string, fam shellFamily, goos string) string {
	if goos != "windows" {
		return command
	}
	if fam == familyPowerShell || fam == familyCmd {
		return command
	}
	if fam == familyWSL {
		return bareCommandName(command)
	}
	if hasShimExt(command) {
		return bareCommandName(command)
	}
	return strings.ReplaceAll(command, `\`, "/")
}

// bareCommandName strips directories and a Windows extension.
func bareCommandName(command string) string {
	b := baseName(command) // already lowercases + trims .exe
	for _, ext := range []string{".cmd", ".bat", ".ps1"} {
		b = strings.TrimSuffix(b, ext)
	}
	return b
}

func hasShimExt(s string) bool {
	l := strings.ToLower(s)
	return strings.HasSuffix(l, ".cmd") || strings.HasSuffix(l, ".bat") || strings.HasSuffix(l, ".ps1")
}

// psQuote wraps s in PowerShell single quotes, doubling embedded ones.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// psInvocation renders `& '<cmd>' '<arg>'…` — the call operator form that
// accepts paths with spaces and never reinterprets arguments.
func psInvocation(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	parts := make([]string, 0, 1+len(argv))
	parts = append(parts, "&")
	for _, a := range argv {
		parts = append(parts, psQuote(a))
	}
	return strings.Join(parts, " ")
}

// psEncode produces the -EncodedCommand payload: base64 of the script in
// UTF-16LE, which is what both Windows PowerShell 5 and pwsh 7 expect.
func psEncode(script string) string {
	u := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(u)*2)
	for _, c := range u {
		buf = append(buf, byte(c), byte(c>>8))
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// PSDecode is the inverse of the -EncodedCommand encoding. Exposed for tests
// and diagnostics only.
func PSDecode(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	u := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		u = append(u, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	return string(utf16.Decode(u)), nil
}
