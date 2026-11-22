//go:build windows

package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestHelperProcessCwd is not a test: the interpreter tests run this test
// binary as the program after the cd, and it prints its working folder.
func TestHelperProcessCwd(t *testing.T) {
	if os.Getenv("GBX_HELPER_CWD") != "1" {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Print(wd)
	os.Exit(0)
}

// helperProgram is the program word list that prints the working folder.
func helperProgram(t *testing.T) []string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{exe, "-test.run=^TestHelperProcessCwd$"}
}

// TestRemoteTerminalArgvWindows runs the cmd and PowerShell forms in the
// real interpreters, the way Win32-OpenSSH starts them: the command line is
// "<shell> /c <command>" or "<shell> -Command <command>", unparsed. A
// folder cmd cannot carry must be refused; every other must be the working
// folder of the program, with no marker left behind.
func TestRemoteTerminalArgvWindows(t *testing.T) {
	program := helperProgram(t)
	shells := []struct {
		family Family
		exe    string
		prefix string
	}{
		{FamilyCmd, "cmd.exe", "cmd.exe /d /c "},
		{FamilyPowerShell, "powershell.exe", "powershell.exe -NoLogo -NoProfile -NonInteractive -Command "},
		{FamilyPowerShell, "pwsh.exe", "pwsh.exe -NoLogo -NoProfile -NonInteractive -Command "},
	}
	ran := 0
	for _, sh := range shells {
		exe, err := exec.LookPath(sh.exe)
		if err != nil {
			continue
		}
		ran++
		base, dirs := hostileDirs(t)
		for _, dir := range dirs {
			cmdline, err := RemoteCommand(sh.family, dir, program)
			if sh.family == FamilyCmd && strings.ContainsAny(filepath.Base(dir), cmdUnsafe) {
				if codeOfErr(err) != CodeNotAvailablePath {
					t.Errorf("cmd accepted %q: %v", filepath.Base(dir), err)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s %q: %v", sh.exe, filepath.Base(dir), err)
			}
			cmd := exec.Command(exe)
			cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: sh.prefix + cmdline}
			cmd.Dir = base
			cmd.Env = append(os.Environ(), "GBX_HELPER_CWD=1")
			out, err := cmd.CombinedOutput()
			if err != nil || !sameDir(strings.TrimSpace(string(out)), dir) {
				t.Errorf("%s %q:\n command %s\n printed %q, %v", sh.exe, filepath.Base(dir), cmdline, out, err)
			}
		}
		noMarker(t, base, sh.exe)
	}
	if ran == 0 {
		t.Fatal("neither cmd.exe nor PowerShell was found")
	}
}

const stubSource = `package main

import (
	"encoding/json"
	"os"
)

func main() {
	data, _ := json.Marshal(os.Args[1:])
	_ = os.WriteFile(os.Getenv("GBX_STUB_OUT"), data, 0o600)
}
`

// buildStub compiles a program that writes its arguments, as JSON, to the
// file GBX_STUB_OUT names.
func buildStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(stubSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module stub\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "ssh-stub.exe")
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stub: %v\n%s", err, out)
	}
	return exe
}

func waitArgv(t *testing.T, out string) []string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if data, err := os.ReadFile(out); err == nil && len(data) > 0 {
			var argv []string
			if err := json.Unmarshal(data, &argv); err != nil {
				t.Fatalf("stub output %q: %v", data, err)
			}
			return argv
		}
		if time.Now().After(deadline) {
			t.Fatal("the stub never ran")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestClientLaunchLayersWindows: the Windows client starts ssh.exe through
// StartConsole, with no cmd.exe in between, and VS Code through its
// code.cmd batch launcher, which cmd.exe parses. With a stub in place of
// each, the argv that arrives must equal the one built, and no marker may
// appear. A folder the batch launcher cannot carry is refused before
// anything starts.
func TestClientLaunchLayersWindows(t *testing.T) {
	stub := buildStub(t)
	base, _ := hostileDirs(t)
	work := t.TempDir()
	hostile := make([]string, 0, len(hostileNames))
	for _, h := range hostileNames {
		hostile = append(hostile, "/srv/"+h.name)
	}

	for i, dir := range hostile {
		remoteCmd, err := RemoteCommand(FamilyPOSIX, dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		argv, err := TerminalArgv(stub, "my-host", remoteCmd)
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(work, "console-"+string(rune('a'+i%26))+".json")
		_ = os.Remove(out)
		env := append(os.Environ(), "GBX_STUB_OUT="+out)
		if err := StartConsole(argv, base, env); err != nil {
			t.Fatalf("StartConsole: %v", err)
		}
		if got := waitArgv(t, out); !reflect.DeepEqual(got, argv[1:]) {
			t.Errorf("%q: ssh received %q, want %q\n command line %s", dir, got, argv[1:], ConsoleCommandLine(argv))
		}
	}

	codeCmd := filepath.Join(work, "code.cmd")
	if err := os.WriteFile(codeCmd, []byte("@echo off\r\n\""+stub+"\" %*\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i, dir := range hostile {
		argv, err := EditorArgv(codeCmd, "my-host", dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckBatchArgs(argv[1:]); err != nil {
			if !strings.ContainsAny(dir, batchUnsafe) {
				t.Errorf("%q refused: %v", dir, err)
			}
			continue
		}
		out := filepath.Join(work, "editor-"+string(rune('a'+i%26))+".json")
		_ = os.Remove(out)
		cmd := exec.Command(codeCmd, argv[1:]...)
		cmd.Dir = base
		cmd.Env = append(os.Environ(), "GBX_STUB_OUT="+out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%q: code.cmd: %v %s", dir, err, b)
		}
		if got := waitArgv(t, out); !reflect.DeepEqual(got, argv[1:]) {
			t.Errorf("%q: the editor received %q, want %q", dir, got, argv[1:])
		}
	}
	noMarker(t, base, "the Windows layers")
	noMarker(t, work, "the Windows layers")
}
