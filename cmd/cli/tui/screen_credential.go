package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/atotto/clipboard"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// credView tracks which sub-view is active inside the credential screen.
type credView int

const (
	credViewMenu  credView = iota // show status + action menu
	credViewSetup                 // token input or SSH setup
	credViewType                  // select new credential type
)

type credentialModel struct {
	cfg           *config.Config
	cfgPath       string
	credMgr       *credential.StatusManager
	accountKey    string
	theme         styles.Theme
	width, height int

	view       credView
	tokenInput textinput.Model
	typeForm   formModel

	busy            bool
	resultOK        bool
	resultMsg       string
	errMsg          string
	guide           string
	infoMsgs        []string // messages from operations (delete, change, etc.)
	confirmDelete    bool
	confirmDeletePAT bool
	confirmRegen     bool
	forceTokenSetup   bool     // skip to PAT input (for mirror token fix or account PAT)
	forceReturnScreen screenID // where to go back from forceTokenSetup (default: dashboard)
	sshPendingKey   bool   // key generated, waiting for user to add pubkey to provider
	sshPubKey       string // public key content
	sshPubKeyURL    string // provider URL to add key
}

func newCredentialModel(cfg *config.Config, cfgPath string, credMgr *credential.StatusManager, accountKey string, theme styles.Theme, w, h int) credentialModel {
	ti := textinput.New()
	ti.Placeholder = "Paste your token here..."
	ti.EchoMode = textinput.EchoNormal
	ti.CharLimit = 256
	ti.Width = 60

	acct := cfg.Accounts[accountKey]
	guide := provider.TokenSetupGuide(acct.Provider, acct.URL, accountKey)

	m := credentialModel{
		cfg:        cfg,
		cfgPath:    cfgPath,
		credMgr:    credMgr,
		accountKey: accountKey,
		theme:      theme,
		width:      w,
		height:     h,
		tokenInput: ti,
		guide:      guide,
	}

	// If no credential configured, go straight to type selection.
	if acct.DefaultCredentialType == "" {
		m.view = credViewType
		m.typeForm = m.buildTypeForm()
	} else {
		m.view = credViewMenu
	}

	return m
}

func (m credentialModel) Init() tea.Cmd {
	acct := m.cfg.Accounts[m.accountKey]
	if acct.DefaultCredentialType == "" {
		return m.typeForm.Init()
	}
	return nil
}

// ── Async commands ──────────────────────────────────────────────

