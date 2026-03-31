package tui

import (
	"fmt"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── selectField ──────────────────────────────────────────────────────────

// selectField is a single-choice selector navigated with ←/→.
type selectField struct {
	Label   string
	Options []string
	Cursor  int
}

func newSelectField(label string, options []string) selectField {
	return selectField{Label: label, Options: options}
}

func (s *selectField) Value() string {
	if s.Cursor < len(s.Options) {
		return s.Options[s.Cursor]
	}
	return ""
}

func (s *selectField) SetValue(v string) {
	for i, opt := range s.Options {
		if opt == v {
			s.Cursor = i
			return
		}
	}
}

func (s *selectField) Left() {
	if s.Cursor > 0 {
		s.Cursor--
	}
}

func (s *selectField) Right() {
	if s.Cursor < len(s.Options)-1 {
		s.Cursor++
	}
}

func (s selectField) View(active bool, theme styles.Theme) string {
	var opts []string
	for i, opt := range s.Options {
		if i == s.Cursor {
			opts = append(opts, lipgloss.NewStyle().
				Foreground(lipgloss.Color(theme.Palette.Brand)).
				Bold(true).
				Render("["+opt+"]"))
		} else {
			opts = append(opts, theme.TextMuted.Render(" "+opt+" "))
		}
	}

	label := s.Label
	if active {
		label = theme.Brand.Render(label)
	} else {
		label = theme.Text.Render(label)
	}
	return label + strings.Join(opts, " ")
}

// ─── formField ────────────────────────────────────────────────────────────

// formFieldKind distinguishes text inputs from selects.
type formFieldKind int

const (
	fieldText formFieldKind = iota
	fieldPassword
	fieldSelect
)

// formField wraps either a textinput or a selectField.
type formField struct {
	Label      string
	Kind       formFieldKind
	TextInput  textinput.Model
	Select     selectField
	ValidateFn func(string) string // returns error message or ""
}

func newTextField(label, placeholder string, charLimit int) formField {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = charLimit
	ti.Width = 40
	return formField{Label: label, Kind: fieldText, TextInput: ti}
}

func newPasswordField(label, placeholder string, charLimit int) formField {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = charLimit
	ti.Width = 40
	ti.EchoMode = textinput.EchoPassword
	return formField{Label: label, Kind: fieldPassword, TextInput: ti}
}

func newSelectFormField(label string, options []string) formField {
	return formField{
		Label:  label,
		Kind:   fieldSelect,
		Select: newSelectField(label, options),
	}
}

func (f formField) Value() string {
	if f.Kind == fieldSelect {
		return f.Select.Value()
	}
	return f.TextInput.Value()
}

// ─── formModel ────────────────────────────────────────────────────────────

// formModel manages a vertical list of form fields.
type formModel struct {
	Fields   []formField
	Active   int
	ErrMsg   string
	theme    styles.Theme
	title    string
	submitted bool
}

func newFormModel(title string, fields []formField, theme styles.Theme) formModel {
	if len(fields) > 0 && fields[0].Kind != fieldSelect {
		fields[0].TextInput.Focus()
	}
	return formModel{
		Fields: fields,
		theme:  theme,
		title:  title,
	}
}

func (m formModel) Init() tea.Cmd {
	if len(m.Fields) > 0 && m.Fields[0].Kind != fieldSelect {
		return textinput.Blink
	}
	return nil
}

func (m *formModel) focusCurrent() {
	for i := range m.Fields {
		if m.Fields[i].Kind == fieldSelect {
			continue
		}
		if i == m.Active {
			m.Fields[i].TextInput.Focus()
		} else {
			m.Fields[i].TextInput.Blur()
		}
	}
}

// Update handles form navigation. Returns true if the form was submitted.
func (m *formModel) Update(msg tea.Msg) (bool, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, Keys.Up):
			if m.Active > 0 {
				m.Active--
				m.focusCurrent()
			}
			return false, nil

		case key.Matches(msg, Keys.Down):
			if m.Active < len(m.Fields)-1 {
				m.Active++
				m.focusCurrent()
			}
			return false, nil

		case msg.String() == "left" || msg.String() == "h":
			if m.Fields[m.Active].Kind == fieldSelect {
				m.Fields[m.Active].Select.Left()
				return false, nil
			}

		case msg.String() == "right" || msg.String() == "l":
			if m.Fields[m.Active].Kind == fieldSelect {
				m.Fields[m.Active].Select.Right()
				return false, nil
			}

		case msg.String() == "tab":
			if m.Active < len(m.Fields)-1 {
				m.Active++
			} else {
				m.Active = 0
			}
			m.focusCurrent()
			return false, nil

		case key.Matches(msg, Keys.Enter):
			// Validate all fields.
			for i, f := range m.Fields {
				if f.ValidateFn != nil {
					if errMsg := f.ValidateFn(f.Value()); errMsg != "" {
						m.Active = i
						m.focusCurrent()
						m.ErrMsg = errMsg
						return false, nil
					}
				}
			}
			m.ErrMsg = ""
			m.submitted = true
			return true, nil
		}
	}

	// Update the active text input.
	if m.Active < len(m.Fields) && m.Fields[m.Active].Kind != fieldSelect {
		var cmd tea.Cmd
		m.Fields[m.Active].TextInput, cmd = m.Fields[m.Active].TextInput.Update(msg)
		return false, cmd
	}
	return false, nil
}

