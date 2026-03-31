//go:build windows

package main

import "syscall"

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	getSystemMetrics = user32.NewProc("GetSystemMetrics")
)

// virtualDesktopBounds returns the bounding rectangle of ALL monitors combined
// using the Windows virtual screen metrics (SM_XVIRTUALSCREEN etc.).
// This covers multi-monitor setups where secondary monitors can have negative
// coordinates (e.g. a monitor to the left of the primary).
func virtualDesktopBounds() (x, y, w, h int, ok bool) {
	// SM_XVIRTUALSCREEN=76, SM_YVIRTUALSCREEN=77,
	// SM_CXVIRTUALSCREEN=78, SM_CYVIRTUALSCREEN=79
	vx, _, _ := getSystemMetrics.Call(76)
	vy, _, _ := getSystemMetrics.Call(77)
	vw, _, _ := getSystemMetrics.Call(78)
	vh, _, _ := getSystemMetrics.Call(79)
	if vw == 0 || vh == 0 {
		return 0, 0, 0, 0, false
	}
	return int(vx), int(vy), int(vw), int(vh), true
}
