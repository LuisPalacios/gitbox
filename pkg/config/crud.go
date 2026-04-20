package config

import "fmt"

// --- Account CRUD ---

// AddAccount adds a new account. Returns error if key already exists.
func (c *Config) AddAccount(key string, acct Account) error {
	if key == "" {
		return fmt.Errorf("account key cannot be empty")
	}
	if _, exists := c.Accounts[key]; exists {
		return fmt.Errorf("account %q already exists", key)
	}
	if err := validateAccount(key, acct); err != nil {
		return err
	}
	if c.Accounts == nil {
		c.Accounts = make(map[string]Account)
	}
	c.Accounts[key] = acct
	return nil
}

// UpdateAccount updates an existing account. Returns error if not found.
func (c *Config) UpdateAccount(key string, acct Account) error {
	if _, exists := c.Accounts[key]; !exists {
		return fmt.Errorf("account %q not found", key)
	}
	if err := validateAccount(key, acct); err != nil {
		return err
	}
	c.Accounts[key] = acct
	return nil
}

// DeleteAccount removes an account. Returns error if sources or mirrors still reference it.
func (c *Config) DeleteAccount(key string) error {
	if _, exists := c.Accounts[key]; !exists {
		return fmt.Errorf("account %q not found", key)
	}
	for srcName, src := range c.Sources {
		if src.Account == key {
			return fmt.Errorf("cannot delete account %q: referenced by source %q", key, srcName)
		}
	}
	for mirrorName, m := range c.Mirrors {
		if m.AccountSrc == key || m.AccountDst == key {
			return fmt.Errorf("cannot delete account %q: referenced by mirror %q", key, mirrorName)
		}
	}
	delete(c.Accounts, key)
	return nil
}

// RenameAccount moves an account from oldKey to newKey and updates all
// source references. Returns error if oldKey not found or newKey already exists.
func (c *Config) RenameAccount(oldKey, newKey string) error {
	if newKey == "" {
		return fmt.Errorf("new account key cannot be empty")
	}
	if _, exists := c.Accounts[oldKey]; !exists {
		return fmt.Errorf("account %q not found", oldKey)
	}
	if _, exists := c.Accounts[newKey]; exists {
		return fmt.Errorf("account %q already exists", newKey)
	}
	c.Accounts[newKey] = c.Accounts[oldKey]
	delete(c.Accounts, oldKey)

	// Update all source references.
	for srcKey, src := range c.Sources {
		if src.Account == oldKey {
			src.Account = newKey
			c.Sources[srcKey] = src
		}
	}

	// Update all mirror references.
	for mirrorKey, m := range c.Mirrors {
		changed := false
		if m.AccountSrc == oldKey {
			m.AccountSrc = newKey
			changed = true
		}
		if m.AccountDst == oldKey {
			m.AccountDst = newKey
			changed = true
		}
		if changed {
			c.Mirrors[mirrorKey] = m
		}
	}
	return nil
}

// GetAccountByKey returns an account by key.
func (c *Config) GetAccountByKey(key string) (Account, bool) {
	acct, ok := c.Accounts[key]
	return acct, ok
}

// ListAccounts returns all accounts.
func (c *Config) ListAccounts() map[string]Account {
	return c.Accounts
}

func validateAccount(key string, acct Account) error {
	if acct.Provider == "" {
		return fmt.Errorf("account %q: provider is required", key)
	}
	if acct.URL == "" {
		return fmt.Errorf("account %q: url is required", key)
	}
	if acct.Username == "" {
		return fmt.Errorf("account %q: username is required", key)
	}
	if acct.Name == "" {
		return fmt.Errorf("account %q: name is required", key)
	}
	if acct.Email == "" {
		return fmt.Errorf("account %q: email is required", key)
	}
	return nil
}

// --- Source CRUD ---

