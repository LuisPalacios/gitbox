// Package doctor probes the host for the external command-line tools gitbox
// relies on (git, GCM, ssh, wsl, ...) and reports whether each is installed,
// where it lives, and what version it is. The results feed the GUI's
// system check and its point-of-use checks, so the user learns about a
// missing dependency before it fails at runtime.
package doctor

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// Tool describes an external binary gitbox might call.
type Tool struct {
	Name         string            // binary name on $PATH, e.g. "git-credential-manager"
	DisplayName  string            // human label, e.g. "Git Credential Manager"
	Purpose      string            // one-line description of why gitbox uses it
	InstallHints map[string]string // runtime.GOOS -> install command
	VersionArgs  []string          // args to probe version; empty = skip version probe
}

// Result is the outcome of checking one Tool on the current host.
type Result struct {
	Tool    Tool
	Found   bool
	Path    string // absolute path when Found
	Version string // first non-empty line of version output, if any
}

// InstallHint returns the install command for the current OS, or "" if none.
func (r Result) InstallHint() string {
	if r.Tool.InstallHints == nil {
		return ""
	}
	return r.Tool.InstallHints[runtime.GOOS]
}

// Extra directories we probe on macOS before falling back to PATH. GUI apps
// launched from Finder get a minimal PATH that excludes Homebrew prefixes, so
// a bare exec.LookPath() misses Homebrew-installed tools.
var darwinExtraDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// Lookup returns the absolute path for name, or "" if the binary is not
// found. It is LookupIn with no extra directories; see there for the
// probe order.
func Lookup(name string) string {
	return LookupIn(name, nil)
}

// LookupIn returns the absolute path for name, or "" if the binary is not
// found. extraDirs are tool-specific well-known install directories probed
// first; each may start with `~`, and may reference environment variables
// as $VAR, ${VAR} or %VAR% (entries that expand to nothing are skipped).
//
// Strategy:
//
//  0. name carries a path separator: it is a stored path (absolute, or
//     `~`-prefixed). Stat it directly and never consult PATH — the caller
//     asked for that exact file.
//  1. extraDirs, in order.
//  2. macOS: Homebrew prefixes before PATH. GUI apps launched from Finder
//     have a minimal PATH that excludes Homebrew directories.
//  3. exec.LookPath. Honors the user's PATH customization (and PATHEXT on
//     Windows, so npm `.cmd` shims resolve).
//  4. ~/.local/bin on every OS. The native Claude Code, Antigravity, Codex,
//     Goose and uv-managed installers drop binaries there, and a GUI
//     launched from the Dock or a desktop menu doesn't inherit the shell
//     rc that adds it to PATH.
//  5. Windows: well-known Git-for-Windows / GCM install dirs. GUI apps
//     occasionally inherit an environment where `C:\Program Files\Git\cmd`
//     is missing from PATH, which made gitbox wrongly report GCM as not
//     installed.
//
// LookupIn never mutates the process environment, so it is safe to call
// from background goroutines while other code spawns subprocesses.
func LookupIn(name string, extraDirs []string) string {
	if name == "" {
		return ""
	}
	if strings.ContainsAny(name, `/\`) {
		return statExecutable(filepath.Dir(expandDir(name)), filepath.Base(name))
	}
	for _, raw := range extraDirs {
		dir := expandDir(raw)
		if dir == "" {
			continue
		}
		if p := statExecutable(dir, name); p != "" {
			return p
		}
	}
	if runtime.GOOS == "darwin" {
		for _, dir := range darwinExtraDirs {
			if p := statExecutable(dir, name); p != "" {
				return p
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p := statExecutable(filepath.Join(home, ".local", "bin"), name); p != "" {
			return p
		}
	}
	if runtime.GOOS == "windows" {
		for _, dir := range windowsFallbackDirs(name) {
			if p := statExecutable(dir, name); p != "" {
				return p
			}
		}
	}
	return ""
}

// winVarRE matches %VAR% references in Windows-style paths.
var winVarRE = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_()]*)%`)

// expandDir resolves a leading `~` to the user's home and expands $VAR,
// ${VAR} and %VAR% references from the environment. A reference whose
// variable is unset yields an empty string, and a path that becomes empty
// (or is left with an unexpanded `~`) returns "" so callers skip it.
func expandDir(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		s = home + s[1:]
	}
	unset := false
	s = winVarRE.ReplaceAllStringFunc(s, func(m string) string {
		v := os.Getenv(m[1 : len(m)-1])
		if v == "" {
			unset = true
		}
		return v
	})
	s = os.Expand(s, func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			unset = true
		}
		return v
	})
	if unset || strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

