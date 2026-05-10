package tui

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/i18n"
	"github.com/LuisPalacios/gitbox/pkg/terminals"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// terminalsScreen owns the v2.1 TerminalApps + Shells + TerminalProfiles
// editor (issue #69; simplified by issue #71).
//
// The screen probes the host directly via pkg/terminals.Sync on first
// render so the CLI works without ever launching the GUI — the previous
// "GUI must run first" coupling is gone.
//
// Profiles are fully editable: the user can toggle Default / Preferred
// / Hidden, rename + reassign existing entries, add new Profiles, and
// delete user-added Profiles. Deletion is restricted to entries with
// source="user" (the source field is internal — never displayed). On
// macOS / Linux the shell selector is optional ("" = login shell);
// on Windows it's mandatory. A banner above the list warns when no
// modern Terminal is installed on Windows.
type terminalsScreen struct {
	cfg     *config.Config
	cfgPath string
	theme   styles.Theme
	tr      i18n.Translator
	width   int
	height  int

	// goos is the host OS — captured once so the Add form can pick the
	// right shape (Windows = mandatory shell, mac/Linux = optional).
	goos string

	// missingModern is true when the host should display the
	// "install Windows Terminal" banner (Windows-only).
	missingModern bool

	// view drives the keyboard model. listView is the default browse
	// surface; addView and editView swap in a small inline form.
	view terminalsView

	// cursor indexes into profiles[]. Stays clamped to [0, len-1] when
	// non-empty, set to -1 when empty so the keyboard model can skip
	// per-row actions cleanly.
	cursor int

	// profiles is rebuilt from cfg every refresh so toggles and saves see
	// a stable ordering — sorted by Name within source priority so the
	// list layout doesn't shift around the cursor when the user toggles
	// Hidden on a row.
	profiles []config.TerminalProfile

	// addDraft / editDraft hold the in-progress form for add/edit views.
	// nameInput is the text input model both views share (only one of
	// add/edit can be active at a time).
	addDraft   *profileDraft
	editDraft  *profileDraft
	nameInput  textinput.Model
	formField  profileFormField
	formSelTerm int
	formSelSh   int

	// status / errMsg are transient feedback lines under the table.
	// Populated after a save / delete action; cleared on the next
	// keystroke that mutates state so the UI doesn't lie about what
	// happened most recently.
	status string
	errMsg string
}

type terminalsView int

const (
	terminalsViewList terminalsView = iota
	terminalsViewAdd
	terminalsViewEdit
)

// profileFormField names the focused field inside the add/edit inline form.
type profileFormField int

const (
	profileFieldName profileFormField = iota
	profileFieldTerminal
	profileFieldShell
)

// profileDraft is the in-progress payload for an add/edit form. For edit
// it carries the original ID so save can target the right row; for add
// the ID is generated at save time from the picked terminal/shell/name.
type profileDraft struct {
	originalID string
	name       string
	terminalID string
	shellID    string
}

// terminalsSyncFn is the Sync entry point used by the TUI screen. It's a
// package-level variable so unit tests can swap in a no-op when their
// fixture should not be mutated by host detection.
var terminalsSyncFn = terminals.Sync

func newTerminalsScreen(cfg *config.Config, cfgPath string, theme styles.Theme, tr i18n.Translator, w, h int) terminalsScreen {
	ti := textinput.New()
	ti.CharLimit = 80
	ti.Width = 40

	s := terminalsScreen{
		cfg:       cfg,
		cfgPath:   cfgPath,
		theme:     theme,
		tr:        tr,
		width:     w,
		height:    h,
		goos:      runtime.GOOS,
		nameInput: ti,
	}
	// Probe the host so the TUI populates apps/shells/profiles without
	// depending on the GUI to have run first. Idempotent — only persists
	// when the resulting payload differs.
	if terminalsSyncFn(s.cfg, s.goos) {
		_ = config.Save(s.cfg, s.cfgPath)
	}
	s.missingModern = terminals.MissingModernTerminal(s.cfg, s.goos)
	s.refreshProfiles()
	return s
}