// AddSource adds a new source. Returns error if key exists or account ref is invalid.
func (c *Config) AddSource(key string, src Source) error {
	if key == "" {
		return fmt.Errorf("source key cannot be empty")
	}
	if _, exists := c.Sources[key]; exists {
		return fmt.Errorf("source %q already exists", key)
	}
	if src.Account == "" {
		return fmt.Errorf("source %q: account is required", key)
	}
	if _, ok := c.Accounts[src.Account]; !ok {
		return fmt.Errorf("source %q: references unknown account %q", key, src.Account)
	}
	if c.Sources == nil {
		c.Sources = make(map[string]Source)
	}
	if src.Repos == nil {
		src.Repos = make(map[string]Repo)
	}
	c.Sources[key] = src
	return nil
}

// UpdateSource updates an existing source. Returns error if not found.
func (c *Config) UpdateSource(key string, src Source) error {
	if _, exists := c.Sources[key]; !exists {
		return fmt.Errorf("source %q not found", key)
	}
	if src.Account == "" {
		return fmt.Errorf("source %q: account is required", key)
	}
	if _, ok := c.Accounts[src.Account]; !ok {
		return fmt.Errorf("source %q: references unknown account %q", key, src.Account)
	}
	c.Sources[key] = src
	return nil
}

// DeleteSource removes a source and all its repos.
func (c *Config) DeleteSource(key string) error {
	if _, exists := c.Sources[key]; !exists {
		return fmt.Errorf("source %q not found", key)
	}
	delete(c.Sources, key)
	return nil
}

// RenameSource moves a source from oldKey to newKey.
// Returns error if oldKey not found or newKey already exists.
func (c *Config) RenameSource(oldKey, newKey string) error {
	if newKey == "" {
		return fmt.Errorf("new source key cannot be empty")
	}
	if _, exists := c.Sources[oldKey]; !exists {
		return fmt.Errorf("source %q not found", oldKey)
	}
	if _, exists := c.Sources[newKey]; exists {
		return fmt.Errorf("source %q already exists", newKey)
	}
	c.Sources[newKey] = c.Sources[oldKey]
	delete(c.Sources, oldKey)
	return nil
}

// ListSources returns all sources.
func (c *Config) ListSources() map[string]Source {
	return c.Sources
}

// --- Repo CRUD ---

// AddRepo adds a repo to a source. Returns error if source not found or repo exists.
func (c *Config) AddRepo(sourceKey, repoKey string, repo Repo) error {
	src, ok := c.Sources[sourceKey]
	if !ok {
		return fmt.Errorf("source %q not found", sourceKey)
	}
	if repoKey == "" {
		return fmt.Errorf("repo key cannot be empty")
	}
	if _, exists := src.Repos[repoKey]; exists {
		return fmt.Errorf("repo %q already exists in source %q", repoKey, sourceKey)
	}
	if src.Repos == nil {
		src.Repos = make(map[string]Repo)
	}
	src.Repos[repoKey] = repo
	c.Sources[sourceKey] = src
	return nil
}

// UpdateRepo updates a repo in a source. Returns error if not found.
func (c *Config) UpdateRepo(sourceKey, repoKey string, repo Repo) error {
	src, ok := c.Sources[sourceKey]
	if !ok {
		return fmt.Errorf("source %q not found", sourceKey)
	}
	if _, exists := src.Repos[repoKey]; !exists {
		return fmt.Errorf("repo %q not found in source %q", repoKey, sourceKey)
	}
	src.Repos[repoKey] = repo
	c.Sources[sourceKey] = src
	return nil
}

// DeleteRepo removes a repo from a source.
func (c *Config) DeleteRepo(sourceKey, repoKey string) error {
	src, ok := c.Sources[sourceKey]
	if !ok {
		return fmt.Errorf("source %q not found", sourceKey)
	}
	if _, exists := src.Repos[repoKey]; !exists {
		return fmt.Errorf("repo %q not found in source %q", repoKey, sourceKey)
	}
	delete(src.Repos, repoKey)
	c.Sources[sourceKey] = src
	return nil
}