func storeTokenCmd(accountKey, token string, acct config.Account) tea.Cmd {
	return func() tea.Msg {
		if err := credential.StoreToken(accountKey, token); err != nil {
			return credSetupDoneMsg{accountKey: accountKey, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
			return credSetupDoneMsg{accountKey: accountKey, err: fmt.Errorf("token stored but API check failed: %w", err)}
		}
		return credSetupDoneMsg{accountKey: accountKey}
	}
}

func sshSetupCmd(cfg *config.Config, acct config.Account, accountKey string) tea.Cmd {
	return func() tea.Msg {
		sshFolder := credential.SSHFolder(cfg)
		hostAlias := credential.SSHHostAlias(accountKey)
		keyPath := credential.SSHKeyPath(sshFolder, accountKey)

		// Check or generate key.
		if _, err := credential.FindSSHKey(sshFolder, hostAlias, "ed25519"); err != nil {
			if _, genErr := credential.GenerateSSHKey(sshFolder, accountKey, "ed25519"); genErr != nil {
				return credSetupDoneMsg{accountKey: accountKey, err: genErr}
			}
		}

		pubKey := ""
		if pk, err := credential.ReadPublicKey(keyPath); err == nil {
			pubKey = pk
		}

		// Ensure SSH config entry exists.
		hostname := hostnameFromURL(acct.URL)
		if acct.SSH != nil && acct.SSH.Hostname != "" {
			hostname = acct.SSH.Hostname
		}
		_ = credential.WriteSSHConfigEntry(sshFolder, credential.SSHConfigEntryOpts{
			Host:     hostAlias,
			Hostname: hostname,
			KeyFile:  keyPath,
			Username: acct.Username,
			Name:     acct.Name,
			Email:    acct.Email,
			URL:      acct.URL,
		})

		// Test connection.
		pubKeyURL := credential.SSHPublicKeyURL(acct.Provider, acct.URL)
		if _, sshErr := credential.TestSSHConnection(sshFolder, hostAlias); sshErr != nil {
			// Connection failed — expected if user hasn't added the key yet.
			return credSetupDoneMsg{
				accountKey:    accountKey,
				sshPendingKey: true,
				pubKey:        pubKey,
				pubKeyURL:     pubKeyURL,
			}
		}

		return credSetupDoneMsg{accountKey: accountKey}
	}
}

func gcmSetupCmd(cfg *config.Config, cfgPath string, credMgr *credential.StatusManager, accountKey string) tea.Cmd {
	return func() tea.Msg {
		acct := cfg.Accounts[accountKey]
		host := hostnameFromURL(acct.URL)

		// Ensure global git config has GCM helper/store settings.
		credential.EnsureGlobalGCMConfig(cfg.Global)

		// Check if GCM already has a stored credential.
		_, _, err := credential.ResolveGCMToken(acct.URL, acct.Username)
		if err == nil {
			// Already authenticated — just verify API access.
			return gcmCheckAPI(cfg, cfgPath, credMgr, acct, accountKey, acct.Username)
		}

		// No credential found — trigger interactive git credential fill.
		// GCM opens the browser for OAuth; we capture the result.
		input := fmt.Sprintf("protocol=https\nhost=%s\nusername=%s\n\n", host, acct.Username)
		homeDir, _ := os.UserHomeDir()
		fillCmd := exec.Command(git.GitBin(), "credential", "fill")
		fillCmd.Dir = homeDir
		fillCmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
		fillCmd.Stdin = strings.NewReader(input)
		// Capture stderr so GCM info messages (e.g. "please complete
		// authentication in your browser...") don't leak onto the TUI.
		var stderrBuf strings.Builder
		fillCmd.Stderr = &stderrBuf
		out, err := fillCmd.Output()
		if err != nil {
			return credSetupDoneMsg{
				accountKey: accountKey,
				err:        fmt.Errorf("GCM authentication failed for %s@%s (browser cancelled or timed out?)", acct.Username, host),
			}
		}

		// Parse fill output for password and actual username.
		gotPassword := false
		realUsername := acct.Username
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "password=") {
				if strings.TrimPrefix(line, "password=") != "" {
					gotPassword = true
				}
			}
			if strings.HasPrefix(line, "username=") {
				if u := strings.TrimPrefix(line, "username="); u != "" {
					realUsername = u
				}
			}
		}
		if !gotPassword {
			return credSetupDoneMsg{
				accountKey: accountKey,
				err:        fmt.Errorf("GCM returned no credential for %s@%s", acct.Username, host),
			}
		}

		// Approve so git stores it persistently.
		approveCmd := exec.Command(git.GitBin(), "credential", "approve")
		approveCmd.Dir = homeDir
		approveCmd.Env = git.Environ()
		approveCmd.Stdin = strings.NewReader(string(out))
		_ = approveCmd.Run()

		// Verify it was stored.
		if _, _, err := credential.ResolveGCMToken(acct.URL, realUsername); err != nil {
			return credSetupDoneMsg{
				accountKey: accountKey,
				err:        fmt.Errorf("GCM authentication completed but credential was not stored for %s@%s", realUsername, host),
			}
		}

		// Handle username casing mismatch (e.g. GitHub returns different casing).
		if realUsername != acct.Username {
			acct.Username = realUsername
			_ = cfg.UpdateAccount(accountKey, acct)
			_ = config.Save(cfg, cfgPath)
		}

		// Reconfigure existing clones.
		reconfigureClones(cfg, accountKey)

		return gcmCheckAPI(cfg, cfgPath, credMgr, acct, accountKey, realUsername)
	}
}

// gcmCheckAPI tests API access after GCM auth and returns the appropriate message.
func gcmCheckAPI(cfg *config.Config, cfgPath string, credMgr *credential.StatusManager, acct config.Account, accountKey, username string) credSetupDoneMsg {
	token, _, apiErr := credential.ResolveAPIToken(acct, accountKey)
	if apiErr != nil {
		return credSetupDoneMsg{accountKey: accountKey, needsPAT: true, gcmUsername: username}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, username); err != nil {
		return credSetupDoneMsg{accountKey: accountKey, needsPAT: true, gcmUsername: username}
	}
	// Invalidate cached status so dashboard refreshes.
	credMgr.Invalidate(accountKey)
	return credSetupDoneMsg{accountKey: accountKey, gcmUsername: username}
}

