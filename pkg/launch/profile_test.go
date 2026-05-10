package launch

import (
	"reflect"
	"testing"
)

// Each table entry exercises one rule from ResolveArgs's contract. Names are
// short on purpose — the input columns make the intent obvious without the
// table getting wider than a screen.
func TestResolveArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   ProfileArgs
		want []string
	}{
		{
			name: "empty template returns nil",
			in:   ProfileArgs{Template: nil, Path: "/repo"},
			want: nil,
		},
		{
			name: "path token substring-replaced inside arg",
			in: ProfileArgs{
				Template: []string{"--working-directory={path}", "--", "{shell_command}"},
				Path:     "/home/me/r",
			},
			want: []string{"--working-directory=/home/me/r", "--"},
		},
		{
			name: "shell_command whole-arg replace when shell set",
			in: ProfileArgs{
				Template:     []string{"-d", "{path}", "{shell_command}"},
				Path:         "/r",
				ShellCommand: "/usr/bin/zsh",
			},
			want: []string{"-d", "/r", "/usr/bin/zsh"},
		},
		{
			name: "shell_command dropped when shell empty",
			in: ProfileArgs{
				Template: []string{"-d", "{path}", "{shell_command}", "{shell_args}"},
				Path:     "/r",
			},
			want: []string{"-d", "/r"},
		},
		{
			name: "shell_args splices flags",
			in: ProfileArgs{
				Template:     []string{"start", "--cwd", "{path}", "--", "{shell_command}", "{shell_args}"},
				Path:         "/r",
				ShellCommand: "wsl.exe",
				ShellArgs:    []string{"-d", "Ubuntu-24.04"},
			},
			want: []string{"start", "--cwd", "/r", "--", "wsl.exe", "-d", "Ubuntu-24.04"},
		},
		{
			name: "shell_args expands to nothing when shell empty",
			in: ProfileArgs{
				Template: []string{"--", "{shell_command}", "{shell_args}", "tail"},
				Path:     "/r",
			},
			want: []string{"--", "tail"},
		},
		{
			name: "command splices harness argv after shell tokens",
			in: ProfileArgs{
				Template:     []string{"-d", "{path}", "{shell_command}", "{shell_args}", "{command}"},
				Path:         "/r",
				ShellCommand: "/bin/bash",
				ShellArgs:    []string{"-l", "-c"},
				HarnessArgv:  []string{"claude", "--debug"},
			},
			want: []string{"-d", "/r", "/bin/bash", "-l", "-c", "claude", "--debug"},
		},
		{
			name: "command alone (legacy AI-harness path) splices in place",
			in: ProfileArgs{
				Template:    []string{"-d", "{path}", "{command}"},
				Path:        "/r",
				HarnessArgv: []string{"codex", "exec"},
			},
			want: []string{"-d", "/r", "codex", "exec"},
		},
		{
			name: "no tokens, no harness — append path (legacy `open -a Terminal`)",
			in: ProfileArgs{
				Template: []string{"-a", "Terminal"},
				Path:     "/r",
			},
			want: []string{"-a", "Terminal", "/r"},
		},
		{
			name: "no tokens, harness present — do not append path",
			in: ProfileArgs{
				Template:    []string{"-a", "Terminal"},
				Path:        "/r",
				HarnessArgv: []string{"claude"},
			},
			want: []string{"-a", "Terminal"},
		},
		{
			name: "shell tokens present + harness nil — no path append",
			in: ProfileArgs{
				Template:     []string{"--", "{shell_command}"},
				Path:         "/r",
				ShellCommand: "/bin/zsh",
			},
			want: []string{"--", "/bin/zsh"},
		},
		{
			name: "multiple {path} occurrences in same arg all replaced",
			in: ProfileArgs{
				Template: []string{"--init={path}/.init", "--cwd={path}"},
				Path:     "/r",
			},
			want: []string{"--init=/r/.init", "--cwd=/r"},
		},
		{
			name: "args-only template: no tokens, empty path appended",
			in: ProfileArgs{
				Template: []string{"--no-color"},
				Path:     "",
			},
			want: []string{"--no-color", ""},
		},
		{
			name: "harness empty slice splices nothing but suppresses append",
			in: ProfileArgs{
				Template:    []string{"-a", "Terminal", "{command}"},
				Path:        "/r",
				HarnessArgv: []string{},
			},
			want: []string{"-a", "Terminal"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveArgs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ResolveArgs:\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}

// Token constants are part of the public contract — guard against accidental
// rename in case a refactor mass-renames "{shell_command}" elsewhere.
func TestTokenConstants(t *testing.T) {
	t.Parallel()
	if TokenPath != "{path}" || TokenShellCommand != "{shell_command}" ||
		TokenShellArgs != "{shell_args}" || TokenCommand != "{command}" {
		t.Fatalf("token constants drifted: %q %q %q %q",
			TokenPath, TokenShellCommand, TokenShellArgs, TokenCommand)
	}
}
