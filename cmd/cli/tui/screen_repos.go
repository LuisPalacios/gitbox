package tui

import (
	"fmt"
	neturl "net/url"
	"os"
	"strings"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type reposModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int
	sourceKey     string
	repoKey       string
	repoStatus    status.RepoStatus
	branch        string
	ahead, behind int
	changed       []git.FileChange
	untracked     []string
	busy          bool
	busyLabel     string
	resultMsg      string
	errMsg         string
	clonePhase     string
	clonePercent   int
	cloneProgressCh <-chan git.CloneProgress
	cloneDoneCh    <-chan error
	deleteStep     int // 0=inactive, 1=type name, 2=final confirm
	deleteInput    textinput.Model
	launcher       launcherOverlay
}

func newReposModel(cfg *config.Config, cfgPath, sourceKey, repoKey string, theme styles.Theme, w, h int) reposModel {
	ti := textinput.New()
	ti.Placeholder = "Type clone name..."
	ti.CharLimit = 256
	return reposModel{
		cfg:         cfg,
		cfgPath:     cfgPath,
		theme:       theme,
		width:       w,
		height:      h,
		sourceKey:   sourceKey,
		repoKey:     repoKey,
		deleteInput: ti,
		launcher:    newLauncherOverlay(cfg).withCfgPath(cfgPath),
	}
}

// repoWorkspaceKeys returns the ordered list of workspace keys this
// clone is a member of. Used to splice a "Workspaces" section into the
// launcher overlay only when meaningful.
func (m reposModel) repoWorkspaceKeys() []string {
	target := m.sourceKey + "/" + m.repoKey
	var keys []string
	for _, wsKey := range m.cfg.OrderedWorkspaceKeys() {
		ws := m.cfg.Workspaces[wsKey]
		for _, mem := range ws.Members {
			if mem.Source+"/"+mem.Repo == target {
				keys = append(keys, wsKey)
				break
			}
		}
	}
	return keys
}

func (m reposModel) repoPath() string {
	src := m.cfg.Sources[m.sourceKey]
	repo := src.Repos[m.repoKey]
	globalFolder := config.ExpandTilde(m.cfg.Global.Folder)
	sourceFolder := src.EffectiveFolder(m.sourceKey)
	return status.ResolveRepoPath(globalFolder, sourceFolder, m.repoKey, repo)
}

func checkRepoStatusCmd(cfg *config.Config, sourceKey, repoKey string) tea.Cmd {
	return func() tea.Msg {
		src := cfg.Sources[sourceKey]
		repo := src.Repos[repoKey]
		globalFolder := config.ExpandTilde(cfg.Global.Folder)
		sourceFolder := src.EffectiveFolder(sourceKey)
		path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)

		rs := status.Check(path)
		rs.Source = sourceKey
		rs.Repo = repoKey
		return statusResultMsg{results: []status.RepoStatus{rs}}
	}
}

// startCloneCmd launches a clone goroutine and returns channels for progress/done.
func startCloneCmd(cfg *config.Config, sourceKey, repoKey string) (<-chan git.CloneProgress, <-chan error) {
	progressCh := make(chan git.CloneProgress, 16)
	doneCh := make(chan error, 1)

	go func() {
		src := cfg.Sources[sourceKey]
		repo := src.Repos[repoKey]
		acct := cfg.Accounts[src.Account]

		globalFolder := config.ExpandTilde(cfg.Global.Folder)
		sourceFolder := src.EffectiveFolder(sourceKey)
		dest := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)

		credType := repo.EffectiveCredentialType(&acct)
		accountKey := src.Account
		plainURL := cloneURL(acct, repoKey, credType)

		cloneURLStr := plainURL
		cloneOpts := git.CloneOpts{Quiet: true}
		if credType == "token" {
			if tok, _, err := credential.ResolveToken(acct, accountKey); err == nil && tok != "" {
				if u, err := neturl.Parse(plainURL); err == nil {
					u.User = neturl.UserPassword(acct.Username, tok)
					cloneURLStr = u.String()
				}
			}
			cloneOpts.ConfigArgs = []string{"credential.helper="}
		}

		err := git.CloneWithProgress(cloneURLStr, dest, cloneOpts, func(p git.CloneProgress) {
			select {
			case progressCh <- p:
			default:
			}
		})
		close(progressCh)

		if err == nil {
			if credType == "token" {
				_ = git.SetRemoteURL(dest, "origin", plainURL)
			}
			_ = credential.ConfigureRepoCredential(dest, acct, accountKey, credType, cfg.Global)
			wantName, wantEmail := identity.ResolveIdentity(repo, acct)
			if _, _, idErr := identity.EnsureRepoIdentity(dest, wantName, wantEmail); idErr != nil {
				err = fmt.Errorf("clone ok but identity failed: %w", idErr)
			}
		}
		doneCh <- err
	}()

	return progressCh, doneCh
}

