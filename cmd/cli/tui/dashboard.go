package tui

import (
	"context"
	"fmt"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// tabID identifies the active tab.
type tabID int

const (
	tabAccounts tabID = iota
	tabMirrors
	tabCount // sentinel
)

// focusArea tracks which area has keyboard focus.
type focusArea int

const (
	focusCards focusArea = iota
	focusList
)

type dashboardModel struct {
	cfg           *config.Config
	cfgPath       string
	credMgr       *credential.StatusManager
	testMode      bool
	theme         styles.Theme
	width, height int

	// Navigation.
	activeTab  tabID
	focus      focusArea
	cardCursor int // selected card index
	listCursor int // selected item in list below cards

	// Accounts tab data.
	accountKeys []string // sorted account keys
	statuses    []status.RepoStatus
	loading     bool

	// Pull-all progress.
	fetchProgressCh    <-chan pullAllProgressMsg
	cloneAllProgressCh <-chan cloneAllProgressMsg
	cloneAllDoneCh     <-chan cloneAllDoneMsg
	pullAllLabel       string            // status bar text
	pullAllActive      map[string]string // repoKey -> "fetching"/"cloning" (for inline indicators)

	// Mirrors tab data.
	mirrorSummaries    []mirror.MirrorSummary
	mirrorLiveResults  map[string][]mirror.StatusResult

	// Debounce: skip refresh if last check was recent.
	lastRefresh time.Time

	orphanCount int // number of orphan repos found on last scan

	showHelp bool
	version  string
}

func newDashboardModel(cfg *config.Config, cfgPath string, credMgr *credential.StatusManager, theme styles.Theme, w, h int) dashboardModel {
	m := dashboardModel{
		cfg:     cfg,
		cfgPath: cfgPath,
		credMgr: credMgr,
		theme:   theme,
		width:   w,
		height:  h,
		loading: true,
	}
	m.refreshAccountKeys()
	// Invalidate all credential statuses so the first render shows "checking",
	// not a stale result from a previous dashboard instance.
	for _, k := range m.accountKeys {
		credMgr.Invalidate(k)
	}
	return m
}

func (m *dashboardModel) refreshAccountKeys() {
	m.accountKeys = make([]string, 0, len(m.cfg.Accounts))
	for k := range m.cfg.Accounts {
		m.accountKeys = append(m.accountKeys, k)
	}
	sort.Strings(m.accountKeys)
}

func (m dashboardModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		checkAllStatusCmd(m.cfg),
		periodicRefreshCmd(),
		checkAllMirrorStatusCmd(m.cfg),
		scanOrphansCmd(m.cfg),
	}
	// Start periodic sync if configured.
	if cmd := periodicSyncCmd(m.cfg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	// Fire credential checks for all accounts in parallel.
	for _, k := range m.accountKeys {
		cmds = append(cmds, credCheckCmd(m.credMgr, m.cfg, k))
	}
	return tea.Batch(cmds...)
}

func checkAllStatusCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		results := status.CheckAll(cfg)
		return statusResultMsg{results: results}
	}
}

// credCheckCmd fires a single credential check via the StatusManager.
// Shared by dashboard and account screens.
func credCheckCmd(mgr *credential.StatusManager, cfg *config.Config, accountKey string) tea.Cmd {
	acct := cfg.Accounts[accountKey]
	epoch := mgr.StartCheck(accountKey)
	return func() tea.Msg {
		result := credential.Check(acct, accountKey, cfg)
		if mgr.CompleteCheck(accountKey, epoch, result) {
			return credStatusUpdatedMsg{accountKey: accountKey}
		}
		return credStatusNoopMsg{}
	}
}

func (m dashboardModel) checkAllCredsCmd() tea.Cmd {
	var cmds []tea.Cmd
	for _, k := range m.accountKeys {
		cmds = append(cmds, credCheckCmd(m.credMgr, m.cfg, k))
	}
	return tea.Batch(cmds...)
}

func reloadConfigCmd(path string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load(path)
		return configReloadedMsg{cfg: cfg, err: err}
	}
}

func checkAllMirrorStatusCmd(cfg *config.Config) tea.Cmd {
	if len(cfg.Mirrors) == 0 {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return mirrorAllStatusMsg{results: mirror.CheckAllMirrors(ctx, cfg)}
	}
}

func periodicRefreshCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return statusRefreshTickMsg{}
	})
}

// periodicSyncCmd returns a tea.Cmd that fires a syncTickMsg after the configured interval.
// Returns nil if periodic sync is disabled.
func periodicSyncCmd(cfg *config.Config) tea.Cmd {
	interval := parseSyncInterval(cfg.Global.PeriodicSync)
	if interval == 0 {
		return nil
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return syncTickMsg{}
	})
}

func parseSyncInterval(s string) time.Duration {
	switch s {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	default:
		return 0
	}
}

func fetchAllCmd(cfg *config.Config) (<-chan pullAllProgressMsg, tea.Cmd) {
	progressCh := make(chan pullAllProgressMsg, 4)

	cmd := func() tea.Msg {
		globalFolder := config.ExpandTilde(cfg.Global.Folder)
		for _, sKey := range cfg.OrderedSourceKeys() {
			src := cfg.Sources[sKey]
			acct, ok := cfg.Accounts[src.Account]
			if !ok {
				continue
			}
			sourceFolder := src.EffectiveFolder(sKey)
			for _, rKey := range src.OrderedRepoKeys() {
				repo := src.Repos[rKey]
				path := status.ResolveRepoPath(globalFolder, sourceFolder, rKey, repo)
				if !git.IsRepo(path) {
					continue
				}
				select {
				case progressCh <- pullAllProgressMsg{repoKey: rKey}:
				default:
				}
				_ = git.FetchQuiet(path)
				wn, we := identity.ResolveIdentity(repo, acct)
				_, _, _ = identity.EnsureRepoIdentity(path, wn, we)
			}
		}
		close(progressCh)
		return fetchAllDoneMsg{}
	}

	return progressCh, cmd
}

