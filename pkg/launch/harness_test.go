package launch

import (
	"reflect"
	"strings"
	"testing"
)

func TestTemplateAcceptsCommand(t *testing.T) {
	cases := []struct {
		tmpl []string
		want bool
	}{
		{[]string{"-d", TokenPath, TokenShellCommand, TokenShellArgs}, true},
		{[]string{"--profile", "X", "-d", TokenPath, TokenCommand}, true},
		{[]string{"--command", TokenShellCommand}, true},
		{[]string{"-a", "Warp"}, false},
		{[]string{"--working-directory", TokenPath}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := TemplateAcceptsCommand(c.tmpl); got != c.want {
			t.Errorf("TemplateAcceptsCommand(%v) = %v, want %v", c.tmpl, got, c.want)
		}
	}
}

func TestHarnessInShell_PowerShell(t *testing.T) {
	for _, shell := range []string{`C:\Program Files\PowerShell\7\pwsh.exe`, `powershell.exe`, "pwsh"} {
		cmd, args := HarnessInShell(shell, nil, []string{`C:\Users\me\AppData\Roaming\npm\claude.cmd`, "--model", "it's"}, "windows")
		if cmd != shell {
			t.Errorf("shell command changed: %q", cmd)
		}
		if len(args) != 3 || args[0] != "-NoExit" || args[1] != "-EncodedCommand" {
			t.Fatalf("args = %v, want [-NoExit -EncodedCommand <b64>]", args)
		}
		script, err := PSDecode(args[2])
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := `& 'C:\Users\me\AppData\Roaming\npm\claude.cmd' '--model' 'it''s'`
		if script != want {
			t.Errorf("script = %q, want %q", script, want)
		}
	}
}

func TestHarnessInShell_Cmd(t *testing.T) {
	_, args := HarnessInShell(`C:\Windows\System32\cmd.exe`, nil, []string{`C:\np m\claude.cmd`, "-v"}, "windows")
	want := []string{"/K", `C:\np m\claude.cmd`, "-v"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

func TestHarnessInShell_POSIXFamilies(t *testing.T) {
	cases := []struct {
		name      string
		shell     string
		shellArgs []string
		harness   []string
		goos      string
		wantArgs  []string
	}{
		{
			name:  "bash login (mac/linux) gains -i and relaunches itself",
			shell: "/bin/bash", shellArgs: []string{"-l"},
			harness: []string{"claude"}, goos: "darwin",
			wantArgs: []string{"-l", "-i", "-c", `'claude'; exec '/bin/bash' '-l'`},
		},
		{
			name:  "zsh with harness args and path with space",
			shell: "/bin/zsh", shellArgs: []string{"-l"},
			harness: []string{"/opt/my bots/bot", "--chatty"}, goos: "linux",
			wantArgs: []string{"-l", "-i", "-c", `'/opt/my bots/bot' '--chatty'; exec '/bin/zsh' '-l'`},
		},
		{
			name:  "git-bash keeps --login -i and gets bare harness name on windows",
			shell: `C:\Program Files\Git\bin\bash.exe`, shellArgs: []string{"--login", "-i"},
			harness: []string{`C:\Users\me\AppData\Roaming\npm\claude.cmd`}, goos: "windows",
			wantArgs: []string{"--login", "-i", "-c", `'claude'; exec 'C:\Program Files\Git\bin\bash.exe' '--login' '-i'`},
		},
		{
			name:  "no shell args at all still works",
			shell: "sh", shellArgs: nil,
			harness: []string{"codex"}, goos: "linux",
			wantArgs: []string{"-i", "-c", `'codex'; exec 'sh'`},
		},
		{
			name:  "fish uses -C and stays interactive by itself",
			shell: "/opt/homebrew/bin/fish", shellArgs: []string{"-l"},
			harness: []string{"gemini"}, goos: "darwin",
			wantArgs: []string{"-l", "-C", `'gemini'`},
		},
		{
			name:  "wsl runs inside the distro via sh -lc with bare harness name",
			shell: `C:\Windows\System32\wsl.exe`, shellArgs: []string{"-d", "Ubuntu-24.04"},
			harness: []string{`C:\Users\me\AppData\Roaming\npm\codex.cmd`, "--full-auto"}, goos: "windows",
			wantArgs: []string{"-d", "Ubuntu-24.04", "--", "sh", "-lc", `'codex' '--full-auto'; exec "${SHELL:-sh}" -l`},
		},
		{
			name:  "unix path is never stripped",
			shell: "/bin/bash", shellArgs: []string{"-l"},
			harness: []string{"/usr/local/bin/aider"}, goos: "linux",
			wantArgs: []string{"-l", "-i", "-c", `'/usr/local/bin/aider'; exec '/bin/bash' '-l'`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd, args := HarnessInShell(c.shell, c.shellArgs, c.harness, c.goos)
			if cmd != c.shell {
				t.Errorf("shell command changed: %q", cmd)
			}
			if !reflect.DeepEqual(args, c.wantArgs) {
				t.Errorf("args =\n  %q\nwant\n  %q", args, c.wantArgs)
			}
		})
	}
}

func TestHarnessCommandFor_BareNameRule(t *testing.T) {
	cases := []struct {
		in   string
		fam  shellFamily
		goos string
		want string
	}{
		{`C:\Users\me\AppData\Roaming\npm\claude.cmd`, familyPOSIX, "windows", "claude"},
		{`C:\Users\me\AppData\Roaming\npm\claude.cmd`, familyWSL, "windows", "claude"},
		{`C:\Users\me\AppData\Roaming\npm\claude.cmd`, familyPowerShell, "windows", `C:\Users\me\AppData\Roaming\npm\claude.cmd`},
		{`C:\Users\me\AppData\Roaming\npm\claude.cmd`, familyCmd, "windows", `C:\Users\me\AppData\Roaming\npm\claude.cmd`},
		// Native binaries keep their path for Git Bash (forward slashes), bare for WSL.
		{`C:\Users\me\.local\bin\claude.exe`, familyPOSIX, "windows", "C:/Users/me/.local/bin/claude.exe"},
		{`C:\Users\me\.local\bin\claude.exe`, familyWSL, "windows", "claude"},
		{"claude.exe", familyPOSIX, "windows", "claude.exe"},
		{"claude", familyPOSIX, "windows", "claude"},
		{`C:\tools\claude.cmd`, familyPOSIX, "linux", `C:\tools\claude.cmd`},
		{"/usr/local/bin/claude", familyPOSIX, "darwin", "/usr/local/bin/claude"},
	}
	for _, c := range cases {
		if got := harnessCommandFor(c.in, c.fam, c.goos); got != c.want {
			t.Errorf("harnessCommandFor(%q, %v, %s) = %q, want %q", c.in, c.fam, c.goos, got, c.want)
		}
	}
}

func TestPSEncodeRoundTrip(t *testing.T) {
	in := `& 'C:\ñ path\claude.cmd' '—dash'`
	out, err := PSDecode(psEncode(in))
	if err != nil || out != in {
		t.Errorf("round trip failed: %q %v", out, err)
	}
	if strings.ContainsAny(psEncode(in), " \"'") {
		t.Errorf("encoded payload must be quote-free")
	}
}
