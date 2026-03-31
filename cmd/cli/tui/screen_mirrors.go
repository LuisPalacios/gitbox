package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type mirrorsView int

const (
	mirrorsViewList mirrorsView = iota
	mirrorsViewDetail
	mirrorsViewAddGroup
	mirrorsViewAddRepo
)

type mirrorsModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int
	view          mirrorsView
	cursor        int
	summaries     []mirror.MirrorSummary
	selectedKey   string
	statuses      []mirror.StatusResult
	busy          bool
	busyLabel     string
	resultMsg     string
	errMsg        string

	// Detail view cursor for repo selection.
	detailCursor int

	// Delete flow (list: group delete, detail: repo delete).
	deleteStep  int // 0=inactive, 1=type name, 2=final confirm
	deleteInput textinput.Model

	// Add group form.
	addGroupForm formModel

	// Add repo form.
	addRepoForm formModel

	// Credential check results.
	credCheckResult *mirrorCredCheckMsg
}

func newMirrorsModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int, selectedKey string) mirrorsModel {
	ti := textinput.New()
	ti.Placeholder = "Type name to confirm..."
	ti.CharLimit = 256
	m := mirrorsModel{
		cfg:         cfg,
		cfgPath:     cfgPath,
		theme:       theme,
		width:       w,
		height:      h,
		selectedKey: selectedKey,
		deleteInput: ti,
	}
	return m
}

func mirrorSummarizeCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		summaries := mirror.Summarize(cfg, nil)
		return mirrorSummaryMsg{summaries: summaries}
	}
}

type mirrorSummaryMsg struct{ summaries []mirror.MirrorSummary }

func mirrorCheckStatusCmd(cfg *config.Config, mirrorKey string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		all := mirror.CheckAllMirrors(ctx, cfg)
		return mirrorStatusMsg{results: all[mirrorKey]}
	}
}

func mirrorSetupAllCmd(cfg *config.Config, cfgPath, mirrorKey string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		results := mirror.SetupAll(ctx, cfg, mirrorKey)
		if err := config.Save(cfg, cfgPath); err != nil {
			return mirrorSetupDoneMsg{result: mirror.SetupResult{Error: err.Error()}}
		}
		if len(results) > 0 {
			return mirrorSetupDoneMsg{result: results[len(results)-1]}
		}
		return mirrorSetupDoneMsg{result: mirror.SetupResult{}}
	}
}

func mirrorDiscoverCmd(cfg *config.Config, cfgPath string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		results, err := mirror.DiscoverMirrors(ctx, cfg, nil)
		if err != nil {
			return mirrorDiscoverDoneMsg{err: err}
		}
		added, _ := mirror.ApplyDiscovery(cfg, results)
		if added > 0 {
			_ = config.Save(cfg, cfgPath)
		}
		return mirrorDiscoverDoneMsg{results: results}
	}
}

func mirrorAddGroupCmd(cfg *config.Config, cfgPath, mirrorKey, srcAcct, dstAcct string) tea.Cmd {
	return func() tea.Msg {
		m := config.Mirror{
			AccountSrc: srcAcct,
			AccountDst: dstAcct,
			Repos:      make(map[string]config.MirrorRepo),
		}
		if err := cfg.AddMirror(mirrorKey, m); err != nil {
			return mirrorGroupAddedMsg{err: err}
		}
		if err := config.Save(cfg, cfgPath); err != nil {
			return mirrorGroupAddedMsg{err: err}
		}
		return mirrorGroupAddedMsg{}
	}
}

func mirrorDeleteGroupCmd(cfg *config.Config, cfgPath, mirrorKey string) tea.Cmd {
	return func() tea.Msg {
		if err := cfg.DeleteMirror(mirrorKey); err != nil {
			return mirrorGroupDeletedMsg{err: err}
		}
		if err := config.Save(cfg, cfgPath); err != nil {
			return mirrorGroupDeletedMsg{err: err}
		}
		return mirrorGroupDeletedMsg{}
	}
}

func mirrorAddRepoCmd(cfg *config.Config, cfgPath, mirrorKey, repoKey, direction, origin string) tea.Cmd {
	return func() tea.Msg {
		repo := config.MirrorRepo{
			Direction: direction,
			Origin:    origin,
		}
		if err := cfg.AddMirrorRepo(mirrorKey, repoKey, repo); err != nil {
			return mirrorRepoAddedMsg{err: err}
		}
		if err := config.Save(cfg, cfgPath); err != nil {
			return mirrorRepoAddedMsg{err: err}
		}
		return mirrorRepoAddedMsg{}
	}
}

