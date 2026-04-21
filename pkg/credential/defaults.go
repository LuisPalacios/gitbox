package credential

import "runtime"

// DefaultCredentialHelper returns the git credential helper gitbox configures
// globally for GCM-backed accounts. "manager" maps to git-credential-manager,
// the cross-platform helper shipped by Git for Windows and available via
// Homebrew on macOS / package managers on Linux.
func DefaultCredentialHelper() string {
	return "manager"
}

// DefaultCredentialStore returns the OS-appropriate GCM credential store:
//
//   - windows → wincredman (Windows Credential Manager)
//   - darwin  → keychain   (macOS Keychain)
//   - other   → secretservice (libsecret / gnome-keyring)
func DefaultCredentialStore() string {
	switch runtime.GOOS {
	case "windows":
		return "wincredman"
	case "darwin":
		return "keychain"
	default:
		return "secretservice"
	}
}
