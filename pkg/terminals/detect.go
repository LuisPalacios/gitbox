package terminals

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/doctor"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// ─── Probe helpers ────────────────────────────────────────────────────────

// probeBinary returns a Probe that walks `names` in order and reports the
// first match found via lookPathWithBrewPATH. Empty when nothing resolves.
func probeBinary(names ...string) func() (string, bool) {
	return func() (string, bool) {
		for _, n := range names {
			if p, err := lookPathWithBrewPATH(n); err == nil {
				return p, true
			}
		}
		return "", false
	}
}

// macAppTerminal builds a CatalogTerminal entry for an /Applications app
// bundle that gitbox launches via `open -a <bundle>`. Both the bundle name
// (used at probe time) and the display name are exposed because some bundle
// names differ from the user-visible label (kitty.app vs Kitty).
func macAppTerminal(id, displayName, bundleBaseName string) CatalogTerminal {
	bundleName := bundleBaseName
	probe := probeMacAppBundle(bundleBaseName)
	resolved := []string{"-a", bundleName}
	return CatalogTerminal{
		ID:   id,
		Name: displayName,
		OS:   "darwin",
		Probe: func() (string, bool) {
			if !probe() {
				return "", false
			}
			return "open", true
		},
		ProbeArgs: func() []string {
			out := make([]string, len(resolved))
			copy(out, resolved)
			return out
		},
		ArgsTemplate: append([]string(nil), resolved...),
	}
}

// probeMacAppBundle returns a probe that checks the conventional macOS app
// bundle install locations for `<name>.app`. Returns true on first hit.
func probeMacAppBundle(name string) func() bool {
	return func() bool {
		bundle := name + ".app"
		roots := []string{"/Applications", "/System/Applications", "/System/Applications/Utilities", "/Applications/Utilities"}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			roots = append(roots, filepath.Join(home, "Applications"))
		}
		for _, root := range roots {
			if _, err := os.Stat(filepath.Join(root, bundle)); err == nil {
				return true
			}
		}
		return false
	}
}

// macAppTerminalWithArgs is the same as macAppTerminal but with caller-
// supplied ArgsTemplate. Used by mac entries that need the `open` Launch-
// Services entry point (so the bundle registers with the window server)
// but pass extra argv to the app via `open --args …` (e.g. WezTerm's
// `--cwd <path>` flag). Probe still looks for the .app bundle in the
// conventional locations; only the launch argv differs.
func macAppTerminalWithArgs(id, displayName, bundleBaseName string, argsTemplate []string) CatalogTerminal {
	probe := probeMacAppBundle(bundleBaseName)
	return CatalogTerminal{
		ID:   id,
		Name: displayName,
		OS:   "darwin",
		Probe: func() (string, bool) {
			if !probe() {
				return "", false
			}
			return "open", true
		},
		ProbeArgs:    func() []string { return nil },
		ArgsTemplate: append([]string(nil), argsTemplate...),
	}
}

// macBundleCLITerminal builds a CatalogTerminal entry for a macOS app whose
// `open -a <App> <folder>` doesn't honour the folder argument as a cwd
// (the app either silently drops it — WezTerm — or surfaces a Finder
// "cannot open in 'folder' format" error — Alacritty). Instead, gitbox
// invokes the app's own CLI binary inside the bundle directly, which DOES
// accept a working-directory flag.
//
// Probe walks the conventional .app install locations and stats
// <bundle>/Contents/MacOS/<binaryName>. ArgsTemplate is the caller-supplied
// argv with the {path} token; the launch path expands it via
// pkg/launch.ResolveArgs the same way it does for Windows/Linux entries.
func macBundleCLITerminal(id, displayName, bundleBaseName, binaryName string, argsTemplate []string) CatalogTerminal {
	bundle := bundleBaseName + ".app"
	return CatalogTerminal{
		ID:   id,
		Name: displayName,
		OS:   "darwin",
		Probe: func() (string, bool) {
			roots := []string{"/Applications", "/System/Applications", "/System/Applications/Utilities", "/Applications/Utilities"}
			if home, err := os.UserHomeDir(); err == nil && home != "" {
				roots = append(roots, filepath.Join(home, "Applications"))
			}
			for _, root := range roots {
				bin := filepath.Join(root, bundle, "Contents", "MacOS", binaryName)
				if _, err := os.Stat(bin); err == nil {
					return bin, true
				}
			}
			return "", false
		},
		ArgsTemplate: append([]string(nil), argsTemplate...),
	}
}