func (s terminalsScreen) Init() tea.Cmd { return nil }

// refreshProfiles re-pulls the Profile slice from cfg, sorted in stable
// presentation order. Called after every save so the table reflects what
// the user just persisted without needing a separate "reload" step.
//
// Issue #71: source-based priority is gone (the source field is no longer
// shown). The new order is: visible rows first, then alphabetical by Name.
// Hidden rows trail at the bottom — same anchoring behaviour as before
// without the internal-bookkeeping slice.
func (s *terminalsScreen) refreshProfiles() {
	src := append([]config.TerminalProfile(nil), s.cfg.Global.TerminalProfiles...)
	sort.SliceStable(src, func(i, j int) bool {
		if src[i].Hidden != src[j].Hidden {
			return !src[i].Hidden
		}
		return strings.ToLower(src[i].Name) < strings.ToLower(src[j].Name)
	})
	s.profiles = src
	if len(s.profiles) == 0 {
		s.cursor = -1
	} else if s.cursor < 0 || s.cursor >= len(s.profiles) {
		s.cursor = 0
	}
}

// ─── Update ───────────────────────────────────────────────────────────────

func (s terminalsScreen) Update(msg tea.Msg) (terminalsScreen, tea.Cmd) {
	switch s.view {
	case terminalsViewAdd, terminalsViewEdit:
		return s.updateForm(msg)
	default:
		return s.updateList(msg)
	}
}

func (s terminalsScreen) updateList(msg tea.Msg) (terminalsScreen, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch {
	case key.Matches(km, Keys.Back):
		return s, func() tea.Msg { return switchScreenMsg{screen: screenSettings} }

	case key.Matches(km, Keys.Up):
		if s.cursor > 0 {
			s.cursor--
		}
		return s, nil

	case key.Matches(km, Keys.Down):
		if s.cursor < len(s.profiles)-1 {
			s.cursor++
		}
		return s, nil

	case km.String() == "d":
		// Set the cursor profile as the sole default. Skipped silently
		// when the cursor is on no row (empty list) — better than a
		// surprise no-op error message.
		if s.cursor < 0 {
			return s, nil
		}
		id := s.profiles[s.cursor].ID
		profs := append([]config.TerminalProfile(nil), s.cfg.Global.TerminalProfiles...)
		for i := range profs {
			profs[i].Default = (profs[i].ID == id)
		}
		return s.persist(profs, s.tr.T("tui.terminals.saved")), nil

	case km.String() == "p":
		if s.cursor < 0 {
			return s, nil
		}
		id := s.profiles[s.cursor].ID
		profs := append([]config.TerminalProfile(nil), s.cfg.Global.TerminalProfiles...)
		for i := range profs {
			if profs[i].ID == id {
				profs[i].Preferred = !profs[i].Preferred
			}
		}
		return s.persist(profs, s.tr.T("tui.terminals.saved")), nil

	case km.String() == "h":
		if s.cursor < 0 {
			return s, nil
		}
		id := s.profiles[s.cursor].ID
		profs := append([]config.TerminalProfile(nil), s.cfg.Global.TerminalProfiles...)
		for i := range profs {
			if profs[i].ID == id {
				profs[i].Hidden = !profs[i].Hidden
			}
		}
		return s.persist(profs, s.tr.T("tui.terminals.saved")), nil

	case km.String() == "x":
		if s.cursor < 0 {
			return s, nil
		}
		p := s.profiles[s.cursor]
		if p.Source != "user" {
			s.errMsg = s.tr.T("tui.terminals.cannot_delete")
			s.status = ""
			return s, nil
		}
		profs := make([]config.TerminalProfile, 0, len(s.cfg.Global.TerminalProfiles)-1)
		for _, q := range s.cfg.Global.TerminalProfiles {
			if q.ID != p.ID {
				profs = append(profs, q)
			}
		}
		return s.persist(profs, s.tr.T("tui.terminals.deleted")), nil

	case km.String() == "e":
		if s.cursor < 0 {
			return s, nil
		}
		p := s.profiles[s.cursor]
		s.editDraft = &profileDraft{
			originalID: p.ID,
			name:       p.Name,
			terminalID: p.TerminalID,
			shellID:    p.ShellID,
		}
		s.view = terminalsViewEdit
		s.formField = profileFieldName
		s.formSelTerm = indexOfApp(s.cfg.Global.TerminalApps, p.TerminalID)
		// Empty ShellID → -1 ("login shell" virtual entry on mac/Linux);
		// fall back to 0 on Windows where ShellID must always be set.
		if p.ShellID == "" && s.goos != "windows" {
			s.formSelSh = -1
		} else {
			s.formSelSh = indexOfShell(s.cfg.Global.Shells, p.ShellID)
		}
		s.nameInput.SetValue(p.Name)
		s.nameInput.Focus()
		s.status, s.errMsg = "", ""
		return s, textinput.Blink

	case km.String() == "a":
		// On Windows the user must pick a shell; on mac/Linux a Profile can
		// be Terminal-only (login shell implicit).
		if len(s.cfg.Global.TerminalApps) == 0 ||
			(s.goos == "windows" && len(s.cfg.Global.Shells) == 0) {
			s.errMsg = s.tr.T("tui.terminals.no_apps_add")
			s.status = ""
			return s, nil
		}
		s.addDraft = &profileDraft{
			terminalID: s.cfg.Global.TerminalApps[0].ID,
		}
		s.view = terminalsViewAdd
		s.formField = profileFieldName
		s.formSelTerm = 0
		// Default shell index: 0 on Windows, -1 ("login shell") on mac/Linux.
		if s.goos == "windows" {
			s.formSelSh = 0
			s.addDraft.shellID = s.cfg.Global.Shells[0].ID
		} else {
			s.formSelSh = -1
		}
		s.nameInput.SetValue("")
		s.nameInput.Focus()
		s.status, s.errMsg = "", ""
		return s, textinput.Blink
	}
	return s, nil
}

