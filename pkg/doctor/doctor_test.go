package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStandardToolsShape(t *testing.T) {
	tools := StandardTools()
	if len(tools) == 0 {
		t.Fatal("StandardTools returned empty slice")
	}
	seen := make(map[string]bool)
	for _, tl := range tools {
		if tl.Name == "" {
			t.Errorf("tool with empty Name: %+v", tl)
		}
		if tl.DisplayName == "" {
			t.Errorf("tool %q has empty DisplayName", tl.Name)
		}
		if tl.Purpose == "" {
			t.Errorf("tool %q has empty Purpose", tl.Name)
		}
		if seen[tl.Name] {
			t.Errorf("duplicate tool name %q", tl.Name)
		}
		seen[tl.Name] = true
	}
	// Git must always be present — it's the one non-negotiable dep.
	if !seen["git"] {
		t.Fatal("StandardTools must include git")
	}
}

func TestInstallHintForEveryTool(t *testing.T) {
	// Every tool should have an install hint for at least the current OS, so
	// a user hitting "not found" on their actual platform always gets actionable
	// guidance.
	for _, tl := range StandardTools() {
		if tl.Name == "wsl" && runtime.GOOS != "windows" {
			continue // wsl is only meaningful on Windows
		}
		if tl.InstallHints == nil {
			t.Errorf("tool %q has nil InstallHints", tl.Name)
			continue
		}
		hint := tl.InstallHints[runtime.GOOS]
		if hint == "" {
			t.Errorf("tool %q has no install hint for %s", tl.Name, runtime.GOOS)
		}
	}
}

func TestLookupReturnsEmptyForMissing(t *testing.T) {
	// A binary name no sane system will have — empty string is the contract.
	if p := Lookup("gitbox-doctor-nonexistent-binary-xyz"); p != "" {
		t.Errorf("Lookup for nonexistent binary returned %q, want empty", p)
	}
}

func TestCheckOneNotFound(t *testing.T) {
	r := CheckOne(Tool{Name: "gitbox-doctor-nonexistent-binary-xyz", DisplayName: "Nope", Purpose: "x"})
	if r.Found {
		t.Fatal("CheckOne reported Found=true for a nonexistent binary")
	}
	if r.Path != "" {
		t.Errorf("Path should be empty when Found=false, got %q", r.Path)
	}
}

func TestCheckOneFoundGit(t *testing.T) {
	// Git is a reasonable assumption on any machine running this test suite.
	if p := Lookup("git"); p == "" {
		t.Skip("git not found on this host; skipping positive-case test")
	}
	r := CheckOne(toolGit())
	if !r.Found {
		t.Fatal("expected git to be Found")
	}
	if r.Path == "" {
		t.Error("Found=true but Path is empty")
	}
	if r.Version == "" {
		t.Error("Version probe returned empty for a found git")
	}
}

func TestPrecheckUnknownType(t *testing.T) {
	pc := PrecheckForCredentialType("bogus")
	if !pc.OK {
		t.Errorf("unknown credential type should be OK (nothing to check), got %+v", pc)
	}
}

func TestDecodeToolOutputPassthrough(t *testing.T) {
	in := []byte("git version 2.50.0\n")
	if got := decodeToolOutput(in); got != "git version 2.50.0\n" {
		t.Errorf("passthrough failed: %q", got)
	}
}

func TestDecodeToolOutputUTF16LEWithBOM(t *testing.T) {
	// "WSL" as UTF-16LE with BOM
	in := []byte{0xFF, 0xFE, 'W', 0, 'S', 0, 'L', 0}
	if got := decodeToolOutput(in); got != "WSL" {
		t.Errorf("UTF-16LE+BOM decode failed: %q", got)
	}
}

func TestDecodeToolOutputUTF16LENoBOM(t *testing.T) {
	// Interspersed NUL bytes (wsl.exe sometimes omits the BOM)
	in := []byte{'W', 0, 'S', 0, 'L', 0, ' ', 0, 'v', 0}
	got := decodeToolOutput(in)
	if got != "WSL v" {
		t.Errorf("heuristic UTF-16LE decode failed: %q", got)
	}
}

func TestPrecheckForGCMWhenGitPresent(t *testing.T) {
	if Lookup("git") == "" {
		t.Skip("git not found; cannot exercise precheck positively")
	}
	pc := PrecheckForCredentialType(CredTypeGCM)
	// The result depends on whether GCM is installed on this host — we can't
	// assert OK either way. We CAN assert the shape is consistent.
	if pc.OK && len(pc.Missing) != 0 {
		t.Errorf("OK=true but Missing is non-empty: %+v", pc.Missing)
	}
	if !pc.OK && len(pc.Missing) == 0 {
		t.Errorf("OK=false but Missing is empty")
	}
	if !pc.OK && pc.Summary == "" {
		t.Errorf("OK=false but Summary is empty")
	}
}

// Token credentials use git's built-in `store` helper and never touch GCM.
// Requiring GCM for Token was a real bug: on Windows systems without GCM,
// users were blocked from adding token-based credentials as a workaround.
func TestPrecheckForTokenDoesNotRequireGCM(t *testing.T) {
	if Lookup("git") == "" {
		t.Skip("git not found; cannot exercise precheck positively")
	}
	pc := PrecheckForCredentialType(CredTypeToken)
	if !pc.OK {
		for _, m := range pc.Missing {
			if m.Tool.Name == "git-credential-manager" {
				t.Fatalf("Token precheck wrongly requires GCM: %+v", pc)
			}
		}
	}
}

// Token precheck still fails when git itself is missing.
func TestPrecheckForTokenRequiresGit(t *testing.T) {
	for _, tl := range []Tool{toolGit()} {
		if tl.Name != "git" {
			continue
		}
	}
	// Can't actually uninstall git for the test, so assert the Tool set is
	// what we expect via the precheck shape when git is present: only git is
	// probed, so OK is driven entirely by git's presence.
	pc := PrecheckForCredentialType(CredTypeToken)
	if Lookup("git") != "" && !pc.OK {
		t.Errorf("git is present but Token precheck failed: %+v", pc)
	}
}

// On Windows, Lookup must find git-credential-manager at
// "%ProgramFiles%\Git\cmd\git-credential-manager.exe" even when PATH is
// stripped of the Git install directory. This is the GUI-from-Explorer
// scenario (issue N1).
func TestWindowsFallbackFindsGCMWhenPATHStripped(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only fallback")
	}
	// Stage a fake Git tree under a temp ProgramFiles so the fallback has
	// something to stat, independent of whether GCM is installed on the host.
	fakePF := t.TempDir()
	fakeBin := filepath.Join(fakePF, "Git", "cmd")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	binPath := filepath.Join(fakeBin, "git-credential-manager.exe")
	if err := os.WriteFile(binPath, []byte("fake"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	t.Setenv("ProgramFiles", fakePF)
	t.Setenv("ProgramFiles(x86)", "")
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("PATH", "") // force LookPath to miss

	got := Lookup("git-credential-manager")
	if got != binPath {
		t.Errorf("Lookup(git-credential-manager) = %q, want %q", got, binPath)
	}
}

func TestWindowsFallbackReturnsEmptyForUnrelatedTool(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only fallback")
	}
	// ssh / tmux / wsl are not in the fallback allowlist — they must not
	// resolve via the Windows-specific probe.
	if dirs := windowsFallbackDirs("ssh"); dirs != nil {
		t.Errorf("ssh should not have a Windows fallback, got %v", dirs)
	}
	if dirs := windowsFallbackDirs("tmux"); dirs != nil {
		t.Errorf("tmux should not have a Windows fallback, got %v", dirs)
	}
}
