package tui

import (
	"fmt"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// launcherOverlay is a modal list of every configured launcher (editors,
// terminals, AI harnesses). It's embedded in screens that want to offer a
// "pick any launcher" entry point (screen_repos, screen_account) on top of
// the direct-key defaults (t / e / a). Arrow keys move between selectable
// items; group headers are non-selectable. Enter launches and closes, Esc
// closes.
type launcherOverlay struct {
	active bool
	path   string // working directory to launch in
	items  []launcherItem
	cursor int // index into items; always lands on a selectable entry
}

// launcherItemKind tags the row in the flat overlay list.
type launcherItemKind int

const (
	launcherRowHeader launcherItemKind = iota
	launcherRowEditor
	launcherRowTerminal
	launcherRowHarness
)

// launcherItem is one row in the overlay list. Headers use kind
// launcherRowHeader and an empty payload; selectable rows have exactly one
// of editor/term/harness set.
type launcherItem struct {
	kind  launcherItemKind
	label string

	editor  *config.EditorEntry
	term    *config.TerminalEntry
	harness *config.AIHarnessEntry
}

// newLauncherOverlay builds the overlay items from the current config.
// Groups appear in the fixed order Terminals / Editors / AI Harnesses;
// empty groups are omitted entirely (no header for zero entries).
func newLauncherOverlay(cfg *config.Config) launcherOverlay {
	lo := launcherOverlay{}
	if cfg == nil {
		return lo
	}
	g := cfg.Global

	if len(g.Terminals) > 0 {
		lo.items = append(lo.items, launcherItem{kind: launcherRowHeader, label: "Terminals"})
		for i := range g.Terminals {
			t := g.Terminals[i]
			lo.items = append(lo.items, launcherItem{kind: launcherRowTerminal, label: t.Name, term: &t})
		}
	}
	if len(g.Editors) > 0 {
		lo.items = append(lo.items, launcherItem{kind: launcherRowHeader, label: "Editors"})
		for i := range g.Editors {
			e := g.Editors[i]
			lo.items = append(lo.items, launcherItem{kind: launcherRowEditor, label: e.Name, editor: &e})
		}
	}
	if len(g.AIHarnesses) > 0 {
		lo.items = append(lo.items, launcherItem{kind: launcherRowHeader, label: "AI Harnesses"})
		for i := range g.AIHarnesses {
			h := g.AIHarnesses[i]
			lo.items = append(lo.items, launcherItem{kind: launcherRowHarness, label: h.Name, harness: &h})
		}
	}

	// Land the cursor on the first selectable row (skip initial header).
	lo.cursor = lo.firstSelectable()
	return lo
}

// hasAny reports whether the overlay has any selectable rows.
func (lo launcherOverlay) hasAny() bool {
	for _, it := range lo.items {
		if it.kind != launcherRowHeader {
			return true
		}
	}
	return false
}

// activate turns the overlay on and sets the working directory launches will
// target. Callers invoke this on the "open launcher" keypress (o / O).
func (lo launcherOverlay) activate(path string) launcherOverlay {
	lo.active = true
	lo.path = path
	if lo.cursor < 0 || lo.cursor >= len(lo.items) || lo.items[lo.cursor].kind == launcherRowHeader {
		lo.cursor = lo.firstSelectable()
	}
	return lo
}

func (lo launcherOverlay) close() launcherOverlay {
	lo.active = false
	return lo
}

func (lo launcherOverlay) firstSelectable() int {
	for i, it := range lo.items {
		if it.kind != launcherRowHeader {
			return i
		}
	}
	return 0
}

func (lo launcherOverlay) lastSelectable() int {
	last := 0
	for i, it := range lo.items {
		if it.kind != launcherRowHeader {
			last = i
		}
	}
	return last
}

// moveUp / moveDown navigate between selectable rows only, skipping headers.
// At the edges the cursor stays put (no wraparound — follows existing TUI
// list behavior).
func (lo launcherOverlay) moveUp() launcherOverlay {
	for i := lo.cursor - 1; i >= 0; i-- {
		if lo.items[i].kind != launcherRowHeader {
			lo.cursor = i
			return lo
		}
	}
	return lo
}

func (lo launcherOverlay) moveDown() launcherOverlay {
	for i := lo.cursor + 1; i < len(lo.items); i++ {
		if lo.items[i].kind != launcherRowHeader {
			lo.cursor = i
			return lo
		}
	}
	return lo
}

// update handles key input while the overlay is active. It returns the new
// overlay state, an optional tea.Cmd (a launch command when Enter is
// pressed), and a bool indicating whether the key was consumed. Origin
// screens should delegate to this before their own key handlers.
func (lo launcherOverlay) update(msg tea.Msg, terminals []config.TerminalEntry) (launcherOverlay, tea.Cmd, bool) {
	if !lo.active {
		return lo, nil, false
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return lo, nil, false
	}
	switch {
	case key.Matches(km, Keys.Back):
		return lo.close(), nil, true
	case key.Matches(km, Keys.Up):
		return lo.moveUp(), nil, true
	case key.Matches(km, Keys.Down):
		return lo.moveDown(), nil, true
	case key.Matches(km, Keys.Enter):
		cmd := lo.launchCurrent(terminals)
		return lo.close(), cmd, true
	}
	// Any other key is consumed so it doesn't leak to the origin screen
	// while the overlay is showing.
	return lo, nil, true
}

// launchCurrent returns the tea.Cmd that fires the selected launcher, or nil
// if the cursor is on a non-selectable row (shouldn't happen in practice).
func (lo launcherOverlay) launchCurrent(terminals []config.TerminalEntry) tea.Cmd {
	if lo.cursor < 0 || lo.cursor >= len(lo.items) {
		return nil
	}
	it := lo.items[lo.cursor]
	switch it.kind {
	case launcherRowEditor:
		if it.editor == nil {
			return nil
		}
		return launchEditorCmd(lo.path, it.editor.Command, it.editor.Name)
	case launcherRowTerminal:
		if it.term == nil {
			return nil
		}
		return launchTerminalCmd(lo.path, *it.term)
	case launcherRowHarness:
		if it.harness == nil {
			return nil
		}
		return launchAIHarnessCmd(lo.path, *it.harness, terminals)
	}
	return nil
}

// view renders the overlay as a centered box. Callers overlay this on top
// of the origin screen's own View() output; lipgloss.Place handles centering
// inside the available viewport.
func (lo launcherOverlay) view(theme styles.Theme, width, height int) string {
	if !lo.active {
		return ""
	}

	title := theme.TextBold.Render("Open in…")
	hint := theme.HelpDesc.Render("↑/↓ navigate · enter launch · esc cancel")

	var body strings.Builder
	body.WriteString(title)
	body.WriteString("\n\n")

	if !lo.hasAny() {
		body.WriteString(theme.TextMuted.Render("No editors, terminals, or AI harnesses configured."))
		body.WriteString("\n\n")
		body.WriteString(hint)
		return renderOverlayBox(theme, body.String(), width, height)
	}

	for i, it := range lo.items {
		switch it.kind {
		case launcherRowHeader:
			if i > 0 {
				body.WriteString("\n")
			}
			body.WriteString(theme.Heading.Render(it.label))
			body.WriteString("\n")
		default:
			prefix := "  "
			line := fmt.Sprintf("%s%s %s", prefix, iconFor(it.kind), it.label)
			if i == lo.cursor {
				body.WriteString(theme.SelectedRow.Render(line))
			} else {
				body.WriteString(theme.NormalRow.Render(line))
			}
			body.WriteString("\n")
		}
	}

	body.WriteString("\n")
	body.WriteString(hint)

	return renderOverlayBox(theme, body.String(), width, height)
}

// iconFor picks a short glyph for each selectable row kind. Kept ASCII-safe
// for terminals that don't render wide emoji well; the GUI uses the full
// Unicode set from the issue spec.
func iconFor(k launcherItemKind) string {
	switch k {
	case launcherRowEditor:
		return "✎"
	case launcherRowTerminal:
		return ">_"
	case launcherRowHarness:
		return "🤖"
	}
	return " "
}

// renderOverlayBox wraps body in a bordered box and centers it in the
// viewport. Width is capped so the box doesn't dominate on wide terminals.
func renderOverlayBox(theme styles.Theme, body string, width, height int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.Palette.BorderDefault)).
		Padding(1, 2).
		Render(body)

	if width <= 0 || height <= 0 {
		return box
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
