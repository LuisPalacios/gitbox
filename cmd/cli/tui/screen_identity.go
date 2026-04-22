package tui

import (
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// identitySection selects which panel on the Global Gitconfig screen
// has keyboard focus. The screen shows identity first, then GCM.
type identitySection int

const (
	sectionIdentity identitySection = iota
	sectionGCM
)

type identityModel struct {
	theme         styles.Theme
	width, height int
	cfg           *config.Config
	cfgPath       string

	// Section: Global Identity (user.name / user.email in ~/.gitconfig).
	hasName        bool
	hasEmail       bool
	name, email    string
	removed        bool
	errMsg         string
	confirmPending bool

	// Section: Global GCM config (credential.helper / credential.credentialStore).
	gcmNeeded         bool
	gcmStatus         credential.GlobalGCMConfigStatus
	gcmFixed          bool
	gcmErrMsg         string
	gcmConfirmPending bool

	activeSection identitySection
}

func newIdentityModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int) identityModel {
	return identityModel{
		theme:         theme,
		width:         w,
		height:        h,
		cfg:           cfg,
		cfgPath:       cfgPath,
		activeSection: sectionIdentity,
	}
}

func (m identityModel) Init() tea.Cmd {
	return tea.Batch(checkGlobalIdentityCmd(), checkGlobalGCMCmd(m.cfg))
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

func checkGlobalGCMCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		return globalGCMMsg{
			needed: credential.IsGlobalGCMConfigNeeded(cfg),
			status: credential.CheckGlobalGCMConfig(cfg),
		}
	}
}

func fixGlobalGCMCmd(cfg *config.Config, cfgPath string) tea.Cmd {
	return func() tea.Msg {
		return gcmFixedMsg{err: credential.FixGlobalGCMConfig(cfg, cfgPath)}
	}
}

func (m identityModel) Update(msg tea.Msg) (identityModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, Keys.Back):
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }

		case key.Matches(msg, Keys.Down):
			if m.activeSection == sectionIdentity && (m.gcmNeeded && (m.gcmStatus.NeedsFix || m.gcmFixed)) {
				m.activeSection = sectionGCM
			}
			return m, nil
		case key.Matches(msg, Keys.Up):
			if m.activeSection == sectionGCM {
				m.activeSection = sectionIdentity
			}
			return m, nil

		case key.Matches(msg, Keys.Enter):
			switch m.activeSection {
			case sectionIdentity:
				if m.confirmPending {
					m.confirmPending = false
					return m, removeGlobalIdentityCmd()
				}
				if (m.hasName || m.hasEmail) && !m.removed {
					m.confirmPending = true
				}
			case sectionGCM:
				if m.gcmConfirmPending {
					m.gcmConfirmPending = false
					return m, fixGlobalGCMCmd(m.cfg, m.cfgPath)
				}
				if m.gcmNeeded && m.gcmStatus.NeedsFix && !m.gcmFixed {
					m.gcmConfirmPending = true
				}
			}
			return m, nil

		case msg.String() == "n":
			if m.confirmPending {
				m.confirmPending = false
				return m, nil
			}
			if m.gcmConfirmPending {
				m.gcmConfirmPending = false
				return m, nil
			}
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

	case globalGCMMsg:
		m.gcmNeeded = msg.needed
		m.gcmStatus = msg.status
		return m, nil

	case gcmFixedMsg:
		if msg.err != nil {
			m.gcmErrMsg = msg.err.Error()
		} else {
			m.gcmFixed = true
			// Refresh status so the panel shows the post-fix state.
			m.gcmStatus = credential.CheckGlobalGCMConfig(m.cfg)
		}
		return m, nil
	}
	return m, nil
}

func (m identityModel) View() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Global Gitconfig") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	b.WriteString(m.renderIdentitySection())

	if m.gcmNeeded {
		b.WriteString("\n")
		b.WriteString(m.renderGCMSection())
	}

	b.WriteString("\n")
	b.WriteString(m.footerHints())
	return b.String()
}

