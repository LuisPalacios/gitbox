package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"os/exec"
	"strings"
	"sync"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails application struct. All exported methods become
// frontend bindings via window.go.main.App.<Method>().
type App struct {
	ctx     context.Context
	cfg     *config.Config
	cfgPath string
	mu      sync.Mutex
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// ─── Lifecycle ────────────────────────────────────────────────

// Startup is called by Wails at application start.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.cfgPath = config.DefaultV2Path()
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		// Config doesn't exist yet — start with empty config for onboarding.
		cfg = &config.Config{
			Schema:   "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/gitbox.schema.json",
			Version:  2,
			Accounts: make(map[string]config.Account),
			Sources:  make(map[string]config.Source),
		}
	}
	a.cfg = cfg
}

// Shutdown is called by Wails when the app is closing.
func (a *App) Shutdown(_ context.Context) {}

// DomReady is called after the frontend DOM is ready.
// We start hidden and show the window here to prevent flickering.
func (a *App) DomReady(_ context.Context) {
	wailsrt.WindowShow(a.ctx)
}

// GetAppVersion returns the application version string for the frontend.
func (a *App) GetAppVersion() string {
	if version == "dev" {
		return fmt.Sprintf("dev-%s", commit)
	}
	return fmt.Sprintf("%s (%s)", version, commit[:minInt(7, len(commit))])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── Config ───────────────────────────────────────────────────

// ConfigDTO is the JSON-friendly config sent to the frontend.
type ConfigDTO struct {
	Version  int                        `json:"version"`
	Global   config.GlobalConfig        `json:"global"`
	Accounts map[string]config.Account  `json:"accounts"`
	Sources  map[string]SourceDTO       `json:"sources"`
}

// SourceDTO mirrors config.Source but exposes repos as a map.
type SourceDTO struct {
	Account   string                 `json:"account"`
	Folder    string                 `json:"folder,omitempty"`
	Repos     map[string]config.Repo `json:"repos"`
	RepoOrder []string               `json:"repoOrder"`
}

// GetConfig returns the full configuration for the frontend.
func (a *App) GetConfig() ConfigDTO {
	a.mu.Lock()
	defer a.mu.Unlock()

	sources := make(map[string]SourceDTO, len(a.cfg.Sources))
	for key, src := range a.cfg.Sources {
		sources[key] = SourceDTO{
			Account:   src.Account,
			Folder:    src.Folder,
			Repos:     src.Repos,
			RepoOrder: src.OrderedRepoKeys(),
		}
	}
	return ConfigDTO{
		Version:  a.cfg.Version,
		Global:   a.cfg.Global,
		Accounts: a.cfg.Accounts,
		Sources:  sources,
	}
}

// ReloadConfig re-reads the config from disk. Call after external edits
// or when the window regains focus.
func (a *App) ReloadConfig() (ConfigDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		return ConfigDTO{}, fmt.Errorf("reloading config: %w", err)
	}
	a.cfg = cfg

	sources := make(map[string]SourceDTO, len(a.cfg.Sources))
	for key, src := range a.cfg.Sources {
		sources[key] = SourceDTO{
			Account:   src.Account,
			Folder:    src.Folder,
			Repos:     src.Repos,
			RepoOrder: src.OrderedRepoKeys(),
		}
	}
	return ConfigDTO{
		Version:  a.cfg.Version,
		Global:   a.cfg.Global,
		Accounts: a.cfg.Accounts,
		Sources:  sources,
	}, nil
}

// GetConfigPath returns the path to the config file.
func (a *App) GetConfigPath() string {
	return a.cfgPath
}

// GetGlobalFolder returns the expanded clone base folder.
func (a *App) GetGlobalFolder() string {
	return config.ExpandTilde(a.cfg.Global.Folder)
}

