package tui

import (
	"context"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/move"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Form field indices. "owner" needs to be first because it's a select
// that populates asynchronously once we know the destination account.
const (
	moveFieldOwner = iota
	moveFieldName
	moveFieldPrivate
	moveFieldDeleteSource
	moveFieldDeleteLocal
	moveFieldCount
)

// --- Messages ---

type moveOrgsMsg struct {
	owners []string
	err    error
}

type moveProgressMsg struct {
	phase move.Phase
	msg   string
	err   error
}

type moveDoneMsg struct {
	result move.Result
}

// --- Model ---

type moveRepoModel struct {
	cfg           *config.Config
	cfgPath       string
	theme         styles.Theme
	width, height int

	// Source context (passed in from screen_repos).
	sourceAccountKey string
	sourceSourceKey  string
	sourceRepoKey    string
	sourceRepoPath   string

	// Destination account selection.
	destAccountKeys []string
	destAccount     selectField
	destAccountKey  string
	accountPicked   bool

	// Form (populated after destAccountKey known + owners resolved).
	form      formModel
	formReady bool

	// Async state.
	loadingOwners bool
	confirmStep   int // 0=form, 1=type-to-confirm
	confirmInput  textinput.Model
	moving        bool
	progress      []moveProgressMsg // collected progress events for display
	resultMsg     string
	errMsg        string
	warnings      []string
}

func newMoveRepoModel(cfg *config.Config, cfgPath string, theme styles.Theme, w, h int, sourceAccountKey, sourceSourceKey, sourceRepoKey, sourceRepoPath string) moveRepoModel {
	ti := textinput.New()
	ti.Placeholder = "Type source repo key to unlock..."
	ti.CharLimit = 256

	m := moveRepoModel{
		cfg:              cfg,
		cfgPath:          cfgPath,
		theme:            theme,
		width:            w,
		height:           h,
		sourceAccountKey: sourceAccountKey,
		sourceSourceKey:  sourceSourceKey,
		sourceRepoKey:    sourceRepoKey,
		sourceRepoPath:   sourceRepoPath,
		confirmInput:     ti,
	}

	// Collect candidate destination accounts (all EXCEPT the source).
	var keys []string
	for k := range cfg.Accounts {
		if k == sourceAccountKey {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		// No other account configured — leave accountPicked=false and
		// show an error in View(). A single-account move is a rename
		// on the same provider, which is a separate concern that
		// should go through Discovery.
		return m
	}
	if len(keys) == 1 {
		m.destAccountKey = keys[0]
		m.accountPicked = true
		return m
	}
	m.destAccountKeys = keys
	m.destAccount = newSelectField("Destination account:", keys)
	return m
}

func (m moveRepoModel) Init() tea.Cmd {
	if m.accountPicked {
		return listOwnersForMoveCmd(m.cfg, m.destAccountKey)
	}
	return nil
}

func (m moveRepoModel) Update(msg tea.Msg) (moveRepoModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.moving {
			return m, nil
		}
		if key.Matches(msg, Keys.Back) {
			// Only allow ESC back when we are NOT actively moving.
			return m, func() tea.Msg { return switchScreenMsg{screen: screenDashboard} }
		}

		// Empty-state: no destination accounts at all.
		if len(m.destAccountKeys) == 0 && !m.accountPicked {
			return m, nil
		}

		// Account-picking phase.
		if !m.accountPicked {
			switch {
			case msg.String() == "left" || msg.String() == "h":
				m.destAccount.Left()
				return m, nil
			case msg.String() == "right" || msg.String() == "l":
				m.destAccount.Right()
				return m, nil
			case key.Matches(msg, Keys.Enter):
				m.destAccountKey = m.destAccount.Value()
				m.accountPicked = true
				m.loadingOwners = true
				return m, listOwnersForMoveCmd(m.cfg, m.destAccountKey)
			}
			return m, nil
		}

		// Confirm (type-to-confirm) phase.
		if m.confirmStep == 1 {
			switch {
			case key.Matches(msg, Keys.Enter):
				if m.confirmInput.Value() == m.sourceRepoKey {
					m.moving = true
					m.confirmStep = 0
					m.progress = nil
					return m, m.startMoveCmd()
				}
				m.errMsg = "Repository name does not match."
				return m, nil
			default:
				m.errMsg = ""
				var cmd tea.Cmd
				m.confirmInput, cmd = m.confirmInput.Update(msg)
				return m, cmd
			}
		}

	case moveOrgsMsg:
		m.loadingOwners = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		}
		m.initForm(msg.owners)
		return m, m.form.Init()

	case moveProgressMsg:
		m.progress = append(m.progress, msg)
		return m, waitForMoveProgress()

	case moveDoneMsg:
		m.moving = false
		if msg.result.Err != nil {
			m.errMsg = msg.result.Err.Error()
		} else {
			m.resultMsg = "Move complete."
			if msg.result.LocalCloneDeleted && msg.result.DestSourceKey != "" && msg.result.NewRepoKey != "" {
				m.resultMsg = fmt.Sprintf("Move complete. Cloning destination into %s/%s in the background.", msg.result.DestSourceKey, msg.result.NewRepoKey)
			}
			m.warnings = msg.result.Warnings
		}
		return m, nil
	}

	// Form phase input.
	if m.accountPicked && m.formReady && m.confirmStep == 0 && !m.moving {
		submitted, cmd := m.form.Update(msg)
		if submitted {
			m.confirmStep = 1
			m.confirmInput.SetValue("")
			m.confirmInput.Focus()
			m.errMsg = ""
			return m, textinput.Blink
		}
		return m, cmd
	}

	return m, nil
}

