// Package config handles loading, saving, and migrating gitbox configuration files.
package config

// Config represents the top-level gitbox configuration (v2 format).
type Config struct {
	Schema     string                `json:"$schema,omitempty"`
	Version    int                   `json:"version"`
	Global     GlobalConfig          `json:"global"`
	Accounts   map[string]Account    `json:"accounts"`
	Sources    map[string]Source     `json:"sources"`
	Mirrors    map[string]Mirror     `json:"mirrors,omitempty"`
	Workspaces map[string]Workspace  `json:"workspaces,omitempty"`

	// SourceOrder preserves the JSON key order for deterministic iteration.
	SourceOrder []string `json:"-"`
	// MirrorOrder preserves the JSON key order for deterministic iteration.
	MirrorOrder []string `json:"-"`
	// WorkspaceOrder preserves the JSON key order for deterministic iteration.
	WorkspaceOrder []string `json:"-"`
}

// OrderedSourceKeys returns source keys in JSON order (falling back to map order).
func (c *Config) OrderedSourceKeys() []string {
	if len(c.SourceOrder) > 0 {
		return c.SourceOrder
	}
	keys := make([]string, 0, len(c.Sources))
	for k := range c.Sources {
		keys = append(keys, k)
	}
	return keys
}

// GlobalConfig holds global settings.
type GlobalConfig struct {
	Folder          string         `json:"folder"`
	PeriodicSync    string         `json:"periodic_sync,omitempty"`
	Window          *WindowState   `json:"window,omitempty"`
	CompactWindow   *WindowState   `json:"compact_window,omitempty"`
	ViewMode        string         `json:"view_mode,omitempty"` // "full" or "compact"
	CredentialSSH   *SSHGlobal     `json:"credential_ssh,omitempty"`
	CredentialGCM   *GCMGlobal     `json:"credential_gcm,omitempty"`
	CredentialToken *TokenGlobal   `json:"credential_token,omitempty"`
	Editors         []EditorEntry  `json:"editors,omitempty"`
	Terminals       []TerminalEntry `json:"terminals,omitempty"`
	AIHarnesses     []AIHarnessEntry `json:"ai_harnesses,omitempty"`

	// PRBadges controls whether PR / review indicators are fetched and shown
	// on clone rows. Pointer semantics so an absent field defaults to true.
	PRBadgesEnabled *bool `json:"pr_badges_enabled,omitempty"`
	// PRIncludeDrafts controls whether draft PRs count in the "my PRs" badge.
	// Pointer semantics so an absent field defaults to true.
	PRIncludeDrafts *bool `json:"pr_include_drafts,omitempty"`
	// CheckGlobalGitignore gates the automatic startup check for the
	// recommended ~/.gitignore_global heal. Explicit actions (CLI commands,
	// TUI screen via G, GUI install button) always run regardless.
	// Pointer semantics so an absent field defaults to true — existing
	// configs and fresh installs both opt in by default.
	CheckGlobalGitignore *bool `json:"check_global_gitignore,omitempty"`
}

// PRBadgesOn reports whether PR badges are enabled, defaulting to true when unset.
func (g GlobalConfig) PRBadgesOn() bool {
	if g.PRBadgesEnabled == nil {
		return true
	}
	return *g.PRBadgesEnabled
}

// PRDraftsIncluded reports whether draft PRs should be counted, defaulting to true when unset.
func (g GlobalConfig) PRDraftsIncluded() bool {
	if g.PRIncludeDrafts == nil {
		return true
	}
	return *g.PRIncludeDrafts
}

// ShouldCheckGlobalGitignore reports whether the automatic startup check
// of ~/.gitignore_global is enabled. Returns true when unset so existing
// configs and fresh installs both opt in by default. Use this accessor
// instead of dereferencing the pointer directly.
func (g GlobalConfig) ShouldCheckGlobalGitignore() bool {
	if g.CheckGlobalGitignore == nil {
		return true
	}
	return *g.CheckGlobalGitignore
}

// EditorEntry defines a user-configured code editor for opening repositories.
type EditorEntry struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

