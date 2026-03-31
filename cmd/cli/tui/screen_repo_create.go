package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// validRepoName allows alphanumeric, hyphen, dot, underscore; no leading dot or hyphen.
var validRepoName = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]*$`)

const (
	createFieldOwner = iota
	createFieldName
	createFieldDesc
	createFieldPrivate
	createFieldCloneAfter
	createFieldCount
)

// --- Messages ---

type repoCreateOrgsMsg struct {
	orgs []string
	err  error
}

type repoCreatedMsg struct {
	owner, repoName string
	cloneAfter      bool
	err             error
}

// --- Model ---

type repoCreateModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int

	// Account selection (when multiple accounts exist).
	accountKeys   []string
	accountSelect selectField
	accountKey    string // resolved account key
	accountPicked bool   // true once we know which account to use

	// Form fields (populated after account is picked + orgs loaded).
	form     formModel
	formReady bool

	// Async state.
	loadingOrgs bool
	creating    bool
	resultMsg   string
	errMsg      string
}

func newRepoCreateModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int, accountKey string) repoCreateModel {
	m := repoCreateModel{
		cfg:     cfg,
		cfgPath: cfgPath,
		theme:   theme,
		width:   w,
		height:  h,
	}

	// Collect account keys.
	keys := make([]string, 0, len(cfg.Accounts))
	for k := range cfg.Accounts {
		keys = append(keys, k)
	}

	if accountKey != "" {
		// Pre-selected account.
		m.accountKey = accountKey
		m.accountPicked = true
	} else if len(keys) == 1 {
		m.accountKey = keys[0]
		m.accountPicked = true
	} else {
		// Multiple accounts — show selector.
		m.accountKeys = keys
		m.accountSelect = newSelectField("Account:", keys)
	}

	return m
}

func (m repoCreateModel) Init() tea.Cmd {
	if m.accountPicked {
		m.loadingOrgs = true
		return listOrgsCmd(m.cfg, m.accountKey)
	}
	return nil
}

func (m repoCreateModel) Update(msg tea.Msg) (repoCreateModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, Keys.Back) {
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }
		}

		// Account selection phase.
		if !m.accountPicked {
			switch {
			case msg.String() == "left" || msg.String() == "h":
				m.accountSelect.Left()
				return m, nil
			case msg.String() == "right" || msg.String() == "l":
				m.accountSelect.Right()
				return m, nil
			case key.Matches(msg, Keys.Enter):
				m.accountKey = m.accountSelect.Value()
				m.accountPicked = true
				m.loadingOrgs = true
				return m, listOrgsCmd(m.cfg, m.accountKey)
			}
			return m, nil
		}

	case repoCreateOrgsMsg:
		m.loadingOrgs = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			// Still show the form with just the username as owner.
			acct := m.cfg.Accounts[m.accountKey]
			m.initForm([]string{acct.Username})
		} else {
			m.initForm(msg.orgs)
		}
		return m, m.form.Init()

	case repoCreatedMsg:
		m.creating = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		repoDisplay := msg.owner + "/" + msg.repoName
		if msg.cloneAfter {
			m.resultMsg = fmt.Sprintf("Created %s and added to config. Clone will happen on next Pull All.", repoDisplay)
		} else {
			m.resultMsg = fmt.Sprintf("Created %s.", repoDisplay)
		}
		return m, nil
	}

	// Form phase.
	if m.formReady && !m.creating && m.resultMsg == "" {
		submitted, cmd := m.form.Update(msg)
		if submitted {
			m.creating = true
			m.errMsg = ""
			return m, m.createRepoCmd()
		}
		return m, cmd
	}

	return m, nil
}

func (m *repoCreateModel) initForm(owners []string) {
	fields := []formField{
		newSelectFormField("Owner:        ", owners),
		newTextField("Name:", "my-repo", 128),
		newTextField("Description:", "(optional)", 256),
		newSelectFormField("Private:      ", []string{"yes", "no"}),
		newSelectFormField("Clone after:  ", []string{"yes", "no"}),
	}

	// Validation for repo name.
	fields[createFieldName].ValidateFn = func(v string) string {
		if v == "" {
			return "repository name is required"
		}
		if !validRepoName.MatchString(v) {
			return "name must be alphanumeric (hyphens/dots/underscores allowed, no leading dot/hyphen)"
		}
		return ""
	}

	m.form = newFormModel("Create Repository", fields, m.theme)
	m.formReady = true
}

func (m repoCreateModel) createRepoCmd() tea.Cmd {
	accountKey := m.accountKey
	owner := m.form.Fields[createFieldOwner].Value()
	repoName := m.form.Fields[createFieldName].Value()
	description := m.form.Fields[createFieldDesc].Value()
	private := m.form.Fields[createFieldPrivate].Value() == "yes"
	cloneAfter := m.form.Fields[createFieldCloneAfter].Value() == "yes"
	cfg := m.cfg
	cfgPath := m.cfgPath

	return func() tea.Msg {
		acct, ok := cfg.Accounts[accountKey]
		if !ok {
			return repoCreatedMsg{owner: owner, repoName: repoName, err: fmt.Errorf("account %q not found", accountKey)}
		}

		token, _, err := credential.ResolveAPIToken(acct, accountKey)
		if err != nil {
			return repoCreatedMsg{owner: owner, repoName: repoName, err: fmt.Errorf("resolving credentials: %w", err)}
		}

		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			return repoCreatedMsg{owner: owner, repoName: repoName, err: err}
		}

		rc, ok := prov.(provider.RepoCreator)
		if !ok {
			return repoCreatedMsg{owner: owner, repoName: repoName, err: fmt.Errorf("provider %q does not support repo creation", acct.Provider)}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// If owner matches the username, create under personal namespace (empty owner).
		apiOwner := owner
		if apiOwner == acct.Username {
			apiOwner = ""
		}

		if err := rc.CreateRepo(ctx, acct.URL, token, acct.Username, apiOwner, repoName, description, private); err != nil {
			return repoCreatedMsg{owner: owner, repoName: repoName, err: err}
		}

		if cloneAfter {
			repoKey := owner + "/" + repoName

			sourceKey := accountKey
			src, srcOK := cfg.Sources[sourceKey]
			if !srcOK {
				src = config.Source{
					Account: accountKey,
					Repos:   make(map[string]config.Repo),
				}
			}
			src.Repos[repoKey] = config.Repo{}
			cfg.Sources[sourceKey] = src

			if err := config.Save(cfg, cfgPath); err != nil {
				return repoCreatedMsg{owner: owner, repoName: repoName, cloneAfter: true, err: fmt.Errorf("repo created but failed to save config: %w", err)}
			}
		}

		return repoCreatedMsg{owner: owner, repoName: repoName, cloneAfter: cloneAfter}
	}
}

func listOrgsCmd(cfg *config.Config, accountKey string) tea.Cmd {
	return func() tea.Msg {
		acct, ok := cfg.Accounts[accountKey]
		if !ok {
			return repoCreateOrgsMsg{err: fmt.Errorf("account %q not found", accountKey)}
		}

		token, _, err := credential.ResolveAPIToken(acct, accountKey)
		if err != nil {
			return repoCreateOrgsMsg{err: fmt.Errorf("resolving credentials: %w", err)}
		}

		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			return repoCreateOrgsMsg{err: err}
		}

		// Start with personal username.
		result := []string{acct.Username}

		// List orgs if supported.
		if ol, ok := prov.(provider.OrgLister); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			orgs, err := ol.ListUserOrgs(ctx, acct.URL, token, acct.Username)
			if err == nil {
				result = append(result, orgs...)
			}
		}

		return repoCreateOrgsMsg{orgs: result}
	}
}

func (m repoCreateModel) View() string {
	var b strings.Builder

	// Account selection phase.
	if !m.accountPicked {
		b.WriteString(m.theme.Title.Render("Create Repository") + "\n")
		b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", 40)) + "\n\n")
		b.WriteString("  " + m.accountSelect.View(true, m.theme) + "\n\n")
		b.WriteString(renderHints(m.theme, "←→ select", "enter confirm", "ESC cancel"))
		return b.String()
	}

	// Loading orgs.
	if m.loadingOrgs {
		b.WriteString(m.theme.Title.Render("Create Repository") + "\n")
		b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", 40)) + "\n\n")
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Loading owners for "+m.accountKey+"...") + "\n")
		return b.String()
	}

	// Creating.
	if m.creating {
		b.WriteString(m.theme.Title.Render("Create Repository") + "\n")
		b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", 40)) + "\n\n")
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Creating repository...") + "\n")
		return b.String()
	}

	// Form.
	if m.formReady {
		b.WriteString(m.form.View())
	}

	// Result.
	if m.resultMsg != "" {
		b.WriteString("\n\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(m.resultMsg))
		b.WriteString("\n\n" + renderHints(m.theme, "ESC back to dashboard"))
	}

	// Error (non-fatal, form stays visible).
	if m.errMsg != "" && m.resultMsg == "" {
		b.WriteString("\n\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.errMsg))
	}

	return b.String()
}
