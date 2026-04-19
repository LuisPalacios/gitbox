package harness

import (
	"reflect"
	"testing"
)

func TestExtractCommand(t *testing.T) {
	tests := map[string]string{
		"`claude`":                       "claude",
		"`Claude` *(App Executable)*":    "Claude",
		"`openhands` *(or Docker)*":      "openhands",
		"`cursor-agent`":                 "cursor-agent",
		"`langgraph` *(CLI/Studio)*":     "langgraph",
		"*N/A (Library)*":                "",
		"`python devika.py`":             "",
		"`docker-compose`":               "docker-compose",
		"`node --inspect foo.js`":        "",
		"":                               "",
		" ` ` ":                          "",
		"plain text with no backticks":   "",
		"`weird/slash`":                  "",
		"` leading-space-inside `":       "leading-space-inside",
	}
	for in, want := range tests {
		if got := extractCommand(in); got != want {
			t.Errorf("extractCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanName(t *testing.T) {
	tests := map[string]string{
		"**Claude Code**":        "Claude Code",
		" **Aider** ":            "Aider",
		"No markers":             "No markers",
		"**With trailing** text": "With trailing",
		"":                       "",
	}
	for in, want := range tests {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsAlignmentRow(t *testing.T) {
	yes := []string{
		"| :--- | :--- | :--- |",
		"|---|---|",
		"| :--- |",
	}
	no := []string{
		"| Tool Name | Company |",
		"| **Aider** | x |",
	}
	for _, s := range yes {
		if !isAlignmentRow(s) {
			t.Errorf("isAlignmentRow(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isAlignmentRow(s) {
			t.Errorf("isAlignmentRow(%q) = true, want false", s)
		}
	}
}

func TestSplitRowRespectsBackticks(t *testing.T) {
	got := splitRow("| **X** | y | `a | b` | last |")
	want := []string{" **X** ", " y ", " `a | b` ", " last "}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitRow pipes inside backticks should not split:\n got %q\nwant %q", got, want)
	}
}

func TestParseDirectoryFilters(t *testing.T) {
	md := `# Header

Some prose.

| Tool Name | Company | Category | OS | Executable / CLI Command | Use | URL |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Claude Code** | Anthropic | Agentic CLI | Any | ` + "`claude`" + ` | thing | x |
| **Gemini CLI** | Google | Agentic CLI | Any | ` + "`gemini`" + ` | thing | x |
| **OpenHands** | AAI | Headless Harness | Any | ` + "`openhands` *(or Docker)*" + ` | thing | x |
| **Claude Desktop** | Anthropic | AI Harness | Any | ` + "`Claude` *(App Executable)*" + ` | thing | x |
| **ADK** | Google | Agentic Framework | Any | *N/A (Library)* | library | x |
| **Cursor** | Anysphere | Agentic IDE | Any | ` + "`cursor`" + ` | IDE | x |
| **Cursor Agent** | Anysphere | Agentic CLI | Any | ` + "`cursor-agent`" + ` | CLI | x |
| **Devika** | Stition | Headless Harness | Any | ` + "`python devika.py`" + ` | script | x |
| **Dify** | LangGenius | Orchestrator | Any | ` + "`docker-compose`" + ` | orch | x |
`

	got := parseDirectory(md)
	wantNames := []string{"Claude Code", "Gemini CLI", "OpenHands", "Claude Desktop", "Cursor", "Cursor Agent"}
	if len(got) != len(wantNames) {
		t.Fatalf("got %d tools, want %d: %+v", len(got), len(wantNames), got)
	}
	for i, t0 := range got {
		if t0.Name != wantNames[i] {
			t.Errorf("got[%d].Name = %q, want %q", i, t0.Name, wantNames[i])
		}
	}
	// Agentic IDEs are now included (Cursor passes the filter). Only
	// Frameworks / Orchestrators / library rows are dropped.
	for _, t0 := range got {
		if t0.Category == "Agentic Framework" || t0.Category == "Orchestrator" {
			t.Errorf("framework/orchestrator row should not appear: %+v", t0)
		}
	}
	// Command extraction survived annotations.
	for _, t0 := range got {
		if t0.Name == "OpenHands" && t0.Command != "openhands" {
			t.Errorf("OpenHands command = %q, want %q", t0.Command, "openhands")
		}
		if t0.Name == "Claude Desktop" && t0.Command != "Claude" {
			t.Errorf("Claude Desktop command = %q, want %q", t0.Command, "Claude")
		}
	}
}

func TestKnownToolsEmbeddedMarkdown(t *testing.T) {
	tools := KnownTools()
	if len(tools) == 0 {
		t.Fatal("embedded tools-directory.md produced zero tools — embed or parser broken")
	}

	// Which specific tools are listed is a curatorial decision — tools-
	// directory.md is user-editable. Don't assert individual names here;
	// the smoke checks below cover the structural invariants.

	// Must NOT include Framework / Orchestrator / Cloud categories.
	// Agentic IDEs (Cursor, Windsurf) ARE now eligible — they launch inside
	// a terminal in a folder, which fits the menu contract.
	forbiddenCategories := map[string]bool{
		"Agentic Framework":          true,
		"Orchestrator":               true,
		"Orchestrator / Platform":    true,
		"Framework / Orchestrator":   true,
		"Harness Builder":            true,
		"Harness / Orchestrator":     true,
	}
	for _, t0 := range tools {
		if forbiddenCategories[t0.Category] {
			t.Errorf("forbidden category leaked through filter: %+v", t0)
		}
	}

	// Commands must match the strict identifier shape.
	for _, t0 := range tools {
		if !cmdTokenRE.MatchString(t0.Command) {
			t.Errorf("tool %q has non-identifier command %q", t0.Name, t0.Command)
		}
	}
}