func listenPullAllProgress(progressCh <-chan pullAllProgressMsg) tea.Cmd {
	return func() tea.Msg {
		p, ok := <-progressCh
		if !ok {
			return pullAllDoneMsg{}
		}
		return p
	}
}

// cloneAllCmd clones all not-cloned repos, sending progress messages via a channel.
func cloneAllCmd(cfg *config.Config) (<-chan cloneAllProgressMsg, <-chan cloneAllDoneMsg) {
	progressCh := make(chan cloneAllProgressMsg, 4)
	doneCh := make(chan cloneAllDoneMsg, 1)

	go func() {
		globalFolder := config.ExpandTilde(cfg.Global.Folder)

		// Collect not-cloned repos.
		type repoRef struct {
			sourceKey, repoKey, credType, accountKey string
			acct                                     config.Account
			repo                                     config.Repo
			dest, plainURL                           string
		}
		var pending []repoRef
		for _, sKey := range cfg.OrderedSourceKeys() {
			src := cfg.Sources[sKey]
			acct, ok := cfg.Accounts[src.Account]
			if !ok {
				continue
			}
			sourceFolder := src.EffectiveFolder(sKey)
			for _, rKey := range src.OrderedRepoKeys() {
				repo := src.Repos[rKey]
				path := status.ResolveRepoPath(globalFolder, sourceFolder, rKey, repo)
				if git.IsRepo(path) {
					continue
				}
				credType := repo.EffectiveCredentialType(&acct)
				pending = append(pending, repoRef{
					sourceKey:  sKey,
					repoKey:    rKey,
					credType:   credType,
					accountKey: src.Account,
					acct:       acct,
					repo:       repo,
					dest:       path,
					plainURL:   cloneURL(acct, rKey, credType),
				})
			}
		}

		cloned, errors := 0, 0
		for i, r := range pending {
			// Report progress BEFORE clone starts (UI shows "cloning <repo>").
			progressCh <- cloneAllProgressMsg{current: i + 1, total: len(pending), repoKey: r.repoKey}

			cloneURLStr := r.plainURL
			cloneOpts := git.CloneOpts{Quiet: true}
			if r.credType == "token" {
				if tok, _, err := credential.ResolveToken(r.acct, r.accountKey); err == nil && tok != "" {
					if u, err := neturl.Parse(r.plainURL); err == nil {
						u.User = neturl.UserPassword(r.acct.Username, tok)
						cloneURLStr = u.String()
					}
				}
				cloneOpts.ConfigArgs = []string{"credential.helper="}
			}

			if err := git.Clone(cloneURLStr, r.dest, cloneOpts); err != nil {
				errors++
				continue
			}

			if r.credType == "token" {
				_ = git.SetRemoteURL(r.dest, "origin", r.plainURL)
			}
			_ = credential.ConfigureRepoCredential(r.dest, r.acct, r.accountKey, r.credType, cfg.Global)
			wn, we := identity.ResolveIdentity(r.repo, r.acct)
			_, _, _ = identity.EnsureRepoIdentity(r.dest, wn, we)
			cloned++
		}

		close(progressCh)
		doneCh <- cloneAllDoneMsg{cloned: cloned, errors: errors}
	}()

	return progressCh, doneCh
}

func listenCloneAllCmd(progressCh <-chan cloneAllProgressMsg, doneCh <-chan cloneAllDoneMsg) tea.Cmd {
	return func() tea.Msg {
		select {
		case p, ok := <-progressCh:
			if ok {
				return p
			}
			return <-doneCh
		case d := <-doneCh:
			return d
		}
	}
}

// sortedStatuses returns statuses sorted by source then repo.
func sortedStatuses(ss []status.RepoStatus) []status.RepoStatus {
	sorted := make([]status.RepoStatus, len(ss))
	copy(sorted, ss)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Source != sorted[j].Source {
			return sorted[i].Source < sorted[j].Source
		}
		return sorted[i].Repo < sorted[j].Repo
	})
	return sorted
}

// accountRepoStats counts clean/dirty/behind/notCloned per account key.
type accountStats struct {
	total, clean, dirty, behind, notCloned, other int
}

func computeAccountStats(statuses []status.RepoStatus, cfg *config.Config) map[string]accountStats {
	// Map source -> account.
	sourceToAccount := make(map[string]string)
	for k, src := range cfg.Sources {
		sourceToAccount[k] = src.Account
	}

	stats := make(map[string]accountStats)
	for _, r := range statuses {
		acctKey := sourceToAccount[r.Source]
		s := stats[acctKey]
		s.total++
		switch r.State {
		case status.Clean:
			s.clean++
		case status.Dirty:
			s.dirty++
		case status.Behind:
			s.behind++
		case status.NotCloned:
			s.notCloned++
		case status.NoUpstream:
			if r.IsDefault {
				s.other++ // No upstream on default branch is a real issue.
			} else {
				s.clean++ // Feature branch with no upstream is normal.
			}
		default:
			s.other++
		}
		stats[acctKey] = s
	}
	return stats
}

