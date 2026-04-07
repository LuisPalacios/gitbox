//go:build !windows

package main

// virtualDesktopBounds is a no-op on non-Windows platforms.
// The caller falls back to the Wails-based screen check.
func virtualDesktopBounds() (x, y, w, h int, ok bool) {
	return 0, 0, 0, 0, false
}
