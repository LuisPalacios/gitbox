package tui

import (
	"time"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/i18n"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"
	tea "github.com/charmbracelet/bubbletea"
)

// screenID identifies which screen is active.
type screenID int

const (
	screenDashboard screenID = iota
	screenOnboarding
	screenAccount
	screenAccountAdd
	screenCredential
	screenDiscovery
	screenRepos
	screenMirrors
	screenSettings
	screenIdentity
	screenRepoCreate
	screenOrphans
	screenWorkspaceAdd
	screenGitignore
	screenMoveRepo
)

// --- Navigation messages ---

type switchScreenMsg struct {
	screen          screenID
	accountKey      string // context for account/credential/discovery screens
	sourceKey       string // context for repos screen
	repoKey         string // context for repos screen
	mirrorKey       string // context for mirrors screen
	forceTokenSetup bool   // go straight to PAT input in credential screen
	// workspaceMembers seeds the workspace add flow with pre-selected
	// members (each entry is "sourceKey/repoKey"). Empty for a blank form.
	workspaceMembers []string
	// repoPath is the resolved local path of the source clone. Populated
	// when opening the move-repo screen so the target model doesn't need
	// to recompute path resolution.
	repoPath string
}

// --- Config messages ---

type configLoadedMsg struct {
	cfg *config.Config
	err error
}

type configSavedMsg struct{ err error }
type configReloadedMsg struct {
	cfg *config.Config
	err error
}

// --- Status messages ---

type statusResultMsg struct{ results []status.RepoStatus }
type statusRefreshTickMsg struct{}

// --- Account messages ---

type accountAddedMsg struct {
	key string
	err error
}
type accountUpdatedMsg struct{ err error }
type accountRenamedMsg struct {
	oldKey, newKey string
	err            error
}
type credChangedMsg struct {
	msgs []string
	err  error
}
type credDeletedMsg struct {
	msgs []string
	err  error
}
type sshRegenMsg struct {
	msgs []string
}

// --- Credential messages ---

// credStatusUpdatedMsg is sent when a credential check completes.
// The StatusManager has already accepted the result (epoch matched).
type credStatusUpdatedMsg struct {
	accountKey string
}

// credStatusNoopMsg is sent when a stale result was discarded.
type credStatusNoopMsg struct{}

type credSetupDoneMsg struct {
	accountKey    string
	err           error
	sshPendingKey bool   // key generated but connection failed (user needs to add pubkey)
	pubKey        string // public key content (for display + clipboard)
	pubKeyURL     string // provider URL to add the key
	needsPAT      bool   // GCM auth OK but API access requires a separate PAT
	gcmUsername   string // actual username GCM returned (may differ in casing)
}

// --- Discovery messages ---

type discoverDoneMsg struct {
	repos      []provider.RemoteRepo
	err        error
	needsToken bool // SSH account needs a PAT for API discovery
}

type reposAddedMsg struct {
	count int
	err   error
}

type discoverTokenStoredMsg struct {
	err error
}

// --- Clone/Pull/Fetch messages ---

type cloneProgressMsg struct {
	phase   string
	percent int
}
type cloneDoneMsg struct {
	sourceKey, repoKey string
	err                error
}
type cloneAllProgressMsg struct {
	current, total int
	repoKey        string
}
type cloneAllDoneMsg struct {
	cloned int
	errors int
}
type pullAllProgressMsg struct {
	repoKey string
}
type pullAllDoneMsg struct{}
type pullDoneMsg struct {
	sourceKey, repoKey string
	err                error
}
type fetchDoneMsg struct {
	sourceKey, repoKey string
	err                error
}
type fetchAllDoneMsg struct{ err error }
type openBrowserDoneMsg struct{ err error }
type openFolderDoneMsg struct{ err error }
type sweepDoneMsg struct {
	sourceKey, repoKey string
	deleted            []string
	err                error
}
type syncTickMsg struct{}

// --- Mirror messages ---

type mirrorStatusMsg struct{ results []mirror.StatusResult }
type mirrorAllStatusMsg struct {
	results map[string][]mirror.StatusResult
}
type mirrorSetupDoneMsg struct{ result mirror.SetupResult }
type mirrorDiscoverProgressMsg struct{ progress mirror.DiscoverProgress }
type mirrorDiscoverDoneMsg struct {
	results []mirror.DiscoveryResult
	err     error
}
type mirrorGroupAddedMsg struct{ err error }
type mirrorGroupDeletedMsg struct{ err error }
type mirrorRepoAddedMsg struct{ err error }
type mirrorRepoDeletedMsg struct{ err error }
type mirrorCredCheckMsg struct {
	srcKey    string
	dstKey    string
	srcResult credential.StatusResult
	dstResult credential.StatusResult
}