func (m dashboardModel) Update(msg tea.Msg) (dashboardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, Keys.Tab):
			m.activeTab = (m.activeTab + 1) % tabCount
			m.cardCursor = 0
			m.listCursor = 0
			m.focus = focusCards
			return m, nil

		case msg.String() == "left" || msg.String() == "h":
			if m.focus == focusCards && m.cardCursor > 0 {
				m.cardCursor--
			}
			return m, nil

		case msg.String() == "right" || msg.String() == "l":
			if m.focus == focusCards {
				maxCard := m.maxCardIndex()
				if m.cardCursor < maxCard {
					m.cardCursor++
				}
			}
			return m, nil

		case key.Matches(msg, Keys.Up):
			if m.focus == focusList {
				if m.listCursor > 0 {
					m.listCursor--
				} else {
					// Jump back to cards.
					m.focus = focusCards
				}
			} else if m.focus == focusCards {
				// Nothing above cards.
			}
			return m, nil

		case key.Matches(msg, Keys.Down):
			if m.focus == focusCards {
				// Jump to list if there are items.
				if m.listItemCount() > 0 {
					m.focus = focusList
					m.listCursor = 0
				}
			} else if m.focus == focusList {
				max := m.listItemCount() - 1
				if m.listCursor < max {
					m.listCursor++
				}
			}
			return m, nil

		case msg.String() == "P":
			// Pull All: fetch cloned repos + clone not-cloned repos.
			m.loading = true
			m.pullAllLabel = ""
			m.pullAllActive = make(map[string]string)
			fCh, fetchCmd := fetchAllCmd(m.cfg)
			m.fetchProgressCh = fCh
			cCh, dCh := cloneAllCmd(m.cfg)
			m.cloneAllProgressCh = cCh
			m.cloneAllDoneCh = dCh
			return m, tea.Batch(fetchCmd, listenPullAllProgress(fCh), listenCloneAllCmd(cCh, dCh))

		case key.Matches(msg, Keys.Refresh):
			m.loading = true
			return m, checkAllStatusCmd(m.cfg)

		case key.Matches(msg, Keys.Reload):
			return m, reloadConfigCmd(m.cfgPath)

		case key.Matches(msg, Keys.Back):
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			return m, tea.Quit

		case key.Matches(msg, Keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, Keys.Enter):
			return m, m.handleEnter()

		case msg.String() == "N" && m.activeTab == tabAccounts:
			// Create new repo on provider.
			if len(m.accountKeys) > 0 {
				acctKey := ""
				if len(m.accountKeys) == 1 {
					acctKey = m.accountKeys[0]
				}
				return m, func() tea.Msg {
					return switchScreenMsg{screen: screenRepoCreate, accountKey: acctKey}
				}
			}
			return m, nil

		case msg.String() == "a" && m.activeTab == tabAccounts:
			return m, func() tea.Msg { return switchScreenMsg{screen: screenAccountAdd} }
		case msg.String() == "d" && m.activeTab == tabAccounts:
			// Discovery shortcut for selected account card.
			if m.focus == focusCards && len(m.accountKeys) > 0 && m.cardCursor < len(m.accountKeys) {
				acctKey := m.accountKeys[m.cardCursor]
				return m, func() tea.Msg {
					return switchScreenMsg{screen: screenDiscovery, accountKey: acctKey}
				}
			}
			return m, nil
		case msg.String() == "s":
			return m, func() tea.Msg { return switchScreenMsg{screen: screenSettings} }
		case msg.String() == "i":
			return m, func() tea.Msg { return switchScreenMsg{screen: screenIdentity} }
		case msg.String() == "O":
			return m, func() tea.Msg { return switchScreenMsg{screen: screenOrphans} }
		case key.Matches(msg, Keys.Help):
			m.showHelp = true
			return m, nil
		}

	case orphansScanDoneMsg:
		if msg.err == nil {
			m.orphanCount = len(msg.orphans)
		}
		return m, nil

	case mirrorAllStatusMsg:
		m.mirrorLiveResults = msg.results
		m.mirrorSummaries = mirror.Summarize(m.cfg, msg.results)
		return m, nil

	case configReloadedMsg:
		if msg.err != nil {
			return m, nil
		}
		m.cfg = msg.cfg
		m.refreshAccountKeys()
		m.mirrorSummaries = mirror.Summarize(m.cfg, m.mirrorLiveResults)
		m.loading = true
		return m, tea.Batch(checkAllStatusCmd(m.cfg), m.checkAllCredsCmd())

	case credStatusUpdatedMsg:
		return m, nil

	case credStatusNoopMsg:
		return m, nil

	case statusResultMsg:
		m.statuses = msg.results
		m.loading = false
		m.lastRefresh = time.Now()
		m.mirrorSummaries = mirror.Summarize(m.cfg, m.mirrorLiveResults)
		return m, nil

	case pullAllProgressMsg:
		m.pullAllLabel = fmt.Sprintf("Fetching: %s", msg.repoKey)
		if m.pullAllActive != nil {
			// Clear previous fetch markers, set current.
			for k, v := range m.pullAllActive {
				if v == "fetching" {
					delete(m.pullAllActive, k)
				}
			}
			m.pullAllActive[msg.repoKey] = "fetching"
		}
		return m, listenPullAllProgress(m.fetchProgressCh)

	case pullAllDoneMsg:
		m.fetchProgressCh = nil
		if m.cloneAllDoneCh == nil {
			m.pullAllLabel = ""
			m.pullAllActive = nil
			m.loading = true
			return m, tea.Batch(checkAllStatusCmd(m.cfg), m.checkAllCredsCmd())
		}
		return m, nil

	case cloneAllProgressMsg:
		m.pullAllLabel = fmt.Sprintf("Cloning %d/%d: %s", msg.current, msg.total, msg.repoKey)
		if m.pullAllActive != nil {
			for k, v := range m.pullAllActive {
				if v == "cloning" {
					delete(m.pullAllActive, k)
				}
			}
			m.pullAllActive[msg.repoKey] = "cloning"
		}
		return m, listenCloneAllCmd(m.cloneAllProgressCh, m.cloneAllDoneCh)

	case cloneAllDoneMsg:
		m.cloneAllProgressCh = nil
		m.cloneAllDoneCh = nil
		if m.fetchProgressCh == nil {
			m.pullAllLabel = ""
			m.pullAllActive = nil
			m.loading = true
			return m, tea.Batch(checkAllStatusCmd(m.cfg), m.checkAllCredsCmd())
		}
		return m, nil

	case fetchAllDoneMsg:
		m.loading = true
		return m, tea.Batch(checkAllStatusCmd(m.cfg), m.checkAllCredsCmd())

	case syncTickMsg:
		// Periodic sync: trigger Pull All if not already running.
		rearm := periodicSyncCmd(m.cfg)
		if m.loading || m.fetchProgressCh != nil || m.cloneAllDoneCh != nil {
			// Already busy — just re-arm the tick.
			return m, rearm
		}
		m.loading = true
		m.pullAllLabel = ""
		m.pullAllActive = make(map[string]string)
		fCh, fetchCmd := fetchAllCmd(m.cfg)
		m.fetchProgressCh = fCh
		cCh, dCh := cloneAllCmd(m.cfg)
		m.cloneAllProgressCh = cCh
		m.cloneAllDoneCh = dCh
		cmds := []tea.Cmd{fetchCmd, listenPullAllProgress(fCh), listenCloneAllCmd(cCh, dCh), checkAllMirrorStatusCmd(m.cfg)}
		if rearm != nil {
			cmds = append(cmds, rearm)
		}
		return m, tea.Batch(cmds...)

	case statusRefreshTickMsg:
		// Debounce: skip if last refresh was < 5s ago.
		if time.Since(m.lastRefresh) < 5*time.Second {
			return m, periodicRefreshCmd()
		}
		m.loading = true
		return m, tea.Batch(
			checkAllStatusCmd(m.cfg),
			periodicRefreshCmd(),
			m.checkAllCredsCmd(),
			checkAllMirrorStatusCmd(m.cfg),
		)
	}
	return m, nil
}