func mirrorDeleteRepoCmd(cfg *config.Config, cfgPath, mirrorKey, repoKey string) tea.Cmd {
	return func() tea.Msg {
		if err := cfg.DeleteMirrorRepo(mirrorKey, repoKey); err != nil {
			return mirrorRepoDeletedMsg{err: err}
		}
		if err := config.Save(cfg, cfgPath); err != nil {
			return mirrorRepoDeletedMsg{err: err}
		}
		return mirrorRepoDeletedMsg{}
	}
}

func mirrorCredCheckCmd(cfg *config.Config, mirrorKey string) tea.Cmd {
	return func() tea.Msg {
		m, ok := cfg.Mirrors[mirrorKey]
		if !ok {
			return mirrorCredCheckMsg{}
		}
		srcAcct := cfg.Accounts[m.AccountSrc]
		dstAcct := cfg.Accounts[m.AccountDst]
		srcResult := credential.Check(srcAcct, m.AccountSrc, cfg)
		dstResult := credential.Check(dstAcct, m.AccountDst, cfg)
		return mirrorCredCheckMsg{
			srcKey:    m.AccountSrc,
			dstKey:    m.AccountDst,
			srcResult: srcResult,
			dstResult: dstResult,
		}
	}
}

func (m mirrorsModel) Init() tea.Cmd {
	return mirrorSummarizeCmd(m.cfg)
}