// ListRepos returns all repos for a source.
func (c *Config) ListRepos(sourceKey string) (map[string]Repo, error) {
	src, ok := c.Sources[sourceKey]
	if !ok {
		return nil, fmt.Errorf("source %q not found", sourceKey)
	}
	return src.Repos, nil
}

// --- Mirror CRUD ---

// AddMirror adds a new mirror group. Returns error if key exists or account refs are invalid.
func (c *Config) AddMirror(key string, m Mirror) error {
	if key == "" {
		return fmt.Errorf("mirror key cannot be empty")
	}
	if _, exists := c.Mirrors[key]; exists {
		return fmt.Errorf("mirror %q already exists", key)
	}
	if err := c.validateMirror(key, m); err != nil {
		return err
	}
	if c.Mirrors == nil {
		c.Mirrors = make(map[string]Mirror)
	}
	if m.Repos == nil {
		m.Repos = make(map[string]MirrorRepo)
	}
	c.Mirrors[key] = m
	return nil
}

// UpdateMirror updates an existing mirror group. Returns error if not found.
func (c *Config) UpdateMirror(key string, m Mirror) error {
	if _, exists := c.Mirrors[key]; !exists {
		return fmt.Errorf("mirror %q not found", key)
	}
	if err := c.validateMirror(key, m); err != nil {
		return err
	}
	c.Mirrors[key] = m
	return nil
}

// DeleteMirror removes a mirror group and all its repos.
func (c *Config) DeleteMirror(key string) error {
	if _, exists := c.Mirrors[key]; !exists {
		return fmt.Errorf("mirror %q not found", key)
	}
	delete(c.Mirrors, key)
	return nil
}

// RenameMirror moves a mirror from oldKey to newKey.
func (c *Config) RenameMirror(oldKey, newKey string) error {
	if newKey == "" {
		return fmt.Errorf("new mirror key cannot be empty")
	}
	if _, exists := c.Mirrors[oldKey]; !exists {
		return fmt.Errorf("mirror %q not found", oldKey)
	}
	if _, exists := c.Mirrors[newKey]; exists {
		return fmt.Errorf("mirror %q already exists", newKey)
	}
	c.Mirrors[newKey] = c.Mirrors[oldKey]
	delete(c.Mirrors, oldKey)
	return nil
}

// ListMirrors returns all mirrors.
func (c *Config) ListMirrors() map[string]Mirror {
	return c.Mirrors
}

// AddMirrorRepo adds a repo to a mirror group. Returns error if mirror not found or repo exists.
func (c *Config) AddMirrorRepo(mirrorKey, repoKey string, repo MirrorRepo) error {
	m, ok := c.Mirrors[mirrorKey]
	if !ok {
		return fmt.Errorf("mirror %q not found", mirrorKey)
	}
	if repoKey == "" {
		return fmt.Errorf("repo key cannot be empty")
	}
	if _, exists := m.Repos[repoKey]; exists {
		return fmt.Errorf("repo %q already exists in mirror %q", repoKey, mirrorKey)
	}
	if err := validateMirrorRepo(mirrorKey, repoKey, repo); err != nil {
		return err
	}
	if m.Repos == nil {
		m.Repos = make(map[string]MirrorRepo)
	}
	m.Repos[repoKey] = repo
	c.Mirrors[mirrorKey] = m
	return nil
}

// UpdateMirrorRepo updates a repo in a mirror group. Returns error if not found.
func (c *Config) UpdateMirrorRepo(mirrorKey, repoKey string, repo MirrorRepo) error {
	m, ok := c.Mirrors[mirrorKey]
	if !ok {
		return fmt.Errorf("mirror %q not found", mirrorKey)
	}
	if _, exists := m.Repos[repoKey]; !exists {
		return fmt.Errorf("repo %q not found in mirror %q", repoKey, mirrorKey)
	}
	if err := validateMirrorRepo(mirrorKey, repoKey, repo); err != nil {
		return err
	}
	m.Repos[repoKey] = repo
	c.Mirrors[mirrorKey] = m
	return nil
}

