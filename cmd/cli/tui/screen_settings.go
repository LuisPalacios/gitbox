package tui

import (
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type settingsField int

const (
	settingsFolder settingsField = iota
	settingsSync
	settingsFieldCount
)

var syncOptions = []string{"off", "5m", "15m", "30m"}

type settingsModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int
	active        settingsField
	folderInput   textinput.Model
	syncIndex     int
	saved         bool
	errMsg        string
}

func newSettingsModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int) settingsModel {
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

	return settingsModel{
		cfg:         cfg,
		cfgPath:     cfgPath,
		theme:       theme,
		width:       w,
		height:      h,
		folderInput: ti,
		syncIndex:   syncIdx,
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
			if m.active == settingsSync && m.syncIndex > 0 {
				m.syncIndex--
			}
			return m, nil

		case msg.String() == "right" || msg.String() == "l":
			if m.active == settingsSync && m.syncIndex < len(syncOptions)-1 {
				m.syncIndex++
			}
			return m, nil

		case key.Matches(msg, Keys.Enter):
			// Save settings.
			m.cfg.Global.Folder = m.folderInput.Value()
			m.cfg.Global.PeriodicSync = syncOptions[m.syncIndex]
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

	b.WriteString(m.theme.Title.Render("Settings") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	// Folder field.
	label := "  Global folder: "
	if m.active == settingsFolder {
		m.folderInput.Focus()
		label = m.theme.Brand.Render(label)
	} else {
		m.folderInput.Blur()
		label = m.theme.Text.Render(label)
	}
	b.WriteString(label + m.folderInput.View() + "\n\n")

	// Sync interval.
	syncLabel := "  Periodic sync: "
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

	// Status message.
	if m.saved {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render("Settings saved.") + "\n")
	}
	if m.errMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.errMsg) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(renderHints(m.theme, "↑↓ navigate", "←→ change", "enter save", "ESC back"))

	return b.String()
}