func (m *moveRepoModel) initForm(owners []string) {
	_, repoName := splitSourceRepoKey(m.sourceRepoKey)
	fields := []formField{
		newSelectFormField("Owner:          ", owners),
		newTextField("New name:", repoName, 128),
		newSelectFormField("Private:        ", []string{"yes", "no"}),
		newSelectFormField("Delete source:  ", []string{"no", "yes"}),
		newSelectFormField("Delete local:   ", []string{"no", "yes"}),
	}
	fields[moveFieldName].TextInput.SetValue(repoName)
	fields[moveFieldName].ValidateFn = func(v string) string {
		if v == "" {
			return "new name is required"
		}
		if !validRepoName.MatchString(v) {
			return "name must be alphanumeric (hyphens/dots/underscores allowed, no leading dot/hyphen)"
		}
		return ""
	}
	m.form = newFormModel("Move repository", fields, m.theme)
	m.formReady = true
}

// buildRequest collects the form values into a pkg/move Request.
func (m moveRepoModel) buildRequest() move.Request {
	owner := m.form.Fields[moveFieldOwner].Value()
	name := m.form.Fields[moveFieldName].Value()
	private := m.form.Fields[moveFieldPrivate].Value() == "yes"
	return move.Request{
		SourceAccountKey:   m.sourceAccountKey,
		SourceSourceKey:    m.sourceSourceKey,
		SourceRepoKey:      m.sourceRepoKey,
		SourceRepoPath:     m.sourceRepoPath,
		DestAccountKey:     m.destAccountKey,
		DestOwner:          owner,
		DestRepoName:       name,
		DestPrivate:        private,
		DeleteSourceRemote: m.form.Fields[moveFieldDeleteSource].Value() == "yes",
		DeleteLocalClone:   m.form.Fields[moveFieldDeleteLocal].Value() == "yes",
	}
}

// moveProgressCh is a global channel used to funnel progress events
// from the background goroutine to the Update loop. Only one move can
// run at a time inside the TUI, so a package-level buffered channel
// avoids having to thread a per-model channel through every tea.Cmd.
var moveProgressCh = make(chan moveProgressMsg, 16)

func (m moveRepoModel) startMoveCmd() tea.Cmd {
	cfg := m.cfg
	cfgPath := m.cfgPath
	req := m.buildRequest()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		result := move.Move(ctx, cfg, cfgPath, req, func(p move.Progress) {
			moveProgressCh <- moveProgressMsg{phase: p.Phase, msg: p.Message, err: p.Err}
		})
		// Signal done by pushing a sentinel; the waiter handles it.
		moveProgressCh <- moveProgressMsg{phase: move.PhaseDone, msg: "__done__", err: nil}
		// Stash the result for the waiter to pick up.
		moveResultCh <- result

		// Auto-clone after a full-move-with-delete-local. Matches
		// the GUI behaviour. Runs in the background after the
		// modal has rendered "Done" so the user can ESC back to
		// the dashboard and watch the new repo pick up a clone.
		if result.Err == nil && req.DeleteLocalClone && result.DestSourceKey != "" && result.NewRepoKey != "" {
			go autoCloneAfterMove(cfg, result.DestSourceKey, result.NewRepoKey)
		}
	}()
	return waitForMoveProgress()
}

// moveResultCh carries the final result once the goroutine exits.
var moveResultCh = make(chan move.Result, 1)