// probeWT resolves wt.exe — App Execution Alias under
// %LOCALAPPDATA%\Microsoft\WindowsApps takes precedence over PATH so the
// config doesn't depend on PATH.
func probeWT() (string, bool) {
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

// probeGitBashShell resolves the real Git for Windows bash.exe (the actual
// shell binary, not the git-bash.exe launcher). Tries the Git install
// paths under Program Files first; deliberately does NOT fall back to
// LookPath("bash.exe") because on Windows that returns
// C:\Windows\System32\bash.exe, which is the WSL launcher — the wrong
// shell for "Git Bash". Returns ("", false) when Git for Windows isn't
// installed.
func probeGitBashShell() (string, bool) {
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		// `Git\bin\bash.exe` is the Git wrapper bash that sets up the
		// expected env; `Git\usr\bin\bash.exe` is the bare MSYS2 bash.
		// Prefer the wrapper.
		for _, sub := range []string{filepath.Join("Git", "bin", "bash.exe"), filepath.Join("Git", "usr", "bin", "bash.exe")} {
			cand := filepath.Join(root, sub)
			if _, err := os.Stat(cand); err == nil {
				return cand, true
			}
		}
	}
	return "", false
}

// probeMintty resolves mintty.exe. Git for Windows ships its own copy under
// `C:\Program Files\Git\usr\bin\mintty.exe` that PATH usually misses, so we
// fall back to the well-known Git install paths after a generic LookPath.
func probeMintty() (string, bool) {
	if p, err := exec.LookPath("mintty.exe"); err == nil {
		return p, true
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		cand := filepath.Join(root, "Git", "usr", "bin", "mintty.exe")
		if _, err := os.Stat(cand); err == nil {
			return cand, true
		}
	}
	return "", false
}

// probePwsh resolves pwsh.exe — PowerShell 7 ships under Program Files\
// PowerShell\7 and is sometimes missing from PATH on minimal installs.
func probePwsh() (string, bool) {
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		return p, true
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		cand := filepath.Join(root, "PowerShell", "7", "pwsh.exe")
		if _, err := os.Stat(cand); err == nil {
			return cand, true
		}
	}
	return "", false
}

// wslDistros lists the installed WSL distros; tests replace it.
var wslDistros = doctor.WSLDistros

// probeWSL resolves wsl.exe only when a distro is installed. The file alone
// proves nothing: Windows ships it as a stub that prompts to install WSL when
// launched, so neither detection nor the profiles it feeds may rely on it, and
// detection must never run it (see doctor.WSLDistros).
func probeWSL() (string, bool) {
	if len(wslDistros()) == 0 {
		return "", false
	}
	return probeBinary("wsl.exe")()
}

