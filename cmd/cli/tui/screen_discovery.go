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
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type discView int

const (
	discViewList       discView = iota // normal repo list
	discViewTokenInput                 // PAT input for SSH accounts
)

type discoveryModel struct {
	cfg           *config.Config
	cfgPath       string
	accountKey    string
	theme         styles.Theme
	width, height int
	view          discView
	repos         []provider.RemoteRepo
	selected      map[int]bool    // keyed by real index in m.repos
	configured    map[string]bool
	cursor        int             // position in visible list (filtered or full)
	scrollOffset  int             // viewport scroll offset
	loading       bool
	adding        bool
	addedCount    int
	errMsg        string

	// Filter.
	filtering   bool
	filterInput textinput.Model
	filtered    []int // indices into m.repos that match filter (nil = no filter)

	// Token input for SSH accounts.
	tokenInput  textinput.Model
	tokenBusy   bool
	tokenURL    string // PAT creation URL
	tokenScopes string // required scopes for discovery
}

func newDiscoveryModel(cfg *config.Config, cfgPath, accountKey string, theme styles.Theme, w, h int) discoveryModel {
	// Build set of already-configured repos.
	configured := make(map[string]bool)
	for _, src := range cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		for k := range src.Repos {
			configured[k] = true
		}
	}

	fi := textinput.New()
	fi.Placeholder = "filter..."
	fi.CharLimit = 100
	fi.Width = 40

	ti := textinput.New()
	ti.Placeholder = "Paste your token here..."
	ti.EchoMode = textinput.EchoNormal
	ti.CharLimit = 256
	ti.Width = 60

	acct := cfg.Accounts[accountKey]

	return discoveryModel{
		cfg:         cfg,
		cfgPath:     cfgPath,
		accountKey:  accountKey,
		theme:       theme,
		width:       w,
		height:      h,
		view:        discViewList,
		selected:    make(map[int]bool),
		configured:  configured,
		loading:     true,
		filterInput: fi,
		tokenInput:  ti,
		tokenURL:    provider.TokenCreationURL(acct.Provider, acct.URL),
		tokenScopes: provider.DiscoveryRequiredScopes(acct.Provider),
	}
}

func discoverReposCmd(cfg *config.Config, accountKey string) tea.Cmd {
	return func() tea.Msg {
		acct, ok := cfg.GetAccountByKey(accountKey)
		if !ok {
			return discoverDoneMsg{err: fmt.Errorf("account %q not found", accountKey)}
		}
		token, _, err := credential.ResolveAPIToken(acct, accountKey)
		if err != nil {
			if acct.DefaultCredentialType == "ssh" {
				return discoverDoneMsg{
					err:        fmt.Errorf("SSH accounts need a Personal Access Token for API discovery"),
					needsToken: true,
				}
			}
			return discoverDoneMsg{err: fmt.Errorf("no API credentials for %q", accountKey)}
		}
		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			return discoverDoneMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		repos, err := prov.ListRepos(ctx, acct.URL, token, acct.Username)
		if err != nil {
			return discoverDoneMsg{err: err}
		}
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].FullName < repos[j].FullName
		})
		return discoverDoneMsg{repos: repos}
	}
}

func addDiscoveredReposCmd(cfg *config.Config, cfgPath, accountKey string, repos []provider.RemoteRepo, indices map[int]bool) tea.Cmd {
	return func() tea.Msg {
		// Find or create source.
		sourceKey := ""
		for k, src := range cfg.Sources {
			if src.Account == accountKey {
				sourceKey = k
				break
			}
		}
		if sourceKey == "" {
			sourceKey = accountKey
			src := config.Source{Account: accountKey, Repos: make(map[string]config.Repo)}
			if err := cfg.AddSource(sourceKey, src); err != nil {
				return reposAddedMsg{err: err}
			}
		}

		count := 0
		for idx := range indices {
			if idx < len(repos) {
				if err := cfg.AddRepo(sourceKey, repos[idx].FullName, config.Repo{}); err == nil {
					count++
				}
			}
		}

		if err := config.Save(cfg, cfgPath); err != nil {
			return reposAddedMsg{err: err}
		}
		return reposAddedMsg{count: count}
	}
}

func storeDiscoveryTokenCmd(accountKey, token string) tea.Cmd {
	return func() tea.Msg {
		if err := credential.StoreToken(accountKey, strings.TrimSpace(token)); err != nil {
			return discoverTokenStoredMsg{err: err}
		}
		return discoverTokenStoredMsg{}
	}
}

// visibleIndices returns the repo indices to display (filtered or all).
func (m discoveryModel) visibleIndices() []int {
	if m.filtered != nil {
		return m.filtered
	}
	idx := make([]int, len(m.repos))
	for i := range m.repos {
		idx[i] = i
	}
	return idx
}

