package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/i18n"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// terminalsScreen owns the v2.1 TerminalApps + Shells + TerminalProfiles
// editor (issue #69 — TUI parity with the GUI's TerminalsModal).
//
// Apps + shells are read-only here: they're populated by the GUI's
// SyncProfiles probe and persisted to gitbox.json. The TUI doesn't run
// the probe itself yet — if the lists are empty the UI tells the user
// to launch the GUI once. (TUI-side detection is deferred to keep this
// commit focused; tracked in the issue.)
//
// Profiles are fully editable: the user can toggle Default / Preferred
// / Hidden, rename + reassign existing entries, add new (Terminal +
// Shell) Profiles, and delete user-added Profiles. Deletion is
// restricted to entries with source="user" — auto-detected, WT-imported,
// WezTerm-imported, and migrated Profiles can only be hidden, never
// removed (their source pipeline would just re-create them on the next
// detect cycle anyway).
type terminalsScreen struct {
	cfg     *config.Config
	cfgPath string
	theme   styles.Theme
	tr      i18n.Translator
	width   int
	height  int

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
		nameInput: ti,
	}
	s.refreshProfiles()
	return s
}

func (s terminalsScreen) Init() tea.Cmd { return nil }

// refreshProfiles re-pulls the Profile slice from cfg, sorted in stable
// presentation order. Called after every save so the table reflects what
// the user just persisted without needing a separate "reload" step.
func (s *terminalsScreen) refreshProfiles() {
	src := append([]config.TerminalProfile(nil), s.cfg.Global.TerminalProfiles...)
	// Sort: visible first, then by source priority (user → wt-profile →
	// wezterm-launchmenu → migrated → detected → other), then by Name.
	// Keeps the cursor anchored even after the user toggles Hidden.
	sort.SliceStable(src, func(i, j int) bool {
		if src[i].Hidden != src[j].Hidden {
			return !src[i].Hidden
		}
		pi, pj := sourcePriority(src[i].Source), sourcePriority(src[j].Source)
		if pi != pj {
			return pi < pj
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

func sourcePriority(src string) int {
	switch src {
	case "user":
		return 0
	case "wt-profile":
		return 1
	case "wezterm-launchmenu":
		return 2
	case "migrated":
		return 3
	case "detected":
		return 4
	}
	return 5
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
		s.formSelSh = indexOfShell(s.cfg.Global.Shells, p.ShellID)
		s.nameInput.SetValue(p.Name)
		s.nameInput.Focus()
		s.status, s.errMsg = "", ""
		return s, textinput.Blink

	case km.String() == "a":
		if len(s.cfg.Global.TerminalApps) == 0 || len(s.cfg.Global.Shells) == 0 {
			s.errMsg = s.tr.T("tui.terminals.no_apps_add")
			s.status = ""
			return s, nil
		}
		s.addDraft = &profileDraft{
			terminalID: s.cfg.Global.TerminalApps[0].ID,
			shellID:    s.cfg.Global.Shells[0].ID,
		}
		s.view = terminalsViewAdd
		s.formField = profileFieldName
		s.formSelTerm = 0
		s.formSelSh = 0
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
			if s.formSelSh > 0 {
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
	if s.formSelTerm < 0 || s.formSelTerm >= len(apps) ||
		s.formSelSh < 0 || s.formSelSh >= len(shells) {
		s.errMsg = s.tr.F("tui.terminals.error", "select a terminal and a shell")
		return s, nil
	}
	termID := apps[s.formSelTerm].ID
	shID := shells[s.formSelSh].ID

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
// a richer Unicode set in its table.
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
	src := p.Source
	if src == "" {
		src = "—"
	}
	return fmt.Sprintf("  %s %s %s %-30s %-18s × %-18s [%s]",
		def, pref, hid, p.Name, termName, shName, src)
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
		return "—"
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

	// Shell selector.
	b.WriteString(s.renderSelector(s.tr.T("tui.terminals.field_shell"),
		shellNames(s.cfg.Global.Shells), s.formSelSh,
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

