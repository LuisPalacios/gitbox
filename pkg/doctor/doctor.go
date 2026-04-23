// Package doctor probes the host for the external command-line tools gitbox
// relies on (git, GCM, ssh, tmux, ...) and reports whether each is installed,
// where it lives, and what version it is. The results feed both the
// `gitbox doctor` CLI command and point-of-use checks in the GUI/TUI so the
// user learns about a missing dependency before it fails at runtime.
package doctor

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
// found. Strategy:
//
//  1. macOS: probe Homebrew prefixes before PATH. GUI apps launched from
//     Finder have a minimal PATH that excludes Homebrew directories.
//  2. Try exec.LookPath. Honors the user's PATH customization.
//  3. Windows: fall back to well-known Git-for-Windows / GCM install dirs.
//     GUI apps occasionally inherit an environment where `C:\Program Files\Git\cmd`
//     is missing from PATH, which made gitbox wrongly report GCM as not
//     installed.
func Lookup(name string) string {
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
	if runtime.GOOS == "windows" {
		for _, dir := range windowsFallbackDirs(name) {
			if p := statExecutable(dir, name); p != "" {
				return p
			}
		}
	}
	return ""
}

// statExecutable returns filepath.Join(dir, name) if the file exists and is
// not a directory, appending ".exe" on Windows when name lacks an extension.
// Returns "" when nothing matches.
func statExecutable(dir, name string) string {
	candidates := []string{filepath.Join(dir, name)}
	if runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(name), ".exe") {
		candidates = append(candidates, filepath.Join(dir, name+".exe"))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return ""
}

// windowsFallbackDirs returns well-known install directories to probe for the
// given tool when exec.LookPath misses it. Only Git-family tools get
// fallbacks: SSH ships in %SystemRoot%\System32\OpenSSH (always on PATH on
// Windows 10+); tmux/tmuxinator run inside WSL so an OS-level fallback would
// be misleading.
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
	for _, line := range strings.Split(text, "\n") {
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
		toolTmux(),
		toolTmuxinator(),
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

func toolTmux() Tool {
	return Tool{
		Name:        "tmux",
		DisplayName: "tmux",
		Purpose:     "Terminal multiplexer. Used by the tmuxinator workspace feature.",
		InstallHints: map[string]string{
			"darwin":  "brew install tmux",
			"linux":   "sudo apt install tmux",
			"windows": "inside WSL: sudo apt install tmux",
		},
		VersionArgs: []string{"-V"},
	}
}

func toolTmuxinator() Tool {
	return Tool{
		Name:        "tmuxinator",
		DisplayName: "tmuxinator",
		Purpose:     "tmux session manager. Used by the tmuxinator workspace feature.",
		InstallHints: map[string]string{
			"darwin":  "gem install tmuxinator",
			"linux":   "gem install tmuxinator",
			"windows": "inside WSL: gem install tmuxinator",
		},
		VersionArgs: []string{"version"},
	}
}

func toolWSL() Tool {
	return Tool{
		Name:        "wsl",
		DisplayName: "Windows Subsystem for Linux",
		Purpose:     "WSL shell bridge. Required on Windows for the tmuxinator workspace feature.",
		InstallHints: map[string]string{
			"windows": "wsl --install   (https://learn.microsoft.com/windows/wsl/install)",
		},
		VersionArgs: []string{"--version"},
	}
}