// TerminalEntry defines a user-configured terminal emulator for opening a
// shell in a repository folder. Args uses the literal token "{path}" as a
// placeholder for the repo path; if no token is present, the path is appended
// as the final argument. Args is required here (unlike EditorEntry) because
// terminal launchers differ wildly across platforms.
//
// The literal token "{command}" is additionally supported to mark where an AI
// harness's argv should be spliced; see AIHarnessEntry for the launch model.
// For terminal-only launches the token expands to zero items (safe no-op),
// and for harness launches the token is replaced by the harness argv.
type TerminalEntry struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// AIHarnessEntry defines a user-configured AI CLI harness (e.g. claude,
// codex, gemini, aider, cursor-agent, opencode). AI harnesses are CLI-only
// and must run inside a terminal — at launch time gitbox picks the first
// entry in global.terminals and spawns
//
//	<terminal> <terminal-args-with-{command}-spliced-to-harness-argv>
//
// inside the target folder. Most harnesses need no extra flags; Args is
// usually empty.
type AIHarnessEntry struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// WindowState stores the GUI window position and size for session persistence.
type WindowState struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// TokenGlobal holds global token/PAT credential settings.
// Presence indicates the platform supports token auth; fields are for future use.
type TokenGlobal struct{}

// SSHGlobal holds global SSH credential management settings.
type SSHGlobal struct {
	SSHFolder string `json:"ssh_folder,omitempty"`
}

// GCMGlobal holds global Git Credential Manager settings.
type GCMGlobal struct {
	Helper          string `json:"helper,omitempty"`
	CredentialStore string `json:"credential_store,omitempty"`
}

// Account represents a Git provider account — WHO you are on a server.
// The map key in Config.Accounts is the human-friendly account ID,
// also used as the default first-level clone folder name.
type Account struct {
	Provider              string     `json:"provider"`
	URL                   string     `json:"url"`
	Username              string     `json:"username"`
	Name                  string     `json:"name"`
	Email                 string     `json:"email"`
	DefaultCredentialType string     `json:"default_credential_type,omitempty"`
	SSH *SSHConfig `json:"ssh,omitempty"`
	GCM *GCMConfig `json:"gcm,omitempty"`
}

