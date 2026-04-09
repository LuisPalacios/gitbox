package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Messages ──

type orphansScanDoneMsg struct {
	orphans []adopt.OrphanRepo
	cfg     *config.Config // non-nil when config was reloaded from disk
	err     error
}

type orphansAdoptDoneMsg struct {
	count     int
	relocated int
	err       error
}

// ── Commands ──

func scanOrphansCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		orphans, err := adopt.FindOrphans(cfg)
		return orphansScanDoneMsg{orphans: orphans, err: err}
	}
}

// reloadAndScanOrphansCmd reloads config from disk before scanning.
// This ensures adoption changes (which are saved to disk) are picked up.
func reloadAndScanOrphansCmd(cfgPath string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return orphansScanDoneMsg{err: err}
		}
		orphans, err := adopt.FindOrphans(cfg)
		return orphansScanDoneMsg{orphans: orphans, cfg: cfg, err: err}
	}
}

func adoptOrphansCmd(cfg *config.Config, cfgPath string, orphans []adopt.OrphanRepo, selected map[int]bool) tea.Cmd {
	return func() tea.Msg {
		adopted := 0
		relocated := 0

		for idx := range selected {
			if idx >= len(orphans) || !selected[idx] {
				continue
			}
			o := orphans[idx]
			if o.MatchedAccount == "" || o.MatchedSource == "" || o.LocalOnly {
				continue
			}

			repoPath := o.Path

			// Relocate if needed.
			if o.NeedsRelocate && o.ExpectedPath != "" {
				if _, err := os.Stat(o.ExpectedPath); err != nil {
					// Destination doesn't exist — safe to move.
					if err := os.MkdirAll(filepath.Dir(o.ExpectedPath), 0o755); err == nil {
						if err := os.Rename(o.Path, o.ExpectedPath); err == nil {
							repoPath = o.ExpectedPath
							relocated++
						}
					}
				}
			}

			// Add repo to config.
			repo := config.Repo{}
			if repoPath == o.Path && o.NeedsRelocate {
				repo.CloneFolder = repoPath // adopt in place with override
			}
			if err := cfg.AddRepo(o.MatchedSource, o.RepoKey, repo); err != nil {
				continue
			}

			// Sanitize .git/config.
			acct := cfg.Accounts[o.MatchedAccount]
			credType := repo.EffectiveCredentialType(&acct)

			newURL := adopt.PlainRemoteURL(acct, o.RepoKey, credType)
			_ = git.SetRemoteURL(repoPath, "origin", newURL)
			_ = credential.ConfigureRepoCredential(repoPath, acct, o.MatchedAccount, credType, cfg.Global)

			name := acct.Name
			email := acct.Email
			_ = git.ConfigSet(repoPath, "user.name", name)
			_ = git.ConfigSet(repoPath, "user.email", email)

			adopted++
		}

		if adopted > 0 {
			if err := config.Save(cfg, cfgPath); err != nil {
				return orphansAdoptDoneMsg{count: adopted, relocated: relocated, err: err}
			}
		}

		return orphansAdoptDoneMsg{count: adopted, relocated: relocated}
	}
}

// ── Model ──

type orphansModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int
	orphans       []adopt.OrphanRepo
	matched       []int          // indices of matched (adoptable) orphans
	selected      map[int]bool   // keyed by index in m.orphans
	cursor        int            // position in matched list
	scrollOffset  int
	loading       bool
	adopting      bool
	adoptedCount  int
	relocatedCount int
	errMsg        string
}

func newOrphansModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int) orphansModel {
	return orphansModel{
		cfg:      cfg,
		cfgPath:  cfgPath,
		theme:    theme,
		width:    w,
		height:   h,
		selected: make(map[int]bool),
		loading:  true,
	}
}

func (m orphansModel) Init() tea.Cmd {
	return scanOrphansCmd(m.cfg)
}

func (m orphansModel) contentHeight() int {
	h := m.height - 10
	if h < 3 {
		h = 3
	}
	return h
}

func (m *orphansModel) ensureCursorVisible() {
	ch := m.contentHeight()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+ch {
		m.scrollOffset = m.cursor - ch + 1
	}
}

