package launch

import (
	"strings"
	"testing"
)

func TestShellQuotePOSIX(t *testing.T) {
	tests := map[string]string{
		"":                        "''",
		"plain":                   "'plain'",
		"/a/b c":                  "'/a/b c'",
		"has'quote":               `'has'\''quote'`,
		"multi 'one' 'two'":       `'multi '\''one'\'' '\''two'\'''`,
		`\\backslashes\\`:         `'\\backslashes\\'`,
		`mixed "double" 'single'`: `'mixed "double" '\''single'\'''`,
	}
	for in, want := range tests {
		if got := ShellQuotePOSIX(in); got != want {
			t.Errorf("ShellQuotePOSIX(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJoinPOSIX(t *testing.T) {
	got := JoinPOSIX([]string{"claude", "--model", "sonnet 4.6", "it's"})
	want := `'claude' '--model' 'sonnet 4.6' 'it'\''s'`
	if got != want {
		t.Errorf("JoinPOSIX = %q, want %q", got, want)
	}
	if got := JoinPOSIX(nil); got != "" {
		t.Errorf("JoinPOSIX(nil) = %q, want empty", got)
	}
}

func TestAppleScriptEscape(t *testing.T) {
	tests := map[string]string{
		"":           "",
		"plain":      "plain",
		`say "hi"`:   `say \"hi\"`,
		`back\slash`: `back\\slash`,
		`both "\"`:   `both \"\\\"`,
	}
	for in, want := range tests {
		if got := AppleScriptEscape(in); got != want {
			t.Errorf("AppleScriptEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMacHarnessShellLine(t *testing.T) {
	tests := []struct {
		name string
		path string
		argv []string
		want string
	}{
		{"simple path + single harness arg", "/Users/me/code/project", []string{"claude"}, `cd '/Users/me/code/project' && 'claude'`},
		{"path with space + multi-arg harness", "/a b/c", []string{"aider", "--model", "sonnet-4.6"}, `cd '/a b/c' && 'aider' '--model' 'sonnet-4.6'`},
		{"path with single quote", "/a/it's here", []string{"claude"}, `cd '/a/it'\''s here' && 'claude'`},
		{"empty harness argv still cd's", "/a", nil, `cd '/a'`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MacHarnessShellLine(tc.path, tc.argv); got != tc.want {
				t.Errorf("MacHarnessShellLine:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestMacAppleScript(t *testing.T) {
	t.Run("Terminal uses do script", func(t *testing.T) {
		got := MacAppleScript("Terminal", "/a", []string{"claude"})
		if !strings.Contains(got, `tell application "Terminal"`) {
			t.Errorf("missing Terminal tell block: %q", got)
		}
		if !strings.Contains(got, `do script "cd '/a' && 'claude'"`) {
			t.Errorf("missing do-script line with expected shell command: %q", got)
		}
	})
	t.Run("iTerm creates a new window and writes text", func(t *testing.T) {
		got := MacAppleScript("iTerm", "/a", []string{"claude"})
		for _, want := range []string{
			`tell application "iTerm"`,
			"create window with default profile",
			"delay ",
			"tell current session of current window",
			`write text "cd '/a' && 'claude'"`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in: %q", want, got)
			}
		}
	})
	t.Run("unsupported app returns empty", func(t *testing.T) {
		if got := MacAppleScript("Warp", "/a", []string{"claude"}); got != "" {
			t.Errorf("expected empty for unsupported app, got %q", got)
		}
	})
	t.Run("escape round-trip for quotes and backslashes", func(t *testing.T) {
		got := MacAppleScript("Terminal", `/a"b\c`, []string{"x"})
		if !strings.Contains(got, `cd '/a\"b\\c' && 'x'`) {
			t.Errorf("escape chain produced unexpected output: %q", got)
		}
	})
}