// statExecutable returns filepath.Join(dir, name) if the file exists, is
// not a directory and (outside Windows) carries an execute bit. On Windows,
// when name lacks an extension, ".exe", ".cmd" and ".bat" are also tried so
// native binaries and npm shims both resolve. Returns "" when nothing
// matches.
func statExecutable(dir, name string) string {
	if dir == "" || name == "" {
		return ""
	}
	candidates := []string{filepath.Join(dir, name)}
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		for _, ext := range []string{".exe", ".cmd", ".bat"} {
			candidates = append(candidates, filepath.Join(dir, name+ext))
		}
	}
	for _, c := range candidates {
		fi, err := os.Stat(c)
		if err != nil || fi.IsDir() {
			continue
		}
		if runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
			continue
		}
		return c
	}
	return ""
}

// windowsFallbackDirs returns well-known install directories to probe for the
// given tool when exec.LookPath misses it. Only Git-family tools get
// fallbacks: SSH ships in %SystemRoot%\System32\OpenSSH (always on PATH on
// Windows 10+); wsl.exe lives in System32 (also always on PATH), so an
// OS-level fallback for those would be misleading.
func windowsFallbackDirs(name string) []string {
	base := strings.TrimSuffix(strings.ToLower(name), ".exe")
	if base != "git" && base != "git-credential-manager" {
		return nil
	}
	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	localAppData := os.Getenv("LOCALAPPDATA")

	dirs := make([]string, 0, 6)
	if pf != "" {
		dirs = append(dirs,
			filepath.Join(pf, "Git", "cmd"),
			filepath.Join(pf, "Git", "mingw64", "bin"),
		)
	}
	if pf86 != "" {
		dirs = append(dirs,
			filepath.Join(pf86, "Git", "cmd"),
			filepath.Join(pf86, "Git", "mingw32", "bin"),
		)
	}
	if localAppData != "" {
		// Git-for-Windows user-scope install.
		dirs = append(dirs, filepath.Join(localAppData, "Programs", "Git", "cmd"))
	}
	if base == "git-credential-manager" && pf != "" {
		// Standalone GCM installer drops the binary outside of Git's tree.
		dirs = append(dirs, filepath.Join(pf, "Git Credential Manager"))
	}
	return dirs
}

// CheckOne probes a single Tool.
func CheckOne(t Tool) Result {
	r := Result{Tool: t}
	path := Lookup(t.Name)
	if path == "" {
		return r
	}
	r.Found = true
	r.Path = path
	if len(t.VersionArgs) > 0 {
		r.Version = probeVersion(path, t.VersionArgs)
	}
	return r
}

// Check probes every tool and returns results in the same order.
func Check(tools []Tool) []Result {
	out := make([]Result, 0, len(tools))
	for _, t := range tools {
		out = append(out, CheckOne(t))
	}
	return out
}

// probeVersion runs `path args...` and returns the first non-empty line of
// combined stdout+stderr output. Errors and non-zero exits are tolerated —
// some tools (notably ssh -V) print to stderr and exit non-zero.
func probeVersion(path string, args []string) string {
	cmd := exec.Command(path, args...)
	git.HideWindow(cmd) // suppress console flash when called from the Windows GUI
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	text := decodeToolOutput(buf.Bytes())
	for line := range strings.SplitSeq(text, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// decodeToolOutput normalizes command output so interior NUL bytes don't
// corrupt terminal column alignment. wsl.exe --version on Windows emits
// UTF-16LE with a BOM; a few other Microsoft tools do the same. Every other
// tool we probe emits ASCII or UTF-8, which this function passes through.
func decodeToolOutput(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		return decodeUTF16LE(raw[2:])
	}
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		return decodeUTF16BE(raw[2:])
	}
	// No BOM, but wsl.exe sometimes skips the BOM yet still outputs UTF-16LE.
	// Heuristic: if more than a third of the bytes are NUL, treat as UTF-16LE.
	if hasManyNulls(raw) {
		return decodeUTF16LE(raw)
	}
	return string(raw)
}