// OpenFileInEditor opens a file with the OS default application.
func (a *App) OpenFileInEditor(path string) error {
	var cmd *exec.Cmd
	switch {
	case isWindows():
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case isDarwin():
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func isWindows() bool {
	return os.PathSeparator == '\\' || strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

func isDarwin() bool {
	return strings.Contains(strings.ToLower(os.Getenv("OSTYPE")), "darwin") ||
		exec.Command("uname").Run() == nil && func() bool {
			out, _ := exec.Command("uname").Output()
			return strings.TrimSpace(string(out)) == "Darwin"
		}()
}

// ─── Global Folder ────────────────────────────────────────────

// IsFirstRun returns true if no global folder is configured (fresh install).
func (a *App) IsFirstRun() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Global.Folder == ""
}

// PickFolder opens the native OS directory picker and returns the selected path.
// Returns empty string if the user cancels.
func (a *App) PickFolder(title string) string {
	dir, err := wailsrt.OpenDirectoryDialog(a.ctx, wailsrt.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return ""
	}
	return dir
}

// SetGlobalFolder validates and sets the global clone folder, then saves config.
// Creates the directory if it doesn't exist.
func (a *App) SetGlobalFolder(folder string) error {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return fmt.Errorf("folder path cannot be empty")
	}

	expanded := config.ExpandTilde(folder)

	// Create the directory if it doesn't exist.
	if err := os.MkdirAll(expanded, 0o755); err != nil {
		return fmt.Errorf("creating folder %s: %w", expanded, err)
	}

	// Verify it's actually a directory.
	info, err := os.Stat(expanded)
	if err != nil {
		return fmt.Errorf("checking folder %s: %w", expanded, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", expanded)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.cfg.Global.Folder = folder
	return config.Save(a.cfg, a.cfgPath)
}

// ─── Status ───────────────────────────────────────────────────

// StatusResult is the frontend-friendly repo status.
type StatusResult struct {
	Source    string `json:"source"`
	Repo     string `json:"repo"`
	Account  string `json:"account"`
	Path     string `json:"path"`
	State    string `json:"state"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Modified int    `json:"modified"`
	Untracked int  `json:"untracked"`
	Conflicts int  `json:"conflicts"`
	Error    string `json:"error,omitempty"`
}

func toStatusResult(rs status.RepoStatus) StatusResult {
	return StatusResult{
		Source:    rs.Source,
		Repo:     rs.Repo,
		Account:  rs.Account,
		Path:     rs.Path,
		State:    rs.State.String(),
		Ahead:    rs.Ahead,
		Behind:   rs.Behind,
		Modified: rs.Modified,
		Untracked: rs.Untracked,
		Conflicts: rs.Conflicts,
		Error:    rs.ErrorMsg,
	}
}

// GetAllStatus returns the sync status of every configured repo.
func (a *App) GetAllStatus() []StatusResult {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	raw := status.CheckAll(cfg)
	results := make([]StatusResult, len(raw))
	for i, rs := range raw {
		results[i] = toStatusResult(rs)
	}
	return results
}

// RefreshStatus checks all repos and emits a "status:updated" event.
func (a *App) RefreshStatus() {
	go func() {
		results := a.GetAllStatus()
		wailsrt.EventsEmit(a.ctx, "status:updated", results)
	}()
}

// ─── Clone ────────────────────────────────────────────────────

// cloneURL builds the clone URL for a repo based on credential type.
func (a *App) cloneURL(acct config.Account, repoKey string, credType string) string {
	switch credType {
	case "ssh":
		host := acct.URL
		if acct.SSH != nil && acct.SSH.Host != "" {
			host = acct.SSH.Host
		} else {
			// Strip scheme for SSH.
			host = stripScheme(acct.URL)
		}
		return fmt.Sprintf("git@%s:%s.git", host, repoKey)
	default:
		// Embed username in HTTPS URL so GCM matches the stored credential
		// (e.g. https://User@github.com/User/repo.git).
		u, err := url.Parse(acct.URL)
		if err == nil && acct.Username != "" {
			u.User = url.User(acct.Username)
			return fmt.Sprintf("%s/%s.git", u.String(), repoKey)
		}
		return fmt.Sprintf("%s/%s.git", acct.URL, repoKey)
	}
}

// CloneRepo clones a single repo. Emits clone:progress and clone:done events.
func (a *App) CloneRepo(sourceKey, repoKey string) {
	go func() {
		a.mu.Lock()
		src, ok := a.cfg.Sources[sourceKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "clone:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("source %q not found", sourceKey),
			})
			return
		}
		repo, ok := src.Repos[repoKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "clone:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("repo %q not found in source %q", repoKey, sourceKey),
			})
			return
		}
		acct := a.cfg.Accounts[src.Account]
		globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
		sourceFolder := src.EffectiveFolder(sourceKey)
		a.mu.Unlock()

		dest := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
		credType := repo.EffectiveCredentialType(&acct)
		url := a.cloneURL(acct, repoKey, credType)

		err := git.CloneWithProgress(url, dest, git.CloneOpts{Quiet: true},
			func(p git.CloneProgress) {
				wailsrt.EventsEmit(a.ctx, "clone:progress", map[string]interface{}{
					"source": sourceKey, "repo": repoKey,
					"phase": p.Phase, "percent": p.Percent,
				})
			})

		result := map[string]interface{}{"source": sourceKey, "repo": repoKey}
		if err != nil {
			result["error"] = err.Error()
		}
		wailsrt.EventsEmit(a.ctx, "clone:done", result)
	}()
}

// ─── Pull ─────────────────────────────────────────────────────

// PullRepo pulls a single repo. Emits pull:done event.
func (a *App) PullRepo(sourceKey, repoKey string) {
	go func() {
		a.mu.Lock()
		src, ok := a.cfg.Sources[sourceKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "pull:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("source %q not found", sourceKey),
			})
			return
		}
		repo, ok := src.Repos[repoKey]
		if !ok {
			a.mu.Unlock()
			wailsrt.EventsEmit(a.ctx, "pull:done", map[string]string{
				"source": sourceKey, "repo": repoKey,
				"error": fmt.Sprintf("repo %q not found in source %q", repoKey, sourceKey),
			})
			return
		}
		globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
		sourceFolder := src.EffectiveFolder(sourceKey)
		a.mu.Unlock()

		path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
		err := git.PullQuiet(path)

		result := map[string]interface{}{"source": sourceKey, "repo": repoKey}
		if err != nil {
			result["error"] = err.Error()
		}
		wailsrt.EventsEmit(a.ctx, "pull:done", result)
	}()
}

// ─── Account CRUD ─────────────────────────────────────────────

// AddAccountRequest is the frontend payload for creating an account.
type AddAccountRequest struct {
	Key            string `json:"key"`
	Provider       string `json:"provider"`
	URL            string `json:"url"`
	Username       string `json:"username"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	DefaultBranch  string `json:"defaultBranch"`
	CredentialType string `json:"credentialType"`
}

// AddAccount creates a new account and a matching source, then saves.
// Populates the credential sub-objects (GCM/SSH/Token) based on credential type.
func (a *App) AddAccount(req AddAccountRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acct := config.Account{
		Provider:              req.Provider,
		URL:                   req.URL,
		Username:              req.Username,
		Name:                  req.Name,
		Email:                 req.Email,
		DefaultBranch:         req.DefaultBranch,
		DefaultCredentialType: req.CredentialType,
	}
	if acct.DefaultBranch == "" {
		acct.DefaultBranch = "main"
	}
	if acct.DefaultCredentialType == "" {
		acct.DefaultCredentialType = "gcm"
	}

	// Populate credential sub-objects.
	switch acct.DefaultCredentialType {
	case "gcm":
		acct.GCM = &config.GCMConfig{
			Provider:    inferGCMProvider(acct.Provider),
			UseHTTPPath: false,
		}
	case "ssh":
		hostname := hostnameFromURL(acct.URL)
		acct.SSH = &config.SSHConfig{
			Host:     credential.SSHHostAlias(req.Key),
			Hostname: hostname,
			KeyType:  "ed25519",
		}
	case "token":
		acct.Token = &config.TokenConfig{}
	}

	if err := a.cfg.AddAccount(req.Key, acct); err != nil {
		return err
	}

	// Create a matching source so repos can be added immediately.
	src := config.Source{
		Account: req.Key,
		Repos:   make(map[string]config.Repo),
	}
	if err := a.cfg.AddSource(req.Key, src); err != nil {
		_ = a.cfg.DeleteAccount(req.Key)
		return err
	}

	return config.Save(a.cfg, a.cfgPath)
}

// ─── Credential Setup ─────────────────────────────────────────

// CredentialSetupResult is the outcome of a credential setup step.
type CredentialSetupResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// CredentialSetupGCM triggers GCM browser authentication for an account.
func (a *App) CredentialSetupGCM(accountKey string) CredentialSetupResult {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}

	// Ensure global git config has the GCM credential sections BEFORE
	// any fill/approve — git needs to know the credential helper to store it.
	a.mu.Lock()
	ensureGlobalCredentialConfig(a.cfg)
	a.mu.Unlock()

	host := hostnameFromURL(acct.URL)

	// Check if GCM already has a stored credential.
	_, _, err := credential.ResolveGCMToken(acct.URL, acct.Username)
	if err == nil {
		return CredentialSetupResult{OK: true, Message: fmt.Sprintf("GCM credential already stored for %s@%s", acct.Username, host)}
	}

	// Trigger interactive git credential fill — GCM opens the browser.
	// Stderr MUST be connected so GCM can launch its browser auth window.
	input := fmt.Sprintf("protocol=https\nhost=%s\nusername=%s\n\n", host, acct.Username)

	fillCmd := exec.Command(git.GitBin(), "credential", "fill")
	fillCmd.Stdin = strings.NewReader(input)
	var stderrBuf bytes.Buffer
	fillCmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	out, err := fillCmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" {
			detail = err.Error()
		}
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("GCM authentication failed for %s@%s: %s", acct.Username, host, detail)}
	}

	// Parse the fill output — extract password and the actual username GCM used.
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
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("GCM returned no credential for %s@%s", acct.Username, host)}
	}

	// If GCM returned a different username casing (e.g. "Jacopalas" vs "jacopalas"),
	// update the account config so future lookups match.
	if realUsername != acct.Username {
		a.mu.Lock()
		acct.Username = realUsername
		_ = a.cfg.UpdateAccount(accountKey, acct)
		_ = config.Save(a.cfg, a.cfgPath)
		a.mu.Unlock()
	}

	// Approve so git stores it persistently.
	approveCmd := exec.Command(git.GitBin(), "credential", "approve")
	approveCmd.Stdin = strings.NewReader(string(out))
	approveCmd.Stderr = os.Stderr
	if err := approveCmd.Run(); err != nil {
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("git credential approve failed: %v", err)}
	}

	// Verify the credential was actually persisted (using the real username).
	_, _, err = credential.ResolveGCMToken(acct.URL, realUsername)
	if err != nil {
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("Credential approved but verification failed for %s@%s: %v", realUsername, host, err)}
	}

	return CredentialSetupResult{OK: true, Message: fmt.Sprintf("GCM credential stored for %s@%s", realUsername, host)}
}