// ── Update ──────────────────────────────────────────────────────

func (m credentialModel) Update(msg tea.Msg) (credentialModel, tea.Cmd) {
	acct := m.cfg.Accounts[m.accountKey]

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}

		switch {
		case key.Matches(msg, Keys.Back):
			if m.confirmDelete || m.confirmDeletePAT || m.confirmRegen {
				m.confirmDelete = false
				m.confirmDeletePAT = false
				m.confirmRegen = false
				return m, nil
			}
			if m.forceTokenSetup {
				// Came from mirror fix — go straight back.
				return m, m.goBack()
			}
			if m.view == credViewSetup || m.view == credViewType {
				// If credential exists, go back to menu; otherwise back to account.
				if acct.DefaultCredentialType != "" {
					m.view = credViewMenu
					m.errMsg = ""
					return m, nil
				}
				return m, m.goBack()
			}
			return m, m.goBack()

		// ── Menu view keys ──

		case msg.String() == "n" && (m.confirmDelete || m.confirmDeletePAT || m.confirmRegen):
			m.confirmDelete = false
			m.confirmDeletePAT = false
			m.confirmRegen = false
			return m, nil

		case key.Matches(msg, Keys.Enter) && m.view == credViewMenu:
			// After an operation completed, only ESC is allowed.
			if m.resultOK || len(m.infoMsgs) > 0 {
				return m, nil
			}
			if m.confirmDelete {
				m.confirmDelete = false
				return m, m.doDeleteCredential()
			}
			if m.confirmDeletePAT {
				m.confirmDeletePAT = false
				return m, m.doDeletePAT()
			}
			if m.confirmRegen {
				m.confirmRegen = false
				return m, m.doRegenerateSSH()
			}
			if acct.DefaultCredentialType == "gcm" {
				m.view = credViewSetup
				m.errMsg = ""
				m.resultOK = false
				if credential.CanOpenBrowser() {
					m.busy = true
					return m, gcmSetupCmd(m.cfg, m.cfgPath, m.credMgr, m.accountKey)
				}
				// SSH/headless — show info message only.
				return m, nil
			}
			// Enter on menu → go to setup.
			m.view = credViewSetup
			m.errMsg = ""
			m.resultOK = false
			m.sshPendingKey = false
			if acct.DefaultCredentialType == "token" {
				m.tokenInput.Focus()
				return m, textinput.Blink
			}
			if acct.DefaultCredentialType == "ssh" {
				// If key already exists, show the copy+test flow, don't regenerate.
				sshFolder := credential.SSHFolder(m.cfg)
				keyPath := credential.SSHKeyPath(sshFolder, m.accountKey)
				if _, err := os.Stat(keyPath); err == nil {
					if pubKey, err := credential.ReadPublicKey(keyPath); err == nil {
						m.sshPendingKey = true
						m.sshPubKey = pubKey
						m.sshPubKeyURL = credential.SSHPublicKeyURL(acct.Provider, acct.URL)
						_ = clipboard.WriteAll(pubKey)
						return m, nil
					}
				}
			}
			return m, nil

		case msg.String() == "T" && m.view == credViewMenu && !m.resultOK && len(m.infoMsgs) == 0:
			m.view = credViewType
			m.typeForm = m.buildTypeForm()
			m.errMsg = ""
			m.infoMsgs = nil
			return m, m.typeForm.Init()

		case msg.String() == "P" && m.view == credViewMenu && !m.confirmDeletePAT && !m.resultOK && len(m.infoMsgs) == 0:
			ct := acct.DefaultCredentialType
			if ct == "ssh" || ct == "gcm" {
				credResult := m.credMgr.Get(m.accountKey)
				if credResult.PAT == credential.StatusOK || credResult.PAT == credential.StatusWarning || credResult.PAT == credential.StatusError {
					m.confirmDeletePAT = true
					return m, nil
				}
			}

		case msg.String() == "X" && m.view == credViewMenu && !m.confirmDelete && !m.resultOK && len(m.infoMsgs) == 0:
			m.confirmDelete = true
			return m, nil

		case msg.String() == "G" && m.view == credViewMenu && acct.DefaultCredentialType == "ssh" && !m.confirmRegen && !m.resultOK && len(m.infoMsgs) == 0:
			m.confirmRegen = true
			return m, nil

		// ── Setup view keys ──

		case msg.String() == "c" && m.view == credViewSetup && m.sshPendingKey && m.sshPubKey != "":
			_ = clipboard.WriteAll(m.sshPubKey)
			m.infoMsgs = []string{"Public key copied to clipboard."}
			return m, nil

		case key.Matches(msg, Keys.Enter) && m.view == credViewSetup:
			// After successful setup, only ESC is allowed.
			if m.resultOK {
				return m, nil
			}
			// Force token input mode (e.g., from mirror "fix credentials").
			if m.forceTokenSetup {
				token := m.tokenInput.Value()
				if token == "" {
					m.errMsg = "Please paste an API token (PAT)."
					return m, nil
				}
				m.busy = true
				m.errMsg = ""
				return m, storeTokenCmd(m.accountKey, token, acct)
			}
			switch acct.DefaultCredentialType {
			case "token":
				token := m.tokenInput.Value()
				if token == "" {
					m.errMsg = "Please paste a token."
					return m, nil
				}
				m.busy = true
				m.errMsg = ""
				return m, storeTokenCmd(m.accountKey, token, acct)
			case "gcm":
				if credential.CanOpenBrowser() {
					m.busy = true
					m.errMsg = ""
					return m, gcmSetupCmd(m.cfg, m.cfgPath, m.credMgr, m.accountKey)
				}
				return m, nil
			case "ssh":
				if m.sshPendingKey {
					// Retry connection test only.
					m.busy = true
					m.errMsg = ""
					hostAlias := credential.SSHHostAlias(m.accountKey)
					sshFolder := credential.SSHFolder(m.cfg)
					return m, func() tea.Msg {
						if _, err := credential.TestSSHConnection(sshFolder, hostAlias); err != nil {
							return credSetupDoneMsg{
								accountKey:    m.accountKey,
								sshPendingKey: true,
								pubKey:        m.sshPubKey,
								pubKeyURL:     m.sshPubKeyURL,
								err:           fmt.Errorf("Connection still failing. Add the public key to your provider first."),
							}
						}
						return credSetupDoneMsg{accountKey: m.accountKey}
					}
				}
				m.busy = true
				m.errMsg = ""
				return m, sshSetupCmd(m.cfg, acct, m.accountKey)
			}
			return m, nil

		// ── Type selection form ──
		// (handled below in form delegation)
		}

	case credSetupDoneMsg:
		m.busy = false
		// GCM writes info messages directly to /dev/tty, bypassing stderr
		// capture. Force a full screen repaint to clear any stale text.
		gcmClear := acct.DefaultCredentialType == "gcm"
		if msg.sshPendingKey {
			m.sshPendingKey = true
			if msg.pubKey != "" {
				m.sshPubKey = msg.pubKey
				m.sshPubKeyURL = msg.pubKeyURL
				// Auto-copy public key to clipboard on first generation.
				_ = clipboard.WriteAll(msg.pubKey)
			}
			if msg.err != nil {
				m.errMsg = msg.err.Error()
			} else {
				m.errMsg = ""
			}
			return m, nil
		}
		if msg.needsPAT {
			// GCM auth succeeded but API needs a separate PAT.
			acct = m.cfg.Accounts[m.accountKey]
			m.forceTokenSetup = true
			m.forceReturnScreen = screenAccount
			m.guide = provider.TokenSetupGuide(acct.Provider, acct.URL, m.accountKey)
			m.tokenInput.SetValue("")
			m.tokenInput.Focus()
			m.view = credViewSetup
			m.resultOK = false
			m.infoMsgs = []string{"GCM credential stored. API access requires a PAT."}
			return m, tea.Batch(textinput.Blink, tea.ClearScreen)
		}
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.resultOK = false
		} else {
			m.resultOK = true
			m.resultMsg = "Credential configured successfully."
			m.errMsg = ""
			m.sshPendingKey = false
			m.tokenInput.Blur()
		}
		if gcmClear {
			// Invalidate cached status and trigger re-check so the menu
			// shows fresh results when the user presses ESC.
			m.credMgr.Invalidate(m.accountKey)
			return m, tea.Batch(tea.ClearScreen, credCheckCmd(m.credMgr, m.cfg, m.accountKey))
		}
		return m, nil

	case credChangedMsg:
		m.busy = false
		// Invalidate cached credential status — old results are stale after type change.
		m.credMgr.Invalidate(m.accountKey)
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.view = credViewMenu
		} else {
			m.infoMsgs = msg.msgs
			m.errMsg = ""
			m.resultOK = false
			newAcct := m.cfg.Accounts[m.accountKey]
			switch newAcct.DefaultCredentialType {
			case "token":
				m.view = credViewSetup
				m.infoMsgs = nil
				m.guide = provider.TokenSetupGuide(newAcct.Provider, newAcct.URL, m.accountKey)
				m.tokenInput.SetValue("")
				m.tokenInput.Focus()
				return m, textinput.Blink
			case "ssh":
				m.view = credViewSetup
				m.infoMsgs = nil
			default:
				// GCM: if on desktop, start browser auth immediately.
				if credential.CanOpenBrowser() {
					m.view = credViewSetup
					m.infoMsgs = nil
					m.busy = true
					return m, gcmSetupCmd(m.cfg, m.cfgPath, m.credMgr, m.accountKey)
				}
				// SSH/headless — stay on menu with info.
				m.view = credViewMenu
				m.resultOK = true
				m.resultMsg = "GCM set. Browser auth requires a desktop session."
				return m, credCheckCmd(m.credMgr, m.cfg, m.accountKey)
			}
		}
		return m, nil

	case credStatusUpdatedMsg:
		// Credential check completed — triggers re-render with fresh status.
		return m, nil

	case credDeletedMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.infoMsgs = msg.msgs
			// Credential deleted — go to type selection.
			m.view = credViewType
			m.typeForm = m.buildTypeForm()
			return m, m.typeForm.Init()
		}
		return m, nil

	case sshRegenMsg:
		m.busy = false
		m.infoMsgs = msg.msgs
		// After regen, go to setup to generate new keys.
		m.view = credViewSetup
		m.errMsg = ""
		m.resultOK = false
		return m, nil
	}

	// Delegate to type form when in type selection view.
	if m.view == credViewType {
		submitted, cmd := m.typeForm.Update(msg)
		if submitted {
			return m, m.doChangeType()
		}
		return m, cmd
	}

	// Update text input when in setup view with token (not after success).
	if m.view == credViewSetup && !m.resultOK {
		var cmd tea.Cmd
		m.tokenInput, cmd = m.tokenInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

// ── Action commands ─────────────────────────────────────────────

func (m credentialModel) goBack() tea.Cmd {
	if m.forceTokenSetup {
		if m.forceReturnScreen == screenAccount {
			accountKey := m.accountKey
			return func() tea.Msg {
				return switchScreenMsg{screen: screenAccount, accountKey: accountKey}
			}
		}
		// Default: came from mirror fix — go back to dashboard (Mirrors tab).
		return func() tea.Msg {
			return switchScreenMsg{screen: screenDashboard, forceTokenSetup: true}
		}
	}
	accountKey := m.accountKey
	return func() tea.Msg {
		return switchScreenMsg{screen: screenAccount, accountKey: accountKey}
	}
}

func (m credentialModel) buildTypeForm() formModel {
	acct := m.cfg.Accounts[m.accountKey]
	options := []string{"gcm", "ssh", "token"}
	fields := []formField{
		newSelectFormField("Credential type:", options),
	}
	if acct.DefaultCredentialType != "" {
		fields[0].Select.SetValue(acct.DefaultCredentialType)
		fields[0].ValidateFn = func(v string) string {
			if v == acct.DefaultCredentialType {
				return "already using " + v
			}
			return ""
		}
	}
	title := "Select Credential Type: " + m.accountKey
	if acct.DefaultCredentialType != "" {
		title = "Change Credential Type: " + m.accountKey
	}
	return newFormModel(title, fields, m.theme)
}

func (m credentialModel) doChangeType() tea.Cmd {
	newType := m.typeForm.Fields[0].Value()
	accountKey := m.accountKey
	cfg := m.cfg
	cfgPath := m.cfgPath
	return func() tea.Msg {
		acct := cfg.Accounts[accountKey]
		if acct.DefaultCredentialType == "" {
			// First-time setup: just set the type, no cleanup needed.
			acct.DefaultCredentialType = newType
			acct.GCM = nil
			acct.SSH = nil

			switch newType {
			case "gcm":
				acct.GCM = &config.GCMConfig{
					Provider:    inferGCMProvider(acct.Provider),
					UseHTTPPath: false,
				}
			case "ssh":
				acct.SSH = &config.SSHConfig{
					Host:     credential.SSHHostAlias(accountKey),
					Hostname: hostnameFromURL(acct.URL),
					KeyType:  "ed25519",
				}
			}
			if err := cfg.UpdateAccount(accountKey, acct); err != nil {
				return credChangedMsg{err: err}
			}
			if err := config.Save(cfg, cfgPath); err != nil {
				return credChangedMsg{err: err}
			}
			return credChangedMsg{msgs: []string{"Credential type set to " + newType}}
		}
		// Existing credential: full change with cleanup.
		msgs, err := changeCredentialType(cfg, cfgPath, accountKey, newType)
		return credChangedMsg{msgs: msgs, err: err}
	}
}

func (m credentialModel) doDeletePAT() tea.Cmd {
	accountKey := m.accountKey
	credMgr := m.credMgr
	cfg := m.cfg
	return func() tea.Msg {
		if err := credential.DeleteToken(accountKey); err != nil {
			return credChangedMsg{err: err}
		}
		credMgr.Invalidate(accountKey)
		// Re-check credentials so the PAT status updates immediately.
		result := credential.Check(cfg.Accounts[accountKey], accountKey, cfg)
		epoch := credMgr.StartCheck(accountKey)
		credMgr.CompleteCheck(accountKey, epoch, result)
		return credChangedMsg{msgs: []string{"PAT deleted for " + accountKey}}
	}
}

func (m credentialModel) doDeleteCredential() tea.Cmd {
	accountKey := m.accountKey
	cfg := m.cfg
	cfgPath := m.cfgPath
	return func() tea.Msg {
		msgs, err := deleteCredential(cfg, cfgPath, accountKey)
		return credDeletedMsg{msgs: msgs, err: err}
	}
}

func (m credentialModel) doRegenerateSSH() tea.Cmd {
	accountKey := m.accountKey
	cfg := m.cfg
	return func() tea.Msg {
		msgs := regenerateSSH(cfg, accountKey)
		return sshRegenMsg{msgs: msgs}
	}
}

// ── View ────────────────────────────────────────────────────────

func (m credentialModel) View() string {
	switch m.view {
	case credViewType:
		return m.viewTypeSelect()
	case credViewSetup:
		return m.viewSetup()
	default:
		return m.viewMenu()
	}
}

func (m credentialModel) viewMenu() string {
	var b strings.Builder
	acct := m.cfg.Accounts[m.accountKey]
	credType := acct.DefaultCredentialType

	b.WriteString(m.theme.Title.Render("Credential: "+m.accountKey) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("Main credential type:"), m.theme.TextBold.Render(credType)))

	// Show credential status.
	credResult := m.credMgr.Get(m.accountKey)
	b.WriteString(fmt.Sprintf("  %-28s ", m.theme.TextMuted.Render("Main credential status:")))
	switch credResult.Primary {
	case credential.StatusOK:
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(styles.SymClean + " " + credResult.PrimaryDetail))
	case credential.StatusChecking, credential.StatusUnknown:
		b.WriteString(m.theme.TextMuted.Render(styles.SymSyncing + " verifying..."))
	case credential.StatusWarning:
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning)).
			Render(styles.SymDiverged + " " + credResult.PrimaryDetail))
	case credential.StatusError:
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(styles.SymError + " " + credResult.PrimaryDetail))
	case credential.StatusNone:
		b.WriteString(m.theme.TextMuted.Render("not configured"))
	}
	b.WriteString("\n")

	// Show PAT status for SSH/GCM accounts.
	if credType == "ssh" || credType == "gcm" {
		b.WriteString(fmt.Sprintf("  %-28s ", m.theme.TextMuted.Render("Advanced API PAT:")))
		switch credResult.PAT {
		case credential.StatusOK:
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean)).
				Render(styles.SymClean + " verified"))
		case credential.StatusChecking, credential.StatusUnknown:
			b.WriteString(m.theme.TextMuted.Render(styles.SymSyncing + " verifying..."))
		case credential.StatusWarning:
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning)).
				Render(styles.SymDiverged + " stored but API check failed"))
		case credential.StatusNone:
			b.WriteString(m.theme.TextMuted.Render("optional — not stored"))
		case credential.StatusError:
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
				Render(styles.SymError + " " + credResult.PATDetail))
		}
		b.WriteString("\n")
	}

	// Show SSH-specific info.
	if credType == "ssh" && acct.SSH != nil {
		b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("Host alias:"), m.theme.Text.Render(acct.SSH.Host)))
		sshFolder := credential.SSHFolder(m.cfg)
		keyPath := credential.SSHKeyPath(sshFolder, m.accountKey)
		b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("Key:"), m.theme.Text.Render(keyPath)))
	}

	if m.confirmDelete {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.AccentWarning)).
			Render("Delete account credential(s) and any security configuration? (enter=yes, n=no)") + "\n")
	}

	if m.confirmDeletePAT {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.AccentWarning)).
			Render("Delete PAT? (enter=yes, n=no)") + "\n")
	}

	if m.confirmRegen {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.AccentDanger)).
			Render("Regenerate SSH keys? Old keys will be deleted. (enter=yes, n=no)") + "\n")
	}

	if len(m.infoMsgs) > 0 {
		b.WriteString("\n")
		for _, msg := range m.infoMsgs {
			b.WriteString("  " + m.theme.TextMuted.Render(msg) + "\n")
		}
	}

	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg) + "\n")
	}

	b.WriteString("\n")
	var hints []string
	if m.resultOK || len(m.infoMsgs) > 0 {
		// Operation just completed — only allow ESC.
		hints = append(hints, "ESC back")
	} else {
		if credType == "ssh" {
			sshFolder := credential.SSHFolder(m.cfg)
			keyPath := credential.SSHKeyPath(sshFolder, m.accountKey)
			if _, err := os.Stat(keyPath); err == nil {
				hints = append(hints, "enter view key")
			} else {
				hints = append(hints, "enter setup")
			}
		} else if credType == "gcm" {
			if credential.CanOpenBrowser() {
				hints = append(hints, "enter browser auth")
			}
		} else {
			hints = append(hints, "enter setup")
		}
		hints = append(hints, "T change type", fmt.Sprintf("X del %s cred", credType))
		if (credType == "ssh" || credType == "gcm") && (credResult.PAT == credential.StatusOK || credResult.PAT == credential.StatusWarning || credResult.PAT == credential.StatusError) {
			hints = append(hints, "P delete PAT")
		}
		if credType == "ssh" {
			hints = append(hints, "G regen SSH")
		}
		hints = append(hints, "ESC back")
	}
	b.WriteString(renderHintsFit(m.theme, m.width, hints...))

	return b.String()
}