// applyFilter rebuilds the filtered index list from the current filter text.
func (m *discoveryModel) applyFilter() {
	text := strings.ToLower(m.filterInput.Value())
	if text == "" {
		m.filtered = nil
		return
	}
	m.filtered = nil
	for i, r := range m.repos {
		if strings.Contains(strings.ToLower(r.FullName), text) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.filtered == nil {
		m.filtered = []int{} // empty, not nil (means filter active, zero results)
	}
	m.cursor = 0
	m.scrollOffset = 0
}

// contentHeight returns how many repo lines fit on screen.
func (m discoveryModel) contentHeight() int {
	h := m.height - 10 // title + divider + filter + summary + hints + margins
	if m.filtering {
		h -= 1
	}
	if h < 3 {
		h = 3
	}
	return h
}

// ensureCursorVisible adjusts scrollOffset so cursor is in the viewport.
func (m *discoveryModel) ensureCursorVisible() {
	ch := m.contentHeight()
	if m.cursor < m.scrollOffset {
		m.scrollOffset = m.cursor
	}
	if m.cursor >= m.scrollOffset+ch {
		m.scrollOffset = m.cursor - ch + 1
	}
}

func (m discoveryModel) Init() tea.Cmd {
	return discoverReposCmd(m.cfg, m.accountKey)
}

func (m discoveryModel) Update(msg tea.Msg) (discoveryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Token input view handles its own keys.
		if m.view == discViewTokenInput {
			return m.updateTokenInput(msg)
		}

		// Filter mode intercepts keys.
		if m.filtering {
			switch {
			case key.Matches(msg, Keys.Back):
				m.filtering = false
				m.filterInput.SetValue("")
				m.filterInput.Blur()
				m.filtered = nil
				m.cursor = 0
				m.scrollOffset = 0
				return m, nil
			case key.Matches(msg, Keys.Enter):
				// Accept filter, exit filter mode but keep results.
				m.filtering = false
				m.filterInput.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				m.applyFilter()
				return m, cmd
			}
		}

		vis := m.visibleIndices()

		switch {
		case key.Matches(msg, Keys.Back):
			return m, func() tea.Msg {
				return switchScreenMsg{screen: screenAccount, accountKey: m.accountKey}
			}

		case msg.String() == "/":
			m.filtering = true
			m.filterInput.SetValue("")
			m.filterInput.Focus()
			m.cursor = 0
			m.scrollOffset = 0
			return m, textinput.Blink

		case key.Matches(msg, Keys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.ensureCursorVisible()
			}
			return m, nil

		case key.Matches(msg, Keys.Down):
			if m.cursor < len(vis)-1 {
				m.cursor++
				m.ensureCursorVisible()
			}
			return m, nil

		case msg.String() == " ": // space to toggle selection
			if !m.loading && len(vis) > 0 && m.cursor < len(vis) {
				realIdx := vis[m.cursor]
				r := m.repos[realIdx]
				if !m.configured[r.FullName] {
					m.selected[realIdx] = !m.selected[realIdx]
				}
			}
			return m, nil

		case msg.String() == "A": // select all visible new
			for _, realIdx := range vis {
				r := m.repos[realIdx]
				if !m.configured[r.FullName] {
					m.selected[realIdx] = true
				}
			}
			return m, nil

		case key.Matches(msg, Keys.Enter):
			if m.adding || m.loading {
				return m, nil
			}
			if len(m.selected) == 0 {
				m.errMsg = "No repos selected. Use space to select."
				return m, nil
			}
			m.adding = true
			m.errMsg = ""
			return m, addDiscoveredReposCmd(m.cfg, m.cfgPath, m.accountKey, m.repos, m.selected)
		}

	case discoverDoneMsg:
		m.loading = false
		if msg.needsToken {
			m.view = discViewTokenInput
			m.tokenInput.SetValue("")
			m.tokenInput.Focus()
			m.errMsg = ""
			return m, textinput.Blink
		}
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.repos = msg.repos
		}
		return m, nil

	case discoverTokenStoredMsg:
		m.tokenBusy = false
		if msg.err != nil {
			m.errMsg = "Failed to store token: " + msg.err.Error()
			return m, nil
		}
		// Token stored — retry discovery.
		m.view = discViewList
		m.loading = true
		m.errMsg = ""
		return m, discoverReposCmd(m.cfg, m.accountKey)

	case reposAddedMsg:
		m.adding = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.addedCount = msg.count
			// Refresh configured set.
			for idx := range m.selected {
				if idx < len(m.repos) {
					m.configured[m.repos[idx].FullName] = true
				}
			}
			m.selected = make(map[int]bool)
		}
		return m, nil
	}
	return m, nil
}