// maxCardIndex returns the max valid card index for the active tab.
func (m dashboardModel) maxCardIndex() int {
	switch m.activeTab {
	case tabAccounts:
		return max(0, len(m.accountKeys)-1)
	case tabMirrors:
		return max(0, len(m.mirrorSummaries)-1)
	}
	return 0
}

// listItemCount returns the number of items in the list area.
func (m dashboardModel) listItemCount() int {
	switch m.activeTab {
	case tabAccounts:
		return len(m.statuses)
	case tabMirrors:
		count := 0
		for _, s := range m.mirrorSummaries {
			count += s.Total
		}
		return count
	}
	return 0
}

func (m dashboardModel) View() string {
	if m.showHelp {
		return m.viewHelp()
	}

	var b strings.Builder

	// Title bar.
	title := m.theme.Title.Render("gitbox")
	ver := m.theme.TextMuted.Render(m.version)
	themeLabel := "dark"
	if m.theme.Palette.BgBase == "#f5f5f5" {
		themeLabel = "light"
	}
	rightBadges := m.theme.TextMuted.Render("[" + themeLabel + "]")
	if m.testMode {
		rightBadges = m.theme.TextMuted.Render("[test]") + " " + rightBadges
	}

	titleLine := lipgloss.JoinHorizontal(lipgloss.Top, title, " ", ver)
	if m.width > 0 {
		gap := m.width - lipgloss.Width(titleLine) - lipgloss.Width(rightBadges)
		if gap < 1 {
			gap = 1
		}
		titleLine = titleLine + strings.Repeat(" ", gap) + rightBadges
	}
	b.WriteString(titleLine + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n")

	// Tabs.
	tabs := []string{"Accounts", "Mirrors"}
	var tabLine []string
	for i, t := range tabs {
		if tabID(i) == m.activeTab {
			tabLine = append(tabLine, m.theme.ActiveTab.Render("["+t+"]"))
		} else {
			tabLine = append(tabLine, m.theme.InactiveTab.Render(" "+t+" "))
		}
	}
	b.WriteString(strings.Join(tabLine, "  ") + "\n\n")

	// Content area.
	switch m.activeTab {
	case tabAccounts:
		b.WriteString(m.viewAccountsTab())
	case tabMirrors:
		b.WriteString(m.viewMirrorsTab())
	}

	// Status bar.
	b.WriteString("\n")
	b.WriteString(m.viewStatusBar())

	return b.String()
}

// ─── Accounts tab ──────────────────────────────────────────────────────────

func (m dashboardModel) viewAccountsTab() string {
	var b strings.Builder

	// Account cards row.
	if len(m.accountKeys) == 0 {
		b.WriteString(m.theme.TextMuted.Render("  No accounts configured.") + "\n")
	} else {
		acctStats := computeAccountStats(m.statuses, m.cfg)
		cards := make([]string, 0, len(m.accountKeys))
		for i, k := range m.accountKeys {
			cards = append(cards, m.renderAccountCard(k, acctStats[k], i == m.cardCursor && m.focus == focusCards))
		}
		b.WriteString(m.renderCardSection(m.renderCardRow(cards)))
	}

	b.WriteString("\n")

	// Repo list grouped by source.
	if m.loading && len(m.statuses) == 0 {
		b.WriteString(m.theme.TextMuted.Render("  Loading...") + "\n")
	} else if len(m.statuses) == 0 {
		b.WriteString(m.theme.TextMuted.Render("  No accounts configured.") + "\n")
	} else {
		b.WriteString(m.viewRepoList())
	}

	return b.String()
}

func (m dashboardModel) renderAccountCard(accountKey string, stats accountStats, selected bool) string {
	acct := m.cfg.Accounts[accountKey]
	credType := acct.DefaultCredentialType

	// Single badge showing primary credential status.
	credResult := m.credMgr.Get(accountKey)
	primaryLabel := credType
	if primaryLabel == "" {
		primaryLabel = "config"
	}
	credBadge := m.renderStatusBadge(credResult.Primary, primaryLabel)

	// Card content lines.
	line1 := fmt.Sprintf("%s  %s", m.theme.TextMuted.Render(acct.Provider), credBadge)
	line2 := m.formatCardStats(stats)

	// Build card name (truncate if needed).
	name := accountKey
	if len(name) > 20 {
		name = name[:17] + "..."
	}

	content := fmt.Sprintf(" %s\n %s\n %s",
		m.theme.TextBold.Render(name),
		line1,
		line2)

	// Card border style.
	borderColor := m.theme.Palette.BorderDefault
	if selected {
		borderColor = m.theme.Palette.Brand
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(24)

	return cardStyle.Render(content)
}

func (m dashboardModel) formatCardStats(stats accountStats) string {
	cloned := stats.total - stats.notCloned
	if stats.total == 0 {
		return m.theme.TextMuted.Render("no repos")
	}

	ratio := fmt.Sprintf("%d/%d", cloned, stats.total)
	issues := stats.total - stats.clean - stats.notCloned
	if issues == 0 && stats.notCloned == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(ratio + " OK")
	}
	if issues > 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Behind)).
			Render(fmt.Sprintf("%s %d attention", ratio, issues))
	}
	return m.theme.TextMuted.Render(ratio)
}

