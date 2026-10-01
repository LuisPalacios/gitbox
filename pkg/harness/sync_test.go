package harness

import (
	"reflect"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// fakeLookup resolves commands from a map. Keys are bare binary names or
// stored paths; values are the path to return. Anything else is "missing".
// It also records every (command, extraDirs) probe for assertions.
type fakeLookup struct {
	have   map[string]string
	probes []string
}

func (f *fakeLookup) fn(command string, extraDirs []string) string {
	f.probes = append(f.probes, command+"|"+strings.Join(extraDirs, ","))
	return f.have[command]
}

// twoKnown returns the first two eligible catalog rows so tests don't
// depend on specific tool names.
func twoKnown(t *testing.T) (Tool, Tool) {
	t.Helper()
	known := KnownTools()
	if len(known) < 2 {
		t.Skip("need at least 2 catalog rows")
	}
	return known[0], known[1]
}

func TestSyncDetectsCatalogToolsOnFreshConfig(t *testing.T) {
	first, second := twoKnown(t)
	lk := &fakeLookup{have: map[string]string{
		first.Command:  "/usr/local/bin/" + first.Command,
		second.Command: "/usr/local/bin/" + second.Command,
	}}
	out, changed := Sync(nil, lk.fn)
	if !changed {
		t.Fatal("fresh detection must report a change")
	}
	if len(out) != 2 {
		t.Fatalf("expected exactly the 2 installed tools, got %+v", out)
	}
	for i, want := range []Tool{first, second} {
		if out[i].Name != want.Name || out[i].Command != "/usr/local/bin/"+want.Command ||
			out[i].Source != SourceDetected || out[i].Missing {
			t.Errorf("entry[%d] = %+v, want detected %s", i, out[i], want.Name)
		}
	}
}

func TestSyncFlagsUninstalledDetectedEntryAndKeepsArgs(t *testing.T) {
	first, _ := twoKnown(t)
	in := []config.AIHarnessEntry{{
		Name: first.Name, Command: "/old/" + first.Command,
		Args: []string{"--flag"}, Source: SourceDetected,
	}}
	lk := &fakeLookup{have: map[string]string{}}
	out, changed := Sync(in, lk.fn)
	if !changed || len(out) != 1 {
		t.Fatalf("changed=%v out=%+v", changed, out)
	}
	if !out[0].Missing {
		t.Errorf("uninstalled entry must be flagged missing: %+v", out[0])
	}
	if out[0].Command != "/old/"+first.Command || !reflect.DeepEqual(out[0].Args, []string{"--flag"}) {
		t.Errorf("missing entry must keep command/args: %+v", out[0])
	}
	// Second pass with nothing changed on the host is a no-op.
	again, changed2 := Sync(out, lk.fn)
	if changed2 {
		t.Errorf("idempotent pass reported change: %+v", again)
	}
}

func TestSyncReinstallClearsMissingAndUpdatesPath(t *testing.T) {
	first, _ := twoKnown(t)
	in := []config.AIHarnessEntry{{
		Name: first.Name, Command: "/old/" + first.Command,
		Args: []string{"--keep"}, Source: SourceDetected, Missing: true,
	}}
	lk := &fakeLookup{have: map[string]string{first.Command: "/new/" + first.Command}}
	out, changed := Sync(in, lk.fn)
	if !changed || len(out) != 1 {
		t.Fatalf("changed=%v out=%+v", changed, out)
	}
	if out[0].Missing || out[0].Command != "/new/"+first.Command {
		t.Errorf("reinstall must clear missing and adopt the new path: %+v", out[0])
	}
	if !reflect.DeepEqual(out[0].Args, []string{"--keep"}) {
		t.Errorf("args lost across reinstall: %+v", out[0])
	}
}

func TestSyncUserEntryFlaggedButNeverRewritten(t *testing.T) {
	first, _ := twoKnown(t)
	in := []config.AIHarnessEntry{
		// A catalog name whose command the user pointed elsewhere.
		{Name: first.Name, Command: "/custom/wrapper", Args: []string{"-x"}, Source: SourceUser},
		// A non-catalog entry.
		{Name: "My Private Bot", Command: "/opt/mybot", Source: SourceUser},
	}
	// The catalog binary IS installed, but the user entry must not be
	// re-pointed at it.
	lk := &fakeLookup{have: map[string]string{first.Command: "/usr/bin/" + first.Command}}
	out, _ := Sync(in, lk.fn)
	if len(out) != 2 {
		t.Fatalf("out=%+v", out)
	}
	for _, h := range out {
		if !h.Missing {
			t.Errorf("user entry with dead path must be missing: %+v", h)
		}
		if h.Source != SourceUser {
			t.Errorf("user source must be preserved: %+v", h)
		}
	}
	if out[0].Command != "/custom/wrapper" || !reflect.DeepEqual(out[0].Args, []string{"-x"}) {
		t.Errorf("user command/args rewritten: %+v", out[0])
	}
	if out[1].Command != "/opt/mybot" {
		t.Errorf("custom entry rewritten: %+v", out[1])
	}
}

func TestSyncClassifiesLegacyEntries(t *testing.T) {
	first, second := twoKnown(t)
	in := []config.AIHarnessEntry{
		{Name: first.Name, Command: `C:\Users\me\AppData\Roaming\npm\` + first.Command + `.cmd`},
		{Name: second.Name, Command: "/custom/wrapper-script"},
		{Name: "Hand Written", Command: "/opt/thing"},
	}
	lk := &fakeLookup{have: map[string]string{
		in[0].Command: in[0].Command,
		in[1].Command: in[1].Command,
		in[2].Command: in[2].Command,
	}}
	out, changed := Sync(in, lk.fn)
	if !changed {
		t.Fatal("classification must be persisted as a change")
	}
	want := map[string]string{
		first.Name:     SourceDetected,
		second.Name:    SourceUser,
		"Hand Written": SourceUser,
	}
	for _, h := range out {
		if h.Source != want[h.Name] {
			t.Errorf("%s classified %q, want %q", h.Name, h.Source, want[h.Name])
		}
		if h.Missing {
			t.Errorf("%s resolved by fake but flagged missing", h.Name)
		}
	}
}

func TestSyncDropsRetiredAndDedupsByName(t *testing.T) {
	first, _ := twoKnown(t)
	retired := RetiredTools()
	if len(retired) == 0 {
		t.Skip("no retired rows in catalog")
	}
	in := []config.AIHarnessEntry{
		{Name: retired[0].Name, Command: "/bin/" + retired[0].Command, Source: SourceDetected},
		{Name: first.Name, Command: "/a/" + first.Command, Source: SourceDetected},
		{Name: first.Name, Command: "/b/" + first.Command, Source: SourceDetected},
		{Name: "Dup", Command: "/bin/dup1", Source: SourceUser},
		{Name: "Dup", Command: "/bin/dup2", Source: SourceUser},
	}
	lk := &fakeLookup{have: map[string]string{
		"/a/" + first.Command: "/a/" + first.Command,
		"/bin/dup1":           "/bin/dup1",
	}}
	out, _ := Sync(in, lk.fn)
	var names []string
	for _, h := range out {
		names = append(names, h.Name+":"+h.Command)
	}
	wantNames := []string{first.Name + ":/a/" + first.Command, "Dup:/bin/dup1"}
	if !reflect.DeepEqual(names, wantNames) {
		t.Errorf("got %v, want %v", names, wantNames)
	}
}

func TestSyncOrdersKnownByCatalogThenCustom(t *testing.T) {
	first, second := twoKnown(t)
	in := []config.AIHarnessEntry{
		{Name: "Zed Custom", Command: "/bin/z", Source: SourceUser},
		{Name: second.Name, Command: "/bin/" + second.Command, Source: SourceDetected},
		{Name: first.Name, Command: "/bin/" + first.Command, Source: SourceDetected},
		{Name: "Alpha Custom", Command: "/bin/a", Source: SourceUser},
	}
	lk := &fakeLookup{have: map[string]string{}}
	for _, h := range in {
		lk.have[h.Command] = h.Command
	}
	out, _ := Sync(in, lk.fn)
	var names []string
	for _, h := range out {
		names = append(names, h.Name)
	}
	want := []string{first.Name, second.Name, "Zed Custom", "Alpha Custom"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("order = %v, want %v", names, want)
	}
}

func TestSyncProbesAlternateCommandsAndExtraDirs(t *testing.T) {
	md := strings.Join([]string{
		"| Tool Name | Company | Category | OS | Executable / CLI Command | Use | URL | Well-known locations |",
		"| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |",
		"| **Cursor CLI** | Cursor | Agentic CLI | all | `agent` or `cursor-agent` | x | `https://cursor.com` | `%LOCALAPPDATA%\\cursor-agent`, `~/.local/bin` |",
	}, "\n")
	tools := parseDirectory(md)
	if len(tools) != 1 {
		t.Fatalf("parse: %+v", tools)
	}
	tool := tools[0]
	if !reflect.DeepEqual(tool.Commands, []string{"agent", "cursor-agent"}) || tool.Command != "agent" {
		t.Errorf("commands = %+v", tool)
	}
	if !reflect.DeepEqual(tool.ExtraDirs, []string{`%LOCALAPPDATA%\cursor-agent`, "~/.local/bin"}) {
		t.Errorf("extra dirs = %+v", tool.ExtraDirs)
	}

	// Only the alternate is installed: probeCatalog must find it and the
	// row's extra dirs must be passed to every probe.
	lk := &fakeLookup{have: map[string]string{"cursor-agent": "/x/cursor-agent"}}
	if got := probeCatalog(tool, lk.fn); got != "/x/cursor-agent" {
		t.Errorf("probeCatalog = %q", got)
	}
	for _, p := range lk.probes {
		if !strings.HasSuffix(p, `|%LOCALAPPDATA%\cursor-agent,~/.local/bin`) {
			t.Errorf("probe without extra dirs: %q", p)
		}
	}
	if !reflect.DeepEqual(lk.probes[:1], []string{`agent|%LOCALAPPDATA%\cursor-agent,~/.local/bin`}) {
		t.Errorf("primary command must be probed first: %v", lk.probes)
	}
}

func TestEqualSensitiveToSourceAndMissing(t *testing.T) {
	base := []config.AIHarnessEntry{{Name: "A", Command: "a", Args: []string{"1"}}}
	same := []config.AIHarnessEntry{{Name: "A", Command: "a", Args: []string{"1"}}}
	if !Equal(base, same) {
		t.Error("identical lists reported different")
	}
	for _, variant := range []config.AIHarnessEntry{
		{Name: "A", Command: "a", Args: []string{"1"}, Source: SourceUser},
		{Name: "A", Command: "a", Args: []string{"1"}, Missing: true},
		{Name: "A", Command: "b", Args: []string{"1"}},
		{Name: "A", Command: "a", Args: []string{"2"}},
		{Name: "A", Command: "a"},
	} {
		if Equal(base, []config.AIHarnessEntry{variant}) {
			t.Errorf("Equal missed a difference: %+v", variant)
		}
	}
	if Equal(base, nil) {
		t.Error("length difference missed")
	}
}

func TestCommandBase(t *testing.T) {
	cases := map[string]string{
		"claude":                           "claude",
		"/opt/homebrew/bin/claude":         "claude",
		`C:\Users\me\AppData\npm\claude.cmd`: "claude",
		`C:\x\agy.EXE`:                      "agy",
		"/home/me/.local/bin/tool.bat":     "tool",
		"/x/script.py":                     "script.py",
	}
	for in, want := range cases {
		if got := commandBase(in); got != want {
			t.Errorf("commandBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSevenColumnRowStillWorksAndNAYieldsNoDirs(t *testing.T) {
	md := strings.Join([]string{
		"| Tool Name | Company | Category | OS | Executable / CLI Command | Use | URL | Well-known locations |",
		"| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |",
		"| **Seven** | v | Agentic CLI | all | `seven` | x | `https://e` |",
		"| **EightNA** | v | Agentic CLI | all | `eightna` | x | `https://e` | *N/A* |",
		"| **EightBlank** | v | Agentic CLI | all | `eightblank` | x | `https://e` |  |",
		"| **NoCmd** | v | Agentic CLI | all | `python devika.py` | x | `https://e` | `~/x` |",
	}, "\n")
	tools := parseDirectory(md)
	if len(tools) != 3 {
		t.Fatalf("expected 3 rows (NoCmd skipped), got %+v", tools)
	}
	for _, tool := range tools {
		if tool.ExtraDirs != nil {
			t.Errorf("%s: expected no extra dirs, got %v", tool.Name, tool.ExtraDirs)
		}
		if len(tool.Commands) != 1 || tool.Commands[0] != tool.Command {
			t.Errorf("%s: commands = %v command = %q", tool.Name, tool.Commands, tool.Command)
		}
	}
}