// updateForm handles the inline add/edit form. Pattern mirrors
// settingsModel: arrows + tab navigate, ←/→ change selects, Enter saves,
// ESC cancels back to the list.
func (s terminalsScreen) updateForm(msg tea.Msg) (terminalsScreen, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch {
	case key.Matches(km, Keys.Back):
		s.view = terminalsViewList
		s.addDraft, s.editDraft = nil, nil
		s.nameInput.Blur()
		return s, nil

	case km.String() == "tab":
		s.formField = (s.formField + 1) % 3
		if s.formField == profileFieldName {
			s.nameInput.Focus()
		} else {
			s.nameInput.Blur()
		}
		return s, nil

	case key.Matches(km, Keys.Up):
		if s.formField > 0 {
			s.formField--
			if s.formField == profileFieldName {
				s.nameInput.Focus()
			} else {
				s.nameInput.Blur()
			}
		}
		return s, nil

	case key.Matches(km, Keys.Down):
		if s.formField < 2 {
			s.formField++
			if s.formField == profileFieldName {
				s.nameInput.Focus()
			} else {
				s.nameInput.Blur()
			}
		}
		return s, nil

	case km.String() == "left" || km.String() == "h":
		switch s.formField {
		case profileFieldTerminal:
			if s.formSelTerm > 0 {
				s.formSelTerm--
			}
		case profileFieldShell:
			// On mac/Linux index -1 is the "(login shell)" virtual entry,
			// so the lower bound is -1 there; on Windows it stays at 0.
			min := 0
			if s.goos != "windows" {
				min = -1
			}
			if s.formSelSh > min {
				s.formSelSh--
			}
		}
		return s, nil

	case km.String() == "right" || km.String() == "l":
		switch s.formField {
		case profileFieldTerminal:
			if s.formSelTerm < len(s.cfg.Global.TerminalApps)-1 {
				s.formSelTerm++
			}
		case profileFieldShell:
			if s.formSelSh < len(s.cfg.Global.Shells)-1 {
				s.formSelSh++
			}
		}
		return s, nil

	case key.Matches(km, Keys.Enter):
		return s.commitForm()
	}

	if s.formField == profileFieldName {
		var cmd tea.Cmd
		s.nameInput, cmd = s.nameInput.Update(msg)
		return s, cmd
	}
	return s, nil
}