// --- Identity messages ---

type globalIdentityMsg struct{ hasName, hasEmail bool }
type identityRemovedMsg struct{ err error }

// --- Global gitconfig GCM messages ---

type globalGCMMsg struct {
	needed bool
	status credential.GlobalGCMConfigStatus
}
type gcmFixedMsg struct{ err error }

// --- Gitignore messages ---

// gitignoreStatusMsg is sent when the dashboard's startup check finishes
// (or after the gitignore screen reloads). Carries a narrow view of the
// package Status and any error encountered during the check.
type gitignoreStatusMsg struct {
	status gitignoreStatusInfo
	err    error
}

// gitignoreStatusInfo mirrors gitignore.Status without forcing this file
// to import the gitignore package — kept narrow to avoid coupling.
type gitignoreStatusInfo struct {
	Path          string
	NeedsAction   bool
	BlockPresent  bool
	BlockUpToDate bool
	Excludesfile  string
	Set           bool
	FileExists    bool
}

type gitignoreInstalledMsg struct {
	updated         bool
	alreadyUpToDate bool
	backupPath      string
	setExcludes     bool
	path            string
	err             error
}

// --- Generic ---

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

// --- Root model ---

type model struct {
	screen        screenID
	cfg           *config.Config
	cfgPath       string
	cfgErr        string
	firstRun      bool
	testMode      bool
	width, height int
	darkTheme     bool
	theme         styles.Theme
	tr            i18n.Translator
	quitting      bool
	credMgr       *credential.StatusManager

	// Screen sub-models.
	dashboard    dashboardModel
	onboarding   onboardingModel
	account      accountModel
	accountAdd   accountAddModel
	credential   credentialModel
	discovery    discoveryModel
	repos        reposModel
	mirrors      mirrorsModel
	settings     settingsModel
	identity     identityModel
	repoCreate   repoCreateModel
	orphans      orphansModel
	workspaceAdd workspaceAddModel
	gitignore    gitignoreModel
	moveRepo     moveRepoModel

	// gitignoreNeedsAction mirrors the latest async global-gitignore check
	// so the dashboard footer can render the urgent "G gitignore!" prefix
	// without re-running the check on every repaint.
	gitignoreNeedsAction bool
}

func newModel(cfgPath string, tr i18n.Translator) model {
	t := styles.NewTheme(true)
	return model{
		screen:    screenDashboard,
		cfgPath:   cfgPath,
		darkTheme: true,
		theme:     t,
		tr:        tr,
		credMgr:   credential.NewStatusManager(),
	}
}

// loadConfigCmd loads config from disk.
func loadConfigCmd(path string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := config.Load(path)
		return configLoadedMsg{cfg: cfg, err: err}
	}
}