// CredentialStoreToken stores a PAT in the OS keyring for an account.
func (a *App) CredentialStoreToken(accountKey, token string) CredentialSetupResult {
	a.mu.Lock()
	_, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}
	if strings.TrimSpace(token) == "" {
		return CredentialSetupResult{OK: false, Message: "Token cannot be empty"}
	}

	if err := credential.StoreToken(accountKey, strings.TrimSpace(token)); err != nil {
		return CredentialSetupResult{OK: false, Message: err.Error()}
	}
	return CredentialSetupResult{OK: true, Message: "Token stored in OS keyring"}
}

// TokenGuideInfo returns the PAT creation URL and guidance for an account.
type TokenGuideInfo struct {
	CreationURL string `json:"creationURL"`
	Guide       string `json:"guide"`
}

// GetTokenGuide returns provider-specific PAT creation info.
func (a *App) GetTokenGuide(accountKey string) TokenGuideInfo {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return TokenGuideInfo{}
	}
	return TokenGuideInfo{
		CreationURL: provider.TokenCreationURL(acct.Provider, acct.URL),
		Guide:       provider.TokenSetupGuide(acct.Provider, acct.URL, accountKey),
	}
}

// CredentialSetupSSH generates an SSH key and configures ~/.ssh/config.
func (a *App) CredentialSetupSSH(accountKey string) CredentialSetupResult {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	cfg := a.cfg
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}

	hostAlias := credential.SSHHostAlias(accountKey)
	keyType := "ed25519"
	if acct.SSH != nil && acct.SSH.KeyType != "" {
		keyType = acct.SSH.KeyType
	}
	hostname := hostnameFromURL(acct.URL)
	if acct.SSH != nil && acct.SSH.Hostname != "" {
		hostname = acct.SSH.Hostname
	}

	sshFolder := "~/.ssh"
	if cfg.Global.CredentialSSH != nil && cfg.Global.CredentialSSH.SSHFolder != "" {
		sshFolder = cfg.Global.CredentialSSH.SSHFolder
	}
	sshFolder = config.ExpandTilde(sshFolder)

	// Ensure ~/.ssh exists.
	if err := os.MkdirAll(sshFolder, 0o700); err != nil {
		return CredentialSetupResult{OK: false, Message: fmt.Sprintf("Creating %s: %v", sshFolder, err)}
	}

	// Write ~/.ssh/config entry if missing.
	found, _ := credential.FindSSHConfigEntry(sshFolder, hostAlias)
	if !found {
		opts := credential.SSHConfigEntryOpts{
			Host:     hostAlias,
			Hostname: hostname,
			KeyFile:  fmt.Sprintf("~/.ssh/gitbox-%s-sshkey", accountKey),
			Username: acct.Username,
			Name:     acct.Name,
			Email:    acct.Email,
			URL:      acct.URL,
		}
		if err := credential.WriteSSHConfigEntry(sshFolder, opts); err != nil {
			return CredentialSetupResult{OK: false, Message: fmt.Sprintf("Writing SSH config: %v", err)}
		}
	}

	// Generate SSH key if missing.
	keyPath := credential.SSHKeyPath(sshFolder, accountKey)
	if _, err := os.Stat(keyPath); err != nil {
		if _, err := credential.GenerateSSHKey(sshFolder, accountKey, keyType); err != nil {
			return CredentialSetupResult{OK: false, Message: fmt.Sprintf("Generating SSH key: %v", err)}
		}
	}

	// Update account SSH config in gitbox.json.
	a.mu.Lock()
	if acct.SSH == nil || acct.SSH.Host != hostAlias {
		if acct.SSH == nil {
			acct.SSH = &config.SSHConfig{}
		}
		acct.SSH.Host = hostAlias
		acct.SSH.Hostname = hostname
		acct.SSH.KeyType = keyType
		_ = a.cfg.UpdateAccount(accountKey, acct)
		_ = config.Save(a.cfg, a.cfgPath)
	}
	a.mu.Unlock()

	// Read public key for display.
	pubKey, _ := credential.ReadPublicKey(keyPath)

	// Test SSH connection.
	if _, sshErr := credential.TestSSHConnection(hostAlias); sshErr != nil {
		addURL := credential.SSHPublicKeyURL(acct.Provider, acct.URL)
		msg := fmt.Sprintf("SSH key generated. Add this public key to your provider:\n\n%s\n\nPaste at: %s", pubKey, addURL)
		return CredentialSetupResult{OK: false, Message: msg}
	}

	return CredentialSetupResult{OK: true, Message: "SSH configured and connection verified"}
}