func (m orphansModel) Update(msg tea.Msg) (orphansModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.loading || m.adopting {
			return m, nil
		}

		switch {
		case key.Matches(msg, Keys.Back):
			return m, func() tea.Msg {
				return switchScreenMsg{screen: screenDashboard}
			}

		case key.Matches(msg, Keys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.ensureCursorVisible()
			}
			return m, nil

		case key.Matches(msg, Keys.Down):
			if m.cursor < len(m.matched)-1 {
				m.cursor++
				m.ensureCursorVisible()
			}
			return m, nil

		case msg.String() == " ": // toggle selection
			if len(m.matched) > 0 && m.cursor < len(m.matched) {
				realIdx := m.matched[m.cursor]
				m.selected[realIdx] = !m.selected[realIdx]
			}
			return m, nil

		case msg.String() == "a": // select all matched
			for _, idx := range m.matched {
				m.selected[idx] = true
			}
			return m, nil

		case msg.String() == "r": // rescan
			m.loading = true
			m.errMsg = ""
			m.adoptedCount = 0
			m.relocatedCount = 0
			return m, scanOrphansCmd(m.cfg)

		case key.Matches(msg, Keys.Enter):
			selCount := 0
			for _, v := range m.selected {
				if v {
					selCount++
				}
			}
			if selCount == 0 {
				m.errMsg = "No repos selected. Use space to select."
				return m, nil
			}
			m.adopting = true
			m.errMsg = ""
			return m, adoptOrphansCmd(m.cfg, m.cfgPath, m.orphans, m.selected)
		}

	case orphansScanDoneMsg:
		m.loading = false
		if msg.cfg != nil {
			m.cfg = msg.cfg // pick up reloaded config
		}
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.orphans = msg.orphans
		m.matched = nil
		m.selected = make(map[int]bool)
		for i, o := range m.orphans {
			if o.MatchedAccount != "" && !o.LocalOnly {
				m.matched = append(m.matched, i)
				m.selected[i] = true // pre-select all matched
			}
		}
		m.cursor = 0
		m.scrollOffset = 0
		return m, nil

	case orphansAdoptDoneMsg:
		m.adopting = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		}
		m.adoptedCount = msg.count
		m.relocatedCount = msg.relocated
		// Reload config from disk and rescan — adoption saved to disk,
		// so reloading ensures the tracked set is up to date.
		m.loading = true
		return m, reloadAndScanOrphansCmd(m.cfgPath)
	}

	return m, nil
}

func (m orphansModel) View() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Orphan Repos") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	if m.loading {
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Scanning for orphans...") + "\n")
		return b.String()
	}

	if m.adopting {
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Adopting...") + "\n")
		return b.String()
	}

	if m.errMsg != "" && len(m.orphans) == 0 {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg) + "\n\n")
		b.WriteString(renderHints(m.theme, "r rescan", "ESC back"))
		return b.String()
	}

	if len(m.orphans) == 0 {
		b.WriteString("  " + m.theme.TextMuted.Render("No orphan repos found.") + "\n\n")
		b.WriteString(renderHints(m.theme, "r rescan", "ESC back"))
		return b.String()
	}

	// ── Matched orphans (selectable) ──
	if len(m.matched) > 0 {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).Bold(true).
			Render(fmt.Sprintf("Ready to adopt (%d):", len(m.matched))) + "\n")

		ch := m.contentHeight()
		end := m.scrollOffset + ch
		if end > len(m.matched) {
			end = len(m.matched)
		}

		for vi := m.scrollOffset; vi < end; vi++ {
			realIdx := m.matched[vi]
			o := m.orphans[realIdx]

			prefix := "  [ ] "
			if m.selected[realIdx] {
				prefix = "  [x] "
			}

			action := "in place"
			if o.NeedsRelocate {
				action = "relocate"
			}

			label := fmt.Sprintf("%s → %s  [%s]", o.RepoKey, o.MatchedSource, action)
			line := prefix + label

			if vi == m.cursor {
				line = m.theme.SelectedRow.Render(line)
			} else {
				line = m.theme.NormalRow.Render(line)
			}
			b.WriteString(line + "\n")
		}

		if len(m.matched) > ch {
			b.WriteString(m.theme.TextMuted.Render(
				fmt.Sprintf("  (%d-%d of %d)", m.scrollOffset+1, end, len(m.matched))) + "\n")
		}
	}

	// ── Unknown account orphans ──
	unknown := 0
	for _, o := range m.orphans {
		if o.MatchedAccount == "" && !o.LocalOnly {
			if unknown == 0 {
				b.WriteString("\n  " + lipgloss.NewStyle().
					Foreground(lipgloss.Color(m.theme.Palette.StatusError)).Bold(true).
					Render("Unknown account:") + "\n")
			}
			unknown++
			b.WriteString(m.theme.TextMuted.Render(
				fmt.Sprintf("    %s  ← %s", o.RepoKey, o.RemoteURL)) + "\n")
		}
	}

	// ── Local-only orphans ──
	localOnly := 0
	for _, o := range m.orphans {
		if o.LocalOnly {
			if localOnly == 0 {
				b.WriteString("\n  " + m.theme.TextMuted.Render("Local only (no remote):") + "\n")
			}
			localOnly++
			b.WriteString(m.theme.TextMuted.Render(fmt.Sprintf("    %s", o.RelPath)) + "\n")
		}
	}

	// ── Summary ──
	b.WriteString("\n")
	selCount := 0
	for _, v := range m.selected {
		if v {
			selCount++
		}
	}
	b.WriteString(m.theme.TextMuted.Render(
		fmt.Sprintf("  %d matched, %d selected", len(m.matched), selCount)))

	if m.adoptedCount > 0 {
		msg := fmt.Sprintf("  Adopted %d", m.adoptedCount)
		if m.relocatedCount > 0 {
			msg += fmt.Sprintf(", relocated %d", m.relocatedCount)
		}
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(msg))
	}

	if m.errMsg != "" && len(m.orphans) > 0 {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg))
	}

	b.WriteString("\n\n")
	b.WriteString(renderHintsFit(m.theme, m.width, "space toggle", "a all", "enter adopt", "r rescan", "ESC back"))

	return b.String()
}