func (m identityModel) renderIdentitySection() string {
	var b strings.Builder
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean))

	heading := "Identity"
	if m.activeSection == sectionIdentity {
		heading = m.theme.TextBold.Render("▸ Identity")
	} else {
		heading = m.theme.TextMuted.Render("  Identity")
	}
	b.WriteString(heading + "\n")

	if m.removed {
		b.WriteString("  " + ok.Render(styles.SymClean+" Global identity removed.") + "\n")
		return b.String()
	}

	if !m.hasName && !m.hasEmail {
		b.WriteString("  " + ok.Render(styles.SymClean+" No global identity set in ~/.gitconfig") + "\n")
		b.WriteString(m.theme.TextMuted.Render("  This is the recommended configuration.") + "\n")
		return b.String()
	}

	b.WriteString("  " + warn.Render(styles.SymDiverged+" WARNING: Global identity set in ~/.gitconfig") + "\n")
	if m.hasName {
		b.WriteString("  user.name  = " + m.theme.TextBold.Render(m.name) + "\n")
	}
	if m.hasEmail {
		b.WriteString("  user.email = " + m.theme.TextBold.Render(m.email) + "\n")
	}
	b.WriteString(m.theme.TextMuted.Render("  gitbox sets per-repo identity from account config.") + "\n")
	b.WriteString(m.theme.TextMuted.Render("  Global identity can override this and cause wrong commits.") + "\n")

	if m.confirmPending {
		b.WriteString("  " + warn.Render("Remove global identity? (enter=yes, n=no)") + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.errMsg) + "\n")
	}
	return b.String()
}

func (m identityModel) renderGCMSection() string {
	var b strings.Builder
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
	ok := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean))

	heading := "Credential helper (GCM)"
	if m.activeSection == sectionGCM {
		heading = m.theme.TextBold.Render("▸ Credential helper (GCM)")
	} else {
		heading = m.theme.TextMuted.Render("  Credential helper (GCM)")
	}
	b.WriteString(heading + "\n")

	if m.gcmFixed || !m.gcmStatus.NeedsFix {
		b.WriteString("  " + ok.Render(styles.SymClean+" Global gitconfig is correctly wired for GCM.") + "\n")
		b.WriteString("  credential.helper         = " + m.theme.TextBold.Render(m.gcmStatus.ExpectedHelper) + "\n")
		b.WriteString("  credential.credentialStore = " + m.theme.TextBold.Render(m.gcmStatus.ExpectedCredentialStore) + "\n")
		return b.String()
	}

	b.WriteString("  " + warn.Render(styles.SymDiverged+" Global gitconfig is missing GCM settings") + "\n")
	b.WriteString("  credential.helper         : " + renderExpectedFound(m.theme, m.gcmStatus.ExpectedHelper, m.gcmStatus.HelperValue, m.gcmStatus.HasHelper) + "\n")
	b.WriteString("  credential.credentialStore: " + renderExpectedFound(m.theme, m.gcmStatus.ExpectedCredentialStore, m.gcmStatus.CredentialStoreValue, m.gcmStatus.HasCredentialStore) + "\n")
	b.WriteString(m.theme.TextMuted.Render("  Without these, GCM fills fall through to a TTY prompt") + "\n")
	b.WriteString(m.theme.TextMuted.Render("  and fail with \"Device not configured\" in the GUI.") + "\n")

	if m.gcmConfirmPending {
		b.WriteString("  " + warn.Render("Write ~/.gitconfig and update gitbox.json? (enter=yes, n=no)") + "\n")
	}

	if m.gcmErrMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.gcmErrMsg) + "\n")
	}
	return b.String()
}

// renderExpectedFound formats an "expected X, found Y" line for mismatched
// config values. When the key is absent it renders "missing" in muted text.
func renderExpectedFound(theme styles.Theme, expected, found string, has bool) string {
	if !has {
		return theme.TextBold.Render(expected) + theme.TextMuted.Render("  (missing)")
	}
	return theme.TextBold.Render(expected) + theme.TextMuted.Render("  (found: "+found+")")
}

func (m identityModel) footerHints() string {
	var hints []string
	switch m.activeSection {
	case sectionIdentity:
		if m.confirmPending {
			// No explicit hint needed: confirm message is shown inline.
		} else if (m.hasName || m.hasEmail) && !m.removed {
			hints = append(hints, "enter remove global identity")
		}
	case sectionGCM:
		if m.gcmConfirmPending {
			// Inline.
		} else if m.gcmNeeded && m.gcmStatus.NeedsFix && !m.gcmFixed {
			hints = append(hints, "enter fix global gitconfig")
		}
	}
	if m.gcmNeeded && (m.gcmStatus.NeedsFix || m.gcmFixed) {
		hints = append(hints, "↑↓ switch section")
	}
	hints = append(hints, "ESC back")
	return renderHints(m.theme, hints...)
}
