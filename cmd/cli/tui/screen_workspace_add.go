package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// workspaceAddView selects which sub-view of the add screen is active.
type workspaceAddView int

const (
	workspaceAddViewForm workspaceAddView = iota
	workspaceAddViewMembers
)

// workspaceAddModel drives the create-workspace flow: fields at top
// (key / name / type / layout) and a multi-select member picker below.
// The two are distinct sub-views so we get a clean single input focus
// at a time, matching the keyboard-only conventions used elsewhere.
type workspaceAddModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int

	view workspaceAddView
	form formModel

	// All "sourceKey/repoKey" entries, ordered by source then repo.
	members []string
	// Index in `members` → selected.
	selected map[int]bool
	// Cursor into `members` while in the member view.
	memberCursor int

	busy    bool
	errMsg  string
	okMsg   string
	done    bool // true after successful create; switches screen back
}

// workspaceAddSubmittedMsg fires when the config.Save after create
// succeeds; the screen then switches back to the dashboard.
type workspaceAddSubmittedMsg struct {
	key string
	err error
}

func newWorkspaceAddModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int, preselect []string) workspaceAddModel {
	// Form fields: key (required), name (optional), type, layout.
	fields := []formField{
		newTextField("Key", "e.g. feat-x", 40),
		newTextField("Name", "Display name (defaults to key)", 60),
		newSelectFormField("Type", []string{"codeWorkspace", "tmuxinator"}),
		newSelectFormField("Layout", []string{"windowsPerRepo", "splitPanes"}),
	}
	// Validate key on submit.
	fields[0].ValidateFn = func(v string) string {
		if strings.TrimSpace(v) == "" {
			return "key is required"
		}
		if _, exists := cfg.Workspaces[v]; exists {
			return fmt.Sprintf("workspace %q already exists", v)
		}
		return ""
	}

	form := newFormModel("Create workspace", fields, theme)

	// Build the member list in a stable order.
	var members []string
	sourceKeys := make([]string, 0, len(cfg.Sources))
	for k := range cfg.Sources {
		sourceKeys = append(sourceKeys, k)
	}
	sort.Strings(sourceKeys)
	for _, sk := range sourceKeys {
		src := cfg.Sources[sk]
		repoKeys := src.OrderedRepoKeys()
		if len(repoKeys) == 0 {
			for rk := range src.Repos {
				repoKeys = append(repoKeys, rk)
			}
			sort.Strings(repoKeys)
		}
		for _, rk := range repoKeys {
			members = append(members, sk+"/"+rk)
		}
	}

	selected := make(map[int]bool)
	for _, pre := range preselect {
		for i, m := range members {
			if m == pre {
				selected[i] = true
				break
			}
		}
	}

	return workspaceAddModel{
		cfg:       cfg,
		cfgPath:   cfgPath,
		theme:     theme,
		width:     w,
		height:    h,
		view:      workspaceAddViewForm,
		form:      form,
		members:   members,
		selected:  selected,
	}
}

func (m workspaceAddModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m workspaceAddModel) Update(msg tea.Msg) (workspaceAddModel, tea.Cmd) {
	switch msg := msg.(type) {
	case workspaceAddSubmittedMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.okMsg = fmt.Sprintf("Workspace %q created.", msg.key)
		m.done = true
		return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }

	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}
		// ESC always cancels back to dashboard unless something has focus.
		if key.Matches(msg, Keys.Back) {
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }
		}

		switch m.view {
		case workspaceAddViewForm:
			// Tab: hand off to member picker (if members exist).
			if msg.String() == "tab" && len(m.members) > 0 {
				m.view = workspaceAddViewMembers
				return m, nil
			}
			submitted, cmd := m.form.Update(msg)
			if submitted {
				// Enter on the form advances to the member picker rather
				// than submitting — users need to pick members first.
				m.form.submitted = false
				if len(m.members) == 0 {
					m.errMsg = "No sources configured — can't pick members"
					return m, nil
				}
				m.view = workspaceAddViewMembers
				m.errMsg = ""
				return m, nil
			}
			return m, cmd

		case workspaceAddViewMembers:
			switch {
			case msg.String() == "tab":
				m.view = workspaceAddViewForm
				return m, nil
			case key.Matches(msg, Keys.Up):
				if m.memberCursor > 0 {
					m.memberCursor--
				}
				return m, nil
			case key.Matches(msg, Keys.Down):
				if m.memberCursor < len(m.members)-1 {
					m.memberCursor++
				}
				return m, nil
			case msg.String() == " ":
				m.selected[m.memberCursor] = !m.selected[m.memberCursor]
				return m, nil
			case msg.String() == "A":
				for i := range m.members {
					m.selected[i] = true
				}
				return m, nil
			case msg.String() == "C":
				m.selected = make(map[int]bool)
				return m, nil
			case key.Matches(msg, Keys.Enter):
				return m.submit()
			}
		}
	}
	return m, nil
}

