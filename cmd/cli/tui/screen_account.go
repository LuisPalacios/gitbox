package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type accountView int

const (
	accountViewDetail accountView = iota
	accountViewEdit
	accountViewRename
)

type accountModel struct {
	cfg           *config.Config
	cfgPath       string
	credMgr       *credential.StatusManager
	theme         styles.Theme
	width, height int
	view          accountView
	selectedKey   string
	statusMsg     string
	errMsg        string
	deleteStep    int // 0=inactive, 1=type name, 2=final confirm
	deleteInput   textinput.Model
	editForm      formModel
	renameForm    formModel
}

func newAccountModel(cfg *config.Config, cfgPath string, credMgr *credential.StatusManager, theme styles.Theme, w, h int, selectedKey string) accountModel {
	ti := textinput.New()
	ti.Placeholder = "Type account name..."
	ti.CharLimit = 128
	return accountModel{
		cfg:         cfg,
		cfgPath:     cfgPath,
		credMgr:     credMgr,
		theme:       theme,
		width:       w,
		height:      h,
		selectedKey: selectedKey,
		deleteInput: ti,
	}
}

func (m accountModel) Init() tea.Cmd {
	if m.selectedKey != "" {
		return credCheckCmd(m.credMgr, m.cfg, m.selectedKey)
	}
	return nil
}

func (m accountModel) Update(msg tea.Msg) (accountModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Delete flow intercepts all keys when active.
		if m.deleteStep > 0 {
			switch {
			case key.Matches(msg, Keys.Back):
				m.deleteStep = 0
				m.deleteInput.SetValue("")
				m.errMsg = ""
				return m, nil
			case key.Matches(msg, Keys.Enter):
				if m.deleteStep == 1 {
					if m.deleteInput.Value() == m.selectedKey {
						m.deleteStep = 2
						m.deleteInput.SetValue("")
						m.errMsg = ""
						return m, nil
					}
					m.errMsg = "Name does not match."
					return m, nil
				}
				if m.deleteStep == 2 {
					if err := m.cfg.DeleteAccount(m.selectedKey); err != nil {
						m.errMsg = err.Error()
					} else {
						if err := config.Save(m.cfg, m.cfgPath); err != nil {
							m.errMsg = err.Error()
						} else {
							return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }
						}
					}
					m.deleteStep = 0
					return m, nil
				}
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

		switch {
		case key.Matches(msg, Keys.Back):
			if m.view == accountViewEdit || m.view == accountViewRename {
				m.view = accountViewDetail
				return m, nil
			}
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }

		case msg.String() == "p" && m.view == accountViewDetail:
			acct := m.cfg.Accounts[m.selectedKey]
			if acct.DefaultCredentialType == "ssh" || acct.DefaultCredentialType == "gcm" {
				credResult := m.credMgr.Get(m.selectedKey)
				if credResult.PAT != credential.StatusOK {
					return m, func() tea.Msg {
						return switchScreenMsg{screen: screenCredential, accountKey: m.selectedKey, forceTokenSetup: true}
					}
				}
			}

		case msg.String() == "d" && m.view == accountViewDetail:
			acct := m.cfg.Accounts[m.selectedKey]
			if acct.DefaultCredentialType == "ssh" {
				credResult := m.credMgr.Get(m.selectedKey)
				if credResult.PAT == credential.StatusNone || credResult.PAT == credential.StatusError {
					return m, func() tea.Msg {
						return switchScreenMsg{screen: screenCredential, accountKey: m.selectedKey, forceTokenSetup: true}
					}
				}
			}
			return m, func() tea.Msg {
				return switchScreenMsg{screen: screenDiscovery, accountKey: m.selectedKey}
			}

		case msg.String() == "D" && m.view == accountViewDetail:
			m.deleteStep = 1
			m.deleteInput.SetValue("")
			m.deleteInput.Focus()
			m.errMsg = ""
			return m, textinput.Blink

		case msg.String() == "c" && m.view == accountViewDetail:
			return m, func() tea.Msg {
				return switchScreenMsg{screen: screenCredential, accountKey: m.selectedKey}
			}

		case msg.String() == "e" && m.view == accountViewDetail:
			m.view = accountViewEdit
			m.editForm = m.buildEditForm()
			return m, m.editForm.Init()

		case msg.String() == "v" && m.view == accountViewDetail:
			return m, credCheckCmd(m.credMgr, m.cfg, m.selectedKey)

		case msg.String() == "R" && m.view == accountViewDetail:
			m.view = accountViewRename
			m.renameForm = m.buildRenameForm()
			return m, m.renameForm.Init()

		case msg.String() == "b" && m.view == accountViewDetail:
			acct := m.cfg.Accounts[m.selectedKey]
			url := git.AccountProfileURL(acct.URL, acct.Username)
			m.statusMsg = ""
			m.errMsg = ""
			return m, openInBrowserCmd(url)

		case msg.String() == "o" && m.view == accountViewDetail:
			globalFolder := config.ExpandTilde(m.cfg.Global.Folder)
			path := filepath.Join(globalFolder, m.selectedKey)
			m.statusMsg = ""
			m.errMsg = ""
			return m, openAccountFolderCmd(path)
		}

	case credStatusUpdatedMsg:
		return m, nil

	case credStatusNoopMsg:
		return m, nil

	case accountUpdatedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.statusMsg = "Account updated."
			m.view = accountViewDetail
		}
		return m, nil

	case openBrowserDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.statusMsg = "Opened in browser."
		}
		return m, nil

	case openFolderDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.statusMsg = "Opened folder."
		}
		return m, nil

	case accountRenamedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.selectedKey = msg.newKey
			m.statusMsg = fmt.Sprintf("Account renamed: %s → %s", msg.oldKey, msg.newKey)
			m.view = accountViewDetail
			return m, credCheckCmd(m.credMgr, m.cfg, m.selectedKey)
		}
		return m, nil
	}

	// Delegate to forms when in form views.
	switch m.view {
	case accountViewEdit:
		submitted, cmd := m.editForm.Update(msg)
		if submitted {
			return m, m.saveEdit()
		}
		return m, cmd
	case accountViewRename:
		submitted, cmd := m.renameForm.Update(msg)
		if submitted {
			return m, m.doRename()
		}
		return m, cmd
	}

	return m, nil
}

