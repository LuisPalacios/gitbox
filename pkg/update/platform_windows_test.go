//go:build windows

package update

import (
	"strings"
	"testing"
)

func TestElevatedScript_Relaunch(t *testing.T) {
	copyLines := []string{`copy /Y "C:\tmp\x\GitboxApp.exe" "C:\Program Files\gitbox\GitboxApp.exe"`}

	script := elevatedScript(copyLines, `C:\tmp\x`, `C:\Program Files\gitbox\GitboxApp.exe`)
	launch := "explorer.exe \"C:\\Program Files\\gitbox\\GitboxApp.exe\""
	if !strings.Contains(script, launch) {
		t.Errorf("script does not relaunch through explorer.exe:\n%s", script)
	}
	if strings.Index(script, copyLines[0]) > strings.Index(script, launch) {
		t.Errorf("script relaunches before copying the files:\n%s", script)
	}

	script = elevatedScript(copyLines, `C:\tmp\x`, "")
	if strings.Contains(script, "explorer.exe") {
		t.Errorf("script relaunches although relaunch is empty:\n%s", script)
	}
}