// renderStatusBadge renders a label colored by credential status.
func (m dashboardModel) renderStatusBadge(s credential.Status, label string) string {
	switch s {
	case credential.StatusOK:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.CredOK)).Render(label)
	case credential.StatusWarning:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.CredWarning)).Render(label)
	case credential.StatusOffline:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.CredOffline)).Render("offline")
	case credential.StatusError:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.CredError)).Render(label)
	case credential.StatusNone:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.CredNone)).Render(label)
	default:
		return m.theme.TextMuted.Render("···")
	}
}

func (m dashboardModel) viewRepoList() string {
	sorted := sortedStatuses(m.statuses)

	// Clamp cursor.
	if m.listCursor >= len(sorted) {
		m.listCursor = len(sorted) - 1
	}
	if m.listCursor < 0 {
		m.listCursor = 0
	}

	// Compute content height: total height minus title(1) + divider(1) + tabs(1) + blank(1) + cards(~4) + blank(1) + statusbar(1) + blank(1)
	contentHeight := m.height - 11
	if contentHeight < 5 {
		contentHeight = 5
	}

	// Viewport scrolling.
	viewStart := 0
	if m.focus == focusList && m.listCursor >= contentHeight {
		viewStart = m.listCursor - contentHeight + 1
	}

	var b strings.Builder
	currentSource := ""
	lineIdx := -1 // tracks position in the flat list (repo items only)
	linesRendered := 0

	for _, r := range sorted {
		lineIdx++

		// Source group header.
		if r.Source != currentSource {
			currentSource = r.Source
			if lineIdx >= viewStart && linesRendered < contentHeight {
				b.WriteString("  " + m.theme.Heading.Render(r.Source) + "\n")
				linesRendered++
			}
		}

		if lineIdx < viewStart {
			continue
		}
		if linesRendered >= contentHeight {
			break
		}

		sym, color := stateSymbolColor(r.State, m.theme.Palette)
		stateLabel := fmt.Sprintf("%-10s", r.State.String())
		detail := formatStatusDetail(r)

		// Check if this repo has an active pull-all operation.
		var inlineProgress string
		if m.pullAllActive != nil {
			if act, ok := m.pullAllActive[r.Repo]; ok {
				inlineProgress = "  " + m.theme.Brand.Render(styles.SymSyncing+" "+act)
			}
		}

		// Branch badge: show when not on the default branch.
		var branchTag string
		if r.Branch == "(detached)" {
			branchTag = " " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).Render("[detached]")
		} else if r.Branch != "" && !r.IsDefault {
			branchTag = " " + m.theme.TextMuted.Render("["+r.Branch+"]")
		}

		symStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		line := fmt.Sprintf("    %s %-10s  %-25s%s  %s%s",
			symStyle.Render(sym),
			symStyle.Render(stateLabel),
			r.Repo,
			branchTag,
			m.theme.TextMuted.Render(detail),
			inlineProgress)

		if m.focus == focusList && lineIdx == m.listCursor {
			line = m.theme.SelectedRow.Render(line)
		}
		b.WriteString(line + "\n")
		linesRendered++
	}
	return b.String()
}