func (m accountModel) View() string {
	switch m.view {
	case accountViewEdit:
		return m.editForm.View()
	case accountViewRename:
		return m.renameForm.View()
	default:
		return m.viewDetail()
	}
}

// openAccountFolderCmd reveals path in the OS file manager, or returns an
// error if the folder does not exist. Mirrors the backend's
// resolveAccountFolder behavior for the TUI's `o` binding.
func openAccountFolderCmd(path string) tea.Cmd {
	return func() tea.Msg {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return openFolderDoneMsg{err: fmt.Errorf("account folder does not exist: %s", path)}
			}
			return openFolderDoneMsg{err: fmt.Errorf("account folder %s: %w", path, err)}
		}
		if !info.IsDir() {
			return openFolderDoneMsg{err: fmt.Errorf("account folder is not a directory: %s", path)}
		}
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("explorer", filepath.FromSlash(path))
		case "darwin":
			cmd = exec.Command("open", path)
		default:
			cmd = exec.Command("xdg-open", path)
		}
		if err := cmd.Start(); err != nil {
			return openFolderDoneMsg{err: err}
		}
		return openFolderDoneMsg{}
	}
}

// ── Edit ────────────────────────────────────────────────────────

const (
	editFieldURL = iota
	editFieldUsername
	editFieldName
	editFieldEmail
)

func (m accountModel) buildEditForm() formModel {
	acct := m.cfg.Accounts[m.selectedKey]
	fields := []formField{
		newTextField("URL:", acct.URL, 256),
		newTextField("Username:", acct.Username, 128),
		newTextField("Name:", acct.Name, 128),
		newTextField("Email:", acct.Email, 256),
	}
	fields[editFieldURL].TextInput.SetValue(acct.URL)
	fields[editFieldUsername].TextInput.SetValue(acct.Username)
	fields[editFieldName].TextInput.SetValue(acct.Name)
	fields[editFieldEmail].TextInput.SetValue(acct.Email)

	fields[editFieldUsername].ValidateFn = func(v string) string {
		if v == "" {
			return "username is required"
		}
		return ""
	}

	return newFormModel("Edit Account: "+m.selectedKey, fields, m.theme)
}

func (m accountModel) saveEdit() tea.Cmd {
	return func() tea.Msg {
		acct := m.cfg.Accounts[m.selectedKey]
		acct.URL = m.editForm.Fields[editFieldURL].Value()
		acct.Username = m.editForm.Fields[editFieldUsername].Value()
		acct.Name = m.editForm.Fields[editFieldName].Value()
		acct.Email = m.editForm.Fields[editFieldEmail].Value()

		if err := m.cfg.UpdateAccount(m.selectedKey, acct); err != nil {
			return accountUpdatedMsg{err: err}
		}
		if err := config.Save(m.cfg, m.cfgPath); err != nil {
			return accountUpdatedMsg{err: err}
		}
		return accountUpdatedMsg{}
	}
}

// ── Detail view ─────────────────────────────────────────────────