// lookPathWithBrewPATH resolves a command using the Homebrew-augmented PATH.
// On macOS, GUI apps inherit a minimal PATH that excludes /opt/homebrew/bin
// and /usr/local/bin, so Homebrew-installed binaries are invisible to a bare
// exec.LookPath. Falls back to standard LookPath on non-macOS platforms.
func lookPathWithBrewPATH(command string) (string, error) {
	if strings.ContainsAny(command, `/\`) {
		return exec.LookPath(command)
	}
	for _, e := range git.Environ() {
		if !strings.HasPrefix(e, "PATH=") {
			continue
		}
		for _, dir := range filepath.SplitList(strings.TrimPrefix(e, "PATH=")) {
			if dir == "" || !filepath.IsAbs(dir) {
				continue
			}
			if resolved, err := exec.LookPath(filepath.Join(dir, command)); err == nil {
				return resolved, nil
			}
		}
		break
	}
	return exec.LookPath(command)
}

// ─── DetectTerminals / DetectShells ───────────────────────────────────────

// DetectTerminals walks the catalog for `goos` and returns one TerminalApp
// per installed entry, in catalog order. ID collisions are skipped (catalog
// invariant — the catalog is unique by ID per OS, but the merge with prior
// config later in Sync handles user-renamed entries).
func DetectTerminals(goos string) []config.TerminalApp {
	cat := catalogTerminalsFor(goos)
	if len(cat) == 0 {
		return nil
	}
	out := make([]config.TerminalApp, 0, len(cat))
	seen := make(map[string]bool, len(cat))
	for _, c := range cat {
		cmd, ok := c.Probe()
		if !ok {
			continue
		}
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		out = append(out, config.TerminalApp{
			ID:           c.ID,
			Name:         c.Name,
			Command:      cmd,
			ArgsTemplate: append([]string(nil), c.ArgsTemplate...),
		})
	}
	return out
}

// DetectShells walks the shell catalog for `goos` and returns one ShellEntry
// per installed entry, in catalog order. On Windows, when WSL is usable,
// per-distro entries are appended after the bare "wsl" entry — the bare
// entry is kept (issue #71) so it can back the standalone "DIRECT" wsl
// Profile that ComposeProfiles emits as a hidden-by-default shortcut.
func DetectShells(goos string) []config.ShellEntry {
	cat := catalogShellsFor(goos)
	if len(cat) == 0 {
		return nil
	}
	out := make([]config.ShellEntry, 0, len(cat))
	seen := make(map[string]bool, len(cat))
	var wslCmd string
	for _, c := range cat {
		cmd, ok := c.Probe()
		if !ok {
			continue
		}
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		out = append(out, config.ShellEntry{
			ID:      c.ID,
			Name:    c.Name,
			Command: cmd,
			Args:    append([]string(nil), c.Args...),
		})
		if c.ID == "wsl" {
			wslCmd = cmd
		}
	}
	// Per-distro WSL entries (Windows only) — appended after the bare wsl
	// row so the catalog-vs-runtime split is preserved.
	if goos == "windows" && wslCmd != "" {
		for _, d := range wslDistros() {
			out = append(out, config.ShellEntry{
				ID:      "wsl-" + slugifyASCII(d),
				Name:    "WSL — " + d,
				Command: wslCmd,
				Args:    []string{"-d", d},
			})
		}
	}
	return out
}

// ─── Login-shell detection (macOS / Linux) ────────────────────────────────

// LoginShellID returns the shell id matching the current user's login shell
// from $SHELL (or /etc/passwd as a fallback). Returns ("", false) on Windows,
// when the user's shell is not in the known set, or when probing fails. Used
// to mark a sensible Default Profile on Unix hosts.
func LoginShellID(goos string) (string, bool) {
	if goos == "windows" {
		return "", false
	}
	shellPath := strings.TrimSpace(os.Getenv("SHELL"))
	if shellPath == "" {
		shellPath = readLoginShellFromPasswd()
	}
	if shellPath == "" {
		return "", false
	}
	switch strings.ToLower(filepath.Base(shellPath)) {
	case "bash":
		return "bash", true
	case "zsh":
		return "zsh", true
	case "fish":
		return "fish", true
	case "ksh":
		return "ksh", true
	case "dash":
		return "dash", true
	}
	return "", false
}

// readLoginShellFromPasswd returns the trailing shell field of the current
// user's /etc/passwd entry, or "" on miss. Best-effort fallback for hosts
// where $SHELL is unset.
func readLoginShellFromPasswd() string {
	uid := os.Getuid()
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return ""
	}
	uidStr := intToString(uid)
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[2] == uidStr {
			return strings.TrimSpace(fields[6])
		}
	}
	return ""
}

// ─── Slug helper ──────────────────────────────────────────────────────────

// slugifyASCII lower-cases s and replaces every non-[a-z0-9] run with a
// single hyphen, trimming leading/trailing hyphens. Stable across runs and
// OS-locale-independent. Matches pkg/config/migrate_terminals.go::slugify.
func slugifyASCII(s string) string {
	if s == "" {
		return ""
	}
	out := make([]rune, 0, len(s))
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out = append(out, r)
			prevDash = false
		default:
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	return strings.TrimRight(string(out), "-")
}

// intToString converts a non-negative int to its decimal string. Avoids the
// fmt import in the detection hot path so this package stays import-light.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
