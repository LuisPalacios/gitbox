package harness

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseWeztermLaunchMenu_CanonicalDocsExample(t *testing.T) {
	// Lifted from tmp/wezterm/docs/config/lua/config/launch_menu.md — this is
	// the example WezTerm itself ships in its docs, so it's the strongest
	// possible "must work" test.
	src := []byte(`
local wezterm = require 'wezterm'
local config = {}

config.launch_menu = {
  {
    args = { 'top' },
  },
  {
    -- Optional label to show in the launcher.
    label = 'Bash',
    args = { 'bash', '-l' },
  },
}

return config
`)
	got, err := ParseWeztermLaunchMenu(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	want := []WeztermLaunchMenuEntry{
		{Label: "", Args: []string{"top"}},
		{Label: "Bash", Args: []string{"bash", "-l"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries mismatch:\n  got  %+v\n  want %+v", got, want)
	}
}

func TestParseWeztermLaunchMenu_ModulePrefixedAssignment(t *testing.T) {
	// Some users wrap their config in a module table — accept that shape.
	src := []byte(`
local M = { config = wezterm.config_builder() }

M.config.launch_menu = {
  { label = "PowerShell 7", args = { "pwsh.exe", "-NoLogo" } },
}

return M
`)
	got, err := ParseWeztermLaunchMenu(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry; got %d (%+v)", len(got), got)
	}
	if got[0].Label != "PowerShell 7" {
		t.Errorf("label = %q, want PowerShell 7", got[0].Label)
	}
	if !reflect.DeepEqual(got[0].Args, []string{"pwsh.exe", "-NoLogo"}) {
		t.Errorf("args = %v, want [pwsh.exe -NoLogo]", got[0].Args)
	}
}

func TestParseWeztermLaunchMenu_MixedQuoteStyles(t *testing.T) {
	// WezTerm's own docs mix single and double quotes; the parser must
	// handle both within the same file.
	src := []byte(`
config.launch_menu = {
  { label = 'Single Q', args = { 'a', 'b' } },
  { label = "Double Q", args = { "c", "d" } },
}
`)
	got, err := ParseWeztermLaunchMenu(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries; got %d", len(got))
	}
	if got[0].Label != "Single Q" || got[1].Label != "Double Q" {
		t.Errorf("labels = [%q, %q]", got[0].Label, got[1].Label)
	}
}

func TestParseWeztermLaunchMenu_StripsLineComments(t *testing.T) {
	// Comments — including `--` inside an entry — should be stripped before
	// the entry parser sees them, so they don't interfere with the regex.
	src := []byte(`
config.launch_menu = {
  -- This whole line is a comment
  {
    label = "Real entry", -- trailing comment
    args = { "bash" },    -- another
  },
}
`)
	got, err := ParseWeztermLaunchMenu(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 || got[0].Label != "Real entry" {
		t.Errorf("entries = %+v, want one with label 'Real entry'", got)
	}
}

func TestParseWeztermLaunchMenu_PreservesDoubleDashInsideStrings(t *testing.T) {
	// `--` inside a string literal must NOT be treated as a comment start.
	src := []byte(`
config.launch_menu = {
  { label = "x -- y", args = { "echo", "-- separator --" } },
}
`)
	got, err := ParseWeztermLaunchMenu(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry; got %d", len(got))
	}
	if got[0].Label != "x -- y" {
		t.Errorf("label = %q, want 'x -- y' (-- inside string preserved)", got[0].Label)
	}
	if !reflect.DeepEqual(got[0].Args, []string{"echo", "-- separator --"}) {
		t.Errorf("args = %v", got[0].Args)
	}
}

func TestParseWeztermLaunchMenu_NoLaunchMenuBlock(t *testing.T) {
	// A wezterm.lua without a launch_menu block returns an error so the
	// caller can fall back to enumerated combos.
	src := []byte(`
local wezterm = require 'wezterm'
local config = {}
config.color_scheme = 'Monokai (terminal.sexy)'
return config
`)
	_, err := ParseWeztermLaunchMenu(src)
	if err == nil {
		t.Fatal("expected error when launch_menu is absent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestParseWeztermLaunchMenu_EmptyLaunchMenuIsNotAnError(t *testing.T) {
	// `config.launch_menu = {}` is valid Lua and a valid user config —
	// the user simply has no custom entries. Returning (nil, nil) lets the
	// caller treat it as "no launch_menu profiles to add".
	src := []byte(`config.launch_menu = {}`)
	got, err := ParseWeztermLaunchMenu(src)
	if err != nil {
		t.Errorf("empty launch_menu should not error; got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty launch_menu should yield zero entries; got %d", len(got))
	}
}

func TestParseWeztermLaunchMenu_UnterminatedBlock(t *testing.T) {
	// A truncated config (missing closing brace) must surface as an error
	// rather than crashing or silently returning partial data.
	src := []byte(`config.launch_menu = { { label = "broken", args = { "bash" }`)
	_, err := ParseWeztermLaunchMenu(src)
	if err == nil {
		t.Fatal("expected error on unterminated launch_menu")
	}
}

func TestParseWeztermLaunchMenu_DropsEmptyEntries(t *testing.T) {
	// A `{}` placeholder entry without label or args is a no-op for the
	// menu — drop it so callers don't get a junk Profile.
	src := []byte(`
config.launch_menu = {
  {},
  { label = "Real" },
  { args = { "real-cmd" } },
}
`)
	got, err := ParseWeztermLaunchMenu(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 non-empty entries; got %d (%+v)", len(got), got)
	}
}

func TestKnownShells(t *testing.T) {
	// Smoke test the embedded shell directory: must yield at least one entry
	// per supported OS, with a non-empty Name and Command on every row.
	specs := KnownShells()
	if len(specs) == 0 {
		t.Fatal("KnownShells returned no entries — check shell-directory.md is embedded")
	}
	seen := map[string]bool{}
	for i, s := range specs {
		if s.Name == "" {
			t.Errorf("shell[%d] has empty Name", i)
		}
		if s.Command == "" {
			t.Errorf("shell[%d] (%s) has empty Command", i, s.Name)
		}
		switch s.OS {
		case "Windows", "macOS", "Linux":
			seen[s.OS] = true
		default:
			t.Errorf("shell[%d] (%s) has unexpected OS %q", i, s.Name, s.OS)
		}
	}
	for _, want := range []string{"Windows", "macOS", "Linux"} {
		if !seen[want] {
			t.Errorf("no shells declared for %s — every supported platform should seed at least one shell", want)
		}
	}
}
