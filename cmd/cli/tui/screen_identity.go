package tui

import (
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type identityModel struct {
	theme          styles.Theme
	width, height  int
	hasName        bool
	hasEmail       bool
	name, email    string
	removed        bool
	errMsg         string
	confirmPending bool
}

func newIdentityModel(theme styles.Theme, w, h int) identityModel {
	return identityModel{
		theme:  theme,
		width:  w,
		height: h,
	}
}

func (m identityModel) Init() tea.Cmd {
	return checkGlobalIdentityCmd()
}

func checkGlobalIdentityCmd() tea.Cmd {
	return func() tea.Msg {
		gs := identity.CheckGlobalIdentity()
		return globalIdentityMsg{hasName: gs.HasName, hasEmail: gs.HasEmail}
	}
}

func removeGlobalIdentityCmd() tea.Cmd {
	return func() tea.Msg {
		err := identity.RemoveGlobalIdentity()
		return identityRemovedMsg{err: err}
	}
}

func (m identityModel) Update(msg tea.Msg) (identityModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, Keys.Back):
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }
		case key.Matches(msg, Keys.Enter):
			if m.confirmPending {
				m.confirmPending = false
				return m, removeGlobalIdentityCmd()
			}
			if (m.hasName || m.hasEmail) && !m.removed {
				m.confirmPending = true
			}
			return m, nil
		case msg.String() == "n" && m.confirmPending:
			m.confirmPending = false
			return m, nil
		}

	case globalIdentityMsg:
		m.hasName = msg.hasName
		m.hasEmail = msg.hasEmail
		gs := identity.CheckGlobalIdentity()
		m.name = gs.Name
		m.email = gs.Email
		return m, nil

	case identityRemovedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.removed = true
			m.hasName = false
			m.hasEmail = false
		}
		return m, nil
	}
	return m, nil
}

func (m identityModel) View() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Global Identity") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	if m.removed {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(styles.SymClean+" Global identity removed.") + "\n\n")
		b.WriteString(renderHints(m.theme, "ESC back"))
		return b.String()
	}

	if !m.hasName && !m.hasEmail {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(styles.SymClean+" No global identity set in ~/.gitconfig") + "\n")
		b.WriteString(m.theme.TextMuted.Render("  This is the recommended configuration.") + "\n\n")
		b.WriteString(renderHints(m.theme, "ESC back"))
		return b.String()
	}

	// Warning: global identity found.
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
	b.WriteString("  " + warn.Render(styles.SymDiverged+" WARNING: Global identity set in ~/.gitconfig") + "\n\n")

	if m.hasName {
		b.WriteString("  user.name  = " + m.theme.TextBold.Render(m.name) + "\n")
	}
	if m.hasEmail {
		b.WriteString("  user.email = " + m.theme.TextBold.Render(m.email) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(m.theme.TextMuted.Render("  gitbox sets per-repo identity from account config.") + "\n")
	b.WriteString(m.theme.TextMuted.Render("  Global identity can override this and cause wrong commits.") + "\n\n")

	if m.confirmPending {
		b.WriteString("  " + warn.Render("Remove global identity? (enter=yes, n=no)") + "\n")
	} else {
		b.WriteString(renderHints(m.theme, "enter remove global identity", "ESC back") + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.errMsg) + "\n")
	}

	return b.String()
}