// ─── Mirrors tab ──────────────────────────────────────────────────────────

func (m dashboardModel) viewMirrorsTab() string {
	var b strings.Builder

	// Mirror group cards row.
	if len(m.mirrorSummaries) == 0 {
		b.WriteString(m.theme.TextMuted.Render("  No mirrors configured.") + "\n")
	} else {
		cards := make([]string, 0, len(m.mirrorSummaries))
		for i, s := range m.mirrorSummaries {
			cards = append(cards, m.renderMirrorCard(s, i == m.cardCursor && m.focus == focusCards))
		}
		b.WriteString(m.renderCardSection(m.renderCardRow(cards)))
	}

	b.WriteString("\n")

	// Mirror detail list grouped by mirror group.
	if len(m.mirrorSummaries) > 0 {
		b.WriteString(m.viewMirrorList())
	}

	return b.String()
}

func (m dashboardModel) renderMirrorCard(s mirror.MirrorSummary, selected bool) string {
	sym := styles.SymClean
	symColor := m.theme.Palette.Clean
	if s.Error > 0 {
		sym = styles.SymError
		symColor = m.theme.Palette.StatusError
	} else if s.Unchecked > 0 {
		sym = styles.SymSyncing
		symColor = m.theme.Palette.Syncing
	}

	symStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(symColor))

	name := s.MirrorKey
	if len(name) > 20 {
		name = name[:17] + "..."
	}
	accounts := s.AccountSrc + " ↔ " + s.AccountDst
	ratio := fmt.Sprintf("%d/%d", s.Active, s.Total)
	var statsLine string
	if s.Error > 0 {
		statsLine = fmt.Sprintf(" %s  %s",
			m.theme.TextBold.Render(ratio),
			lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
				Render(fmt.Sprintf("%d errors", s.Error)))
	} else if s.Unchecked > 0 {
		statsLine = fmt.Sprintf(" %s  %s",
			m.theme.TextBold.Render(ratio),
			lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Syncing)).
				Render(fmt.Sprintf("%d unchecked", s.Unchecked)))
	} else if s.Total > 0 {
		statsLine = fmt.Sprintf(" %s  %s",
			m.theme.TextBold.Render(ratio),
			lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean)).
				Render("All synced"))
	} else {
		statsLine = " " + m.theme.TextMuted.Render("No repos")
	}

	content := fmt.Sprintf(" %s %s\n %s\n%s",
		symStyle.Render(sym),
		m.theme.TextBold.Render(name),
		m.theme.TextMuted.Render(accounts),
		statsLine)

	borderColor := m.theme.Palette.BorderDefault
	if selected {
		borderColor = m.theme.Palette.Brand
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(28)

	return cardStyle.Render(content)
}

func (m dashboardModel) viewMirrorList() string {
	if m.cfg == nil {
		return ""
	}

	var b strings.Builder
	lineIdx := 0
	for _, s := range m.mirrorSummaries {
		b.WriteString("  " + m.theme.Heading.Render(s.AccountSrc+" ↔ "+s.AccountDst) + "\n")

		mirr, ok := m.cfg.Mirrors[s.MirrorKey]
		if !ok {
			continue
		}
		repoKeys := make([]string, 0, len(mirr.Repos))
		for k := range mirr.Repos {
			repoKeys = append(repoKeys, k)
		}
		sort.Strings(repoKeys)

		for _, rk := range repoKeys {
			mr := mirr.Repos[rk]

			// Build direction label: "src → dst" or "dst ← src".
			var dirLabel string
			if mr.Direction == "push" {
				origin := s.AccountSrc
				backup := s.AccountDst
				if mr.Origin == "dst" {
					origin, backup = backup, origin
				}
				dirLabel = origin + " → " + backup
			} else {
				origin := s.AccountSrc
				backup := s.AccountDst
				if mr.Origin == "dst" {
					origin, backup = backup, origin
				}
				dirLabel = backup + " ← " + origin
			}

			sym := styles.SymNotCloned
			color := m.theme.Palette.Syncing
			statusText := "unchecked"

			// Use live results if available.
			if liveResults, ok := m.mirrorLiveResults[s.MirrorKey]; ok {
				for _, sr := range liveResults {
					if sr.RepoKey == rk {
						if sr.Error != "" {
							sym = styles.SymError
							color = m.theme.Palette.StatusError
							statusText = friendlyMirrorError(sr.Error)
						} else if sr.SyncStatus == "synced" {
							sym = styles.SymClean
							color = m.theme.Palette.Clean
							statusText = "synced"
						} else if sr.SyncStatus == "behind" {
							sym = styles.SymBehind
							color = m.theme.Palette.Behind
							statusText = "behind"
						} else {
							sym = styles.SymClean
							color = m.theme.Palette.Clean
							statusText = "OK"
						}
						break
					}
				}
			}

			symStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))

			statusRendered := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(statusText)
			if strings.HasPrefix(statusText, "missing API token") {
				statusRendered += "  " + m.theme.HelpKey.Render("↵ fix")
			}

			line := fmt.Sprintf("    %s  %-30s  %s  %s",
				symStyle.Render(sym), rk,
				m.theme.TextMuted.Render(dirLabel),
				statusRendered)

			if m.focus == focusList && lineIdx == m.listCursor {
				line = m.theme.SelectedRow.Render(line)
			}
			b.WriteString(line + "\n")
			lineIdx++
		}
	}
	return b.String()
}