// inferGCMProvider maps a gitbox provider name to the GCM provider hint.
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

// hostnameFromURL extracts the hostname from a URL.
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

// ensureGlobalCredentialConfig sets global git credential config for GCM accounts.
func ensureGlobalCredentialConfig(cfg *config.Config) {
	if cfg.Global.CredentialGCM != nil {
		if cfg.Global.CredentialGCM.Helper != "" {
			_ = git.GlobalConfigSet("credential.helper", cfg.Global.CredentialGCM.Helper)
		}
		if cfg.Global.CredentialGCM.CredentialStore != "" {
			_ = git.GlobalConfigSet("credential.credentialStore", cfg.Global.CredentialGCM.CredentialStore)
		}
	}
	seen := make(map[string]bool)
	for _, acct := range cfg.Accounts {
		if acct.DefaultCredentialType != "gcm" {
			continue
		}
		host := strings.TrimSuffix(acct.URL, "/")
		if seen[host] {
			continue
		}
		seen[host] = true
		gcmProv := ""
		if acct.GCM != nil && acct.GCM.Provider != "" {
			gcmProv = acct.GCM.Provider
		}
		section := fmt.Sprintf("credential.%s", host)
		if gcmProv != "" {
			_ = git.GlobalConfigSet(section+".provider", gcmProv)
		}
		_ = git.GlobalConfigSet(section+".useHttpPath", "false")
	}
}