// SSHConfig holds SSH authentication settings for an account.
type SSHConfig struct {
	Host     string `json:"host,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	KeyType  string `json:"key_type,omitempty"`
}

// GCMConfig holds Git Credential Manager settings for an account.
type GCMConfig struct {
	Provider    string `json:"provider,omitempty"`
	UseHTTPPath bool   `json:"useHttpPath"`
}

// Source represents WHAT you clone — references an account, contains repos.
// The map key is the source ID. The clone folder defaults to the source key
// unless overridden by the Folder field.
type Source struct {
	Account string          `json:"account"`
	Folder  string          `json:"folder,omitempty"`
	Repos   map[string]Repo `json:"repos"`

	// RepoOrder preserves the JSON key order for deterministic iteration.
	RepoOrder []string `json:"-"`
}

// OrderedRepoKeys returns repo keys in JSON order (falling back to map order).
func (s *Source) OrderedRepoKeys() []string {
	if len(s.RepoOrder) > 0 {
		return s.RepoOrder
	}
	keys := make([]string, 0, len(s.Repos))
	for k := range s.Repos {
		keys = append(keys, k)
	}
	return keys
}

// EffectiveFolder returns the clone folder for this source.
// If Folder is set, use it. Otherwise, use the source key (passed as argument).
func (s *Source) EffectiveFolder(sourceKey string) string {
	if s.Folder != "" {
		return s.Folder
	}
	return sourceKey
}

// Repo represents a single repository configuration.
// Repo names use "org/repo" format — the org part becomes the second-level folder.
type Repo struct {
	CredentialType string `json:"credential_type,omitempty"`
	Name           string `json:"name,omitempty"`
	Email          string `json:"email,omitempty"`
	IdFolder       string `json:"id_folder,omitempty"`    // overrides 2nd level dir (org). Default: part before / in repo key.
	CloneFolder    string `json:"clone_folder,omitempty"` // overrides 3rd level dir (clone name). If absolute (/ ~ ../), replaces entire path.
}

// EffectiveCredentialType returns the credential type for this repo.
// If set on the repo, use it. Otherwise, inherit from the account default.
func (r *Repo) EffectiveCredentialType(acct *Account) string {
	if r.CredentialType != "" {
		return r.CredentialType
	}
	if acct != nil {
		return acct.DefaultCredentialType
	}
	return ""
}

// OrderedMirrorKeys returns mirror keys in JSON order (falling back to map order).
func (c *Config) OrderedMirrorKeys() []string {
	if len(c.MirrorOrder) > 0 {
		return c.MirrorOrder
	}
	keys := make([]string, 0, len(c.Mirrors))
	for k := range c.Mirrors {
		keys = append(keys, k)
	}
	return keys
}

// Mirror groups repos mirrored between two accounts.
// The map key in Config.Mirrors is a human-friendly ID (e.g., "forgejo-github").
type Mirror struct {
	AccountSrc string                `json:"account_src"`
	AccountDst string                `json:"account_dst"`
	Repos    map[string]MirrorRepo `json:"repos"`

	// RepoOrder preserves the JSON key order for deterministic iteration.
	RepoOrder []string `json:"-"`
}

// OrderedRepoKeys returns mirror repo keys in JSON order (falling back to map order).
func (m *Mirror) OrderedRepoKeys() []string {
	if len(m.RepoOrder) > 0 {
		return m.RepoOrder
	}
	keys := make([]string, 0, len(m.Repos))
	for k := range m.Repos {
		keys = append(keys, k)
	}
	return keys
}

// MirrorRepo tracks a single repo mirror relationship.
// The map key is the repo full name on its origin account (e.g., "org/repo").
type MirrorRepo struct {
	Direction  string `json:"direction"`              // "push" or "pull"
	Origin     string `json:"origin"`                 // "src" or "dst" — which account owns the source of truth
	TargetRepo string `json:"target_repo,omitempty"`  // target full name; defaults to same as key
	LastSync   string `json:"last_sync,omitempty"`     // RFC3339 from last known sync
	Error      string `json:"error,omitempty"`
}

// Workspace types.
const (
	WorkspaceTypeCode        = "codeWorkspace"
	WorkspaceTypeTmuxinator  = "tmuxinator"
	WorkspaceLayoutWindows   = "windowsPerRepo"
	WorkspaceLayoutSplit     = "splitPanes"
)

// Workspace bundles a set of clones that belong together for a task.
// The map key in Config.Workspaces is the human-friendly workspace ID.
type Workspace struct {
	Type       string            `json:"type"`                  // "codeWorkspace" | "tmuxinator"
	Name       string            `json:"name,omitempty"`        // human-friendly display name
	File       string            `json:"file,omitempty"`        // absolute path to generated file on disk
	Layout     string            `json:"layout,omitempty"`      // tmuxinator only: "windowsPerRepo" | "splitPanes"
	Members    []WorkspaceMember `json:"members"`               // ordered list of member clones
	Discovered bool              `json:"discovered,omitempty"`  // true if adopted from disk
}

// WorkspaceMember references a single clone (by source + repo) inside a workspace.
type WorkspaceMember struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
}

// OrderedWorkspaceKeys returns workspace keys in JSON order (falling back to map order).
func (c *Config) OrderedWorkspaceKeys() []string {
	if len(c.WorkspaceOrder) > 0 {
		return c.WorkspaceOrder
	}
	keys := make([]string, 0, len(c.Workspaces))
	for k := range c.Workspaces {
		keys = append(keys, k)
	}
	return keys
}

// EffectiveName returns Name if set, otherwise the workspace key.
func (w *Workspace) EffectiveName(key string) string {
	if w.Name != "" {
		return w.Name
	}
	return key
}

// GetAccount resolves the account for a source. Returns nil if not found.
func (c *Config) GetAccount(sourceName string) *Account {
	src, ok := c.Sources[sourceName]
	if !ok {
		return nil
	}
	acct, ok := c.Accounts[src.Account]
	if !ok {
		return nil
	}
	return &acct
}
