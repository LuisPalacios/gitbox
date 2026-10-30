package doctor

import (
	"slices"

	"golang.org/x/sys/windows/registry"
)

// lxssKeyPath is the per-user registry key, under HKCU, where Windows registers
// each WSL distro. Tests point it at a temporary key.
var lxssKeyPath = `Software\Microsoft\Windows\CurrentVersion\Lxss`

// wslStateInstalled is the Lxss "State" value of a fully installed distro.
const wslStateInstalled = 1

// WSLDistros returns the installed WSL distros, sorted by name. It reads the
// registry instead of running `wsl.exe --list`, which is what makes it safe to
// call during startup: see probeWSLWindows. Absence of WSL returns nil.
func WSLDistros() []string {
	root, err := registry.OpenKey(registry.CURRENT_USER, lxssKeyPath, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer func() { _ = root.Close() }()
	ids, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var distros []string
	for _, id := range ids {
		if name := readDistroName(lxssKeyPath + `\` + id); name != "" {
			distros = append(distros, name)
		}
	}
	slices.Sort(distros)
	return distros
}

// readDistroName returns the name of the distro in the given key, or "" when
// the key is not a distro (Lxss also holds keys such as Notifications) or the
// distro is still being installed or removed.
func readDistroName(path string) string {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer func() { _ = k.Close() }()
	if state, _, err := k.GetIntegerValue("State"); err == nil && state != wslStateInstalled {
		return ""
	}
	name, _, err := k.GetStringValue("DistributionName")
	if err != nil {
		return ""
	}
	return name
}