func (m accountModel) viewDetail() string {
	var b strings.Builder
	acct := m.cfg.Accounts[m.selectedKey]

	b.WriteString(m.theme.Title.Render("Account: "+m.selectedKey) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("Provider:"), m.theme.Text.Render(acct.Provider)))
	b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("URL:"), m.theme.Text.Render(acct.URL)))
	b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("Username:"), m.theme.Text.Render(acct.Username)))
	b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("Name:"), m.theme.Text.Render(acct.Name)))
	b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("Email:"), m.theme.Text.Render(acct.Email)))

	credType := acct.DefaultCredentialType
	if credType == "" {
		credType = "none"
	}
	b.WriteString(fmt.Sprintf("  %-28s %s\n", m.theme.TextMuted.Render("Main credential type:"), m.theme.Text.Render(credType)))

	// Credential status — primary.
	credResult := m.credMgr.Get(m.selectedKey)
	b.WriteString(fmt.Sprintf("  %-28s ", m.theme.TextMuted.Render("Main credential status:")))
	switch credResult.Primary {
	case credential.StatusChecking, credential.StatusUnknown:
		b.WriteString(m.theme.TextMuted.Render(styles.SymSyncing + " verifying..."))
	case credential.StatusOK:
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(styles.SymClean + " " + credResult.PrimaryDetail))
	case credential.StatusWarning:
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.AccentWarning)).
			Render(styles.SymDiverged + " " + credResult.PrimaryDetail))
	case credential.StatusOffline:
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.CredOffline)).
			Render(styles.SymOffline + " " + credResult.PrimaryDetail))
	case credential.StatusError:
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(styles.SymError + " " + credResult.PrimaryDetail))
	case credential.StatusNone:
		b.WriteString(m.theme.TextMuted.Render("not configured"))
	}
	b.WriteString("\n")

	// PAT for advanced API — shown for SSH and GCM accounts.
	if credType == "ssh" || credType == "gcm" {
		b.WriteString(fmt.Sprintf("  %-28s ", m.theme.TextMuted.Render("Advanced API PAT:")))
		switch credResult.PAT {
		case credential.StatusChecking, credential.StatusUnknown:
			b.WriteString(m.theme.TextMuted.Render(styles.SymSyncing + " verifying..."))
		case credential.StatusOK:
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.theme.Palette.Clean)).
				Render(styles.SymClean + " verified"))
		case credential.StatusWarning:
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.theme.Palette.AccentWarning)).
				Render(styles.SymDiverged + " stored but API check failed"))
		case credential.StatusOffline:
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.theme.Palette.CredOffline)).
				Render(styles.SymOffline + " " + credResult.PATDetail))
		case credential.StatusNone:
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.theme.Palette.CredNone)).
				Render("optional — press p (PAT) to add one"))
		case credential.StatusError:
			b.WriteString(lipgloss.NewStyle().
				Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
				Render(styles.SymError + " " + credResult.PATDetail))
		}
		b.WriteString("\n")
	}

	if m.statusMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(m.statusMsg) + "\n")
	}

	if m.deleteStep == 1 {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
		b.WriteString("\n  " + warnStyle.Render(fmt.Sprintf("Type \"%s\" to confirm deletion:", m.selectedKey)) + "\n")
		b.WriteString("  " + m.deleteInput.View() + "\n")
		b.WriteString("  " + m.theme.TextMuted.Render("ESC to cancel") + "\n")
	} else if m.deleteStep == 2 {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
		b.WriteString("\n  " + warnStyle.Render(fmt.Sprintf(
			"Are you absolutely sure? This will delete account %s and all its credentials.", m.selectedKey)) + "\n")
		b.WriteString("  " + warnStyle.Render("enter=yes, ESC=cancel") + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render("Error: "+m.errMsg) + "\n")
	}

	b.WriteString("\n")
	hints := []string{"e edit", "R rename", "c credential"}
	if (acct.DefaultCredentialType == "ssh" || acct.DefaultCredentialType == "gcm") && credResult.PAT != credential.StatusOK {
		hints = append(hints, "p PAT")
	}
	hints = append(hints, "d discover", "v verify", "b browser", "o folder", "D delete", "ESC back")
	b.WriteString(renderHintsFit(m.theme, m.width, hints...))

	return b.String()
}

// ── Rename ──────────────────────────────────────────────────────

func (m accountModel) buildRenameForm() formModel {
	fields := []formField{
		newTextField("New key:", m.selectedKey, 64),
	}
	fields[0].TextInput.SetValue(m.selectedKey)
	fields[0].ValidateFn = func(v string) string {
		if v == "" {
			return "key cannot be empty"
		}
		if !validAccountKey.MatchString(v) {
			return "use lowercase letters, numbers, and hyphens"
		}
		if v != m.selectedKey {
			if _, exists := m.cfg.Accounts[v]; exists {
				return fmt.Sprintf("account %q already exists", v)
			}
		}
		return ""
	}
	return newFormModel("Rename Account: "+m.selectedKey, fields, m.theme)
}

func (m accountModel) doRename() tea.Cmd {
	newKey := m.renameForm.Fields[0].Value()
	oldKey := m.selectedKey
	cfg := m.cfg
	cfgPath := m.cfgPath
	return func() tea.Msg {
		if err := renameAccount(cfg, cfgPath, oldKey, newKey); err != nil {
			return accountRenamedMsg{oldKey: oldKey, newKey: newKey, err: err}
		}
		return accountRenamedMsg{oldKey: oldKey, newKey: newKey}
	}
}
