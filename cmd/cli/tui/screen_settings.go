package tui

import (
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/i18n"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type settingsField int

const (
	settingsFolder settingsField = iota
	settingsLanguage
	settingsSync
	settingsGitignoreCheck
	settingsFieldCount
)

var syncOptions = []string{"off", "5m", "15m", "30m"}
var languageOptions = []string{"en", "es"}

type settingsModel struct {
	cfg            *config.Config
	cfgPath        string
	theme          styles.Theme
	tr             i18n.Translator
	width, height  int
	active         settingsField
	folderInput    textinput.Model
	syncIndex      int
	languageIndex  int
	gitignoreCheck bool
	saved          bool
	errMsg         string
}

func newSettingsModel(cfg *config.Config, cfgPath string, theme styles.Theme, tr i18n.Translator, w, h int) settingsModel {
	ti := textinput.New()
	ti.Placeholder = "~/00.git"
	ti.SetValue(cfg.Global.Folder)
	ti.CharLimit = 256
	ti.Width = 50

	syncIdx := 0
	for i, opt := range syncOptions {
		if opt == cfg.Global.PeriodicSync {
			syncIdx = i
			break
		}
	}
	langIdx := 0
	for i, opt := range languageOptions {
		if opt == i18n.Normalize(cfg.Global.Language) {
			langIdx = i
			break
		}
	}

	return settingsModel{
		cfg:            cfg,
		cfgPath:        cfgPath,
		theme:          theme,
		tr:             tr,
		width:          w,
		height:         h,
		folderInput:    ti,
		syncIndex:      syncIdx,
		languageIndex:  langIdx,
		gitignoreCheck: cfg.Global.ShouldCheckGlobalGitignore(),
	}
}

func (m settingsModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m settingsModel) Update(msg tea.Msg) (settingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, Keys.Back):
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }

		case key.Matches(msg, Keys.Up):
			if m.active > 0 {
				m.active--
			}
			return m, nil

		case key.Matches(msg, Keys.Down):
			if m.active < settingsFieldCount-1 {
				m.active++
			}
			return m, nil

		case msg.String() == "left" || msg.String() == "h":
			if m.active == settingsLanguage && m.languageIndex > 0 {
				m.languageIndex--
			}
			if m.active == settingsSync && m.syncIndex > 0 {
				m.syncIndex--
			}
			if m.active == settingsGitignoreCheck {
				m.gitignoreCheck = !m.gitignoreCheck
			}
			return m, nil

		case msg.String() == "right" || msg.String() == "l":
			if m.active == settingsLanguage && m.languageIndex < len(languageOptions)-1 {
				m.languageIndex++
			}
			if m.active == settingsSync && m.syncIndex < len(syncOptions)-1 {
				m.syncIndex++
			}
			if m.active == settingsGitignoreCheck {
				m.gitignoreCheck = !m.gitignoreCheck
			}
			return m, nil

		case msg.String() == " ":
			if m.active == settingsGitignoreCheck {
				m.gitignoreCheck = !m.gitignoreCheck
			}
			return m, nil

		case key.Matches(msg, Keys.Enter):
			// Save settings.
			m.cfg.Global.Folder = m.folderInput.Value()
			m.cfg.Global.Language = languageOptions[m.languageIndex]
			m.cfg.Global.PeriodicSync = syncOptions[m.syncIndex]
			gpref := m.gitignoreCheck
			m.cfg.Global.CheckGlobalGitignore = &gpref
			if err := config.Save(m.cfg, m.cfgPath); err != nil {
				m.errMsg = err.Error()
			} else {
				m.saved = true
				m.errMsg = ""
			}
			return m, nil
		}
	}

	// Update text input when folder field is active.
	if m.active == settingsFolder {
		var cmd tea.Cmd
		m.folderInput, cmd = m.folderInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m settingsModel) View() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render(m.tr.T("tui.settings.title")) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	// Folder field.
	label := m.tr.T("tui.settings.root_folder")
	if m.active == settingsFolder {
		m.folderInput.Focus()
		label = m.theme.Brand.Render(label)
	} else {
		m.folderInput.Blur()
		label = m.theme.Text.Render(label)
	}
	b.WriteString(label + m.folderInput.View() + "\n\n")

	langLabel := m.tr.T("tui.settings.language")
	if m.active == settingsLanguage {
		langLabel = m.theme.Brand.Render(langLabel)
	} else {
		langLabel = m.theme.Text.Render(langLabel)
	}
	var langs []string
	for i, opt := range languageOptions {
		if i == m.languageIndex {
			langs = append(langs, lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.theme.Palette.Brand)).
				Bold(true).
				Render("["+opt+"]"))
		} else {
			langs = append(langs, m.theme.TextMuted.Render(" "+opt+" "))
		}
	}
	b.WriteString(langLabel + strings.Join(langs, " ") + "\n\n")

	// Sync interval.
	syncLabel := m.tr.T("tui.settings.periodic_sync")
	if m.active == settingsSync {
		syncLabel = m.theme.Brand.Render(syncLabel)
	} else {
		syncLabel = m.theme.Text.Render(syncLabel)
	}
	var opts []string
	for i, opt := range syncOptions {
		if i == m.syncIndex {
			opts = append(opts, lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.theme.Palette.Brand)).
				Bold(true).
				Render("["+opt+"]"))
		} else {
			opts = append(opts, m.theme.TextMuted.Render(" "+opt+" "))
		}
	}
	b.WriteString(syncLabel + strings.Join(opts, " ") + "\n\n")

	// Global-gitignore automatic-check toggle. Explicit actions
	// (`G` on the dashboard, `gitbox gitignore check|install` on the
	// CLI, the GUI install button) always run — this only gates the
	// startup auto-check that produces the red footer hint.
	checkbox := "[ ]"
	if m.gitignoreCheck {
		checkbox = "[x]"
	}
	gLabel := "  "
	if m.active == settingsGitignoreCheck {
		gLabel += m.theme.Brand.Render(checkbox + " " + m.tr.T("tui.settings.gitignore"))
	} else {
		gLabel += m.theme.Text.Render(checkbox + " " + m.tr.T("tui.settings.gitignore"))
	}
	b.WriteString(gLabel + "\n\n")

	// Status message.
	if m.saved {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(m.tr.T("tui.settings.saved")) + "\n")
	}
	if m.errMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.tr.F("tui.settings.error", m.errMsg)) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(renderHints(m.theme, m.tr.T("tui.hint.navigate"), m.tr.T("tui.hint.change"), m.tr.T("tui.hint.save"), m.tr.T("tui.hint.back")))

	return b.String()
}