// DeleteMirrorRepo removes a repo from a mirror group.
func (c *Config) DeleteMirrorRepo(mirrorKey, repoKey string) error {
	m, ok := c.Mirrors[mirrorKey]
	if !ok {
		return fmt.Errorf("mirror %q not found", mirrorKey)
	}
	if _, exists := m.Repos[repoKey]; !exists {
		return fmt.Errorf("repo %q not found in mirror %q", repoKey, mirrorKey)
	}
	delete(m.Repos, repoKey)
	c.Mirrors[mirrorKey] = m
	return nil
}

// ListMirrorRepos returns all repos for a mirror group.
func (c *Config) ListMirrorRepos(mirrorKey string) (map[string]MirrorRepo, error) {
	m, ok := c.Mirrors[mirrorKey]
	if !ok {
		return nil, fmt.Errorf("mirror %q not found", mirrorKey)
	}
	return m.Repos, nil
}

func (c *Config) validateMirror(key string, m Mirror) error {
	if m.AccountSrc == "" {
		return fmt.Errorf("mirror %q: account_src is required", key)
	}
	if m.AccountDst == "" {
		return fmt.Errorf("mirror %q: account_dst is required", key)
	}
	if m.AccountSrc == m.AccountDst {
		return fmt.Errorf("mirror %q: account_src and account_dst must be different", key)
	}
	if _, ok := c.Accounts[m.AccountSrc]; !ok {
		return fmt.Errorf("mirror %q: references unknown account %q (account_src)", key, m.AccountSrc)
	}
	if _, ok := c.Accounts[m.AccountDst]; !ok {
		return fmt.Errorf("mirror %q: references unknown account %q (account_dst)", key, m.AccountDst)
	}
	return nil
}

func validateMirrorRepo(mirrorKey, repoKey string, repo MirrorRepo) error {
	switch repo.Direction {
	case "push", "pull":
	default:
		return fmt.Errorf("mirror %q repo %q: direction must be \"push\" or \"pull\"", mirrorKey, repoKey)
	}
	switch repo.Origin {
	case "src", "dst":
	default:
		return fmt.Errorf("mirror %q repo %q: origin must be \"src\" or \"dst\"", mirrorKey, repoKey)
	}
	return nil
}

// --- Workspace CRUD ---

// AddWorkspace adds a new workspace. Returns error if key exists or members are invalid.
func (c *Config) AddWorkspace(key string, w Workspace) error {
	if key == "" {
		return fmt.Errorf("workspace key cannot be empty")
	}
	if _, exists := c.Workspaces[key]; exists {
		return fmt.Errorf("workspace %q already exists", key)
	}
	if err := c.validateWorkspace(key, w); err != nil {
		return err
	}
	if c.Workspaces == nil {
		c.Workspaces = make(map[string]Workspace)
	}
	if w.Members == nil {
		w.Members = []WorkspaceMember{}
	}
	c.Workspaces[key] = w
	return nil
}

// UpdateWorkspace updates an existing workspace. Returns error if not found.
func (c *Config) UpdateWorkspace(key string, w Workspace) error {
	if _, exists := c.Workspaces[key]; !exists {
		return fmt.Errorf("workspace %q not found", key)
	}
	if err := c.validateWorkspace(key, w); err != nil {
		return err
	}
	c.Workspaces[key] = w
	return nil
}

// DeleteWorkspace removes a workspace from the config.
// Note: does not delete the on-disk file — callers handle that.
func (c *Config) DeleteWorkspace(key string) error {
	if _, exists := c.Workspaces[key]; !exists {
		return fmt.Errorf("workspace %q not found", key)
	}
	delete(c.Workspaces, key)
	return nil
}

