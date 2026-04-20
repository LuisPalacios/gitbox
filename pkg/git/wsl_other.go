//go:build !windows

package git

import "errors"

// IsWSLAvailable reports whether the current host can launch commands inside
// WSL. Always false on non-Windows platforms.
func IsWSLAvailable() bool { return false }

// WSLPath converts a Linux-side path (as seen from inside WSL) to a path the
// Windows host can access. Always returns an error on non-Windows platforms.
func WSLPath(_ string) (string, error) {
	return "", errors.New("WSL is only available on Windows")
}

// WSLLinuxPath converts a Windows-side path to the equivalent path inside
// WSL. Always returns an error on non-Windows platforms.
func WSLLinuxPath(_ string) (string, error) {
	return "", errors.New("WSL is only available on Windows")
}

// WSLHome returns the Linux-side home directory of the default WSL distro
// (e.g. "/home/luis"). Always returns an error on non-Windows platforms.
func WSLHome() (string, error) {
	return "", errors.New("WSL is only available on Windows")
}