func (m credentialModel) viewSetup() string {
	var b strings.Builder
	acct := m.cfg.Accounts[m.accountKey]
	credType := acct.DefaultCredentialType

	b.WriteString(m.theme.Title.Render("Credential Setup: "+m.accountKey) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	if m.forceTokenSetup {
		b.WriteString(fmt.Sprintf("  Primary credential: %s\n", m.theme.TextBold.Render(credType)))
		b.WriteString("  " + m.theme.TextMuted.Render("Mirrors require an API token (PAT) for status checks.") + "\n\n")
		b.WriteString(m.theme.TextMuted.Render(m.guide) + "\n\n")
		b.WriteString("  " + m.tokenInput.View() + "\n\n")
		if m.busy {
			b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Storing and verifying...") + "\n")
		}
		if m.resultOK {
			b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean)).
				Render(styles.SymClean+" "+m.resultMsg) + "\n")
		}
		if m.errMsg != "" {
			b.WriteString("\n  " + lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
				Render(m.errMsg) + "\n")
		}
		b.WriteString("\n")
		if m.resultOK {
			b.WriteString(renderHints(m.theme, "ESC back"))
		} else {
			b.WriteString(renderHints(m.theme, "enter store token", "ESC back"))
		}
		return b.String()
	}

	b.WriteString(fmt.Sprintf("  Type: %s\n\n", m.theme.TextBold.Render(credType)))

	switch credType {
	case "token":
		b.WriteString(m.theme.TextMuted.Render(m.guide) + "\n\n")
		b.WriteString("  " + m.tokenInput.View() + "\n\n")
		if m.busy {
			b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Storing and verifying...") + "\n")
		}

	case "ssh":
		if m.sshPendingKey {
			// Key generated, waiting for user to add it to provider.
			ok := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean))
			b.WriteString("  " + ok.Render(styles.SymClean+" SSH key generated. Public key copied to clipboard.") + "\n\n")
			b.WriteString(m.theme.TextMuted.Render("  Public key:") + "\n")
			b.WriteString("  " + m.theme.Text.Render(m.sshPubKey) + "\n\n")
			b.WriteString(m.theme.TextMuted.Render("  Paste it at: ") + m.theme.Brand.Render(m.sshPubKeyURL) + "\n")
			b.WriteString(m.theme.TextMuted.Render("  Then press enter to test the connection.") + "\n")
		} else {
			b.WriteString(m.theme.TextMuted.Render("  SSH key will be generated (ed25519), config written, and connection tested.") + "\n")
			sshFolder := credential.SSHFolder(m.cfg)
			keyPath := credential.SSHKeyPath(sshFolder, m.accountKey)
			b.WriteString(m.theme.TextMuted.Render(fmt.Sprintf("  Key path: %s", keyPath)) + "\n")
			b.WriteString(m.theme.TextMuted.Render(fmt.Sprintf("  Add public key at: %s", credential.SSHPublicKeyURL(acct.Provider, acct.URL))) + "\n")
		}

		b.WriteString("\n")
		if m.busy {
			b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Testing SSH connection...") + "\n")
		} else if !m.resultOK && !m.sshPendingKey {
			b.WriteString(renderHintsFit(m.theme, m.width, "enter generate key + test connection", "ESC back") + "\n")
		}

	case "gcm":
		if m.busy {
			b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Opening browser for authentication...") + "\n")
			b.WriteString("  " + m.theme.TextMuted.Render("Complete the login in your browser, then return here.") + "\n")
		} else if !credential.CanOpenBrowser() {
			b.WriteString("  " + m.theme.TextMuted.Render("GCM browser authentication requires a desktop session.") + "\n\n")
			b.WriteString("  " + m.theme.TextMuted.Render("Options:") + "\n")
			b.WriteString("  " + m.theme.TextMuted.Render("  • Run from a desktop terminal:") + "\n")
			b.WriteString("  " + m.theme.Text.Render("    gitbox credential setup "+m.accountKey) + "\n")
			b.WriteString("  " + m.theme.TextMuted.Render("  • GCM will prompt automatically on next clone/fetch") + "\n")
		} else if !m.resultOK {
			b.WriteString("  " + m.theme.TextMuted.Render("Press enter to authenticate via browser (OAuth).") + "\n")
		}

	default:
		b.WriteString(m.theme.TextMuted.Render("  No credential type configured.") + "\n")
	}

	if len(m.infoMsgs) > 0 {
		b.WriteString("\n")
		for _, msg := range m.infoMsgs {
			b.WriteString("  " + m.theme.TextMuted.Render(msg) + "\n")
		}
	}

	if m.resultOK {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(styles.SymClean+" "+m.resultMsg) + "\n")
	}
	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg) + "\n")
	}

	b.WriteString("\n")
	switch {
	case credType == "token" && !m.busy && !m.resultOK && m.errMsg == "":
		b.WriteString(renderHints(m.theme, "enter submit", "ESC back"))
	case credType == "ssh" && m.sshPendingKey && !m.busy:
		b.WriteString(renderHints(m.theme, "c copy key", "enter test connection", "ESC back"))
	case credType == "ssh" && !m.busy && !m.resultOK && !m.sshPendingKey:
		// action line already rendered inline above
	case credType == "gcm" && !m.busy && !m.resultOK && credential.CanOpenBrowser():
		b.WriteString(renderHints(m.theme, "enter authenticate", "ESC back"))
	case credType == "gcm" && m.errMsg != "" && credential.CanOpenBrowser():
		b.WriteString(renderHints(m.theme, "enter retry", "ESC back"))
	default:
		b.WriteString(renderHints(m.theme, "ESC back"))
	}

	return b.String()
}

func (m credentialModel) viewTypeSelect() string {
	var b strings.Builder

	b.WriteString(m.typeForm.View())

	if len(m.infoMsgs) > 0 {
		b.WriteString("\n")
		for _, msg := range m.infoMsgs {
			b.WriteString("  " + m.theme.TextMuted.Render(msg) + "\n")
		}
	}

	return b.String()
}
