//go:build !windows

package doctor

// WSLDistros returns nil: WSL only exists on Windows.
func WSLDistros() []string { return nil }
