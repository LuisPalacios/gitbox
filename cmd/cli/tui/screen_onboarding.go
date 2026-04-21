package tui

import (
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type onboardingModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int
	folderInput   textinput.Model
	done          bool
	errMsg        string
}

func newOnboardingModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int) onboardingModel {
	ti := textinput.New()
	ti.Placeholder = "~/00.git"
	ti.SetValue("~/00.git")
	ti.CharLimit = 256
	ti.Width = 50
	ti.Focus()

	return onboardingModel{
		cfg:     cfg,
		cfgPath: cfgPath,
		theme:   theme,
		width:   w,
		height:  h,
		folderInput: ti,
	}
}

func (m onboardingModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m onboardingModel) Update(msg tea.Msg) (onboardingModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, Keys.Enter):
			folder := m.folderInput.Value()
			if folder == "" {
				folder = "~/00.git"
			}
			m.cfg.Global.Folder = folder

			m.cfg.Global.CredentialSSH = &config.SSHGlobal{SSHFolder: "~/.ssh"}
			m.cfg.Global.CredentialGCM = &config.GCMGlobal{
				Helper:          credential.DefaultCredentialHelper(),
				CredentialStore: credential.DefaultCredentialStore(),
			}
			m.cfg.Global.CredentialToken = &config.TokenGlobal{}

			if err := config.EnsureDir(m.cfgPath); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
			if err := config.Save(m.cfg, m.cfgPath); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}

			m.done = true
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }

		case key.Matches(msg, Keys.Quit):
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.folderInput, cmd = m.folderInput.Update(msg)
	return m, cmd
}

func (m onboardingModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(m.theme.Title.Render("  Welcome to gitbox") + "\n\n")
	b.WriteString(m.theme.Text.Render("  Manage Git clones across multiple accounts and providers.") + "\n\n")
	b.WriteString(m.theme.TextMuted.Render("  No configuration file found. Let's set one up.") + "\n\n")

	b.WriteString(m.theme.Text.Render("  Root folder for all git clones:") + "\n")
	b.WriteString("  " + m.folderInput.View() + "\n\n")

	b.WriteString(m.theme.TextMuted.Render("  Press enter to create config, then add accounts via the CLI:") + "\n")
	b.WriteString(m.theme.TextMuted.Render("    gitbox account add <key> --provider github ...") + "\n\n")

	if m.errMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.errMsg) + "\n\n")
	}

	b.WriteString(renderHints(m.theme, "enter create config", "ESC quit"))

	return b.String()
}