// ─── Delete ───────────────────────────────────────────────────

// DeleteRepo removes a repo from the config and deletes its local folder.
// Returns an error message or empty string on success.
func (a *App) DeleteRepo(sourceKey, repoKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		return fmt.Errorf("source %q not found", sourceKey)
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		return fmt.Errorf("repo %q not found in source %q", repoKey, sourceKey)
	}

	// Resolve the local path and remove the folder if it exists.
	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)
	sourceFolder := src.EffectiveFolder(sourceKey)
	path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)

	if git.IsRepo(path) {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("deleting folder %s: %w", path, err)
		}
	}

	// Remove from config and save.
	if err := a.cfg.DeleteRepo(sourceKey, repoKey); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// DeleteAccount removes an account, all its sources, and all local clone folders.
func (a *App) DeleteAccount(accountKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, ok := a.cfg.Accounts[accountKey]; !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	globalFolder := config.ExpandTilde(a.cfg.Global.Folder)

	// Delete all clone folders and source directories for this account.
	for sourceKey, src := range a.cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if git.IsRepo(path) {
				_ = os.RemoveAll(path)
			}
		}
		// Remove the entire source directory (e.g. ~/00.git/github-Jacopalas/).
		sourcePath := filepath.Join(globalFolder, sourceFolder)
		_ = os.RemoveAll(sourcePath)
		delete(a.cfg.Sources, sourceKey)
	}

	// Delete the account.
	delete(a.cfg.Accounts, accountKey)

	return config.Save(a.cfg, a.cfgPath)
}

