package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateGCMProbe leaves the probe nothing to find but what a test stages:
// no PATH, no install roots, no Homebrew prefixes.
func isolateGCMProbe(t *testing.T) {
	t.Helper()
	isolateHome(t)
	saved := darwinExtraDirs
	darwinExtraDirs = nil
	t.Cleanup(func() { darwinExtraDirs = saved })
}

// stageFakeGit puts a git on PATH that answers --exec-path with execPath and
// either prints a version for `credential-manager --version` or refuses it
// the way a real git does when no GCM is reachable.
func stageFakeGit(t *testing.T, execPath string, dispatchesGCM bool) {
	t.Helper()
	dir := t.TempDir()
	version, exit := "", 1
	if dispatchesGCM {
		version, exit = "9.9.9+fake", 0
	}
	var name, script string
	if runtime.GOOS == "windows" {
		name = "git.cmd"
		script = fmt.Sprintf("@echo off\r\n"+
			"if \"%%1\"==\"--exec-path\" (echo %s& exit /b 0)\r\n"+
			"if \"%%1\"==\"credential-manager\" (echo %s& exit /b %d)\r\n"+
			"exit /b 1\r\n", filepath.ToSlash(execPath), version, exit)
	} else {
		name = "git"
		script = fmt.Sprintf("#!/bin/sh\n"+
			"[ \"$1\" = --exec-path ] && { echo '%s'; exit 0; }\n"+
			"[ \"$1\" = credential-manager ] && { echo '%s'; exit %d; }\n"+
			"exit 1\n", execPath, version, exit)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// stageGCM drops a fake GCM binary under root/parts and returns its path.
func stageGCM(t *testing.T, root string, parts ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return writeExe(t, dir, exeName("git-credential-manager"))
}

// Git for Windows moved from mingw64 to ucrt64, and the standalone installer
// has a system and a user location. None of them is on PATH, so each must
// resolve from its install root alone.
func TestProbeGCMFindsWindowsInstallDirs(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only fallback")
	}
	cases := []struct {
		name  string
		env   string
		parts []string
	}{
		{"Git ucrt64", "ProgramFiles", []string{"Git", "ucrt64", "bin"}},
		{"Git mingw64", "ProgramFiles", []string{"Git", "mingw64", "bin"}},
		{"Git clangarm64", "ProgramFiles", []string{"Git", "clangarm64", "bin"}},
		{"Git x86 mingw32", "ProgramFiles(x86)", []string{"Git", "mingw32", "bin"}},
		{"Git user scope ucrt64", "LOCALAPPDATA", []string{"Programs", "Git", "ucrt64", "bin"}},
		{"standalone Program Files", "ProgramFiles", []string{"Git Credential Manager"}},
		{"standalone Program Files x86", "ProgramFiles(x86)", []string{"Git Credential Manager"}},
		{"standalone user AppData", "LOCALAPPDATA", []string{"Programs", "Git Credential Manager"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateGCMProbe(t)
			root := t.TempDir()
			t.Setenv(tc.env, root)
			want := stageGCM(t, root, tc.parts...)

			r := CheckOne(toolGCM())
			if !r.Found || r.Path != want {
				t.Errorf("Found=%v Path=%q, want true and %q", r.Found, r.Path, want)
			}
		})
	}
}

// Negative control: Git trees that exist but hold no GCM still report it
// missing, with the install hint the user needs.
func TestProbeGCMReportsMissing(t *testing.T) {
	isolateGCMProbe(t)
	root := t.TempDir()
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		t.Setenv(env, root)
	}
	for _, parts := range [][]string{{"Git", "ucrt64", "bin"}, {"Git", "mingw64", "bin"}, {"Programs", "Git Credential Manager"}} {
		if err := os.MkdirAll(filepath.Join(append([]string{root}, parts...)...), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stageFakeGit(t, filepath.Join(root, "Git", "ucrt64", "libexec", "git-core"), false)

	r := CheckOne(toolGCM())
	if r.Found || r.Path != "" {
		t.Errorf("Found=%v Path=%q, want not found", r.Found, r.Path)
	}
	pc := PrecheckForCredentialType(CredTypeGCM)
	if pc.OK || !strings.Contains(pc.Summary, "Git Credential Manager") {
		t.Errorf("precheck = %+v, want GCM reported missing", pc)
	}
}

// Git reaches GCM through its own tree, whatever the toolchain prefix is
// called. No install list knows "newprefix": only asking git can find it.
func TestProbeGCMAsksGit(t *testing.T) {
	cases := []struct {
		name  string
		parts []string
	}{
		{"prefix bin", []string{"newprefix", "bin"}},
		{"exec path", []string{"newprefix", "libexec", "git-core"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateGCMProbe(t)
			root := t.TempDir()
			want := stageGCM(t, root, tc.parts...)
			stageFakeGit(t, filepath.Join(root, "newprefix", "libexec", "git-core"), true)

			r := CheckOne(toolGCM())
			if !r.Found || r.Path != want || r.Version != "9.9.9+fake" {
				t.Errorf("Found=%v Path=%q Version=%q, want true, %q, 9.9.9+fake", r.Found, r.Path, r.Version, want)
			}
		})
	}
}

// Git's word counts even when no probe can see the binary: GCM works, so it
// is not missing. Only the path stays unknown.
func TestProbeGCMTrustsGitWithoutAPath(t *testing.T) {
	isolateGCMProbe(t)
	stageFakeGit(t, filepath.Join(t.TempDir(), "libexec", "git-core"), true)

	r := CheckOne(toolGCM())
	if !r.Found || r.Path != "" || r.Version != "9.9.9+fake" {
		t.Errorf("Found=%v Path=%q Version=%q, want true, empty, 9.9.9+fake", r.Found, r.Path, r.Version)
	}
}

// A binary git can't dispatch still counts: an absolute credential.helper
// path can use it, as with a standalone install off PATH.
func TestProbeGCMFallsBackToFileWhenGitRefuses(t *testing.T) {
	isolateGCMProbe(t)
	root := t.TempDir()
	stageFakeGit(t, filepath.Join(root, "libexec", "git-core"), false)
	want := stageGCM(t, root, "bin")
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+filepath.Dir(want))

	r := CheckOne(toolGCM())
	if !r.Found || r.Path != want {
		t.Errorf("Found=%v Path=%q, want true and %q", r.Found, r.Path, want)
	}
}
