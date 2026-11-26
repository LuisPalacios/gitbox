package config

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// RemoteEntry is one remote-mode destination (#233). Alias is an ssh_config
// Host alias that only the system ssh resolves: gitbox stores no hostname,
// user or key.
type RemoteEntry struct {
	Alias string `json:"alias"`
	// ServerDir overrides where gitbox-server is installed on the remote,
	// for homes mounted noexec. Empty means the default under the home.
	ServerDir string `json:"server_dir,omitempty"`
	// Editor is the remote-capable editor command. Empty means auto.
	Editor string `json:"editor,omitempty"`
	// ServerOS, ServerArch and ServerVersion are what the host's server
	// reported the last time this remote's window connected, and
	// LastConnected when (RFC 3339). The Remote hosts window shows them
	// (#330); nothing else depends on them.
	ServerOS      string `json:"server_os,omitempty"`
	ServerArch    string `json:"server_arch,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`
	LastConnected string `json:"last_connected,omitempty"`
	// Window holds the geometry of this remote's own window, so it never
	// fights the local window over global.window.
	Window *WindowState `json:"window,omitempty"`
}

// ClientPrefs are the global keys that belong to the machine showing the
// window, not to the host that has the data. In remote mode the client reads
// and writes them in its own gitbox.json and lays them over the config that
// comes from the server. Every other global key is domain data.
//
// SidebarCollapsed is collapsed.sidebar: the rest of collapsed names
// accounts, sources and repos, so it stays with the domain.
type ClientPrefs struct {
	Theme            string
	Language         string
	UIMode           string
	ShowPullPlan     *bool
	Window           *WindowState
	SidebarCollapsed bool
	Editors          []EditorEntry
	Terminals        []TerminalEntry
	TerminalApps     []TerminalApp
	Shells           []ShellEntry
	TerminalProfiles []TerminalProfile
	PeriodicSync     string

	Remotes    []RemoteEntry
	RemoteTint string
}

// ExtractPrefs returns a detached copy of the client keys of c.
func ExtractPrefs(c *Config) ClientPrefs {
	g := &c.Global
	p := ClientPrefs{
		Theme:            g.Theme,
		Language:         g.Language,
		UIMode:           g.UIMode,
		ShowPullPlan:     g.ShowPullPlan,
		Window:           g.Window,
		SidebarCollapsed: g.Collapsed != nil && g.Collapsed.Sidebar,
		Editors:          g.Editors,
		Terminals:        g.Terminals,
		TerminalApps:     g.TerminalApps,
		Shells:           g.Shells,
		TerminalProfiles: g.TerminalProfiles,
		PeriodicSync:     g.PeriodicSync,
		Remotes:          g.Remotes,
		RemoteTint:       g.RemoteTint,
	}
	return p.clone()
}

// ApplyPrefs overwrites the client keys of c with a detached copy of p. It
// leaves every domain key alone, including the rest of collapsed.
func ApplyPrefs(c *Config, p ClientPrefs) {
	p = p.clone()
	g := &c.Global
	g.Theme = p.Theme
	g.Language = p.Language
	g.UIMode = p.UIMode
	g.ShowPullPlan = p.ShowPullPlan
	g.Window = p.Window
	g.Editors = p.Editors
	g.Terminals = p.Terminals
	g.TerminalApps = p.TerminalApps
	g.Shells = p.Shells
	g.TerminalProfiles = p.TerminalProfiles
	g.PeriodicSync = p.PeriodicSync
	g.Remotes = p.Remotes
	g.RemoteTint = p.RemoteTint

	switch {
	case p.SidebarCollapsed && g.Collapsed == nil:
		g.Collapsed = &CollapsedState{Sidebar: true}
	case g.Collapsed != nil:
		// Copy before writing: the state may be shared with another Config.
		cs := *g.Collapsed
		cs.Sidebar = p.SidebarCollapsed
		g.Collapsed = &cs
	}
}

// StripClientPrefs zeroes the client keys of c. The server uses it on every
// config it sends: the client supplies its own.
func StripClientPrefs(c *Config) {
	ApplyPrefs(c, ClientPrefs{})
	if cs := c.Global.Collapsed; cs != nil && len(cs.Sources) == 0 && len(cs.Repos) == 0 && cs.View == "" {
		c.Global.Collapsed = nil
	}
}

// PreserveClientPrefs copies the client keys of onDisk into dst. The server
// calls it before every save, so a host that also runs GitboxApp locally
// keeps its own theme, editors, window geometry and remotes.
func PreserveClientPrefs(dst, onDisk *Config) {
	ApplyPrefs(dst, ExtractPrefs(onDisk))
}

// WindowFor returns the saved geometry of the window for a destination: the
// local window when alias is empty, otherwise that remote's own window.
func (p *ClientPrefs) WindowFor(alias string) *WindowState {
	if alias == "" {
		return p.Window
	}
	if r := p.remote(alias); r != nil {
		return r.Window
	}
	return nil
}

// SetWindow stores the geometry of the window for a destination. It reports
// false when alias names no remote.
func (p *ClientPrefs) SetWindow(alias string, ws *WindowState) bool {
	if alias == "" {
		p.Window = ws
		return true
	}
	r := p.remote(alias)
	if r == nil {
		return false
	}
	r.Window = ws
	return true
}