// listenCloneCmd returns a Cmd that waits for the next progress or done message.
func listenCloneCmd(progressCh <-chan git.CloneProgress, doneCh <-chan error, sourceKey, repoKey string) tea.Cmd {
	return func() tea.Msg {
		select {
		case p, ok := <-progressCh:
			if ok {
				return cloneProgressMsg{phase: p.Phase, percent: p.Percent}
			}
			// Channel closed, wait for done.
			return cloneDoneMsg{sourceKey: sourceKey, repoKey: repoKey, err: <-doneCh}
		case err := <-doneCh:
			return cloneDoneMsg{sourceKey: sourceKey, repoKey: repoKey, err: err}
		}
	}
}

func pullRepoCmd(path, sourceKey, repoKey string) tea.Cmd {
	return func() tea.Msg {
		err := git.Pull(path)
		return pullDoneMsg{sourceKey: sourceKey, repoKey: repoKey, err: err}
	}
}

func fetchRepoCmd(path, sourceKey, repoKey string) tea.Cmd {
	return func() tea.Msg {
		err := git.Fetch(path)
		return fetchDoneMsg{sourceKey: sourceKey, repoKey: repoKey, err: err}
	}
}

func sweepRepoCmd(path, sourceKey, repoKey string) tea.Cmd {
	return func() tea.Msg {
		result, err := git.SweepBranches(path)
		if err != nil {
			return sweepDoneMsg{sourceKey: sourceKey, repoKey: repoKey, err: err}
		}
		deleted, errs := git.DeleteStaleBranches(path, result)
		var firstErr error
		if len(errs) > 0 {
			firstErr = errs[0]
		}
		return sweepDoneMsg{
			sourceKey: sourceKey, repoKey: repoKey,
			deleted: deleted, err: firstErr,
		}
	}
}

func openInBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg {
		err := git.OpenInBrowser(url)
		return openBrowserDoneMsg{err: err}
	}
}

func deleteRepoCmd(cfg *config.Config, cfgPath, sourceKey, repoKey, repoPath string) tea.Cmd {
	return func() tea.Msg {
		// Remove local clone if it exists.
		if git.IsRepo(repoPath) {
			if err := os.RemoveAll(repoPath); err != nil {
				return errMsg{err: fmt.Errorf("remove clone: %w", err)}
			}
		}
		// Remove from config.
		if err := cfg.DeleteRepo(sourceKey, repoKey); err != nil {
			return errMsg{err: err}
		}
		if err := config.Save(cfg, cfgPath); err != nil {
			return errMsg{err: err}
		}
		return switchScreenMsg{screen: screenDashboard}
	}
}

func (m reposModel) Init() tea.Cmd {
	return checkRepoStatusCmd(m.cfg, m.sourceKey, m.repoKey)
}

