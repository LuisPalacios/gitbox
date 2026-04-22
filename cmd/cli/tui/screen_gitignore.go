package tui

import (
	"fmt"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/gitignore"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type gitignoreModel struct {
	theme          styles.Theme
	width, height  int
	loaded         bool
	status         gitignore.Status
	loadErr        string
	confirmPending bool
	installing     bool
	result         *gitignore.InstallResult
	installErr     string
}

func newGitignoreModel(theme styles.Theme, w, h int) gitignoreModel {
	return gitignoreModel{theme: theme, width: w, height: h}
}

func (m gitignoreModel) Init() tea.Cmd {
	return checkGitignoreCmd()
}

// checkGitignoreCmd runs gitignore.Check() asynchronously and returns a
// gitignoreStatusMsg with the result.
func checkGitignoreCmd() tea.Cmd {
	return func() tea.Msg {
		s, err := gitignore.Check()
		return gitignoreStatusMsg{status: gitignoreStatusInfoFromPkg(s), err: err}
	}
}

func gitignoreStatusInfoFromPkg(s gitignore.Status) gitignoreStatusInfo {
	return gitignoreStatusInfo{
		Path:          s.Path,
		NeedsAction:   s.NeedsAction,
		BlockPresent:  s.BlockPresent,
		BlockUpToDate: s.BlockUpToDate,
		Excludesfile:  s.Excludesfile,
		Set:           s.ExcludesfileSet,
		FileExists:    s.FileExists,
	}
}

func installGitignoreCmd() tea.Cmd {
	return func() tea.Msg {
		res, err := gitignore.Install()
		return gitignoreInstalledMsg{
			updated:         res.Updated,
			alreadyUpToDate: res.AlreadyUpToDate,
			backupPath:      res.BackupPath,
			setExcludes:     res.SetExcludesfile,
			path:            res.Path,
			err:             err,
		}
	}
}

func (m gitignoreModel) Update(msg tea.Msg) (gitignoreModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, Keys.Back):
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }

		case key.Matches(msg, Keys.Enter):
			if m.installing || !m.loaded {
				return m, nil
			}
			if m.confirmPending {
				m.confirmPending = false
				m.installing = true
				m.installErr = ""
				return m, installGitignoreCmd()
			}
			if m.status.NeedsAction {
				m.confirmPending = true
			}
			return m, nil

		case msg.String() == "n" && m.confirmPending:
			m.confirmPending = false
			return m, nil
		}

	case gitignoreStatusMsg:
		m.loaded = true
		if msg.err != nil {
			m.loadErr = msg.err.Error()
			return m, nil
		}
		// Re-fetch the full Status from the package so the screen has
		// access to fields not carried in gitignoreStatusInfo.
		s, err := gitignore.Check()
		if err != nil {
			m.loadErr = err.Error()
			return m, nil
		}
		m.status = s
		return m, nil

	case gitignoreInstalledMsg:
		m.installing = false
		if msg.err != nil {
			m.installErr = msg.err.Error()
			return m, nil
		}
		m.result = &gitignore.InstallResult{
			Path:            msg.path,
			BackupPath:      msg.backupPath,
			SetExcludesfile: msg.setExcludes,
			Updated:         msg.updated,
			AlreadyUpToDate: msg.alreadyUpToDate,
		}
		// Re-check so the status display reflects the post-install state.
		return m, checkGitignoreCmd()
	}
	return m, nil
}

func (m gitignoreModel) View() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Global Gitignore") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	if !m.loaded {
		b.WriteString(m.theme.TextMuted.Render("  Checking ~/.gitignore_global ...") + "\n")
		return b.String()
	}

	if m.loadErr != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.loadErr) + "\n\n")
		b.WriteString(renderHints(m.theme, "ESC back"))
		return b.String()
	}

	clean := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))

	// Excludesfile line.
	if m.status.ExcludesfileSet {
		b.WriteString("  " + clean.Render(styles.SymClean+" core.excludesfile") + " " +
			m.theme.TextBold.Render(m.status.Excludesfile) + "\n")
	} else {
		b.WriteString("  " + warn.Render(styles.SymDiverged+" core.excludesfile not set") + "\n")
		b.WriteString(m.theme.TextMuted.Render("    will set to "+m.status.DefaultPath) + "\n")
	}

	// File state line.
	switch {
	case !m.status.FileExists:
		b.WriteString("  " + warn.Render(styles.SymDiverged+" file missing") + " " +
			m.theme.TextMuted.Render(m.status.Path) + "\n")
	case !m.status.BlockPresent:
		b.WriteString("  " + warn.Render(styles.SymDiverged+" managed block not present") + "\n")
		b.WriteString(m.theme.TextMuted.Render("    user content will be preserved") + "\n")
	case !m.status.BlockUpToDate:
		b.WriteString("  " + warn.Render(styles.SymDiverged+" managed block is out of date") + "\n")
	default:
		b.WriteString("  " + clean.Render(styles.SymClean+" managed block up to date") + "\n")
	}

	// Duplicates line — shown whenever managed-block patterns also live
	// outside the sentinel markers. Install will sanitize them away.
	if m.status.HasDuplicates {
		b.WriteString("  " + warn.Render(styles.SymDiverged+" "+
			fmt.Sprintf("%d managed pattern(s) duplicated outside the block", len(m.status.Duplicates))) + "\n")
		shown := m.status.Duplicates
		const maxShown = 4
		if len(shown) > maxShown {
			shown = shown[:maxShown]
		}
		for _, d := range shown {
			b.WriteString(m.theme.TextMuted.Render("    "+d) + "\n")
		}
		if len(m.status.Duplicates) > maxShown {
			b.WriteString(m.theme.TextMuted.Render(fmt.Sprintf("    … and %d more", len(m.status.Duplicates)-maxShown)) + "\n")
		}
	}
	b.WriteString("\n")

	b.WriteString(m.theme.TextMuted.Render("  The recommended block covers OS junk for macOS, Windows and Linux") + "\n")
	b.WriteString(m.theme.TextMuted.Render("  (.DS_Store, Thumbs.db, *~, etc.) so per-project .gitignore files") + "\n")
	b.WriteString(m.theme.TextMuted.Render("  don't have to repeat them.") + "\n\n")

	// Result of the most recent install action.
	if m.result != nil {
		switch {
		case m.result.AlreadyUpToDate && !m.result.SetExcludesfile:
			b.WriteString("  " + clean.Render(styles.SymClean+" Already up to date.") + "\n\n")
		default:
			parts := []string{}
			if m.result.Updated {
				parts = append(parts, "block written")
			}
			if m.result.BackupPath != "" {
				parts = append(parts, "backup at "+m.result.BackupPath)
			}
			if m.result.SetExcludesfile {
				parts = append(parts, "core.excludesfile set")
			}
			b.WriteString("  " + clean.Render(styles.SymClean+" Done — "+strings.Join(parts, ", ")) + "\n\n")
		}
	}

	if m.installErr != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.installErr) + "\n\n")
	}

	if m.installing {
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Installing...") + "\n")
		return b.String()
	}

	if m.confirmPending {
		b.WriteString("  " + warn.Render("Install/update the recommended block? (enter=yes, n=no)") + "\n")
		return b.String()
	}

	if m.status.NeedsAction {
		b.WriteString(renderHints(m.theme, "enter install recommended block", "ESC back"))
	} else {
		b.WriteString(renderHints(m.theme, "ESC back"))
	}

	return b.String()
}