// ChangeCredentialType changes an account's credential type and populates the
// appropriate sub-object. Returns the updated account key for re-rendering.
func (a *App) ChangeCredentialType(accountKey, newType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acct, ok := a.cfg.Accounts[accountKey]
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	acct.DefaultCredentialType = newType

	// Clear old credential sub-objects and populate the new one.
	acct.GCM = nil
	acct.SSH = nil
	acct.Token = nil

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
	case "token":
		acct.Token = &config.TokenConfig{}
	}

	if err := a.cfg.UpdateAccount(accountKey, acct); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// ─── Discovery ────────────────────────────────────────────────

// DiscoverResult is a repo found during discovery.
type DiscoverResult struct {
	FullName    string `json:"fullName"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	Fork        bool   `json:"fork"`
	Archived    bool   `json:"archived"`
}

// Discover lists remote repos for an account. Emits discover:done event.
func (a *App) Discover(accountKey string) {
	go func() {
		a.mu.Lock()
		acct, ok := a.cfg.Accounts[accountKey]
		a.mu.Unlock()

		if !ok {
			wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
				"accountKey": accountKey,
				"error":      fmt.Sprintf("account %q not found", accountKey),
			})
			return
		}

		token, _, err := credential.ResolveAPIToken(acct, accountKey)
		if err != nil {
			wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
				"accountKey": accountKey,
				"error":      fmt.Sprintf("no API token available: %v", err),
			})
			return
		}

		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
				"accountKey": accountKey,
				"error":      err.Error(),
			})
			return
		}

		repos, err := prov.ListRepos(context.Background(), acct.URL, token, acct.Username)
		if err != nil {
			wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
				"accountKey": accountKey,
				"error":      err.Error(),
			})
			return
		}

		results := make([]DiscoverResult, len(repos))
		for i, r := range repos {
			results[i] = DiscoverResult{
				FullName:    r.FullName,
				Description: r.Description,
				Private:     r.Private,
				Fork:        r.Fork,
				Archived:    r.Archived,
			}
		}

		wailsrt.EventsEmit(a.ctx, "discover:done", map[string]interface{}{
			"accountKey": accountKey,
			"repos":      results,
		})
	}()
}

// AddDiscoveredRepos adds discovered repos to the config and saves.
// The key can be either a source key or an account key — if it's an account
// key, the first source referencing that account is used.
func (a *App) AddDiscoveredRepos(key string, repoNames []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	sourceKey := key
	if _, ok := a.cfg.Sources[sourceKey]; !ok {
		// Not a direct source key — find source by account key.
		found := false
		for sk, src := range a.cfg.Sources {
			if src.Account == key {
				sourceKey = sk
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no source found for key %q", key)
		}
	}

	for _, name := range repoNames {
		if err := a.cfg.AddRepo(sourceKey, name, config.Repo{}); err != nil {
			return err
		}
	}
	return config.Save(a.cfg, a.cfgPath)
}

// ─── Credential Verification ──────────────────────────────────

// CredentialStatus is the result of verifying an account's credentials.
type CredentialStatus struct {
	Status  string `json:"status"`  // "ok", "warning", "error"
	Message string `json:"message"`
}

// CredentialVerify checks if credentials are working for an account.
func (a *App) CredentialVerify(accountKey string) CredentialStatus {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return CredentialStatus{Status: "error", Message: "account not found"}
	}

	_, _, err := credential.ResolveAPIToken(acct, accountKey)
	if err != nil {
		return CredentialStatus{Status: "error", Message: err.Error()}
	}

	err = provider.TestAuth(context.Background(), acct.Provider, acct.URL, "", acct.Username)
	if err != nil {
		return CredentialStatus{Status: "warning", Message: err.Error()}
	}

	return CredentialStatus{Status: "ok", Message: "credentials verified"}
}

// ─── Helpers ──────────────────────────────────────────────────

func stripScheme(rawURL string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if len(rawURL) > len(prefix) && rawURL[:len(prefix)] == prefix {
			return rawURL[len(prefix):]
		}
	}
	return rawURL
}