func (m reposModel) Update(msg tea.Msg) (reposModel, tea.Cmd) {
	// Launcher overlay intercepts input when active; must run before the
	// KeyMsg switch so t/e/a/o and other letters don't double-fire.
	if lo, cmd, handled := m.launcher.update(msg, m.cfg.Global.Terminals); handled {
		m.launcher = lo
		m.resultMsg = ""
		m.errMsg = ""
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.busy {
			return m, nil
		}

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
					if m.deleteInput.Value() == m.repoKey {
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
					m.busyLabel = "Deleting..."
					return m, deleteRepoCmd(m.cfg, m.cfgPath, m.sourceKey, m.repoKey, m.repoPath())
				}
			default:
				if m.deleteStep == 1 {
					m.errMsg = "" // clear error as soon as user types
					var cmd tea.Cmd
					m.deleteInput, cmd = m.deleteInput.Update(msg)
					return m, cmd
				}
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, Keys.Back):
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }

		case msg.String() == "D":
			m.deleteStep = 1
			m.deleteInput.SetValue("")
			m.deleteInput.Focus()
			m.errMsg = ""
			return m, textinput.Blink

		case msg.String() == "M":
			// Move requires a cloned, clean, in-sync repo. Guard
			// mirrors the GUI kebab's disabled-tooltip copy.
			if !git.IsRepo(m.repoPath()) {
				m.errMsg = "Clone the repo first (c) before moving."
				m.resultMsg = ""
				return m, nil
			}
			if m.repoStatus.State != status.Clean || m.ahead > 0 || m.behind > 0 {
				m.errMsg = "Move requires a clean, in-sync clone. Commit/push/fetch first."
				m.resultMsg = ""
				return m, nil
			}
			return m, func() tea.Msg {
				return switchScreenMsg{
					screen:    screenMoveRepo,
					sourceKey: m.sourceKey,
					repoKey:   m.repoKey,
					repoPath:  m.repoPath(),
				}
			}

		case msg.String() == "c":
			if m.repoStatus.State == status.NotCloned {
				m.busy = true
				m.busyLabel = "Cloning..."
				m.clonePhase = ""
				m.clonePercent = 0
				m.resultMsg = ""
				m.errMsg = ""
				pCh, dCh := startCloneCmd(m.cfg, m.sourceKey, m.repoKey)
				m.cloneProgressCh = pCh
				m.cloneDoneCh = dCh
				return m, listenCloneCmd(pCh, dCh, m.sourceKey, m.repoKey)
			}

		case msg.String() == "p":
			path := m.repoPath()
			if git.IsRepo(path) {
				m.busy = true
				m.busyLabel = "Pulling..."
				m.resultMsg = ""
				m.errMsg = ""
				return m, pullRepoCmd(path, m.sourceKey, m.repoKey)
			}

		case msg.String() == "f":
			path := m.repoPath()
			if git.IsRepo(path) {
				m.busy = true
				m.busyLabel = "Fetching..."
				m.resultMsg = ""
				m.errMsg = ""
				return m, fetchRepoCmd(path, m.sourceKey, m.repoKey)
			}

		case msg.String() == "b":
			source := m.cfg.Sources[m.sourceKey]
			acct := m.cfg.Accounts[source.Account]
			url := git.RepoWebURL(acct.URL, m.repoKey)
			m.resultMsg = ""
			m.errMsg = ""
			return m, openInBrowserCmd(url)

		case msg.String() == "s":
			path := m.repoPath()
			if git.IsRepo(path) {
				m.busy = true
				m.busyLabel = "Sweeping..."
				m.resultMsg = ""
				m.errMsg = ""
				return m, sweepRepoCmd(path, m.sourceKey, m.repoKey)
			}

		case msg.String() == "t":
			// terminals[0] — skip if no terminal configured or repo not cloned.
			return m.launchDefaultTerminal()

		case msg.String() == "e":
			// editors[0] — skip if no editor configured or repo not cloned.
			return m.launchDefaultEditor()

		case msg.String() == "a":
			// ai_harnesses[0] — skip if none configured or repo not cloned.
			return m.launchDefaultHarness()

		case msg.String() == "o":
			// Full launcher overlay listing every configured launcher.
			return m.openLauncher()
		}

	case launchDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.resultMsg = "Opened in " + msg.target + "."
		}
		return m, nil

	case cloneProgressMsg:
		m.clonePhase = msg.phase
		m.clonePercent = msg.percent
		m.busyLabel = fmt.Sprintf("%s %d%%", msg.phase, msg.percent)
		// Chain: listen for next progress or done.
		return m, listenCloneCmd(m.cloneProgressCh, m.cloneDoneCh, m.sourceKey, m.repoKey)

	case statusResultMsg:
		if len(msg.results) > 0 {
			m.repoStatus = msg.results[0]
			// Get detailed status if cloned.
			path := m.repoPath()
			if git.IsRepo(path) {
				br, ahead, behind, changed, untracked, _ := git.DetailedStatus(path)
				m.branch = br
				m.ahead = ahead
				m.behind = behind
				m.changed = changed
				m.untracked = untracked
			}
		}
		return m, nil

	case cloneDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.resultMsg = "Cloned successfully."
		}
		return m, checkRepoStatusCmd(m.cfg, m.sourceKey, m.repoKey)

	case pullDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.resultMsg = "Pulled successfully."
		}
		return m, checkRepoStatusCmd(m.cfg, m.sourceKey, m.repoKey)

	case fetchDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.resultMsg = "Fetched successfully."
		}
		return m, checkRepoStatusCmd(m.cfg, m.sourceKey, m.repoKey)

	case sweepDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else if len(msg.deleted) == 0 {
			m.resultMsg = "No stale branches found."
		} else {
			m.resultMsg = fmt.Sprintf("Swept %d branch(es): %s", len(msg.deleted), strings.Join(msg.deleted, ", "))
		}
		return m, checkRepoStatusCmd(m.cfg, m.sourceKey, m.repoKey)

	case openBrowserDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		} else {
			m.resultMsg = "Opened in browser."
		}
		return m, nil

	case errMsg:
		m.busy = false
		m.errMsg = msg.Error()
		return m, nil
	}
	return m, nil
}