func (m mirrorsModel) Update(msg tea.Msg) (mirrorsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}

		// Delete flow intercepts all keys when active.
		if m.deleteStep > 0 {
			return m.updateDelete(msg)
		}

		// Delegate to forms when in form views.
		if m.view == mirrorsViewAddGroup {
			return m.updateAddGroup(msg)
		}
		if m.view == mirrorsViewAddRepo {
			return m.updateAddRepo(msg)
		}

		switch {
		case key.Matches(msg, Keys.Back):
			if m.view == mirrorsViewDetail {
				m.view = mirrorsViewList
				m.statuses = nil
				m.resultMsg = ""
				m.errMsg = ""
				m.credCheckResult = nil
				m.detailCursor = 0
				return m, mirrorSummarizeCmd(m.cfg)
			}
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }

		case key.Matches(msg, Keys.Up):
			if m.view == mirrorsViewList {
				if m.cursor > 0 {
					m.cursor--
				}
			} else if m.view == mirrorsViewDetail {
				if m.detailCursor > 0 {
					m.detailCursor--
				}
			}
			return m, nil

		case key.Matches(msg, Keys.Down):
			if m.view == mirrorsViewList && m.cursor < len(m.summaries)-1 {
				m.cursor++
			} else if m.view == mirrorsViewDetail && m.detailCursor < len(m.statuses)-1 {
				m.detailCursor++
			}
			return m, nil

		case key.Matches(msg, Keys.Enter):
			if m.view == mirrorsViewList && len(m.summaries) > 0 {
				m.selectedKey = m.summaries[m.cursor].MirrorKey
				m.view = mirrorsViewDetail
				m.detailCursor = 0
				m.busy = true
				m.busyLabel = "Checking mirror status..."
				return m, mirrorCheckStatusCmd(m.cfg, m.selectedKey)
			}
			return m, nil

		case msg.String() == "s" && m.view == mirrorsViewDetail:
			m.busy = true
			m.busyLabel = "Setting up pending mirrors..."
			m.errMsg = ""
			return m, mirrorSetupAllCmd(m.cfg, m.cfgPath, m.selectedKey)

		case msg.String() == "d":
			if m.view == mirrorsViewList {
				m.busy = true
				m.busyLabel = "Discovering mirrors..."
				m.errMsg = ""
				return m, mirrorDiscoverCmd(m.cfg, m.cfgPath)
			}

		case msg.String() == "a":
			if m.view == mirrorsViewList {
				return m.startAddGroup()
			}
			if m.view == mirrorsViewDetail {
				return m.startAddRepo()
			}

		case msg.String() == "D":
			if m.view == mirrorsViewList && len(m.summaries) > 0 {
				m.deleteStep = 1
				m.deleteInput.SetValue("")
				m.deleteInput.Focus()
				m.errMsg = ""
				return m, textinput.Blink
			}
			if m.view == mirrorsViewDetail && len(m.statuses) > 0 {
				m.deleteStep = 1
				m.deleteInput.SetValue("")
				m.deleteInput.Focus()
				m.errMsg = ""
				return m, textinput.Blink
			}

		case msg.String() == "C" && m.view == mirrorsViewDetail:
			m.busy = true
			m.busyLabel = "Checking credentials..."
			m.errMsg = ""
			m.credCheckResult = nil
			return m, mirrorCredCheckCmd(m.cfg, m.selectedKey)
		}

	case mirrorSummaryMsg:
		m.summaries = msg.summaries
		// If a key was pre-selected (from dashboard card), jump to detail.
		if m.selectedKey != "" && m.view == mirrorsViewList {
			for _, s := range m.summaries {
				if s.MirrorKey == m.selectedKey {
					m.view = mirrorsViewDetail
					m.busy = true
					m.busyLabel = "Checking mirror status..."
					return m, mirrorCheckStatusCmd(m.cfg, m.selectedKey)
				}
			}
		}
		return m, nil

	case mirrorStatusMsg:
		m.busy = false
		m.statuses = msg.results
		if m.detailCursor >= len(m.statuses) && len(m.statuses) > 0 {
			m.detailCursor = len(m.statuses) - 1
		}
		return m, nil

	case mirrorSetupDoneMsg:
		m.busy = false
		if msg.result.Error != "" {
			m.errMsg = msg.result.Error
		} else {
			m.resultMsg = "Setup complete."
		}
		return m, mirrorCheckStatusCmd(m.cfg, m.selectedKey)

	case mirrorDiscoverDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			total := 0
			for _, r := range msg.results {
				total += len(r.Discovered)
			}
			m.resultMsg = fmt.Sprintf("Discovered %d mirror relationships.", total)
		}
		return m, mirrorSummarizeCmd(m.cfg)

	case mirrorGroupAddedMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.view = mirrorsViewAddGroup
		} else {
			m.resultMsg = "Mirror group added."
			m.view = mirrorsViewList
		}
		return m, mirrorSummarizeCmd(m.cfg)

	case mirrorGroupDeletedMsg:
		m.busy = false
		m.deleteStep = 0
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.resultMsg = "Mirror group deleted."
		}
		return m, mirrorSummarizeCmd(m.cfg)

	case mirrorRepoAddedMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.view = mirrorsViewAddRepo
		} else {
			m.resultMsg = "Mirror repo added."
			m.view = mirrorsViewDetail
		}
		return m, mirrorCheckStatusCmd(m.cfg, m.selectedKey)

	case mirrorRepoDeletedMsg:
		m.busy = false
		m.deleteStep = 0
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.resultMsg = "Mirror repo deleted."
		}
		return m, mirrorCheckStatusCmd(m.cfg, m.selectedKey)

	case mirrorCredCheckMsg:
		m.busy = false
		result := msg
		m.credCheckResult = &result
		return m, nil
	}
	return m, nil
}

// ── Delete flow ────────────────────────────────────────────────────

func (m mirrorsModel) updateDelete(msg tea.KeyMsg) (mirrorsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, Keys.Back):
		m.deleteStep = 0
		m.deleteInput.SetValue("")
		m.errMsg = ""
		return m, nil

	case key.Matches(msg, Keys.Enter):
		if m.view == mirrorsViewList {
			return m.handleGroupDeleteEnter()
		}
		return m.handleRepoDeleteEnter()

	default:
		if m.deleteStep == 1 {
			m.errMsg = ""
			var cmd tea.Cmd
			m.deleteInput, cmd = m.deleteInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m mirrorsModel) handleGroupDeleteEnter() (mirrorsModel, tea.Cmd) {
	targetKey := m.summaries[m.cursor].MirrorKey
	if m.deleteStep == 1 {
		if m.deleteInput.Value() == targetKey {
			m.deleteStep = 2
			m.deleteInput.SetValue("")
			m.errMsg = ""
			return m, nil
		}
		m.errMsg = "Name does not match."
		return m, nil
	}
	if m.deleteStep == 2 {
		m.deleteStep = 0
		m.busy = true
		m.busyLabel = "Deleting mirror group..."
		return m, mirrorDeleteGroupCmd(m.cfg, m.cfgPath, targetKey)
	}
	return m, nil
}