func (m formModel) View() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render(m.title) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", 40)) + "\n\n")

	for i, f := range m.Fields {
		active := i == m.Active
		switch f.Kind {
		case fieldSelect:
			b.WriteString("  " + f.Select.View(active, m.theme) + "\n\n")
		default:
			label := fmt.Sprintf("  %-18s", f.Label)
			if active {
				label = m.theme.Brand.Render(label)
			} else {
				label = m.theme.Text.Render(label)
			}
			b.WriteString(label + f.TextInput.View() + "\n\n")
		}
	}

	if m.ErrMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.ErrMsg) + "\n\n")
	}

	// Build context-aware help text.
	var hints []string
	if len(m.Fields) > 1 {
		hints = append(hints, "↑↓ navigate", "tab next")
	}
	hasSelect := false
	for _, f := range m.Fields {
		if f.Kind == fieldSelect {
			hasSelect = true
			break
		}
	}
	if hasSelect {
		hints = append(hints, "←→ select")
	}
	hints = append(hints, "enter submit", "ESC cancel")
	b.WriteString(renderHints(m.theme, hints...))

	return b.String()
}

// renderHints renders keyboard hints with keys in Brand (blue) and
// descriptions in TextMuted. Each hint is "key desc" (first word is the key).
// Example: renderHints(theme, "enter submit", "ESC back") →
//
//	"  enter submit  ESC back"  (enter/ESC in blue, submit/back in muted)
func renderHints(theme styles.Theme, hints ...string) string {
	var parts []string
	for _, h := range hints {
		if i := strings.IndexByte(h, ' '); i > 0 {
			parts = append(parts, theme.HelpKey.Render(h[:i])+" "+theme.HelpDesc.Render(h[i+1:]))
		} else {
			parts = append(parts, theme.HelpKey.Render(h))
		}
	}
	return "  " + strings.Join(parts, "  ")
}

// renderHintsFit renders hints that fit within maxWidth, dropping hints
// from the right (before the last hint) when the bar is too wide.
// The last hint (typically "ESC back" / "ESC quit") is always kept.
func renderHintsFit(theme styles.Theme, maxWidth int, hints ...string) string {
	full := renderHints(theme, hints...)
	if maxWidth <= 0 || lipgloss.Width(full) <= maxWidth {
		return full
	}
	if len(hints) <= 2 {
		return full
	}

	// Drop hints right-to-left, keeping the last one (ESC).
	last := hints[len(hints)-1]
	for n := len(hints) - 2; n >= 1; n-- {
		trial := make([]string, n, n+1)
		copy(trial, hints[:n])
		trial = append(trial, last)
		bar := renderHints(theme, trial...)
		if lipgloss.Width(bar) <= maxWidth {
			return bar
		}
	}
	return renderHints(theme, last)
}

// ─── confirmDialog ────────────────────────────────────────────────────────

// confirmDialog renders a yes/no prompt inline.
type confirmDialog struct {
	Message string
	Active  bool
}

func (c confirmDialog) View(theme styles.Theme) string {
	if !c.Active {
		return ""
	}
	return "\n  " + lipgloss.NewStyle().
		Foreground(lipgloss.Color(theme.Palette.AccentDanger)).
		Render(c.Message+" (enter=yes, n=no)") + "\n"
}
