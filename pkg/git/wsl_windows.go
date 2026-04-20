package git

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

var (
	wslOnce      sync.Once
	wslAvailable bool
)

// IsWSLAvailable reports whether this Windows host has a usable WSL
// distribution. `wsl.exe --status` alone is not enough — on the GitHub
// Windows runner (and on bare Windows installs that ship `wsl.exe` without a
// distro) it exits 0 yet every actual `wsl.exe -- …` invocation then fails
// with an inscrutable status. We follow up with a no-op exec inside WSL to
// confirm a distro is reachable. Cached after the first probe so repeated
// calls are free; HideWindow keeps the GUI parent flash-free.
func IsWSLAvailable() bool {
	wslOnce.Do(func() {
		status := exec.Command("wsl.exe", "--status")
		HideWindow(status)
		if status.Run() != nil {
			wslAvailable = false
			return
		}
		probe := exec.Command("wsl.exe", "--", "true")
		HideWindow(probe)
		wslAvailable = probe.Run() == nil
	})
	return wslAvailable
}

// WSLPath converts a Linux-side path (as seen from inside WSL) to a path the
// Windows host can access — typically a `\\wsl.localhost\<distro>\…` UNC path.
// Returns an error if WSL is not available or the conversion fails.
func WSLPath(linuxPath string) (string, error) {
	if !IsWSLAvailable() {
		return "", fmt.Errorf("WSL is not available on this host")
	}
	if strings.TrimSpace(linuxPath) == "" {
		return "", fmt.Errorf("WSL path cannot be empty")
	}
	// `wslpath -w` performs the Linux→Windows conversion. We pipe through
	// `sh -c` so leading `~` expands to the WSL user's home before conversion.
	script := fmt.Sprintf("wslpath -w %s", shellQuote(linuxPath))
	cmd := exec.Command("wsl.exe", "--", "sh", "-c", script)
	HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("wsl wslpath: %w", err)
	}
	return strings.TrimSpace(stripUTF16BOM(string(out))), nil
}

// WSLLinuxPath converts a Windows-side path (e.g. "C:\foo\bar") to the
// equivalent path inside WSL ("/mnt/c/foo/bar" or, for paths already on the
// WSL filesystem, the native Linux path). Returns an error if WSL is not
// available or the conversion fails.
func WSLLinuxPath(winPath string) (string, error) {
	if !IsWSLAvailable() {
		return "", fmt.Errorf("WSL is not available on this host")
	}
	if strings.TrimSpace(winPath) == "" {
		return "", fmt.Errorf("Windows path cannot be empty")
	}
	script := fmt.Sprintf("wslpath -u %s", shellQuote(winPath))
	cmd := exec.Command("wsl.exe", "--", "sh", "-c", script)
	HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("wsl wslpath -u: %w", err)
	}
	return strings.TrimSpace(stripUTF16BOM(string(out))), nil
}

// WSLHome returns the Linux-side home directory of the default WSL distro
// (e.g. "/home/luis"). Returns an error if WSL is not available.
func WSLHome() (string, error) {
	if !IsWSLAvailable() {
		return "", fmt.Errorf("WSL is not available on this host")
	}
	cmd := exec.Command("wsl.exe", "--", "sh", "-c", "printf %s \"$HOME\"")
	HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("wsl HOME: %w", err)
	}
	return strings.TrimSpace(stripUTF16BOM(string(out))), nil
}

// shellQuote wraps s in single quotes, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// stripUTF16BOM removes the UTF-16 byte-order mark some `wsl.exe` builds
// prepend to stdout when invoked from a non-WSL parent.
func stripUTF16BOM(s string) string {
	const bom = "\ufeff"
	return strings.TrimPrefix(s, bom)
}