func (m workspaceAddModel) selectedMembers() []config.WorkspaceMember {
	out := make([]config.WorkspaceMember, 0, len(m.selected))
	for i, m2 := range m.members {
		if !m.selected[i] {
			continue
		}
		slash := strings.IndexByte(m2, '/')
		if slash <= 0 {
			continue
		}
		out = append(out, config.WorkspaceMember{Source: m2[:slash], Repo: m2[slash+1:]})
	}
	return out
}

func (m workspaceAddModel) submit() (workspaceAddModel, tea.Cmd) {
	key := strings.TrimSpace(m.form.Fields[0].Value())
	if key == "" {
		m.view = workspaceAddViewForm
		m.form.Active = 0
		m.form.ErrMsg = "key is required"
		return m, nil
	}
	if _, exists := m.cfg.Workspaces[key]; exists {
		m.view = workspaceAddViewForm
		m.form.Active = 0
		m.form.ErrMsg = fmt.Sprintf("workspace %q already exists", key)
		return m, nil
	}
	members := m.selectedMembers()
	if len(members) == 0 {
		m.errMsg = "Select at least one member (space / A)"
		return m, nil
	}

	name := strings.TrimSpace(m.form.Fields[1].Value())
	wsType := m.form.Fields[2].Value()
	layout := ""
	if wsType == config.WorkspaceTypeTmuxinator {
		layout = m.form.Fields[3].Value()
	}

	ws := config.Workspace{
		Type:    wsType,
		Name:    name,
		Layout:  layout,
		Members: members,
	}

	m.busy = true
	m.errMsg = ""

	cfg := m.cfg
	cfgPath := m.cfgPath
	return m, func() tea.Msg {
		if err := cfg.AddWorkspace(key, ws); err != nil {
			return workspaceAddSubmittedMsg{err: err}
		}
		if err := config.Save(cfg, cfgPath); err != nil {
			return workspaceAddSubmittedMsg{err: err}
		}
		return workspaceAddSubmittedMsg{key: key}
	}
}

func (m workspaceAddModel) View() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Create workspace") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", 40)) + "\n\n")

	// Form (always rendered so the user sees the context).
	b.WriteString(m.renderFormCompact())
	b.WriteString("\n")

	// Member picker.
	b.WriteString(m.renderMemberPicker())

	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.errMsg) + "\n")
	}
	if m.okMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(m.okMsg) + "\n")
	}

	b.WriteString("\n")
	if m.view == workspaceAddViewForm {
		b.WriteString(renderHints(m.theme,
			"↑↓ navigate", "←→ select", "tab members", "enter next", "ESC cancel"))
	} else {
		b.WriteString(renderHints(m.theme,
			"↑↓ move", "space toggle", "A all", "C clear", "tab form", "enter create", "ESC cancel"))
	}
	return b.String()
}

func (m workspaceAddModel) renderFormCompact() string {
	var b strings.Builder
	for i, f := range m.form.Fields {
		active := m.view == workspaceAddViewForm && i == m.form.Active
		switch f.Kind {
		case fieldSelect:
			b.WriteString("  " + f.Select.View(active, m.theme) + "\n")
		default:
			label := fmt.Sprintf("  %-8s", f.Label)
			if active {
				label = m.theme.Brand.Render(label)
			} else {
				label = m.theme.Text.Render(label)
			}
			b.WriteString(label + " " + f.TextInput.View() + "\n")
		}
	}
	if m.form.ErrMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Form: "+m.form.ErrMsg) + "\n")
	}
	return b.String()
}

func (m workspaceAddModel) renderMemberPicker() string {
	var b strings.Builder
	title := "Members"
	if m.view == workspaceAddViewMembers {
		title = m.theme.Brand.Render(title)
	} else {
		title = m.theme.Text.Render(title)
	}
	selCount := 0
	for _, v := range m.selected {
		if v {
			selCount++
		}
	}
	b.WriteString(fmt.Sprintf("  %s  %s\n", title,
		m.theme.TextMuted.Render(fmt.Sprintf("%d/%d selected", selCount, len(m.members)))))
	if len(m.members) == 0 {
		b.WriteString("  " + m.theme.TextMuted.Render("(no sources configured)") + "\n")
		return b.String()
	}
	// Scroll window centered on cursor when the list is taller than available rows.
	maxRows := 10
	start := 0
	if m.memberCursor > maxRows/2 {
		start = m.memberCursor - maxRows/2
	}
	end := start + maxRows
	if end > len(m.members) {
		end = len(m.members)
	}
	for i := start; i < end; i++ {
		marker := "[ ]"
		if m.selected[i] {
			marker = "[x]"
		}
		cursor := "  "
		if m.view == workspaceAddViewMembers && i == m.memberCursor {
			cursor = "> "
		}
		line := fmt.Sprintf("%s%s %s", cursor, marker, m.members[i])
		if m.view == workspaceAddViewMembers && i == m.memberCursor {
			line = m.theme.Brand.Render(line)
		} else {
			line = m.theme.Text.Render(line)
		}
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}