// autoCloneAfterMove clones the destination repository once a move
// has finished with DeleteLocalClone set. Mirrors the auth-URL
// construction in startCloneCmd — cloneURL builds the plain form,
// then token accounts embed the PAT and pass `credential.helper=`.
// heal.Repo runs on success to seat per-repo identity + credential
// helpers. Errors are intentionally swallowed: the move itself
// already succeeded, and the dashboard shows the repo as "not
// cloned" for the user to retry manually if this auto-clone fails.
func autoCloneAfterMove(cfg *config.Config, sourceKey, repoKey string) {
	src, ok := cfg.Sources[sourceKey]
	if !ok {
		return
	}
	repo := src.Repos[repoKey]
	acct := cfg.Accounts[src.Account]
	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	sourceFolder := src.EffectiveFolder(sourceKey)
	dest := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
	credType := repo.EffectiveCredentialType(&acct)

	plainURL := cloneURL(acct, repoKey, credType)
	cloneURLStr := plainURL
	cloneOpts := git.CloneOpts{Quiet: true}
	if credType == "token" {
		if tok, _, err := credential.ResolveToken(acct, src.Account); err == nil && tok != "" {
			if u, err := neturl.Parse(plainURL); err == nil {
				u.User = neturl.UserPassword(acct.Username, tok)
				cloneURLStr = u.String()
			}
		}
		cloneOpts.ConfigArgs = []string{"credential.helper="}
	}
	if err := git.CloneWithProgress(cloneURLStr, dest, cloneOpts, func(git.CloneProgress) {}); err == nil {
		_ = heal.Repo(cfg, sourceKey, repoKey)
	}
}

// waitForMoveProgress returns a Cmd that blocks on the next progress
// event and relays it back into Update. The sentinel "__done__" message
// is translated into moveDoneMsg so the screen can wrap up.
func waitForMoveProgress() tea.Cmd {
	return func() tea.Msg {
		p := <-moveProgressCh
		if p.msg == "__done__" {
			return moveDoneMsg{result: <-moveResultCh}
		}
		return p
	}
}

// listOwnersForMoveCmd loads the destination account's personal
// username + organizations. Mirrors listOrgsCmd from
// screen_repo_create.go but kept local to avoid cross-screen coupling.
func listOwnersForMoveCmd(cfg *config.Config, accountKey string) tea.Cmd {
	return func() tea.Msg {
		acct, ok := cfg.Accounts[accountKey]
		if !ok {
			return moveOrgsMsg{err: fmt.Errorf("account %q not found", accountKey)}
		}
		token, _, err := credential.ResolveAPIToken(acct, accountKey)
		if err != nil {
			return moveOrgsMsg{owners: []string{acct.Username}, err: err}
		}
		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			return moveOrgsMsg{owners: []string{acct.Username}, err: err}
		}
		owners := []string{acct.Username}
		if ol, ok := prov.(provider.OrgLister); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if orgs, err := ol.ListUserOrgs(ctx, acct.URL, token, acct.Username); err == nil {
				owners = append(owners, orgs...)
			}
		}
		return moveOrgsMsg{owners: owners}
	}
}