func (m reposModel) View() string {
	var b strings.Builder

	sym, color := stateSymbolColor(m.repoStatus.State, m.theme.Palette)
	symStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))

	b.WriteString(m.theme.Title.Render(m.repoKey) + "  " + symStyle.Render(sym+" "+m.repoStatus.State.String()) + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	b.WriteString(fmt.Sprintf("  %-15s %s\n", m.theme.TextMuted.Render("Source:"), m.theme.Text.Render(m.sourceKey)))
	b.WriteString(fmt.Sprintf("  %-15s %s\n", m.theme.TextMuted.Render("Path:"), m.theme.Text.Render(m.repoPath())))

	if m.branch == "(detached)" {
		b.WriteString(fmt.Sprintf("  %-15s %s\n", m.theme.TextMuted.Render("Branch:"),
			lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).Render("HEAD detached")))
	} else if m.branch != "" {
		b.WriteString(fmt.Sprintf("  %-15s %s\n", m.theme.TextMuted.Render("Branch:"), m.theme.Text.Render(m.branch)))
	}
	if m.ahead > 0 || m.behind > 0 {
		b.WriteString(fmt.Sprintf("  %-15s %d ahead, %d behind\n",
			m.theme.TextMuted.Render("Tracking:"), m.ahead, m.behind))
	}

	// Changed files.
	if len(m.changed) > 0 {
		b.WriteString("\n  " + m.theme.Heading.Render("Changed files:") + "\n")
		limit := 10
		for i, f := range m.changed {
			if i >= limit {
				b.WriteString(fmt.Sprintf("  ... and %d more\n", len(m.changed)-limit))
				break
			}
			b.WriteString(fmt.Sprintf("    %-10s %s\n",
				m.theme.TextMuted.Render(f.Kind), m.theme.Text.Render(f.Path)))
		}
	}

	// Untracked.
	if len(m.untracked) > 0 {
		b.WriteString("\n  " + m.theme.Heading.Render("Untracked:") + "\n")
		limit := 5
		for i, f := range m.untracked {
			if i >= limit {
				b.WriteString(fmt.Sprintf("    ... and %d more\n", len(m.untracked)-limit))
				break
			}
			b.WriteString("    " + m.theme.TextMuted.Render(f) + "\n")
		}
	}

	if m.busy {
		if m.clonePhase != "" {
			// Progress bar for clone.
			barWidth := 30
			filled := barWidth * m.clonePercent / 100
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			b.WriteString(fmt.Sprintf("\n  %s %s %s %d%%\n",
				m.theme.TextMuted.Render(m.clonePhase),
				m.theme.Brand.Render(bar),
				m.theme.TextMuted.Render(""),
				m.clonePercent))
		} else {
			b.WriteString("\n  " + m.theme.TextMuted.Render(styles.SymSyncing+" "+m.busyLabel) + "\n")
		}
	}
	if m.resultMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.Clean)).
			Render(styles.SymClean+" "+m.resultMsg) + "\n")
	}
	if m.deleteStep == 1 {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
		b.WriteString("\n  " + warnStyle.Render(fmt.Sprintf("Type \"%s\" to remove the local clone:", m.repoKey)) + "\n")
		b.WriteString("  " + m.theme.TextMuted.Render("(Only removes the local folder and config entry. The remote repo is NOT affected.)") + "\n")
		b.WriteString("  " + m.deleteInput.View() + "\n")
		b.WriteString("  " + m.theme.TextMuted.Render("ESC to cancel") + "\n")
	} else if m.deleteStep == 2 {
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning))
		b.WriteString("\n  " + warnStyle.Render(fmt.Sprintf(
			"Remove local clone of %s? The remote repo will NOT be deleted.", m.repoKey)) + "\n")
		b.WriteString("  " + warnStyle.Render("enter=yes, ESC=cancel") + "\n")
	}

	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(m.theme.Palette.StatusError)).
			Render(m.errMsg) + "\n")
	}

	// Actions.
	b.WriteString("\n")
	actions := []string{}
	if m.repoStatus.State == status.NotCloned {
		actions = append(actions, "c clone")
	} else {
		actions = append(actions, "f fetch", "p pull", "s sweep")
		if len(m.cfg.Global.Terminals) > 0 {
			actions = append(actions, "t terminal")
		}
		if len(m.cfg.Global.Editors) > 0 {
			actions = append(actions, "e editor")
		}
		if len(m.cfg.Global.AIHarnesses) > 0 {
			actions = append(actions, "a AI")
		}
		if m.launcher.hasAny() {
			actions = append(actions, "o open in…")
		}
	}
	actions = append(actions, "b open browser")
	if m.repoStatus.State == status.Clean && m.ahead == 0 && m.behind == 0 {
		actions = append(actions, "M move")
	}
	actions = append(actions, "D remove clone", "ESC back")
	b.WriteString(renderHints(m.theme, actions...))

	if m.launcher.active {
		return m.launcher.view(m.theme, m.width, m.height)
	}
	return b.String()
}