// ─── Shared card rendering ────────────────────────────────────────────────

func (m dashboardModel) renderCardSection(content string) string {
	w := m.width - 2 // account for border chars
	if w < 20 {
		w = 20
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(m.theme.Palette.CardSectionBorder)).
		Width(w).
		Padding(0, 1).
		Render(content) + "\n"
}

func (m dashboardModel) renderCardRow(cards []string) string {
	if len(cards) == 0 {
		return ""
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

// ─── Enter handler ────────────────────────────────────────────────────────

func (m dashboardModel) handleEnter() tea.Cmd {
	switch m.activeTab {
	case tabAccounts:
		if m.focus == focusCards && len(m.accountKeys) > 0 && m.cardCursor < len(m.accountKeys) {
			key := m.accountKeys[m.cardCursor]
			return func() tea.Msg { return switchScreenMsg{screen: screenAccount, accountKey: key} }
		}
		if m.focus == focusList {
			sorted := sortedStatuses(m.statuses)
			if m.listCursor < len(sorted) {
				r := sorted[m.listCursor]
				return func() tea.Msg {
					return switchScreenMsg{
						screen:    screenRepos,
						sourceKey: r.Source,
						repoKey:   r.Repo,
					}
				}
			}
		}
	case tabMirrors:
		if m.focus == focusCards && len(m.mirrorSummaries) > 0 && m.cardCursor < len(m.mirrorSummaries) {
			return func() tea.Msg {
				return switchScreenMsg{
					screen:   screenMirrors,
					mirrorKey: m.mirrorSummaries[m.cardCursor].MirrorKey,
				}
			}
		}
		if m.focus == focusList {
			// If the selected mirror repo has a token error, jump to PAT setup.
			if acctKey := m.mirrorListErrorAcct(); acctKey != "" {
				return func() tea.Msg {
					return switchScreenMsg{screen: screenCredential, accountKey: acctKey, forceTokenSetup: true}
				}
			}
		}
	}
	return nil
}

// ─── Help view ────────────────────────────────────────────────────────────

// mirrorListErrorAcct returns the account key from a "missing API token in <acct>" error
// for the currently selected mirror list row, or "" if not applicable.
func (m dashboardModel) mirrorListErrorAcct() string {
	idx := 0
	for _, s := range m.mirrorSummaries {
		results := m.mirrorLiveResults[s.MirrorKey]
		mirr, ok := m.cfg.Mirrors[s.MirrorKey]
		if !ok {
			continue
		}
		repoKeys := make([]string, 0, len(mirr.Repos))
		for k := range mirr.Repos {
			repoKeys = append(repoKeys, k)
		}
		sort.Strings(repoKeys)
		for _, rk := range repoKeys {
			if idx == m.listCursor {
				for _, sr := range results {
					if sr.RepoKey == rk && strings.HasPrefix(sr.Error, "missing API token in ") {
						return strings.TrimPrefix(sr.Error, "missing API token in ")
					}
				}
				return ""
			}
			idx++
		}
	}
	return ""
}

func (m dashboardModel) viewHelp() string {
	var b strings.Builder
	b.WriteString(m.theme.Title.Render("Keyboard Shortcuts") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	help := []struct{ key, desc string }{
		{"←/h  →/l", "Navigate cards"},
		{"↑/k  ↓/j", "Navigate clones / move to cards"},
		{"tab", "Switch tab (Accounts → Mirrors)"},
		{"enter", "Open detail for selected card or clone"},
		{"a", "Add new account (Accounts tab)"},
		{"N", "Create new repo on provider"},
		{"d", "Discover repos for selected account"},
		{"P", "Pull all (fetch cloned + clone new)"},
		{"r", "Refresh status"},
		{"R", "Reload config from disk"},
		{"t", "Toggle dark / light theme"},
		{"s", "Settings"},
		{"i", "Identity check (global ~/.gitconfig)"},
		{"ESC", "Quit"},
		{"?", "Toggle this help"},
	}

	for _, h := range help {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			m.theme.HelpKey.Render(fmt.Sprintf("%-12s", h.key)),
			m.theme.HelpDesc.Render(h.desc)))
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Title.Render("Status Symbols") + "\n\n")

	syms := []struct{ sym, color, desc string }{
		{styles.SymClean, m.theme.Palette.Clean, "Clean — up to date"},
		{styles.SymDirty, m.theme.Palette.Dirty, "Dirty — local changes"},
		{styles.SymBehind, m.theme.Palette.Behind, "Behind — needs pull"},
		{styles.SymAhead, m.theme.Palette.Ahead, "Ahead — needs push"},
		{styles.SymDiverged, m.theme.Palette.Diverged, "Diverged — ahead and behind"},
		{styles.SymConflict, m.theme.Palette.Conflict, "Conflict — merge conflicts"},
		{styles.SymNotCloned, m.theme.Palette.NotCloned, "Not cloned"},
		{styles.SymError, m.theme.Palette.StatusError, "Error"},
	}

	for _, s := range syms {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color(s.color)).Render(s.sym),
			m.theme.HelpDesc.Render(s.desc)))
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Title.Render("Tips") + "\n\n")
	tips := []string{
		"PAT (Personal Access Token) are optional for SSH accounts when you",
		"need repo discovery, repo creation, and mirrors. Also optional for",
		"GCM accounts when you need mirrors.",
		"Press p on the account detail screen to add one.",
	}
	for _, t := range tips {
		b.WriteString("  " + m.theme.HelpDesc.Render(t) + "\n")
	}

	b.WriteString("\n" + renderHints(m.theme, "ESC close help"))
	return b.String()
}