func (m discoveryModel) updateTokenInput(msg tea.KeyMsg) (discoveryModel, tea.Cmd) {
	if m.tokenBusy {
		return m, nil
	}
	switch {
	case key.Matches(msg, Keys.Back):
		return m, func() tea.Msg {
			return switchScreenMsg{screen: screenAccount, accountKey: m.accountKey}
		}
	case key.Matches(msg, Keys.Enter):
		token := m.tokenInput.Value()
		if token == "" {
			m.errMsg = "Please paste a token."
			return m, nil
		}
		m.tokenBusy = true
		m.errMsg = ""
		return m, storeDiscoveryTokenCmd(m.accountKey, token)
	}
	// Delegate to textinput for character input.
	var cmd tea.Cmd
	m.tokenInput, cmd = m.tokenInput.Update(msg)
	return m, cmd
}

func (m discoveryModel) View() string {
	if m.view == discViewTokenInput {
		return m.viewTokenInput()
	}

	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Discover Repos: "+m.accountKey) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	if m.loading {
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Fetching repos...") + "\n")
		return b.String()
	}

	if m.errMsg != "" && len(m.repos) == 0 {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg) + "\n\n")
		b.WriteString(renderHints(m.theme, "ESC back"))
		return b.String()
	}

	// Filter bar.
	if m.filtering {
		b.WriteString("  / " + m.filterInput.View() + "\n")
	} else if m.filterInput.Value() != "" {
		b.WriteString("  " + m.theme.TextMuted.Render("filter: "+m.filterInput.Value()) + "\n")
	}

	vis := m.visibleIndices()
	ch := m.contentHeight()

	// Render visible slice with scroll offset.
	end := m.scrollOffset + ch
	if end > len(vis) {
		end = len(vis)
	}
	for vi := m.scrollOffset; vi < end; vi++ {
		realIdx := vis[vi]
		r := m.repos[realIdx]

		prefix := "  [ ] "
		if m.configured[r.FullName] {
			prefix = "  [=] "
		} else if m.selected[realIdx] {
			prefix = "  [x] "
		}

		label := r.FullName
		if r.Fork {
			label += " (fork)"
		}
		if r.Archived {
			label += " (archived)"
		}

		line := prefix + label
		if vi == m.cursor {
			line = m.theme.SelectedRow.Render(line)
		} else if m.configured[r.FullName] {
			line = m.theme.TextMuted.Render(line)
		} else {
			line = m.theme.NormalRow.Render(line)
		}
		b.WriteString(line + "\n")
	}

	// Scroll indicator.
	if len(vis) > ch {
		b.WriteString(m.theme.TextMuted.Render(
			fmt.Sprintf("  (%d-%d of %d)", m.scrollOffset+1, end, len(vis))) + "\n")
	}

	selCount := len(m.selected)
	newCount := 0
	for _, r := range m.repos {
		if !m.configured[r.FullName] {
			newCount++
		}
	}

	b.WriteString("\n")
	b.WriteString(m.theme.TextMuted.Render(
		fmt.Sprintf("  %d/%d new repos, %d selected", newCount, len(m.repos), selCount)))

	if m.addedCount > 0 {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(fmt.Sprintf("Added %d repos to config.", m.addedCount)))
	}

	if m.errMsg != "" && len(m.repos) > 0 {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg))
	}

	b.WriteString("\n\n")
	if m.filtering {
		b.WriteString(renderHints(m.theme, "enter accept filter", "ESC clear filter"))
	} else {
		hints := []string{"space toggle", "A all", "/ filter", "enter add selected", "ESC back"}
		b.WriteString(renderHintsFit(m.theme, m.width, hints...))
	}

	return b.String()
}

func (m discoveryModel) viewTokenInput() string {
	var b strings.Builder

	b.WriteString(m.theme.Title.Render("Discovery Token: "+m.accountKey) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	b.WriteString(m.theme.TextMuted.Render(
		"  SSH accounts need a Personal Access Token (PAT) for API access\n"+
			"  (repo discovery, creation, and mirroring).") + "\n\n")

	if m.tokenScopes != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", m.theme.TextMuted.Render("Required scopes:"), m.theme.Text.Render(m.tokenScopes)))
	}
	if m.tokenURL != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", m.theme.TextMuted.Render("Create token at:"), m.theme.Brand.Render(m.tokenURL)))
	}
	b.WriteString("\n")

	b.WriteString("  " + m.tokenInput.View() + "\n\n")

	if m.tokenBusy {
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Storing token and discovering repos...") + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg) + "\n")
	}

	b.WriteString("\n")
	if !m.tokenBusy {
		b.WriteString(renderHints(m.theme, "enter store & discover", "ESC back"))
	} else {
		b.WriteString(renderHints(m.theme, "ESC back"))
	}

	return b.String()
}
