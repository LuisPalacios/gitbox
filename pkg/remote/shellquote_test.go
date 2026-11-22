package remote

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hostileNames are the folder names of TestRemoteTerminalArgv (#243): the
// ones of the issue, and variants whose injection would leave a marker
// file M behind. posixOnly ones cannot exist on Windows.
var hostileNames = []struct {
	name      string
	posixOnly bool
}{
	{name: "it's here"},
	{name: "$(id)"},
	{name: "`id`"},
	{name: "a;rm -rf ~"},
	{name: "a&&b"},
	{name: "%PATH%"},
	{name: "$env:HOME"},
	{name: `a b\c`, posixOnly: true},
	{name: `back\\slash\'quote`, posixOnly: true},
	{name: "$(touch M)"},
	{name: "`touch M`"},
	{name: "a;touch M"},
	{name: "a&&touch M"},
	{name: "a|touch M", posixOnly: true},
	{name: "x'; touch M; '"},
	{name: "%COMSPEC%"},
	{name: "a&echo>M", posixOnly: true},
	{name: "a&mkdir M&b"},
	{name: "a & echo x > M & b", posixOnly: true},
	{name: "$(New-Item M)"},
	{name: "it’s ’; New-Item M; ‘"},
	{name: "ünïcödé ✓"},
	{name: "-dash"},
}

// hostileDirs creates every hostile folder this OS allows under a fresh
// base and returns the base and the folders.
func hostileDirs(t *testing.T) (string, []string) {
	t.Helper()
	base := t.TempDir()
	var dirs []string
	for _, h := range hostileNames {
		if h.posixOnly && runtime.GOOS == "windows" {
			continue
		}
		dir := filepath.Join(base, h.name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", h.name, err)
		}
		dirs = append(dirs, dir)
	}
	return base, dirs
}

// noMarker fails when an injected command left a file or folder M
// anywhere under base.
func noMarker(t *testing.T, base, what string) {
	t.Helper()
	_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Name() == "M" {
			t.Errorf("%s: an injected command ran and wrote %s", what, p)
		}
		return nil
	})
}

// sameDir reports whether got names want: exactly, or the same folder with
// the same last element (Windows may spell a temp folder's parents short).
func sameDir(got, want string) bool {
	if got == want {
		return true
	}
	gi, err1 := os.Stat(got)
	wi, err2 := os.Stat(want)
	return err1 == nil && err2 == nil && os.SameFile(gi, wi) && filepath.Base(got) == filepath.Base(want)
}

// quoteFor quotes s as one word for family.
func quoteFor(family Family, s string) (string, bool) {
	switch family {
	case FamilyFish:
		return QuoteFish(s), true
	case FamilyCmd:
		return QuoteCmd(s)
	case FamilyPowerShell:
		return QuotePowerShell(s), true
	}
	return QuotePOSIX(s), true
}

func TestQuoters(t *testing.T) {
	for _, tc := range []struct {
		family Family
		in     string
		want   string
		ok     bool
	}{
		{FamilyPOSIX, "it's", `'it'\''s'`, true},
		{FamilyOther, "a b", `'a b'`, true},
		{FamilyFish, `it's \x`, `'it\'s \\x'`, true},
		{FamilyPowerShell, "it's ’x‘", `'it''s ’’x‘‘'`, true},
		{FamilyCmd, `C:\a&b (c)`, `"C:\a&b (c)"`, true},
		{FamilyCmd, `C:\%PATH%`, "", false},
		{FamilyCmd, `C:\a!b`, "", false},
		{FamilyCmd, `C:\a"b`, "", false},
	} {
		got, ok := quoteFor(tc.family, tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("quote(%s, %q) = %q, %v; want %q, %v", tc.family, tc.in, got, ok, tc.want, tc.ok)
		}
	}
	for shell, want := range map[string]Family{
		"/bin/bash": FamilyPOSIX, "/usr/bin/zsh": FamilyPOSIX, "/bin/dash": FamilyPOSIX, "/bin/ksh": FamilyPOSIX,
		"/bin/sh": FamilyPOSIX, "/usr/bin/fish": FamilyFish, `C:\Windows\System32\cmd.exe`: FamilyCmd,
		`C:\Program Files\PowerShell\7\pwsh.exe`: FamilyPowerShell, "powershell": FamilyPowerShell,
		"/bin/tcsh": FamilyOther, "/usr/bin/nu": FamilyOther, "": FamilyOther,
	} {
		if got := ShellFamily(shell); got != want {
			t.Errorf("ShellFamily(%q) = %s, want %s", shell, got, want)
		}
	}
}