// commitForm validates and persists the form's draft. Add appends a new
// user Profile; edit replaces the original-ID row. Both routes return the
// model to listView and refresh the sort.
func (s terminalsScreen) commitForm() (terminalsScreen, tea.Cmd) {
	name := strings.TrimSpace(s.nameInput.Value())
	if name == "" {
		s.errMsg = s.tr.F("tui.terminals.error", "name is required")
		return s, nil
	}
	apps := s.cfg.Global.TerminalApps
	shells := s.cfg.Global.Shells
	if s.formSelTerm < 0 || s.formSelTerm >= len(apps) {
		s.errMsg = s.tr.F("tui.terminals.error", "select a terminal")
		return s, nil
	}
	// Windows requires a Shell pick (Profiles are Terminal × Shell pairs).
	// macOS / Linux allow an empty Shell — the special index -1 maps to the
	// "login shell" virtual entry rendered first in the selector.
	termID := apps[s.formSelTerm].ID
	var shID string
	if s.goos == "windows" {
		if s.formSelSh < 0 || s.formSelSh >= len(shells) {
			s.errMsg = s.tr.F("tui.terminals.error", "select a shell")
			return s, nil
		}
		shID = shells[s.formSelSh].ID
	} else if s.formSelSh >= 0 && s.formSelSh < len(shells) {
		shID = shells[s.formSelSh].ID
	}

	profs := append([]config.TerminalProfile(nil), s.cfg.Global.TerminalProfiles...)
	switch s.view {
	case terminalsViewEdit:
		for i := range profs {
			if profs[i].ID == s.editDraft.originalID {
				profs[i].Name = name
				profs[i].TerminalID = termID
				profs[i].ShellID = shID
				break
			}
		}
	case terminalsViewAdd:
		id := nextProfileID(profs, termID, shID, name)
		profs = append(profs, config.TerminalProfile{
			ID:         id,
			Name:       name,
			TerminalID: termID,
			ShellID:    shID,
			Source:     "user",
		})
	}
	s.view = terminalsViewList
	s.addDraft, s.editDraft = nil, nil
	s.nameInput.Blur()
	return s.persist(profs, s.tr.T("tui.terminals.saved")), nil
}

// persist writes the new TerminalProfiles slice to disk and refreshes
// the local cache. On error the status is replaced with the error so the
// user sees what failed without us silently swallowing it.
func (s terminalsScreen) persist(profs []config.TerminalProfile, label string) terminalsScreen {
	s.cfg.Global.TerminalProfiles = profs
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		s.errMsg = s.tr.F("tui.terminals.error", err.Error())
		s.status = ""
		return s
	}
	s.errMsg = ""
	s.status = label
	s.refreshProfiles()
	return s
}

