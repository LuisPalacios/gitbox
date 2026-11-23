//go:build windows

package update

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

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
		exe, err := SelfPath()
		if err != nil {
			t.Fatal(err)
		}
		r, err := BeginRecovery(exe, t.TempDir())
		if err != nil || r == nil {
			t.Fatalf("starting recovery: %v", err)
		}
		if err := r.Confirm(); err != nil {
			t.Fatal(err)
		}
		if err := r.Finish(false); err != nil {
			t.Fatal(err)
		}
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

func TestInstallElevatedPayload_SecondTransactionCleansOnlyRecordedBackup(t *testing.T) {
	exe := filepath.Join(t.TempDir(), windowsExecutableName)
	writeFile(t, exe, "first version")
	unrelated := exe + ".old"
	writeFile(t, unrelated, "legacy backup")

	if err := installElevatedPayload(exe, sha("first version"), "v1.2.3", []byte("second version")); err != nil {
		t.Fatal(err)
	}
	firstBackup := elevatedTestBackup(t, exe)
	if got := readFile(t, firstBackup); got != "first version" {
		t.Fatalf("first recorded backup = %q", got)
	}
	if err := installElevatedPayload(exe, sha("first version"), "v1.2.3", []byte("stale candidate")); !errors.Is(err, errInstalledUpdateChanged) {
		t.Fatalf("update prepared against the previous executable returned %v", err)
	}
	if got := readFile(t, firstBackup); got != "first version" {
		t.Fatalf("stale update changed the previous backup: %q", got)
	}
	// Simulate Program Files by leaving the previous marker and backup
	// intact, as they would be after an unprivileged startup.
	if err := installElevatedPayload(exe, sha("second version"), "v1.2.3", []byte("third version")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, exe); got != "third version" {
		t.Fatalf("installed bytes = %q", got)
	}
	if _, err := os.Lstat(firstBackup); !os.IsNotExist(err) {
		t.Fatalf("first recorded backup still exists: %v", err)
	}
	if got := readFile(t, elevatedTestBackup(t, exe)); got != "second version" {
		t.Fatalf("second recorded backup = %q", got)
	}
	if got := readFile(t, unrelated); got != "legacy backup" {
		t.Fatalf("unrelated backup changed: %q", got)
	}
}

func elevatedTestBackup(t *testing.T, exe string) string {
	t.Helper()
	var state updateState
	if err := json.Unmarshal([]byte(readFile(t, exe+".update-state.json")), &state); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(filepath.Dir(exe), state.Old)
}

func TestReadUpdateHandle_DuplicatesOnlyBoundedFileInputs(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "update")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("release bytes"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	raw := formatUpdateHandle(windows.Handle(file.Fd()))
	if data, err := readUpdateHandle(windows.CurrentProcess(), raw, 6); err == nil || data != nil {
		t.Fatalf("oversized input = %q, %v", data, err)
	}
	if data, err := readUpdateHandle(windows.CurrentProcess(), raw, 20); err != nil || string(data) != "release bytes" {
		t.Fatalf("bounded input = %q, %v", data, err)
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(event)
	if data, err := readUpdateHandle(windows.CurrentProcess(), formatUpdateHandle(event), 20); err == nil || data != nil {
		t.Fatalf("non-file input = %q, %v", data, err)
	}
	if _, err := parseUpdateHandle("18446744073709551615"); err == nil {
		t.Fatal("accepted a pseudo-handle")
	}
}

func TestRecoveryRequiresProtectedWindowsState(t *testing.T) {
	for _, tc := range []struct {
		name, sddl string
		allowed    bool
	}{
		{"protected", "O:BAD:(A;;FA;;;BA)(A;;FA;;;SY)(A;;FRFX;;;BU)", true},
		{"writable", "O:BAD:(A;;FA;;;BA)(A;;FW;;;BU)", false},
		{"untrusted owner", "O:BUD:(A;;FA;;;BA)", false},
		{"writable parent", "O:BAD:(A;;DC;;;BU)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sd, err := windows.SecurityDescriptorFromString(tc.sddl)
			if err != nil {
				t.Fatal(err)
			}
			if got := adminControlledDescriptor(sd); got != tc.allowed {
				t.Fatalf("protected = %v", got)
			}
		})
	}
}