// RenameWorkspace moves a workspace from oldKey to newKey.
func (c *Config) RenameWorkspace(oldKey, newKey string) error {
	if newKey == "" {
		return fmt.Errorf("new workspace key cannot be empty")
	}
	if _, exists := c.Workspaces[oldKey]; !exists {
		return fmt.Errorf("workspace %q not found", oldKey)
	}
	if _, exists := c.Workspaces[newKey]; exists {
		return fmt.Errorf("workspace %q already exists", newKey)
	}
	c.Workspaces[newKey] = c.Workspaces[oldKey]
	delete(c.Workspaces, oldKey)
	return nil
}

// ListWorkspaces returns all workspaces.
func (c *Config) ListWorkspaces() map[string]Workspace {
	return c.Workspaces
}

// AddWorkspaceMember appends a member to a workspace. Returns error if duplicate
// or if the referenced source/repo does not exist in config.
func (c *Config) AddWorkspaceMember(key string, member WorkspaceMember) error {
	w, ok := c.Workspaces[key]
	if !ok {
		return fmt.Errorf("workspace %q not found", key)
	}
	if err := c.validateWorkspaceMember(key, member); err != nil {
		return err
	}
	for _, existing := range w.Members {
		if existing.Source == member.Source && existing.Repo == member.Repo {
			return fmt.Errorf("workspace %q: member %s/%s already present", key, member.Source, member.Repo)
		}
	}
	w.Members = append(w.Members, member)
	c.Workspaces[key] = w
	return nil
}

// DeleteWorkspaceMember removes a member from a workspace by source+repo.
func (c *Config) DeleteWorkspaceMember(key, source, repo string) error {
	w, ok := c.Workspaces[key]
	if !ok {
		return fmt.Errorf("workspace %q not found", key)
	}
	out := make([]WorkspaceMember, 0, len(w.Members))
	removed := false
	for _, m := range w.Members {
		if m.Source == source && m.Repo == repo {
			removed = true
			continue
		}
		out = append(out, m)
	}
	if !removed {
		return fmt.Errorf("workspace %q: member %s/%s not found", key, source, repo)
	}
	w.Members = out
	c.Workspaces[key] = w
	return nil
}

// ListWorkspaceMembers returns the members of a workspace.
func (c *Config) ListWorkspaceMembers(key string) ([]WorkspaceMember, error) {
	w, ok := c.Workspaces[key]
	if !ok {
		return nil, fmt.Errorf("workspace %q not found", key)
	}
	return w.Members, nil
}

func (c *Config) validateWorkspace(key string, w Workspace) error {
	switch w.Type {
	case WorkspaceTypeCode, WorkspaceTypeTmuxinator:
	default:
		return fmt.Errorf("workspace %q: type must be %q or %q", key, WorkspaceTypeCode, WorkspaceTypeTmuxinator)
	}
	if w.Layout != "" {
		if w.Type != WorkspaceTypeTmuxinator {
			return fmt.Errorf("workspace %q: layout is only valid for tmuxinator workspaces", key)
		}
		switch w.Layout {
		case WorkspaceLayoutWindows, WorkspaceLayoutSplit:
		default:
			return fmt.Errorf("workspace %q: layout must be %q or %q", key, WorkspaceLayoutWindows, WorkspaceLayoutSplit)
		}
	}
	for _, m := range w.Members {
		if err := c.validateWorkspaceMember(key, m); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateWorkspaceMember(key string, m WorkspaceMember) error {
	if m.Source == "" {
		return fmt.Errorf("workspace %q: member source is required", key)
	}
	if m.Repo == "" {
		return fmt.Errorf("workspace %q: member repo is required", key)
	}
	src, ok := c.Sources[m.Source]
	if !ok {
		return fmt.Errorf("workspace %q: member references unknown source %q", key, m.Source)
	}
	if _, ok := src.Repos[m.Repo]; !ok {
		return fmt.Errorf("workspace %q: member references unknown repo %q in source %q", key, m.Repo, m.Source)
	}
	return nil
}