func nextProfileID(existing []config.TerminalProfile, termID, shID, name string) string {
	base := slugifyTUI(termID + "-" + shID + "-" + name)
	if base == "" {
		base = fmt.Sprintf("user-%d", len(existing)+1)
	}
	taken := make(map[string]bool, len(existing))
	for _, p := range existing {
		taken[p.ID] = true
	}
	id := base
	for n := 2; taken[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// slugifyTUI is the package-local copy of the GUI's slug function. Lower-
// cases the input, replaces non-[a-z0-9] runs with single hyphens, and
// trims leading/trailing hyphens. Two helpers for the two frontends are
// fine — this one only sees ASCII Profile inputs.
func slugifyTUI(in string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(in) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func indexOfApp(apps []config.TerminalApp, id string) int {
	for i, a := range apps {
		if a.ID == id {
			return i
		}
	}
	return 0
}

func indexOfShell(shells []config.ShellEntry, id string) int {
	for i, s := range shells {
		if s.ID == id {
			return i
		}
	}
	return 0
}

// ─── View ─────────────────────────────────────────────────────────────────

func (s terminalsScreen) View() string {
	switch s.view {
	case terminalsViewAdd, terminalsViewEdit:
		return s.viewForm()
	default:
		return s.viewList()
	}
}

func (s terminalsScreen) viewList() string {
	var b strings.Builder
	b.WriteString(s.theme.Title.Render(s.tr.T("tui.terminals.title")) + "\n")
	b.WriteString(s.theme.TextMuted.Render(strings.Repeat("─", max(s.width, 60))) + "\n\n")

	// Issue #71: Windows hosts with no modern Terminal installed get a banner
	// at the top of the screen, mirroring the GUI Manager.
	if s.missingModern {
		warn := lipgloss.NewStyle().
			Foreground(lipgloss.Color(s.theme.Palette.AccentWarning)).
			Bold(true)
		b.WriteString("  " + warn.Render("⚠ Install Windows Terminal for the best experience.") + "\n")
		b.WriteString("  " + s.theme.TextMuted.Render("gitbox is using bare-shell launches as a fallback. A modern terminal hosts shells better.") + "\n\n")
	}

	// Detected terminals.
	b.WriteString(s.theme.Heading.Render(s.tr.T("tui.terminals.detected_apps")) + "\n")
	if len(s.cfg.Global.TerminalApps) == 0 {
		b.WriteString("  " + s.theme.TextMuted.Render(s.tr.T("tui.terminals.no_apps")) + "\n")
	} else {
		for _, a := range s.cfg.Global.TerminalApps {
			b.WriteString(fmt.Sprintf("  %-22s %s\n",
				s.theme.Text.Render(a.Name),
				s.theme.TextMuted.Render(a.Command)))
		}
	}
	b.WriteString("\n")

	// Detected shells.
	b.WriteString(s.theme.Heading.Render(s.tr.T("tui.terminals.detected_shells")) + "\n")
	if len(s.cfg.Global.Shells) == 0 {
		b.WriteString("  " + s.theme.TextMuted.Render(s.tr.T("tui.terminals.no_shells")) + "\n")
	} else {
		for _, sh := range s.cfg.Global.Shells {
			argsCol := strings.Join(sh.Args, " ")
			b.WriteString(fmt.Sprintf("  %-22s %-32s %s\n",
				s.theme.Text.Render(sh.Name),
				s.theme.TextMuted.Render(sh.Command),
				s.theme.TextMuted.Render(argsCol)))
		}
	}
	b.WriteString("\n")

	// Profiles.
	b.WriteString(s.theme.Heading.Render(s.tr.T("tui.terminals.profiles")) + "\n")
	if len(s.profiles) == 0 {
		b.WriteString("  " + s.theme.TextMuted.Render(s.tr.T("tui.terminals.no_profiles")) + "\n")
	} else {
		for i, p := range s.profiles {
			row := s.renderProfileRow(p)
			if i == s.cursor {
				b.WriteString(s.theme.SelectedRow.Render(row))
			} else if p.Hidden {
				b.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color(s.theme.Palette.TextMuted)).
					Faint(true).Render(row))
			} else {
				b.WriteString(s.theme.NormalRow.Render(row))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	// Status / error feedback line under the table.
	if s.status != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(s.theme.Palette.Clean)).
			Render(s.status) + "\n")
	}
	if s.errMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(s.theme.Palette.StatusError)).
			Render(s.errMsg) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(renderHints(s.theme,
		s.tr.T("tui.terminals.hint_actions"),
		s.tr.T("tui.hint.back")))
	return b.String()
}

// renderProfileRow lays out a single Profile line. The flag glyphs are
// kept ASCII-friendly so dumb terminals don't render boxes; the GUI uses
// a richer Unicode set in its table. Issue #71: source column dropped.
func (s terminalsScreen) renderProfileRow(p config.TerminalProfile) string {
	def := " "
	if p.Default {
		def = "*"
	}
	pref := " "
	if p.Preferred {
		pref = "+"
	}
	hid := " "
	if p.Hidden {
		hid = "H"
	}
	termName := s.appName(p.TerminalID)
	shName := s.shellName(p.ShellID)
	return fmt.Sprintf("  %s %s %s %-30s %-18s × %-18s",
		def, pref, hid, p.Name, termName, shName)
}