func (m mirrorsModel) handleRepoDeleteEnter() (mirrorsModel, tea.Cmd) {
	if m.detailCursor >= len(m.statuses) {
		return m, nil
	}
	targetRepoKey := m.statuses[m.detailCursor].RepoKey
	if m.deleteStep == 1 {
		if m.deleteInput.Value() == targetRepoKey {
			m.deleteStep = 2
			m.deleteInput.SetValue("")
			m.errMsg = ""
			return m, nil
		}
		m.errMsg = "Name does not match."
		return m, nil
	}
	if m.deleteStep == 2 {
		m.deleteStep = 0
		m.busy = true
		m.busyLabel = "Deleting mirror repo..."
		return m, mirrorDeleteRepoCmd(m.cfg, m.cfgPath, m.selectedKey, targetRepoKey)
	}
	return m, nil
}

// ── Add group form ─────────────────────────────────────────────────

const (
	addGroupFieldSrc = iota
	addGroupFieldDst
	addGroupFieldKey
)

func (m mirrorsModel) startAddGroup() (mirrorsModel, tea.Cmd) {
	acctKeys := sortedAccountKeys(m.cfg)
	if len(acctKeys) < 2 {
		m.errMsg = "Need at least 2 accounts to create a mirror group."
		return m, nil
	}

	fields := []formField{
		newSelectFormField("Source:       ", acctKeys),
		newSelectFormField("Destination:  ", acctKeys),
		newTextField("Mirror key:", "source-dest", 64),
	}

	// Validation: source != destination.
	fields[addGroupFieldDst].ValidateFn = func(v string) string {
		src := fields[addGroupFieldSrc].Value()
		if v == src {
			return "destination must differ from source"
		}
		return ""
	}

	// Validation: key not empty and not duplicate.
	fields[addGroupFieldKey].ValidateFn = func(v string) string {
		if v == "" {
			return "mirror key is required"
		}
		if _, exists := m.cfg.Mirrors[v]; exists {
			return fmt.Sprintf("mirror %q already exists", v)
		}
		return ""
	}

	m.addGroupForm = newFormModel("Add Mirror Group", fields, m.theme)
	m.view = mirrorsViewAddGroup
	m.errMsg = ""
	m.resultMsg = ""
	return m, m.addGroupForm.Init()
}

func (m mirrorsModel) updateAddGroup(msg tea.KeyMsg) (mirrorsModel, tea.Cmd) {
	if key.Matches(msg, Keys.Back) {
		m.view = mirrorsViewList
		return m, nil
	}

	submitted, cmd := m.addGroupForm.Update(msg)
	if submitted {
		src := m.addGroupForm.Fields[addGroupFieldSrc].Value()
		dst := m.addGroupForm.Fields[addGroupFieldDst].Value()
		mirrorKey := m.addGroupForm.Fields[addGroupFieldKey].Value()
		// Auto-fill key if empty (shouldn't happen due to validation, but defensive).
		if mirrorKey == "" {
			mirrorKey = src + "-" + dst
		}
		m.view = mirrorsViewList
		m.busy = true
		m.busyLabel = "Adding mirror group..."
		return m, mirrorAddGroupCmd(m.cfg, m.cfgPath, mirrorKey, src, dst)
	}

	// Auto-fill the mirror key when source/destination change.
	src := m.addGroupForm.Fields[addGroupFieldSrc].Value()
	dst := m.addGroupForm.Fields[addGroupFieldDst].Value()
	currentKey := m.addGroupForm.Fields[addGroupFieldKey].Value()
	autoKey := src + "-" + dst
	// Only auto-fill if the key field looks auto-generated or empty.
	if currentKey == "" || isAutoKey(currentKey, m.cfg) {
		m.addGroupForm.Fields[addGroupFieldKey].TextInput.SetValue(autoKey)
	}

	return m, cmd
}

// isAutoKey checks if the current key looks like an auto-generated "src-dst" key.
func isAutoKey(k string, cfg *config.Config) bool {
	for srcKey := range cfg.Accounts {
		for dstKey := range cfg.Accounts {
			if k == srcKey+"-"+dstKey {
				return true
			}
		}
	}
	return false
}

// ── Add repo form ──────────────────────────────────────────────────

const (
	addRepoFieldDirection = iota
	addRepoFieldOrigin
	addRepoFieldRepo
)

