package main

import (
	"slices"
	"strings"
	"testing"
)

func TestMsysToWindowsPath(t *testing.T) {
	tests := map[string]string{
		`/c/Users/me/AppData/Local`: `C:\Users\me\AppData\Local`,
		`/d/code/repo`:              `D:\code\repo`,
		`/c`:                        `C:`,
		`C:\already\windows`:        `C:\already\windows`,
		`/not/a/drive/path`:         `/not/a/drive/path`,
		``:                          ``,
		`/`:                         `/`,
	}
	for in, want := range tests {
		if got := msysToWindowsPath(in); got != want {
			t.Errorf("msysToWindowsPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeWindowsTerminalEnv(t *testing.T) {
	in := []string{
		"MSYSTEM=MINGW64",
		"MSYS_NO_PATHCONV=1",
		"HOME=/c/Users/me",
		"SHELL=/usr/bin/bash",
		"OSTYPE=msys",
		"HOSTNAME=devbox",
		"LOGNAME=me",
		"MINGW_CHOST=x86_64-w64-mingw32",
		"MINGW_PACKAGE_PREFIX=mingw-w64-x86_64",
		"MINGW_PREFIX=/mingw64",
		"EXEPATH=C:\\Program Files\\Git\\bin",
		"MSYSCON=mintty.exe",
		"LOCALAPPDATA=/c/Users/me/AppData/Local",
		"APPDATA=/c/Users/me/AppData/Roaming",
		"USERPROFILE=/c/Users/me",
		"TEMP=/c/Users/me/AppData/Local/Temp",
		"PATH=/usr/bin:/mingw64/bin", // not normalised (deliberate)
		"FOO=/c/not-normalised",      // unknown key kept as-is
		"PS1=> ",
	}
	out := sanitizeWindowsTerminalEnv(in)

	// Unix-signal env vars must be dropped — oh-my-posh keys off these to
	// decide whether to invoke cygpath. See #72 follow-up: the user's
	// observed cygpath error was driven by OSTYPE=msys leaking into the
	// spawned pwsh, not by MSYSTEM (which is already dropped) or HOME.
	dropPrefixes := []string{
		"MSYSTEM=", "MSYS=", "MSYS2_PATH_TYPE=", "MSYS_NO_PATHCONV=",
		"MSYSCON=", "HOME=", "SHELL=", "OSTYPE=", "HOSTNAME=", "LOGNAME=",
		"MINGW_CHOST=", "MINGW_PACKAGE_PREFIX=", "MINGW_PREFIX=", "EXEPATH=",
	}
	for _, e := range out {
		for _, p := range dropPrefixes {
			if strings.HasPrefix(e, p) {
				t.Errorf("Unix-signal var %q should be dropped; still present in output: %q", strings.TrimSuffix(p, "="), e)
			}
		}
	}
	// Known Windows vars should be normalised.
	wantNormalised := map[string]string{
		"LOCALAPPDATA": `C:\Users\me\AppData\Local`,
		"APPDATA":      `C:\Users\me\AppData\Roaming`,
		"USERPROFILE":  `C:\Users\me`,
		"TEMP":         `C:\Users\me\AppData\Local\Temp`,
	}
	for k, want := range wantNormalised {
		found := false
		for _, e := range out {
			if strings.HasPrefix(e, k+"=") {
				found = true
				if got := strings.TrimPrefix(e, k+"="); got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
		}
		if !found {
			t.Errorf("%s missing from sanitised env", k)
		}
	}
	// Unknown keys kept verbatim.
	if slices.Contains(out, "FOO=/c/not-normalised") {
		return
	}
	t.Error("unknown key FOO was not preserved verbatim")
}
