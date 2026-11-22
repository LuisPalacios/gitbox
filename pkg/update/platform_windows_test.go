//go:build windows

package update

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// I use disposable copies of this PE executable to exercise the same native
// move and startup-cleanup paths without updating an installed GitboxApp.
func TestWindowsExecutable_RunningUpdateAndStartupCleanup(t *testing.T) {
	switch os.Getenv("GITBOX_TEST_WINDOWS_UPDATE_CHILD") {
	case "previous":
		fmt.Println("ready")
		if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
			t.Fatal(err)
		}
		return
	case "installed":
		CleanupOldBinary()
		return
	}

	executable, err := SelfPath()
	if err != nil {
		t.Fatal(err)
	}
	pe, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "GitboxApp.exe")
	// A PE file can carry an overlay after its image. This makes the previous
	// executable distinct while both copies remain runnable.
	if err := os.WriteFile(dst, append(pe, []byte("previous test version")...), 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(filepath.Dir(dst), "other.old")
	writeFile(t, sibling, "keep")

	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	const run = "-test.run=^TestWindowsExecutable_RunningUpdateAndStartupCleanup$"
	old := exec.CommandContext(ctx, dst, run)
	old.Env = append(os.Environ(), "GITBOX_TEST_WINDOWS_UPDATE_CHILD=previous")
	input, err := old.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := old.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if old.ProcessState == nil {
			_ = input.Close()
			_ = old.Wait()
		}
	}()
	reader := bufio.NewReader(output)
	ready, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(ready) != "ready" {
		t.Fatalf("previous executable failed to start: %q, %v", ready, err)
	}
	if err := replaceExecutable(executable, dst, realReplaceOps()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst + ".old"); err != nil {
		t.Fatalf("retaining the runnable backup: %v", err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := old.Wait(); err != nil {
		t.Fatal(err)
	}

	installed := exec.CommandContext(ctx, dst, run)
	installed.Env = append(os.Environ(), "GITBOX_TEST_WINDOWS_UPDATE_CHILD=installed")
	if output, err := installed.CombinedOutput(); err != nil {
		t.Fatalf("installed executable failed to start: %v\n%s", err, output)
	}
	for _, path := range []string{dst + ".old", dst + ".update-state.json"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("cleanup did not remove %s: %v", filepath.Base(path), err)
		}
	}
	if got := readFile(t, sibling); got != "keep" {
		t.Fatalf("unrelated .old contains %q, want keep", got)
	}
}
