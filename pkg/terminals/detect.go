package terminals

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/LuisPalacios/gitbox/pkg/config"
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

// probeGitBash resolves git-bash.exe — Git for Windows installs it under
// Program Files but does not always wire it into PATH.
func probeGitBash() (string, bool) {
	if p, err := exec.LookPath("git-bash.exe"); err == nil {
		return p, true
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if root == "" {
			continue
		}
		cand := filepath.Join(root, "Git", "git-bash.exe")
		if _, err := os.Stat(cand); err == nil {
			return cand, true
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

// lookPathWithBrewPATH resolves a command using the Homebrew-augmented PATH.
// On macOS, GUI apps inherit a minimal PATH that excludes /opt/homebrew/bin
// and /usr/local/bin, so Homebrew-installed binaries are invisible to a bare
// exec.LookPath. Falls back to standard LookPath on non-macOS platforms.
func lookPathWithBrewPATH(command string) (string, error) {
	env := git.Environ()
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
// per installed entry, in catalog order. On Windows, when wsl.exe resolves
// AND `wsl --list --quiet` returns at least one distro, the bare "wsl" entry
// is replaced by one entry per distro — keeping the bare entry as a fallback
// would be confusing because it points at the default distro implicitly.
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
		if c.ID == "wsl" {
			wslCmd = cmd
			// Skip the bare wsl entry here; we re-emit it below either as a
			// fallback (no distros) or as per-distro entries.
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
	}
	// Per-distro WSL entries (Windows only).
	if goos == "windows" && wslCmd != "" {
		distros := DiscoverWSLDistros()
		if len(distros) > 0 {
			for _, d := range distros {
				out = append(out, config.ShellEntry{
					ID:      "wsl-" + slugifyASCII(d),
					Name:    "WSL — " + d,
					Command: wslCmd,
					Args:    []string{"-d", d},
				})
			}
		} else {
			// Fallback bare WSL entry when no distros are reachable.
			out = append(out, config.ShellEntry{
				ID:      "wsl",
				Name:    "WSL (default distro)",
				Command: wslCmd,
			})
		}
	}
	return out
}

// ─── WSL distro discovery ─────────────────────────────────────────────────

// DiscoverWSLDistros returns the list of installed WSL distributions. Returns
// nil when wsl.exe is missing, no distros are installed, or the host is not
// Windows. None of those are errors — just absence of WSL state.
func DiscoverWSLDistros() []string {
	cmd := exec.Command("wsl.exe", "--list", "--quiet")
	git.HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	text := decodeWSLBytes(out)
	var distros []string
	for _, line := range strings.Split(text, "\n") {
		name := strings.TrimSpace(line)
		// `wsl --list --quiet` prints a trailing empty line; some builds also
		// emit a "(Default)" suffix that --quiet was supposed to suppress.
		name = strings.TrimSuffix(name, " (Default)")
		if name == "" {
			continue
		}
		distros = append(distros, name)
	}
	return distros
}

// decodeWSLBytes converts wsl.exe's UTF-16 LE output (BOM-prefixed in modern
// builds, sometimes BOM-less) into UTF-8. Falls back to the raw string when
// the bytes don't look like UTF-16 — a few `wsl.exe` ports emit UTF-8.
func decodeWSLBytes(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return decodeUTF16LE(b[2:])
	}
	if len(b) >= 2 && len(b)%2 == 0 && b[1] == 0 {
		return decodeUTF16LE(b)
	}
	return strings.TrimPrefix(string(b), "\ufeff")
}

func decodeUTF16LE(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u16 := make([]uint16, len(b)/2)
	for i := range u16 {
		u16[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u16))
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
	for _, line := range strings.Split(string(data), "\n") {
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