func (s terminalsScreen) appName(id string) string {
	for _, a := range s.cfg.Global.TerminalApps {
		if a.ID == id {
			return a.Name
		}
	}
	if id == "" {
		return "—"
	}
	return id
}

func (s terminalsScreen) shellName(id string) string {
	for _, sh := range s.cfg.Global.Shells {
		if sh.ID == id {
			return sh.Name
		}
	}
	if id == "" {
		// On macOS / Linux a Profile with empty ShellID launches the host's
		// login shell — surface that explicitly so the row reads "iTerm2 ×
		// login shell" instead of an opaque em-dash.
		if s.goos == "windows" {
			return "—"
		}
		return "login shell"
	}
	return id
}

func (s terminalsScreen) viewForm() string {
	var b strings.Builder
	titleKey := "tui.terminals.add_title"
	if s.view == terminalsViewEdit {
		titleKey = "tui.terminals.edit_title"
	}
	b.WriteString(s.theme.Title.Render(s.tr.T(titleKey)) + "\n")
	b.WriteString(s.theme.TextMuted.Render(strings.Repeat("─", max(s.width, 40))) + "\n\n")

	// Name field.
	nameLabel := fmt.Sprintf("  %-12s ", s.tr.T("tui.terminals.field_name"))
	if s.formField == profileFieldName {
		nameLabel = s.theme.Brand.Render(nameLabel)
	} else {
		nameLabel = s.theme.Text.Render(nameLabel)
	}
	b.WriteString(nameLabel + s.nameInput.View() + "\n\n")

	// Terminal selector.
	b.WriteString(s.renderSelector(s.tr.T("tui.terminals.field_terminal"),
		appNames(s.cfg.Global.TerminalApps), s.formSelTerm,
		s.formField == profileFieldTerminal) + "\n\n")

	// Shell selector. On macOS / Linux the catalog Profiles are Terminal-
	// only — the "(login shell)" virtual entry lives at index -1 of the
	// selector and produces ShellID="" when picked. On Windows the shell
	// pick is mandatory; the virtual entry is suppressed.
	shellOpts := shellNames(s.cfg.Global.Shells)
	shellCursor := s.formSelSh
	if s.goos != "windows" {
		shellOpts = append([]string{"(login shell)"}, shellOpts...)
		shellCursor = s.formSelSh + 1
	}
	b.WriteString(s.renderSelector(s.tr.T("tui.terminals.field_shell"),
		shellOpts, shellCursor,
		s.formField == profileFieldShell) + "\n\n")

	if s.errMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(s.theme.Palette.StatusError)).
			Render(s.errMsg) + "\n\n")
	}

	b.WriteString(renderHints(s.theme,
		s.tr.T("tui.hint.navigate"),
		s.tr.T("tui.hint.change"),
		s.tr.T("tui.hint.save"),
		s.tr.T("tui.hint.back")))
	return b.String()
}

func (s terminalsScreen) renderSelector(label string, options []string, cursor int, active bool) string {
	header := fmt.Sprintf("  %-12s ", label)
	if active {
		header = s.theme.Brand.Render(header)
	} else {
		header = s.theme.Text.Render(header)
	}
	if len(options) == 0 {
		return header + s.theme.TextMuted.Render("(none)")
	}
	if cursor < 0 || cursor >= len(options) {
		cursor = 0
	}
	current := options[cursor]
	pos := fmt.Sprintf("[%d/%d]", cursor+1, len(options))
	value := lipgloss.NewStyle().
		Foreground(lipgloss.Color(s.theme.Palette.Brand)).
		Bold(true).
		Render("[" + current + "]")
	return header + value + " " + s.theme.TextMuted.Render(pos)
}

func appNames(apps []config.TerminalApp) []string {
	out := make([]string, len(apps))
	for i, a := range apps {
		out[i] = a.Name
	}
	return out
}

func shellNames(shells []config.ShellEntry) []string {
	out := make([]string, len(shells))
	for i, sh := range shells {
		out[i] = sh.Name
	}
	return out
}