func (m mirrorsModel) startAddRepo() (mirrorsModel, tea.Cmd) {
	fields := []formField{
		newSelectFormField("Direction:    ", []string{"push", "pull"}),
		newSelectFormField("Origin:       ", []string{"src", "dst"}),
		newTextField("Repository:", "org/repo", 256),
	}

	fields[addRepoFieldRepo].ValidateFn = func(v string) string {
		if v == "" {
			return "repository name is required (e.g. org/repo)"
		}
		return ""
	}

	m.addRepoForm = newFormModel("Add Mirror Repo to: "+m.selectedKey, fields, m.theme)
	m.view = mirrorsViewAddRepo
	m.errMsg = ""
	m.resultMsg = ""
	return m, m.addRepoForm.Init()
}

func (m mirrorsModel) updateAddRepo(msg tea.KeyMsg) (mirrorsModel, tea.Cmd) {
	if key.Matches(msg, Keys.Back) {
		m.view = mirrorsViewDetail
		return m, nil
	}

	submitted, cmd := m.addRepoForm.Update(msg)
	if submitted {
		direction := m.addRepoForm.Fields[addRepoFieldDirection].Value()
		origin := m.addRepoForm.Fields[addRepoFieldOrigin].Value()
		repoKey := m.addRepoForm.Fields[addRepoFieldRepo].Value()
		m.view = mirrorsViewDetail
		m.busy = true
		m.busyLabel = "Adding mirror repo..."
		return m, mirrorAddRepoCmd(m.cfg, m.cfgPath, m.selectedKey, repoKey, direction, origin)
	}
	return m, cmd
}

// ── Views ──────────────────────────────────────────────────────────

func (m mirrorsModel) View() string {
	switch m.view {
	case mirrorsViewDetail:
		return m.viewDetail()
	case mirrorsViewAddGroup:
		return m.addGroupForm.View()
	case mirrorsViewAddRepo:
		return m.addRepoForm.View()
	default:
		return m.viewList()
	}
}