// splitSourceRepoKey splits "owner/name" → ("owner", "name").
func splitSourceRepoKey(key string) (string, string) {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

// phaseLabel maps a move.Phase to a human-readable bullet for the
// progress list. Kept independent of pkg/move so UI copy can evolve
// separately from backend naming.
func phaseLabel(p move.Phase) string {
	switch p {
	case move.PhasePreflight:
		return "Preflight"
	case move.PhaseFetch:
		return "Fetch"
	case move.PhaseCreateDest:
		return "Create destination"
	case move.PhasePushMirror:
		return "Push mirror"
	case move.PhaseRewireOrigin:
		return "Rewire origin"
	case move.PhaseDeleteSource:
		return "Delete source remote"
	case move.PhaseDeleteLocal:
		return "Delete local clone"
	case move.PhaseUpdateConfig:
		return "Update config"
	case move.PhaseDone:
		return "Done"
	default:
		return string(p)
	}
}

func (m moveRepoModel) View() string {
	var b strings.Builder
	b.WriteString(m.theme.Title.Render("Move repository") + "\n")
	b.WriteString(m.theme.TextMuted.Render(strings.Repeat("─", max(m.width, 40))) + "\n\n")

	// Preamble: what we're moving.
	b.WriteString(fmt.Sprintf("  %-18s %s\n", m.theme.TextMuted.Render("Source repo:"), m.theme.Text.Render(m.sourceRepoKey)))
	b.WriteString(fmt.Sprintf("  %-18s %s\n", m.theme.TextMuted.Render("Source account:"), m.theme.Text.Render(m.sourceAccountKey)))
	b.WriteString(fmt.Sprintf("  %-18s %s\n\n", m.theme.TextMuted.Render("Local path:"), m.theme.Text.Render(m.sourceRepoPath)))

	// Empty state (no other account).
	if len(m.destAccountKeys) == 0 && !m.accountPicked {
		warn := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError))
		b.WriteString("  " + warn.Render("No other accounts are configured. Add a second account first to move a repo across accounts/providers.") + "\n\n")
		b.WriteString(renderHints(m.theme, "ESC back"))
		return b.String()
	}

	// Account picker.
	if !m.accountPicked {
		b.WriteString("  " + m.destAccount.View(true, m.theme) + "\n\n")
		b.WriteString(renderHints(m.theme, "←→ select", "enter confirm", "ESC cancel"))
		return b.String()
	}

	// Owners loading.
	if m.loadingOwners {
		b.WriteString("  " + m.theme.TextMuted.Render(styles.SymSyncing+" Loading owners for "+m.destAccountKey+"...") + "\n")
		return b.String()
	}

	// Progress or result.
	if m.moving || len(m.progress) > 0 || m.resultMsg != "" {
		b.WriteString("  " + m.theme.Heading.Render("Progress:") + "\n")
		for _, p := range m.progress {
			sym := styles.SymClean
			color := m.theme.Palette.Clean
			if p.err != nil {
				sym = "!"
				color = m.theme.Palette.AccentWarning
			}
			if p.phase == move.PhaseDone {
				sym = styles.SymClean
				color = m.theme.Palette.Clean
			}
			line := fmt.Sprintf("    %s %s — %s",
				lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(sym),
				phaseLabel(p.phase), p.msg)
			if p.err != nil {
				line += " " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).Render("("+p.err.Error()+")")
			}
			b.WriteString(line + "\n")
		}
		if m.moving {
			b.WriteString("\n  " + m.theme.TextMuted.Render(styles.SymSyncing+" Running...") + "\n")
		}
		if m.resultMsg != "" {
			b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.Clean)).Render(styles.SymClean+" "+m.resultMsg) + "\n")
			for _, w := range m.warnings {
				b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentWarning)).Render("⚠ "+w) + "\n")
			}
			b.WriteString("\n" + renderHints(m.theme, "ESC back"))
		}
		if m.errMsg != "" {
			b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).Render("Error: "+m.errMsg) + "\n")
			b.WriteString("\n" + renderHints(m.theme, "ESC back"))
		}
		return b.String()
	}

	// Type-to-confirm step.
	if m.confirmStep == 1 {
		req := m.buildRequest()
		warn := lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.AccentDanger))
		b.WriteString("  " + warn.Render("WARNING: This is destructive.") + "\n\n")
		b.WriteString(fmt.Sprintf("  %-18s %s\n", m.theme.TextMuted.Render("Destination:"), m.theme.Text.Render(req.DestAccountKey+" → "+req.DestOwner+"/"+req.DestRepoName)))
		if req.DeleteSourceRemote {
			b.WriteString("  " + warn.Render("• Source repo on "+m.sourceAccountKey+" WILL be deleted.") + "\n")
		} else {
			b.WriteString("  " + m.theme.TextMuted.Render("• Source repo on "+m.sourceAccountKey+" will be kept.") + "\n")
		}
		if req.DeleteLocalClone {
			b.WriteString("  " + warn.Render("• Local clone at "+m.sourceRepoPath+" WILL be removed.") + "\n")
		} else {
			b.WriteString("  " + m.theme.TextMuted.Render("• Local clone will be kept; its origin will be rewired.") + "\n")
		}
		b.WriteString("\n  " + warn.Render(fmt.Sprintf("Type \"%s\" to unlock and press enter:", m.sourceRepoKey)) + "\n")
		b.WriteString("  " + m.confirmInput.View() + "\n")
		if m.errMsg != "" {
			b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).Render(m.errMsg) + "\n")
		}
		b.WriteString("\n" + renderHints(m.theme, "enter confirm", "ESC cancel"))
		return b.String()
	}

	// Form.
	if m.formReady {
		b.WriteString(m.form.View())
	}
	if m.errMsg != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(m.theme.Palette.StatusError)).Render("Error: "+m.errMsg) + "\n")
	}
	return b.String()
}
