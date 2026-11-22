package remote

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// TestEditorArgv: the folder is an element of its own, and the alias is the
// one the caller passes, never a word of the folder.
func TestEditorArgv(t *testing.T) {
	dir := "/srv/a b;$(id)"
	got, err := EditorArgv("/usr/local/bin/code", "my-host", dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/local/bin/code", "--remote", "ssh-remote+my-host", dir}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("EditorArgv = %q, want %q", got, want)
	}
	for _, command := range []string{"code", `C:\Users\me\AppData\Local\Programs\Microsoft VS Code\bin\code.cmd`, "/opt/homebrew/bin/code-insiders", "CODE.EXE"} {
		if !EditorSupportsRemote(command) {
			t.Errorf("%q should open remote folders", command)
		}
	}
	for _, command := range []string{"zed", "codium", "/usr/bin/vim", "code-x", ""} {
		if EditorSupportsRemote(command) {
			t.Errorf("%q should not open remote folders", command)
		}
		if _, err := EditorArgv(command, "my-host", "/srv"); codeOfErr(err) != CodeNotAvailablePath {
			t.Errorf("EditorArgv(%q) = %v", command, err)
		}
	}
	for _, alias := range []string{"", "-oProxyCommand=id", "a b", "host;id", strings.Repeat("a", 65)} {
		if _, err := EditorArgv("code", alias, "/srv"); codeOfErr(err) != CodeSSHConfig {
			t.Errorf("alias %q accepted: %v", alias, err)
		}
	}
	if _, err := EditorArgv("code", "my-host", "/srv/a\nb"); err == nil {
		t.Error("a folder with a newline was accepted")
	}
	if cmds := RemoteEditorCommands(); !reflect.DeepEqual(cmds, []string{"code", "code-insiders"}) {
		t.Errorf("RemoteEditorCommands = %q", cmds)
	}
	for _, e := range RemoteEditors {
		if e.Verified == "" {
			t.Errorf("%s has no verification note", e.Command)
		}
	}
}

func TestTerminalArgvShape(t *testing.T) {
	got, err := TerminalArgv("/usr/bin/ssh", "my-host", `cd -- '/srv/x' && exec "$SHELL" -l`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/usr/bin/ssh", "-t", "--", "my-host", `cd -- '/srv/x' && exec "$SHELL" -l`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TerminalArgv = %q", got)
	}
	if _, err := TerminalArgv("ssh", "-F/tmp/x", "true"); codeOfErr(err) != CodeSSHConfig {
		t.Errorf("an alias that is an option was accepted: %v", err)
	}
	if _, err := TerminalArgv("ssh", "h", "a\nb"); codeOfErr(err) != CodeNotAvailablePath {
		t.Errorf("a remote command with a newline was accepted: %v", err)
	}
}

func TestCheckBatchArgs(t *testing.T) {
	for _, ok := range []string{"/srv/a b", "/srv/a(b)", "/srv/it's", "/srv/a;b", "/srv/$(id)"} {
		if err := CheckBatchArgs([]string{"--remote", "ssh-remote+h", ok}); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"/srv/%PATH%", "/srv/a&b", "/srv/a|b", "/srv/a<b", "/srv/a>b", "/srv/a^b", "/srv/a!b", `/srv/a"b`} {
		if err := CheckBatchArgs([]string{bad}); codeOfErr(err) != CodeNotAvailablePath {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	if !IsBatchFile(`C:\x\code.CMD`) || !IsBatchFile("run.bat") || IsBatchFile(`C:\x\Code.exe`) {
		t.Error("IsBatchFile")
	}
}

// stubSSH writes a POSIX script that records its arguments, NUL-separated,
// in out.
func stubSSH(t *testing.T, out string) string {
	t.Helper()
	stub := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\n: > '" + out + "'\nfor a in \"$@\"; do printf '%s\\0' \"$a\" >> '" + out + "'; done\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func readArgv(t *testing.T, out string) []string {
	t.Helper()
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the stub ssh did not run: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00")
}

// TestClientLaunchLayers sends every hostile folder through the layers of
// this client and recovers the argv that reaches ssh, which must equal the
// one built, with no marker left behind.
//
//   - macOS: the line is compiled as an AppleScript literal by osascript,
//     as Terminal.app's do script and iTerm's write text receive it, and the
//     line osascript gives back runs in sh with a stub ssh.
//   - Windows: launch_windows_test.go starts a stub through StartConsole.
//   - Linux: terminals take the argv itself (pkg/terminals'
//     TestResolveProgramLaunch); only the remote layer applies.
func TestClientLaunchLayers(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the AppleScript layer exists on macOS clients only")
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		t.Skip("osascript is missing")
	}
	base, dirs := hostileDirs(t)
	out := filepath.Join(t.TempDir(), "argv")
	ssh := stubSSH(t, out)
	for _, dir := range dirs {
		remoteCmd, err := RemoteCommand(FamilyPOSIX, dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		argv, err := TerminalArgv(ssh, "my-host", remoteCmd)
		if err != nil {
			t.Fatal(err)
		}
		line := LocalShellLine("/bin/zsh", argv)
		// The literal production embeds in do script "…" / write text "…".
		literal := launch.AppleScriptEscape(line)
		if !strings.Contains(launch.MacAppleScriptLine("Terminal", line), `do script "`+literal+`"`) ||
			!strings.Contains(launch.MacAppleScriptLine("iTerm", line), `write text "`+literal+`"`) {
			t.Fatalf("the AppleScript does not carry the escaped line")
		}
		compiled, err := exec.Command("osascript", "-e", `return "`+literal+`"`).Output()
		if err != nil {
			t.Fatalf("osascript: %v", err)
		}
		recovered := strings.TrimSuffix(string(compiled), "\n")
		if recovered != line {
			t.Errorf("%q: osascript gave back\n %s\nwant\n %s", filepath.Base(dir), recovered, line)
			continue
		}
		cmd := exec.Command("/bin/sh", "-c", recovered)
		cmd.Dir = base
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%q: the line failed: %v %s", filepath.Base(dir), err, stderr.String())
		}
		if got := readArgv(t, out); !reflect.DeepEqual(got, argv[1:]) {
			t.Errorf("%q: ssh received %q, want %q", filepath.Base(dir), got, argv[1:])
		}
	}
	noMarker(t, base, "the macOS layers")
}

// LocalShellLine quotes for fish when my login shell is fish; when fish is
// installed the line runs in it and the argv must come back the same.
func TestLocalShellLineFish(t *testing.T) {
	argv := []string{"/usr/bin/ssh", "-t", "--", "h", `cd -- '/srv/a\\b'\''c' && exec "$SHELL" -l`}
	line := LocalShellLine("/opt/homebrew/bin/fish", argv)
	if !strings.HasPrefix(line, "exec '/usr/bin/ssh' ") || !strings.Contains(line, `\\\\b`) {
		t.Errorf("fish line %s", line)
	}
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is not installed")
	}
	out := filepath.Join(t.TempDir(), "argv")
	argv[0] = stubSSH(t, out)
	if err := exec.Command(fish, "-c", LocalShellLine(fish, argv)).Run(); err != nil {
		t.Fatal(err)
	}
	if got := readArgv(t, out); !reflect.DeepEqual(got, argv[1:]) {
		t.Errorf("fish passed %q, want %q", got, argv[1:])
	}
}