// RemoteFacts are the last-known facts of a remote (#330).
type RemoteFacts struct {
	ServerOS, ServerArch, ServerVersion string
	LastConnected                       time.Time
}

// SetRemoteFacts records what a remote's server reported on connecting. It
// reports false when alias names no remote.
func (p *ClientPrefs) SetRemoteFacts(alias string, f RemoteFacts) bool {
	r := p.remote(alias)
	if r == nil {
		return false
	}
	r.ServerOS, r.ServerArch, r.ServerVersion = f.ServerOS, f.ServerArch, f.ServerVersion
	r.LastConnected = f.LastConnected.UTC().Format(time.RFC3339)
	return true
}

// ForgetRemoteServer clears the server version recorded for a remote after
// its server was uninstalled (#336). The host's system and the last
// connection stay: they still describe the host. It reports false when
// alias names no remote.
func (p *ClientPrefs) ForgetRemoteServer(alias string) bool {
	r := p.remote(alias)
	if r == nil {
		return false
	}
	r.ServerVersion = ""
	return true
}

func (p *ClientPrefs) remote(alias string) *RemoteEntry {
	for i := range p.Remotes {
		if p.Remotes[i].Alias == alias {
			return &p.Remotes[i]
		}
	}
	return nil
}

func (p ClientPrefs) clone() ClientPrefs {
	if p.ShowPullPlan != nil {
		show := *p.ShowPullPlan
		p.ShowPullPlan = &show
	}
	p.Window = cloneWindow(p.Window)
	p.Editors = slices.Clone(p.Editors)
	p.Terminals = slices.Clone(p.Terminals)
	for i := range p.Terminals {
		p.Terminals[i].Args = slices.Clone(p.Terminals[i].Args)
	}
	p.TerminalApps = slices.Clone(p.TerminalApps)
	for i := range p.TerminalApps {
		p.TerminalApps[i].ArgsTemplate = slices.Clone(p.TerminalApps[i].ArgsTemplate)
	}
	p.Shells = slices.Clone(p.Shells)
	for i := range p.Shells {
		p.Shells[i].Args = slices.Clone(p.Shells[i].Args)
	}
	p.TerminalProfiles = slices.Clone(p.TerminalProfiles)
	for i := range p.TerminalProfiles {
		p.TerminalProfiles[i].Args = slices.Clone(p.TerminalProfiles[i].Args)
	}
	p.Remotes = slices.Clone(p.Remotes)
	for i := range p.Remotes {
		p.Remotes[i].Window = cloneWindow(p.Remotes[i].Window)
	}
	return p
}

func cloneWindow(ws *WindowState) *WindowState {
	if ws == nil {
		return nil
	}
	c := *ws
	return &c
}

// remoteAliasRE keeps an alias a single ssh argument that can never be read
// as an option or carry shell syntax.
var remoteAliasRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RemoteTintPresets are the colors a remote window can be tinted with
// (#331). The first one is the default. Only these are accepted: the
// frontend's contrast test covers each of them in both themes.
var RemoteTintPresets = []string{"#3b82f6", "#14b8a6", "#8b5cf6", "#f59e0b", "#f43f5e", "#22c55e"}

// DefaultRemoteTint is the tint of a configuration without the key.
var DefaultRemoteTint = RemoteTintPresets[0]

// RemoteTintOf is the tint a saved value stands for: the value when it is a
// preset, otherwise the default blue.
func RemoteTintOf(saved string) string {
	saved = strings.ToLower(strings.TrimSpace(saved))
	if slices.Contains(RemoteTintPresets, saved) {
		return saved
	}
	return DefaultRemoteTint
}

// ValidRemoteAlias reports whether alias may name a remote: 1 to 64 letters,
// digits, '.', '_' or '-', not starting with '-'.
func ValidRemoteAlias(alias string) bool {
	return remoteAliasRE.MatchString(alias) && !strings.HasPrefix(alias, "-")
}

// remoteDriveRE matches the start of an absolute path on a Windows remote.
var remoteDriveRE = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// ValidateRemotes checks global.remotes: well-formed unique aliases, and a
// server_dir that is absolute or starts with "~/".
func ValidateRemotes(remotes []RemoteEntry) error {
	seen := make(map[string]bool, len(remotes))
	for i, r := range remotes {
		if !ValidRemoteAlias(r.Alias) {
			return fmt.Errorf("global.remotes[%d]: alias %q must be 1 to 64 letters, digits, '.', '_' or '-', and not start with '-'", i, r.Alias)
		}
		if seen[r.Alias] {
			return fmt.Errorf("global.remotes[%d]: duplicate alias %q", i, r.Alias)
		}
		seen[r.Alias] = true
		if d := r.ServerDir; d != "" && !strings.HasPrefix(d, "/") && !strings.HasPrefix(d, "~/") && !remoteDriveRE.MatchString(d) {
			return fmt.Errorf("global.remotes[%d]: server_dir %q must be absolute or start with \"~/\"", i, d)
		}
	}
	return nil
}