// openLauncher activates the launcher overlay. No-op with a hint when the
// repo folder does not exist on disk (launchers need a real working
// directory) or when the config has no launchers at all.
func (m reposModel) openLauncher() (reposModel, tea.Cmd) {
	path := m.repoPath()
	if !pathIsDir(path) {
		m.errMsg = "Clone the repo first (c) — launchers need a local folder."
		m.resultMsg = ""
		return m, nil
	}
	if !m.launcher.hasAny() {
		m.errMsg = "No editors, terminals, or AI harnesses are configured."
		m.resultMsg = ""
		return m, nil
	}
	// Thread workspace memberships into the overlay so the Workspaces
	// section only appears when this clone belongs to one.
	m.launcher = m.launcher.activateWithWorkspaces(path, m.repoWorkspaceKeys())
	m.resultMsg = ""
	m.errMsg = ""
	return m, nil
}

// launchDefaultTerminal / launchDefaultEditor / launchDefaultHarness wire
// the t / e / a keys to the first entry of each category. Launching against
// a missing folder would surface confusing shell errors, so each helper
// silently no-ops when the category is empty and surfaces a guard message
// when the repo isn't on disk.
func (m reposModel) launchDefaultTerminal() (reposModel, tea.Cmd) {
	terms := m.cfg.Global.Terminals
	if len(terms) == 0 {
		return m, nil
	}
	path := m.repoPath()
	if !pathIsDir(path) {
		m.errMsg = "Clone the repo first (c) before opening a terminal."
		m.resultMsg = ""
		return m, nil
	}
	m.resultMsg = ""
	m.errMsg = ""
	return m, launchTerminalCmd(path, terms[0])
}

func (m reposModel) launchDefaultEditor() (reposModel, tea.Cmd) {
	editors := m.cfg.Global.Editors
	if len(editors) == 0 {
		return m, nil
	}
	path := m.repoPath()
	if !pathIsDir(path) {
		m.errMsg = "Clone the repo first (c) before opening an editor."
		m.resultMsg = ""
		return m, nil
	}
	m.resultMsg = ""
	m.errMsg = ""
	return m, launchEditorCmd(path, editors[0].Command, editors[0].Name)
}

func (m reposModel) launchDefaultHarness() (reposModel, tea.Cmd) {
	harnesses := m.cfg.Global.AIHarnesses
	if len(harnesses) == 0 {
		return m, nil
	}
	path := m.repoPath()
	if !pathIsDir(path) {
		m.errMsg = "Clone the repo first (c) before opening an AI harness."
		m.resultMsg = ""
		return m, nil
	}
	m.resultMsg = ""
	m.errMsg = ""
	return m, launchAIHarnessCmd(path, harnesses[0], m.cfg.Global.Terminals)
}

// pathIsDir reports whether path refers to an existing directory. Small
// helper to keep the launcher guards readable and consistent between the
// repos and account screens.
func pathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