// TestRemoteTerminalArgv builds the remote command of every hostile folder
// in each quoting family and runs it in the real interpreters of that
// family this host has, as sshd would: <shell> -c <command>. The program
// after the cd prints its working folder, which must be the folder, and no
// injected command may run.
//
// The cases do not skip under -short: they are the AC of #243 and run in
// every gate lane. POSIX hosts run sh, dash, bash, zsh, ksh, fish
// and tcsh (for FamilyOther) when installed; Windows runs cmd and
// PowerShell (launch_windows_test.go). The sshd lane repeats the POSIX and
// fish cases over a real ssh (integration_test.go).
func TestRemoteTerminalArgv(t *testing.T) {
	// A CR, LF or NUL is refused in every family, before anything runs.
	for _, family := range []Family{FamilyPOSIX, FamilyFish, FamilyCmd, FamilyPowerShell, FamilyOther} {
		for _, bad := range []string{"/srv/a\nb", "/srv/a\rb", "/srv/a\x00b"} {
			if _, err := RemoteCommand(family, bad, nil); codeOfErr(err) != CodeNotAvailablePath {
				t.Errorf("%s accepted %q: %v", family, bad, err)
			}
			if err := ValidateRemotePath("linux", bad); codeOfErr(err) != CodeNotAvailablePath {
				t.Errorf("ValidateRemotePath accepted %q", bad)
			}
		}
		if _, err := RemoteCommand(family, "/srv", []string{"claude\nid"}); codeOfErr(err) != CodeHarnessNotAllowed {
			t.Errorf("%s accepted a program with a newline: %v", family, err)
		}
	}
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX interpreters run on POSIX hosts; cmd and PowerShell run in TestRemoteTerminalArgvWindows")
	}
	shells := map[string]Family{
		"sh": FamilyPOSIX, "dash": FamilyPOSIX, "bash": FamilyPOSIX, "zsh": FamilyPOSIX, "ksh": FamilyPOSIX,
		"fish": FamilyFish, "tcsh": FamilyOther, "csh": FamilyOther,
	}
	// The program is a script that prints its working folder, in a folder
	// whose name needs quoting too. (The test binary itself would do, but
	// a race build starts too slowly for hundreds of runs.)
	bin := filepath.Join(t.TempDir(), "b'in $(touch M)")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	pwdScript := filepath.Join(bin, "print cwd")
	if err := os.WriteFile(pwdScript, []byte("#!/bin/sh\nprintf %s \"$PWD\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	program := []string{pwdScript}
	ran := 0
	for name, family := range shells {
		shell, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		ran++
		families := []Family{family}
		if family == FamilyPOSIX {
			families = append(families, FamilyOther) // the wrapper is POSIX too
		}
		for _, fam := range families {
			base, dirs := hostileDirs(t)
			for _, dir := range dirs {
				cmdline, err := RemoteCommand(fam, dir, program)
				if err != nil {
					t.Fatalf("%s/%s %q: %v", name, fam, dir, err)
				}
				cmd := exec.Command(shell, "-c", cmdline)
				cmd.Dir = base
				cmd.Env = append(os.Environ(), "GBX_HELPER_CWD=1", "SHELL="+shell)
				out, err := cmd.CombinedOutput()
				if err != nil || !sameDir(string(out), dir) {
					t.Errorf("%s/%s %q:\n command %s\n printed %q, %v", name, fam, filepath.Base(dir), cmdline, out, err)
				}
			}
			noMarker(t, base, name+"/"+string(fam))
		}
		if family == FamilyPOSIX || family == FamilyFish {
			loginShell(t, name, shell, family)
		}
	}
	if ran == 0 {
		t.Fatal("no POSIX interpreter found")
	}
	t.Logf("ran %d interpreters", ran)
}

// loginShell checks the form without a program: it execs "$SHELL" -l in
// the folder. SHELL is a script that prints its working folder.
func loginShell(t *testing.T, name, shell string, family Family) {
	t.Helper()
	base, dirs := hostileDirs(t)
	login := filepath.Join(t.TempDir(), "login")
	if err := os.WriteFile(login, []byte("#!/bin/sh\nprintf %s \"$PWD\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		cmdline, err := RemoteCommand(family, dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(shell, "-c", cmdline)
		cmd.Dir = base
		cmd.Env = append(os.Environ(), "SHELL="+login)
		out, err := cmd.CombinedOutput()
		if err != nil || !sameDir(string(out), dir) {
			t.Errorf("%s login %q:\n command %s\n printed %q, %v", name, filepath.Base(dir), cmdline, out, err)
		}
	}
	noMarker(t, base, name+" login")
}

func codeOfErr(err error) Code {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return ""
}

func TestValidateRemotePath(t *testing.T) {
	long := "/" + strings.Repeat("a", MaxRemotePath)
	for _, tc := range []struct {
		os, path string
		ok       bool
	}{
		{"linux", "/srv/it's here", true},
		{"linux", "srv/rel", false},
		{"linux", "", false},
		{"linux", long, false},
		{"linux", long[:MaxRemotePath], true},
		{"darwin", "/Users/me", true},
		{"windows", `C:\Users\me`, true},
		{"windows", `c:/Users/me`, true},
		{"windows", `\\server\share`, true},
		{"windows", `/srv`, false},
		{"windows", `C:rel`, false},
	} {
		if err := ValidateRemotePath(tc.os, tc.path); (err == nil) != tc.ok {
			t.Errorf("ValidateRemotePath(%s, %.40q) = %v, want ok=%v", tc.os, tc.path, err, tc.ok)
		}
	}
}
