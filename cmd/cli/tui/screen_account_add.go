package tui

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var validAccountKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

// Provider list and default URLs — mirrors GUI's providerURLs.
var (
	providerOptions = []string{"github", "gitlab", "gitea", "forgejo", "bitbucket"}
	providerURLs    = map[string]string{
		"github":    "https://github.com",
		"gitlab":    "https://gitlab.com",
		"bitbucket": "https://bitbucket.org",
	}
	credTypeOptions = []string{"gcm", "ssh", "token"}
)

const (
	addFieldKey = iota
	addFieldProvider
	addFieldURL
	addFieldUsername
	addFieldName
	addFieldEmail
	addFieldCredType
	addFieldCount
)

type accountAddModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int
	form          formModel
	saving        bool
	resultMsg     string
	errMsg        string

	// Track last provider to auto-fill URL on change.
	lastProvider string
}

func newAccountAddModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int) accountAddModel {
	fields := []formField{
		newTextField("Account key:", "my-github", 64),
		newSelectFormField("Provider:     ", providerOptions),
		newTextField("URL:", "https://github.com", 256),
		newTextField("Username:", "", 128),
		newTextField("Name:", "(for git commits)", 128),
		newTextField("Email:", "(for git commits)", 256),
		newSelectFormField("Credential:   ", credTypeOptions),
	}

	// Validation for account key.
	fields[addFieldKey].ValidateFn = func(v string) string {
		if v == "" {
			return "account key is required"
		}
		if !validAccountKey.MatchString(v) {
			return "key must be alphanumeric (hyphens allowed, no leading hyphen)"
		}
		if _, exists := cfg.Accounts[v]; exists {
			return fmt.Sprintf("account %q already exists", v)
		}
		return ""
	}

	// Validation for URL.
	fields[addFieldURL].ValidateFn = func(v string) string {
		if v == "" {
			return "URL is required"
		}
		return ""
	}

	// Validation for username.
	fields[addFieldUsername].ValidateFn = func(v string) string {
		if v == "" {
			return "username is required"
		}
		return ""
	}

	// Set default URL for first provider.
	if u, ok := providerURLs["github"]; ok {
		fields[addFieldURL].TextInput.SetValue(u)
	}

	m := accountAddModel{
		cfg:          cfg,
		cfgPath:      cfgPath,
		theme:        theme,
		width:        w,
		height:       h,
		form:         newFormModel("Add Account", fields, theme),
		lastProvider: "github",
	}
	return m
}

func (m accountAddModel) Init() tea.Cmd {
	return m.form.Init()
}

func (m accountAddModel) Update(msg tea.Msg) (accountAddModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, Keys.Back) {
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }
		}

	case accountAddedMsg:
		m.saving = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.resultMsg = fmt.Sprintf("Account %q created.", msg.key)
			// Navigate to credential setup for the new account.
			return m, func() tea.Msg {
				return switchScreenMsg{screen: screenCredential, accountKey: msg.key}
			}
		}
		return m, nil
	}

	// Auto-fill URL when provider changes.
	currentProvider := m.form.Fields[addFieldProvider].Value()
	if currentProvider != m.lastProvider {
		m.lastProvider = currentProvider
		if u, ok := providerURLs[currentProvider]; ok {
			m.form.Fields[addFieldURL].TextInput.SetValue(u)
		} else {
			m.form.Fields[addFieldURL].TextInput.SetValue("")
		}
	}

	submitted, cmd := m.form.Update(msg)
	if submitted {
		return m, m.saveAccount()
	}
	return m, cmd
}

func (m accountAddModel) saveAccount() tea.Cmd {
	return func() tea.Msg {
		accountKey := m.form.Fields[addFieldKey].Value()
		prov := m.form.Fields[addFieldProvider].Value()
		rawURL := m.form.Fields[addFieldURL].Value()
		username := m.form.Fields[addFieldUsername].Value()
		name := m.form.Fields[addFieldName].Value()
		email := m.form.Fields[addFieldEmail].Value()
		credType := m.form.Fields[addFieldCredType].Value()

		acct := config.Account{
			Provider:              prov,
			URL:                   rawURL,
			Username:              username,
			Name:                  name,
			Email:                 email,
			DefaultCredentialType: credType,
		}

		// Populate credential sub-objects (mirrors GUI app.go:841-857).
		switch credType {
		case "gcm":
			acct.GCM = &config.GCMConfig{
				Provider:    inferGCMProvider(prov),
				UseHTTPPath: false,
			}
		case "ssh":
			hostname := hostnameFromURL(rawURL)
			acct.SSH = &config.SSHConfig{
				Host:     credential.SSHHostAlias(accountKey),
				Hostname: hostname,
				KeyType:  "ed25519",
			}
		}

		if err := m.cfg.AddAccount(accountKey, acct); err != nil {
			return accountAddedMsg{key: accountKey, err: err}
		}

		// Create matching source.
		src := config.Source{
			Account: accountKey,
			Repos:   make(map[string]config.Repo),
		}
		if err := m.cfg.AddSource(accountKey, src); err != nil {
			_ = m.cfg.DeleteAccount(accountKey)
			return accountAddedMsg{key: accountKey, err: err}
		}

		if err := config.Save(m.cfg, m.cfgPath); err != nil {
			return accountAddedMsg{key: accountKey, err: err}
		}
		return accountAddedMsg{key: accountKey}
	}
}

func (m accountAddModel) View() string {
	var b strings.Builder

	b.WriteString(m.form.View())

	if m.resultMsg != "" {
		b.WriteString("\n\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(m.resultMsg))
	}
	if m.errMsg != "" {
		b.WriteString("\n\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.errMsg))
	}

	return b.String()
}

// ─── Helpers (replicated from GUI app.go) ─────────────────────────────────

func inferGCMProvider(prov string) string {
	switch prov {
	case "github":
		return "github"
	case "gitlab":
		return "gitlab"
	case "bitbucket":
		return "bitbucket"
	default:
		return "generic"
	}
}

func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		for _, prefix := range []string{"https://", "http://"} {
			if strings.HasPrefix(rawURL, prefix) {
				return strings.TrimPrefix(rawURL, prefix)
			}
		}
		return rawURL
	}
	return u.Hostname()
}