func (m model) Init() tea.Cmd {
	return loadConfigCmd(m.cfgPath)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Global handlers.
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		// Theme toggle on dashboard only.
		if msg.String() == "t" && m.screen == screenDashboard {
			m.darkTheme = !m.darkTheme
			m.theme = styles.NewTheme(m.darkTheme)
			m.dashboard.theme = m.theme
			return m, nil
		}
		// Global 'w' key: jump to the Workspaces tab from any dashboard
		// screen. Restricted to the dashboard so it doesn't steal 'w'
		// presses inside sub-screens that might use it for text input.
		// If the user has an active repo selection on the Accounts tab,
		// let the dashboard's own handler take over — that flow opens
		// the add-workspace screen pre-seeded with the selection.
		if msg.String() == "w" && m.screen == screenDashboard &&
			!(m.dashboard.activeTab == tabAccounts && len(m.dashboard.selectedClones) > 0) {
			m.dashboard.activeTab = tabWorkspaces
			m.dashboard.cardCursor = 0
			m.dashboard.listCursor = 0
			m.dashboard.focus = focusList
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Forward to active sub-screen so it can adapt layout.
		switch m.screen {
		case screenDashboard:
			m.dashboard.width = msg.Width
			m.dashboard.height = msg.Height
		case screenAccount:
			m.account.width = msg.Width
			m.account.height = msg.Height
		case screenCredential:
			m.credential.width = msg.Width
			m.credential.height = msg.Height
		case screenDiscovery:
			m.discovery.width = msg.Width
			m.discovery.height = msg.Height
		case screenOrphans:
			m.orphans.width = msg.Width
			m.orphans.height = msg.Height
		case screenRepos:
			m.repos.width = msg.Width
			m.repos.height = msg.Height
		case screenMirrors:
			m.mirrors.width = msg.Width
			m.mirrors.height = msg.Height
		case screenSettings:
			m.settings.width = msg.Width
			m.settings.height = msg.Height
		case screenRepoCreate:
			m.repoCreate.width = msg.Width
			m.repoCreate.height = msg.Height
		case screenWorkspaceAdd:
			m.workspaceAdd.width = msg.Width
			m.workspaceAdd.height = msg.Height
		}
		return m, nil

	case configLoadedMsg:
		if msg.err != nil {
			m.cfgErr = msg.err.Error()
			m.firstRun = true
			m.cfg = &config.Config{
				Version:  2,
				Global:   config.GlobalConfig{},
				Accounts: make(map[string]config.Account),
				Sources:  make(map[string]config.Source),
			}
		} else {
			m.cfg = msg.cfg
			m.firstRun = m.cfg.Global.Folder == ""
		}
		if m.firstRun {
			m.screen = screenOnboarding
			m.onboarding = newOnboardingModel(m.cfg, m.cfgPath, m.theme, m.width, m.height)
			return m, m.onboarding.Init()
		}
		m.dashboard = newDashboardModel(m.cfg, m.cfgPath, m.credMgr, m.theme, m.width, m.height)
		m.dashboard.testMode = m.testMode
		return m, m.dashboard.Init()

	case switchScreenMsg:
		// Hitting the gitignore screen from the dashboard implicitly clears
		// the urgent footer hint — the user is already acting on it. The
		// screen's own re-check will decide whether to set it again.
		if msg.screen == screenGitignore {
			m.gitignoreNeedsAction = false
			m.dashboard.gitignoreNeedsAction = false
		}
		return m.switchTo(msg)

	case gitignoreStatusMsg:
		// Async startup check or screen re-check finished. Persist the
		// result at the root so the dashboard footer can render the
		// urgent "G gitignore!" prefix without re-running the check.
		if msg.err == nil {
			m.gitignoreNeedsAction = msg.status.NeedsAction
			m.dashboard.gitignoreNeedsAction = msg.status.NeedsAction
		}
		// Fall through so the active screen (likely the gitignore screen)
		// can also consume the message.
	}

	// Delegate to active screen.
	var cmd tea.Cmd
	switch m.screen {
	case screenDashboard:
		m.dashboard, cmd = m.dashboard.Update(msg)
	case screenOnboarding:
		m.onboarding, cmd = m.onboarding.Update(msg)
	case screenAccount:
		m.account, cmd = m.account.Update(msg)
	case screenAccountAdd:
		m.accountAdd, cmd = m.accountAdd.Update(msg)
	case screenCredential:
		m.credential, cmd = m.credential.Update(msg)
	case screenDiscovery:
		m.discovery, cmd = m.discovery.Update(msg)
	case screenOrphans:
		m.orphans, cmd = m.orphans.Update(msg)
	case screenRepos:
		m.repos, cmd = m.repos.Update(msg)
	case screenMirrors:
		m.mirrors, cmd = m.mirrors.Update(msg)
	case screenSettings:
		m.settings, cmd = m.settings.Update(msg)
	case screenIdentity:
		m.identity, cmd = m.identity.Update(msg)
	case screenRepoCreate:
		m.repoCreate, cmd = m.repoCreate.Update(msg)
	case screenWorkspaceAdd:
		m.workspaceAdd, cmd = m.workspaceAdd.Update(msg)
	case screenGitignore:
		m.gitignore, cmd = m.gitignore.Update(msg)
	case screenMoveRepo:
		m.moveRepo, cmd = m.moveRepo.Update(msg)
	}
	return m, cmd
}