func (m mirrorsModel) viewList() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Mirrors") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	if len(m.summaries) == 0 {
		b.WriteString(m.theme.TextMuted.Render("  No mirrors configured.") + "\n")
	} else {
		for i, s := range m.summaries {
			sym := styles.SymClean
			color := m.theme.Palette.Clean
			if s.Error > 0 {
				sym = styles.SymError
				color = m.theme.Palette.StatusError
			} else if s.Unchecked > 0 {
				sym = styles.SymSyncing
				color = m.theme.Palette.Syncing
			}

			symStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
			detail := fmt.Sprintf("%d repos, %d active, %d unchecked, %d error",
				s.Total, s.Active, s.Unchecked, s.Error)

			line := fmt.Sprintf("  %s  %-25s  %s ↔ %s  %s",
				symStyle.Render(sym), s.MirrorKey,
				s.AccountSrc, s.AccountDst,
				m.theme.TextMuted.Render(detail))

			if i == m.cursor {
				line = m.theme.SelectedRow.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}

	if m.busy {
		b.WriteString("\n  " + m.theme.TextMuted.Render(styles.SymSyncing+" "+m.busyLabel) + "\n")
	}
	if m.resultMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(m.resultMsg) + "\n")
	}
	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg) + "\n")
	}

	// Delete prompts.
	if m.deleteStep == 1 && len(m.summaries) > 0 {
		targetKey := m.summaries[m.cursor].MirrorKey
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
		b.WriteString("\n  " + warnStyle.Render(fmt.Sprintf("Type \"%s\" to confirm deletion:", targetKey)) + "\n")
		b.WriteString("  " + m.theme.TextMuted.Render("(Only removes config entry. Server mirror configurations remain.)") + "\n")
		b.WriteString("  " + m.deleteInput.View() + "\n")
		b.WriteString("  " + m.theme.TextMuted.Render("ESC to cancel") + "\n")
	} else if m.deleteStep == 2 && len(m.summaries) > 0 {
		targetKey := m.summaries[m.cursor].MirrorKey
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
		b.WriteString("\n  " + warnStyle.Render(fmt.Sprintf(
			"Are you absolutely sure? This will delete mirror group %s.", targetKey)) + "\n")
		b.WriteString("  " + warnStyle.Render("enter=yes, ESC=cancel") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(renderHintsFit(m.theme, m.width, "↑↓ navigate", "enter detail", "a add", "d discover", "D delete", "ESC back"))

	return b.String()
}

func (m mirrorsModel) viewDetail() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Mirror: "+m.selectedKey) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	if m.busy {
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" "+m.busyLabel) + "\n")
		return b.String()
	}

	if len(m.statuses) == 0 {
		b.WriteString(m.theme.TextMuted.Render("  No repos in this mirror group.") + "\n")
	} else {
		for i, s := range m.statuses {
			sym := styles.SymClean
			color := m.theme.Palette.Clean
			if s.Error != "" {
				sym = styles.SymError
				color = m.theme.Palette.StatusError
			} else if s.SyncStatus == "behind" {
				sym = styles.SymBehind
				color = m.theme.Palette.Behind
			}

			dirSym := styles.SymMirrorPush
			if s.Direction == "pull" {
				dirSym = styles.SymMirrorPull
			}

			symStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
			detail := s.SyncStatus
			if s.Error != "" {
				detail = friendlyMirrorError(s.Error)
			}
			if s.Warning != "" {
				detail += " ⚠ " + s.Warning
			}

			line := fmt.Sprintf("  %s  %s  %-40s  %s",
				symStyle.Render(sym), dirSym, s.RepoKey,
				m.theme.TextMuted.Render(detail))

			if i == m.detailCursor {
				line = m.theme.SelectedRow.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}

	// Credential check results.
	if m.credCheckResult != nil {
		b.WriteString("\n")
		b.WriteString(m.renderCredStatus("src ("+m.credCheckResult.srcKey+")", m.credCheckResult.srcResult))
		b.WriteString(m.renderCredStatus("dst ("+m.credCheckResult.dstKey+")", m.credCheckResult.dstResult))
	}

	if m.resultMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(m.resultMsg) + "\n")
	}
	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg) + "\n")
	}

	// Delete prompts.
	if m.deleteStep == 1 && len(m.statuses) > 0 {
		targetRepoKey := m.statuses[m.detailCursor].RepoKey
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
		b.WriteString("\n  " + warnStyle.Render(fmt.Sprintf("Type \"%s\" to confirm deletion:", targetRepoKey)) + "\n")
		b.WriteString("  " + m.theme.TextMuted.Render("(Only removes config entry. Server mirror configurations remain.)") + "\n")
		b.WriteString("  " + m.deleteInput.View() + "\n")
		b.WriteString("  " + m.theme.TextMuted.Render("ESC to cancel") + "\n")
	} else if m.deleteStep == 2 && len(m.statuses) > 0 {
		targetRepoKey := m.statuses[m.detailCursor].RepoKey
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
		b.WriteString("\n  " + warnStyle.Render(fmt.Sprintf(
			"Remove mirror repo %s from group?", targetRepoKey)) + "\n")
		b.WriteString("  " + warnStyle.Render("enter=yes, ESC=cancel") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(renderHintsFit(m.theme, m.width, "↑↓ navigate", "a add repo", "s setup pending", "C check creds", "D delete repo", "ESC back"))

	return b.String()
}

func (m mirrorsModel) renderCredStatus(label string, result credential.StatusResult) string {
	var sym, detail string
	var color string

	switch result.Primary {
	case credential.StatusOK:
		sym = styles.SymClean
		color = m.theme.Palette.Clean
		detail = "OK"
		if result.PrimaryDetail != "" {
			detail = result.PrimaryDetail
		}
	case credential.StatusWarning:
		sym = styles.SymDiverged
		color = m.theme.Palette.AccentWarning
		detail = result.PrimaryDetail
	case credential.StatusError:
		sym = styles.SymError
		color = m.theme.Palette.StatusError
		detail = result.PrimaryDetail
	case credential.StatusNone:
		sym = styles.SymError
		color = m.theme.Palette.StatusError
		detail = "not configured"
	default:
		sym = styles.SymSyncing
		color = m.theme.Palette.Syncing
		detail = "unknown"
	}

	st := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	return fmt.Sprintf("  %-25s %s %s\n", m.theme.TextMuted.Render(label+":"), st.Render(sym), st.Render(detail))
}

// ── Helpers ────────────────────────────────────────────────────────

func sortedAccountKeys(cfg *config.Config) []string {
	keys := make([]string, 0, len(cfg.Accounts))
	for k := range cfg.Accounts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// friendlyMirrorError maps raw Go error strings to concise, user-friendly messages.
func friendlyMirrorError(raw string) string {
	// Errors from CheckStatus are already user-friendly (e.g. "missing API token in git-parchis-luis").
	// Just truncate if too long for display.
	if len(raw) > 60 {
		return raw[:57] + "..."
	}
	return raw
}