// ─── Status bar ───────────────────────────────────────────────────────────

func (m dashboardModel) viewStatusBar() string {
	// Summary counts.
	clean, dirty, behind, notCloned, other := 0, 0, 0, 0, 0
	for _, r := range m.statuses {
		switch r.State {
		case status.Clean:
			clean++
		case status.Dirty:
			dirty++
		case status.Behind:
			behind++
		case status.NotCloned:
			notCloned++
		default:
			other++
		}
	}

	var summary string
	if m.activeTab == tabMirrors {
		// Mirror summary.
		totalMirrors, activeMirrors, errorMirrors := 0, 0, 0
		for _, s := range m.mirrorSummaries {
			totalMirrors += s.Total
			activeMirrors += s.Active
			errorMirrors += s.Error
		}
		summary = fmt.Sprintf("%d mirrors", totalMirrors)
		if activeMirrors > 0 {
			summary += fmt.Sprintf(", %d synced", activeMirrors)
		}
		if errorMirrors > 0 {
			summary += fmt.Sprintf(", %d error", errorMirrors)
		}
	} else {
		// Accounts summary.
		total := len(m.statuses)
		summary = fmt.Sprintf("%d clones", total)
		if total > 0 {
			parts := []string{}
			if clean > 0 {
				parts = append(parts, fmt.Sprintf("%d clean", clean))
			}
			if dirty > 0 {
				parts = append(parts, fmt.Sprintf("%d dirty", dirty))
			}
			if behind > 0 {
				parts = append(parts, fmt.Sprintf("%d behind", behind))
			}
			if notCloned > 0 {
				parts = append(parts, fmt.Sprintf("%d not cloned", notCloned))
			}
			if other > 0 {
				parts = append(parts, fmt.Sprintf("%d other", other))
			}
			summary += ": " + strings.Join(parts, ", ")
		}
	}

	if m.orphanCount > 0 {
		summary += fmt.Sprintf("  %d orphan(s)", m.orphanCount)
	}

	if m.pullAllLabel != "" {
		summary += "  " + styles.SymSyncing + " " + m.pullAllLabel
	} else if m.loading {
		summary += "  " + styles.SymSyncing + " refreshing..."
	}

	left := m.theme.StatusBar.Render(summary)
	hintsWidth := m.width - lipgloss.Width(left) - 2

	var right string
	switch m.activeTab {
	case tabMirrors:
		right = renderHintsFit(m.theme, hintsWidth, "←→ cards", "↑↓ mirrors", "tab section", "r refresh", "R reload", "? help", "ESC quit")
	default:
		hints := []string{"←→ cards", "↑↓ clones", "tab section", "P pull all", "r refresh", "R reload", "a add", "N new repo"}
		if m.orphanCount > 0 {
			hints = append(hints, "O orphans")
		}
		hints = append(hints, "? help", "ESC quit")
		right = renderHintsFit(m.theme, hintsWidth, hints...)
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		gap = 2
	}

	return left + strings.Repeat(" ", gap) + right
}

// ─── Shared helpers ───────────────────────────────────────────────────────

// stateSymbolColor returns the symbol and color for a status state.
func stateSymbolColor(s status.State, p styles.Palette) (string, string) {
	switch s {
	case status.Clean:
		return styles.SymClean, p.Clean
	case status.Behind:
		return styles.SymBehind, p.Behind
	case status.Dirty:
		return styles.SymDirty, p.Dirty
	case status.Ahead:
		return styles.SymAhead, p.Ahead
	case status.Diverged:
		return styles.SymDiverged, p.Diverged
	case status.Conflict:
		return styles.SymConflict, p.Conflict
	case status.NotCloned:
		return styles.SymNotCloned, p.NotCloned
	case status.NoUpstream:
		return styles.SymNoUp, p.NoUpstream
	case status.Error:
		return styles.SymError, p.StatusError
	default:
		return "?", p.TextMuted
	}
}

// formatStatusDetail returns a human-readable detail string for a repo status.
func formatStatusDetail(r status.RepoStatus) string {
	switch r.State {
	case status.Behind:
		return fmt.Sprintf("%d behind", r.Behind)
	case status.Ahead:
		return fmt.Sprintf("%d ahead", r.Ahead)
	case status.Diverged:
		return fmt.Sprintf("%d ahead, %d behind", r.Ahead, r.Behind)
	case status.Dirty:
		parts := []string{}
		if r.Modified > 0 {
			parts = append(parts, fmt.Sprintf("%d mod", r.Modified))
		}
		if r.Untracked > 0 {
			parts = append(parts, fmt.Sprintf("%d unt", r.Untracked))
		}
		if len(parts) == 0 {
			return "changes"
		}
		return strings.Join(parts, ", ")
	case status.Conflict:
		return fmt.Sprintf("%d conflicts", r.Conflicts)
	case status.NoUpstream:
		if r.IsDefault {
			return "no upstream"
		}
		return "local branch"
	case status.Error:
		return r.ErrorMsg
	default:
		return ""
	}
}
