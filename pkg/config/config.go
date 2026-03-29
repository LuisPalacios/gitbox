// Package config handles loading, saving, and migrating gitbox configuration files.
package config

// Config represents the top-level gitbox configuration (v2 format).
type Config struct {
	Schema   string              `json:"$schema,omitempty"`
	Version  int                 `json:"version"`
	Global   GlobalConfig        `json:"global"`
	Accounts map[string]Account  `json:"accounts"`
	Sources  map[string]Source   `json:"sources"`

	// SourceOrder preserves the JSON key order for deterministic iteration.
	SourceOrder []string `json:"-"`
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
	Folder          string       `json:"folder"`
	PeriodicSync    string       `json:"periodic_sync,omitempty"`
	Window          *WindowState `json:"window,omitempty"`
	CompactWindow   *WindowState `json:"compact_window,omitempty"`
	ViewMode        string       `json:"view_mode,omitempty"` // "full" or "compact"
	CredentialSSH   *SSHGlobal   `json:"credential_ssh,omitempty"`
	CredentialGCM   *GCMGlobal   `json:"credential_gcm,omitempty"`
	CredentialToken *TokenGlobal `json:"credential_token,omitempty"`
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
	DefaultBranch         string     `json:"default_branch,omitempty"`
	DefaultCredentialType string     `json:"default_credential_type,omitempty"`
	SSH                   *SSHConfig   `json:"ssh,omitempty"`
	GCM                   *GCMConfig   `json:"gcm,omitempty"`
	Token                 *TokenConfig `json:"token,omitempty"`
}

// TokenConfig holds token (PAT) authentication settings for an account.
// The token itself is NEVER stored here — it lives in the OS credential
// store (via git's credential helper) or in environment variables.
type TokenConfig struct {
	EnvVar string `json:"env_var,omitempty"` // custom env var name override (default: GITBOX_TOKEN_<ACCOUNT_KEY>)
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