// switchTo initializes and switches to the target screen.
func (m model) switchTo(msg switchScreenMsg) (model, tea.Cmd) {
	prevScreen := m.screen
	m.screen = msg.screen
	var cmd tea.Cmd
	switch msg.screen {
	case screenDashboard:
		// Reload config from disk to pick up any changes made in sub-screens.
		if fresh, err := config.Load(m.cfgPath); err == nil {
			m.cfg = fresh
		}
		// Reuse existing dashboard model — just update config and recompute keys.
		m.dashboard.cfg = m.cfg
		m.dashboard.refreshAccountKeys()
		m.dashboard.mirrorSummaries = mirror.Summarize(m.cfg, m.dashboard.mirrorLiveResults)
		m.dashboard.width = m.width
		m.dashboard.height = m.height
		m.dashboard.theme = m.theme
		// Restore to Mirrors tab if coming from mirror-related screen.
		if msg.forceTokenSetup || msg.mirrorKey != "" {
			m.dashboard.activeTab = tabMirrors
		}
		// Always refresh status + mirrors when returning.
		cmds := []tea.Cmd{}
		if len(m.dashboard.statuses) == 0 || time.Since(m.dashboard.lastRefresh) > 5*time.Second {
			m.dashboard.loading = true
			cmds = append(cmds, checkAllStatusCmd(m.cfg), m.dashboard.checkAllCredsCmd())
		}
		cmds = append(cmds, checkAllMirrorStatusCmd(m.cfg))
		cmd = tea.Batch(cmds...)
	case screenOnboarding:
		m.onboarding = newOnboardingModel(m.cfg, m.cfgPath, m.theme, m.width, m.height)
		cmd = m.onboarding.Init()
	case screenAccount:
		m.account = newAccountModel(m.cfg, m.cfgPath, m.credMgr, m.theme, m.width, m.height, msg.accountKey)
		cmd = m.account.Init()
	case screenAccountAdd:
		m.accountAdd = newAccountAddModel(m.cfg, m.cfgPath, m.theme, m.width, m.height)
		cmd = m.accountAdd.Init()
	case screenCredential:
		m.credential = newCredentialModel(m.cfg, m.cfgPath, m.credMgr, msg.accountKey, m.theme, m.width, m.height)
		if msg.forceTokenSetup {
			m.credential.view = credViewSetup
			m.credential.forceTokenSetup = true
			m.credential.forceReturnScreen = prevScreen // remember where we came from
			m.credential.tokenInput.Focus()
		}
		cmd = m.credential.Init()
	case screenDiscovery:
		m.discovery = newDiscoveryModel(m.cfg, m.cfgPath, msg.accountKey, m.theme, m.width, m.height)
		cmd = m.discovery.Init()
	case screenRepos:
		m.repos = newReposModel(m.cfg, m.cfgPath, msg.sourceKey, msg.repoKey, m.theme, m.width, m.height)
		cmd = m.repos.Init()
	case screenMirrors:
		m.mirrors = newMirrorsModel(m.cfg, m.cfgPath, m.theme, m.width, m.height, msg.mirrorKey)
		cmd = m.mirrors.Init()
	case screenSettings:
		m.settings = newSettingsModel(m.cfg, m.cfgPath, m.theme, m.tr, m.width, m.height)
		cmd = m.settings.Init()
	case screenIdentity:
		m.identity = newIdentityModel(m.cfg, m.cfgPath, m.theme, m.width, m.height)
		cmd = m.identity.Init()
	case screenRepoCreate:
		m.repoCreate = newRepoCreateModel(m.cfg, m.cfgPath, m.theme, m.width, m.height, msg.accountKey)
		cmd = m.repoCreate.Init()
	case screenOrphans:
		m.orphans = newOrphansModel(m.cfg, m.cfgPath, m.theme, m.width, m.height)
		cmd = m.orphans.Init()
	case screenWorkspaceAdd:
		m.workspaceAdd = newWorkspaceAddModel(m.cfg, m.cfgPath, m.theme, m.width, m.height, msg.workspaceMembers)
		cmd = m.workspaceAdd.Init()
	case screenGitignore:
		m.gitignore = newGitignoreModel(m.theme, m.width, m.height)
		cmd = m.gitignore.Init()
	case screenMoveRepo:
		src := m.cfg.Sources[msg.sourceKey]
		accountKey := src.Account
		m.moveRepo = newMoveRepoModel(m.cfg, m.cfgPath, m.theme, m.width, m.height, accountKey, msg.sourceKey, msg.repoKey, msg.repoPath)
		cmd = m.moveRepo.Init()
	}
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	switch m.screen {
	case screenDashboard:
		return m.dashboard.View()
	case screenOnboarding:
		return m.onboarding.View()
	case screenAccount:
		return m.account.View()
	case screenAccountAdd:
		return m.accountAdd.View()
	case screenCredential:
		return m.credential.View()
	case screenDiscovery:
		return m.discovery.View()
	case screenRepos:
		return m.repos.View()
	case screenMirrors:
		return m.mirrors.View()
	case screenSettings:
		return m.settings.View()
	case screenIdentity:
		return m.identity.View()
	case screenRepoCreate:
		return m.repoCreate.View()
	case screenOrphans:
		return m.orphans.View()
	case screenWorkspaceAdd:
		return m.workspaceAdd.View()
	case screenGitignore:
		return m.gitignore.View()
	case screenMoveRepo:
		return m.moveRepo.View()
	default:
		return "Unknown screen"
	}
}
