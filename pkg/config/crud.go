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

// DeleteAccount removes an account. Returns error if sources still reference it.
func (c *Config) DeleteAccount(key string) error {
	if _, exists := c.Accounts[key]; !exists {
		return fmt.Errorf("account %q not found", key)
	}
	for srcName, src := range c.Sources {
		if src.Account == key {
			return fmt.Errorf("cannot delete account %q: referenced by source %q", key, srcName)
		}
	}
	delete(c.Accounts, key)
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
