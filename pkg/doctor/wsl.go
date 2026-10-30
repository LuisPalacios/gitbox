package doctor

import (
	"os"
	"path/filepath"
)

// probeWSLWindows reports WSL without ever running C:\Windows\System32\wsl.exe.
// Windows ships that file as an install stub: when the WSL package is absent,
// running it with any argument asks the user to install WSL in a window of its
// own, even when the caller passes CREATE_NO_WINDOW. Only the package's own
// binary is safe to run, so the version comes from there.
func probeWSLWindows() (path, version string, found bool) {
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		pkg := filepath.Join(programFiles, "WSL", "wsl.exe")
		if _, err := os.Stat(pkg); err == nil {
			return pkg, probeVersion(pkg, []string{"--version"}), true
		}
	}
	// A registered distro means a working WSL from before the package existed.
	if len(WSLDistros()) > 0 {
		if p := Lookup("wsl"); p != "" {
			return p, "", true
		}
	}
	return "", "", false
}
