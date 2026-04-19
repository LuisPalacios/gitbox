package harness

import (
	"reflect"
	"testing"
)

func TestExtractBacktickedArgs(t *testing.T) {
	tests := map[string]struct {
		in   string
		want []string
	}{
		"empty cell":         {"", nil},
		"whitespace only":    {"   ", nil},
		"single token":       {"`-d`", []string{"-d"}},
		"two tokens":         {"`-d` `{path}`", []string{"-d", "{path}"}},
		"three tokens":       {"`--profile` `Ubuntu` `-d`", []string{"--profile", "Ubuntu", "-d"}},
		"embedded spaces":    {"` --flag ` `{path}`", []string{"--flag", "{path}"}},
		"equals sign":        {"`--workdir={path}`", []string{"--workdir={path}"}},
		"mixed with command": {"`--working-directory={path}` `--` `{command}`", []string{"--working-directory={path}", "--", "{command}"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := extractBacktickedArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extractBacktickedArgs(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeOS(t *testing.T) {
	tests := map[string]string{
		"Windows": "Windows",
		"win":     "Windows",
		"macOS":   "macOS",
		"mac":     "macOS",
		"darwin":  "macOS",
		"OSX":     "macOS",
		"Linux":   "Linux",
		"LINUX":   "Linux",
		"freebsd": "",
		"":        "",
	}
	for in, want := range tests {
		if got := normalizeOS(in); got != want {
			t.Errorf("normalizeOS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTerminalDirectory(t *testing.T) {
	md := `# Header
Some prose.

| Name | OS | Command | Default Args |
| :--- | :--- | :--- | :--- |
| **Windows Terminal** | Windows | ` + "`wt.exe`" + ` | ` + "`-d` `{path}` `{command}`" + ` |
| **PowerShell 7** | Windows | ` + "`pwsh.exe`" + ` | |
| **Terminal** | macOS | ` + "`open`" + ` | ` + "`-a` `Terminal`" + ` |
| **GNOME Terminal** | Linux | ` + "`gnome-terminal`" + ` | ` + "`--working-directory={path}` `--` `{command}`" + ` |
| **Bogus** | freebsd | ` + "`xyz`" + ` | |
| **NoCommand** | Linux | *N/A* | |
`
	got := parseTerminalDirectory(md)
	want := []TerminalSpec{
		{Name: "Windows Terminal", OS: "Windows", Command: "wt.exe", Args: []string{"-d", "{path}", "{command}"}},
		{Name: "PowerShell 7", OS: "Windows", Command: "pwsh.exe"},
		{Name: "Terminal", OS: "macOS", Command: "open", Args: []string{"-a", "Terminal"}},
		{Name: "GNOME Terminal", OS: "Linux", Command: "gnome-terminal", Args: []string{"--working-directory={path}", "--", "{command}"}},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d specs, want %d: %+v", len(got), len(want), got)
	}
	for i := range got {
		if got[i].Name != want[i].Name || got[i].OS != want[i].OS || got[i].Command != want[i].Command {
			t.Errorf("specs[%d] = %+v, want %+v", i, got[i], want[i])
		}
		if !reflect.DeepEqual(got[i].Args, want[i].Args) {
			t.Errorf("specs[%d].Args = %v, want %v", i, got[i].Args, want[i].Args)
		}
	}
}

func TestKnownTerminalsEmbeddedMarkdown(t *testing.T) {
	specs := KnownTerminals()
	if len(specs) == 0 {
		t.Fatal("embedded terminal-directory.md produced zero specs — embed or parser broken")
	}

	// Every spec must name a non-empty Name / Command / valid OS.
	for _, s := range specs {
		if s.Name == "" || s.Command == "" {
			t.Errorf("incomplete spec: %+v", s)
		}
		if normalizeOS(s.OS) == "" {
			t.Errorf("invalid OS %q on %q", s.OS, s.Name)
		}
	}

	// Must include the currently-shipping terminals for each OS so the
	// migration from hardcoded Go is lossless.
	wantNames := map[string]bool{
		// Windows
		"Windows Terminal": false, "PowerShell 7": false, "Git Bash": false, "WSL": false, "Command Prompt": false,
		// macOS
		"Terminal": false, "iTerm": false, "Warp": false,
		// Linux
		"GNOME Terminal": false, "Konsole": false, "Kitty": false, "Alacritty": false, "Xfce Terminal": false, "Terminator": false,
	}
	for _, s := range specs {
		if _, ok := wantNames[s.Name]; ok {
			wantNames[s.Name] = true
		}
	}
	for name, seen := range wantNames {
		if !seen {
			t.Errorf("embedded terminal directory is missing required terminal %q", name)
		}
	}
}