func hasManyNulls(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	nulls := 0
	for _, c := range b {
		if c == 0 {
			nulls++
		}
	}
	return nulls*3 > len(b)
}

func decodeUTF16LE(b []byte) string {
	n := len(b) / 2
	runes := make([]rune, 0, n)
	for i := 0; i < n*2; i += 2 {
		runes = append(runes, rune(uint16(b[i])|uint16(b[i+1])<<8))
	}
	return string(runes)
}

func decodeUTF16BE(b []byte) string {
	n := len(b) / 2
	runes := make([]rune, 0, n)
	for i := 0; i < n*2; i += 2 {
		runes = append(runes, rune(uint16(b[i])<<8|uint16(b[i+1])))
	}
	return string(runes)
}

// StandardTools returns every external tool gitbox may call across all of
// its features. The CLI `doctor` command prints this list verbatim; the GUI
// settings panel renders the same data, and the point-of-use helpers pick a
// subset relevant to a specific credential flow.
func StandardTools() []Tool {
	return []Tool{
		toolGit(),
		toolGCM(),
		toolSSH(),
		toolSSHKeygen(),
		toolSSHAdd(),
		toolWSL(),
	}
}

func toolGit() Tool {
	return Tool{
		Name:        "git",
		DisplayName: "Git",
		Purpose:     "Core version control. Required for every gitbox feature.",
		InstallHints: map[string]string{
			"darwin":  "brew install git   (or: xcode-select --install)",
			"linux":   "sudo apt install git   (or your distro's package manager)",
			"windows": "winget install Git.Git   (or: https://git-scm.com/download/win)",
		},
		VersionArgs: []string{"--version"},
	}
}

func toolGCM() Tool {
	return Tool{
		Name:        "git-credential-manager",
		DisplayName: "Git Credential Manager",
		Purpose:     "HTTPS token storage. Required when any account uses the 'gcm' credential type.",
		InstallHints: map[string]string{
			"darwin":  "brew install --cask git-credential-manager",
			"linux":   "https://github.com/git-ecosystem/git-credential-manager/blob/main/docs/install.md",
			"windows": "bundled with Git for Windows   (or: winget install GitHub.GitCredentialManager)",
		},
		VersionArgs: []string{"--version"},
	}
}

func toolSSH() Tool {
	return Tool{
		Name:        "ssh",
		DisplayName: "OpenSSH client",
		Purpose:     "SSH transport. Required when any account uses the 'ssh' credential type.",
		InstallHints: map[string]string{
			"darwin":  "preinstalled with macOS",
			"linux":   "sudo apt install openssh-client",
			"windows": "built into Windows 10+   (enable 'OpenSSH Client' optional feature)",
		},
		VersionArgs: []string{"-V"}, // prints to stderr, exits 255 — probeVersion tolerates it
	}
}

func toolSSHKeygen() Tool {
	return Tool{
		Name:        "ssh-keygen",
		DisplayName: "ssh-keygen",
		Purpose:     "SSH key generation. Used by the SSH credential setup flow.",
		InstallHints: map[string]string{
			"darwin":  "preinstalled with macOS",
			"linux":   "sudo apt install openssh-client",
			"windows": "built into Windows 10+",
		},
		// ssh-keygen has no simple version flag across versions; skip probe.
	}
}

func toolSSHAdd() Tool {
	return Tool{
		Name:        "ssh-add",
		DisplayName: "ssh-add",
		Purpose:     "Load SSH keys into the ssh-agent.",
		InstallHints: map[string]string{
			"darwin":  "preinstalled with macOS",
			"linux":   "sudo apt install openssh-client",
			"windows": "built into Windows 10+",
		},
	}
}

func toolWSL() Tool {
	return Tool{
		Name:        "wsl",
		DisplayName: "Windows Subsystem for Linux",
		Purpose:     "WSL shell bridge. Used on Windows to launch WSL-based terminal profiles and convert paths.",
		InstallHints: map[string]string{
			"windows": "wsl --install   (https://learn.microsoft.com/windows/wsl/install)",
		},
		VersionArgs: []string{"--version"},
	}
}
