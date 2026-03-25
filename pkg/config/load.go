package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// Load reads and parses a configuration file. It auto-detects v1 vs v2 format.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	return Parse(data)
}

// Parse parses JSON configuration data, auto-detecting v1 vs v2 format.
func Parse(data []byte) (*Config, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	_, hasAccounts := raw["accounts"]
	_, hasSources := raw["sources"]

	if hasAccounts && !hasSources {
		return parseV1(data)
	}
	return parseV2(data)
}

func parseV2(data []byte) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing v2 config: %w", err)
	}
	if cfg.Version != 2 {
		return nil, fmt.Errorf("expected version 2, got %d", cfg.Version)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	// Extract JSON key order for sources and repos.
	extractKeyOrder(data, &cfg)

	return &cfg, nil
}

// extractKeyOrder parses the raw JSON to capture the insertion order of
// "sources" keys and each source's "repos" keys, since Go maps lose ordering.
func extractKeyOrder(data []byte, cfg *Config) {
	var raw struct {
		Sources map[string]json.RawMessage `json:"sources"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	// Extract source key order by tokenizing.
	cfg.SourceOrder = extractMapKeyOrder(data, "sources")

	// Extract repo key order per source.
	for sourceName, sourceRaw := range raw.Sources {
		repoOrder := extractMapKeyOrderFromObject(sourceRaw, "repos")
		if len(repoOrder) > 0 {
			src := cfg.Sources[sourceName]
			src.RepoOrder = repoOrder
			cfg.Sources[sourceName] = src
		}
	}
}

// extractMapKeyOrder extracts the ordered keys of a named object field from JSON.
func extractMapKeyOrder(data []byte, field string) []string {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	return findAndExtractKeys(dec, field)
}

// extractMapKeyOrderFromObject extracts ordered keys of a named field within a JSON object.
func extractMapKeyOrderFromObject(data json.RawMessage, field string) []string {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	return findAndExtractKeys(dec, field)
}

// findAndExtractKeys navigates a JSON decoder to find a field and extract its object keys in order.
func findAndExtractKeys(dec *json.Decoder, field string) []string {
	// Read opening {
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return nil
	}

	for dec.More() {
		// Read key.
		t, err = dec.Token()
		if err != nil {
			return nil
		}
		key, ok := t.(string)
		if !ok {
			return nil
		}

		if key == field {
			// This is our target — extract its keys.
			return extractObjectKeys(dec)
		}

		// Skip value.
		skipValue(dec)
	}
	return nil
}

// extractObjectKeys reads a JSON object and returns its keys in order, skipping values.
func extractObjectKeys(dec *json.Decoder) []string {
	// Read opening {
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return nil
	}

	var keys []string
	for dec.More() {
		t, err = dec.Token()
		if err != nil {
			break
		}
		key, ok := t.(string)
		if !ok {
			break
		}
		keys = append(keys, key)
		skipValue(dec)
	}
	// Read closing }
	dec.Token()
	return keys
}

// skipValue skips one JSON value (object, array, or primitive).
func skipValue(dec *json.Decoder) {
	t, err := dec.Token()
	if err != nil {
		return
	}
	switch t {
	case json.Delim('{'):
		for dec.More() {
			dec.Token() // key
			skipValue(dec)
		}
		dec.Token() // }
	case json.Delim('['):
		for dec.More() {
			skipValue(dec)
		}
		dec.Token() // ]
	}
	// Primitives (string, number, bool, null) are already consumed by Token().
}

// --- v1 types ---

type v1Config struct {
	Schema   string                `json:"$schema,omitempty"`
	Global   v1Global              `json:"global"`
	Accounts map[string]v1Account  `json:"accounts"`
}

type v1Global struct {
	Folder        string       `json:"folder"`
	CredentialSSH *v1SSHGlobal `json:"credential_ssh,omitempty"`
	CredentialGCM *v1GCMGlobal `json:"credential_gcm,omitempty"`
}

type v1SSHGlobal struct {
	Enabled   string `json:"enabled,omitempty"`
	SSHFolder string `json:"ssh_folder,omitempty"`
}

type v1GCMGlobal struct {
	Enabled         string `json:"enabled,omitempty"`
	Helper          string `json:"helper,omitempty"`
	CredentialStore string `json:"credentialStore,omitempty"`
}

type v1Account struct {
	URL            string            `json:"url"`
	Username       string            `json:"username"`
	Folder         string            `json:"folder"`
	Name           string            `json:"name"`
	Email          string            `json:"email"`
	GCMProvider    string            `json:"gcm_provider,omitempty"`
	GCMUseHTTPPath string            `json:"gcm_useHttpPath,omitempty"`
	SSHHost        string            `json:"ssh_host,omitempty"`
	SSHHostname    string            `json:"ssh_hostname,omitempty"`
	SSHType        string            `json:"ssh_type,omitempty"`
	Repos          map[string]v1Repo `json:"repos"`
}

type v1Repo struct {
	CredentialType string `json:"credential_type"`
	Name           string `json:"name,omitempty"`
	Email          string `json:"email,omitempty"`
	Folder         string `json:"folder,omitempty"`
}

// --- v1 → v2 conversion ---

func parseV1(data []byte) (*Config, error) {
	var v1 v1Config
	if err := json.Unmarshal(data, &v1); err != nil {
		return nil, fmt.Errorf("parsing v1 config: %w", err)
	}
	return convertV1ToV2(&v1), nil
}

// mergeKey groups v1 accounts that represent the same login.
type mergeKey struct {
	hostname string
	username string
}

// convertV1ToV2 transforms a v1 config into v2 format.
// Accounts with the same (hostname, username) are deduplicated into one account + one source.
// Repos get "org/repo" naming from the URL path of each v1 account.
func convertV1ToV2(v1 *v1Config) *Config {
	cfg := &Config{
		Version:  2,
		Global:   convertV1Global(v1.Global),
		Accounts: make(map[string]Account),
		Sources:  make(map[string]Source),
	}

	// Group v1 accounts by (hostname, username).
	groups := make(map[mergeKey][]string)
	for name := range v1.Accounts {
		acct := v1.Accounts[name]
		key := mergeKey{
			hostname: extractHostFromURL(acct.URL),
			username: acct.Username,
		}
		groups[key] = append(groups[key], name)
	}

	// Process each group.
	for mk, names := range groups {
		// Sort for deterministic output.
		sort.Strings(names)

		// Use the first account for shared identity fields.
		first := v1.Accounts[names[0]]

		// Derive account key: find common prefix of v1 names, or use first name.
		accountKey := deriveAccountKey(names, mk.hostname, mk.username)

		// Build v2 Account.
		account := Account{
			Provider:      inferProvider(first.GCMProvider),
			URL:           schemeHost(first.URL),
			Username:      first.Username,
			Name:          first.Name,
			Email:         first.Email,
			DefaultBranch: "main",
		}

		// Take SSH from first account that has it.
		for _, n := range names {
			a := v1.Accounts[n]
			if a.SSHHost != "" || a.SSHHostname != "" || a.SSHType != "" {
				account.SSH = &SSHConfig{
					Host: a.SSHHost, Hostname: a.SSHHostname, KeyType: a.SSHType,
				}
				break
			}
		}

		// Take GCM from first account that has it.
		for _, n := range names {
			a := v1.Accounts[n]
			if a.GCMProvider != "" || a.GCMUseHTTPPath != "" {
				account.GCM = &GCMConfig{
					Provider: a.GCMProvider, UseHTTPPath: parseBoolString(a.GCMUseHTTPPath),
				}
				break
			}
		}

		// Build v2 Source — merge all repos with org/repo prefix.
		source := Source{
			Account: accountKey,
			Repos:   make(map[string]Repo),
		}

		// Collect all repos with org/ prefix.
		// Track which v1 folder each org uses. When an org has multiple v1 folders
		// (split account), repos from non-primary folders get id_folder override.
		orgFirstFolder := make(map[string]string) // org → first v1 folder seen

		for _, n := range names {
			a := v1.Accounts[n]
			org := extractOrgFromURL(a.URL)

			// Record the first folder seen for this org.
			if _, seen := orgFirstFolder[org]; !seen {
				orgFirstFolder[org] = a.Folder
			}

			for repoName, v1Repo := range a.Repos {
				fullRepoName := repoName
				if org != "" {
					fullRepoName = org + "/" + repoName
				}

				repo := Repo{
					CredentialType: v1Repo.CredentialType,
					Name:           v1Repo.Name,
					Email:          v1Repo.Email,
				}

				// Explicit v1 repo folder override → clone_folder.
				if v1Repo.Folder != "" {
					repo.CloneFolder = v1Repo.Folder
				}

				// Split-account detection: if this v1 account's folder differs
				// from the primary folder for this org, set id_folder.
				if a.Folder != orgFirstFolder[org] {
					repo.IdFolder = a.Folder
				}

				source.Repos[fullRepoName] = repo
			}
		}

		// Detect default_credential_type (most common across all repos).
		account.DefaultCredentialType = detectDefaultCredentialType(source.Repos)

		// Strip credential_type from repos where it matches the default.
		for repoName, repo := range source.Repos {
			if repo.CredentialType == account.DefaultCredentialType {
				repo.CredentialType = ""
				source.Repos[repoName] = repo
			}
		}

		cfg.Accounts[accountKey] = account
		cfg.Sources[accountKey] = source
	}

	return cfg
}

// deriveAccountKey finds a good key name for a group of v1 account names.
// Uses the longest common prefix if meaningful, otherwise the first name.
func deriveAccountKey(names []string, hostname string, username string) string {
	if len(names) == 1 {
		return names[0]
	}

	// Find longest common prefix.
	prefix := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, prefix) && len(prefix) > 0 {
			prefix = prefix[:len(prefix)-1]
		}
	}
	prefix = strings.TrimRight(prefix, "-._/")

	// Accept prefix if it's specific enough.
	// Reject generic prefixes like "github" that match just the provider name.
	hostBase := strings.Split(hostname, ".")[0]
	if len(prefix) >= 5 && prefix != hostBase {
		return prefix
	}

	// Fallback: build from hostname + username.
	// "github.com" + "MyUser" → "github-MyUser"
	parts := strings.Split(hostname, ".")
	host := parts[0]
	if host == "git" && len(parts) >= 2 {
		host = parts[1] // "git.example.org" → "example"
	}
	return host + "-" + username
}

// detectDefaultCredentialType returns the most common credential_type across repos.
// On tie, picks alphabetically first (deterministic).
func detectDefaultCredentialType(repos map[string]Repo) string {
	counts := make(map[string]int)
	for _, r := range repos {
		if r.CredentialType != "" {
			counts[r.CredentialType]++
		}
	}
	best := ""
	bestCount := 0
	for ct, count := range counts {
		if count > bestCount || (count == bestCount && (best == "" || ct < best)) {
			best = ct
			bestCount = count
		}
	}
	return best
}

func convertV1Global(v1g v1Global) GlobalConfig {
	g := GlobalConfig{Folder: v1g.Folder}
	if v1g.CredentialSSH != nil {
		g.CredentialSSH = &SSHGlobal{
			SSHFolder: v1g.CredentialSSH.SSHFolder,
		}
	}
	if v1g.CredentialGCM != nil {
		g.CredentialGCM = &GCMGlobal{
			Helper:          v1g.CredentialGCM.Helper,
			CredentialStore: v1g.CredentialGCM.CredentialStore,
		}
	}
	return g
}

// --- helpers ---

func parseBoolString(s string) bool { return s == "true" }

func inferProvider(gcmProvider string) string {
	switch gcmProvider {
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

func extractHostFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Hostname()
}

func extractOrgFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	path := strings.Trim(u.Path, "/")
	if path == "" {
		return ""
	}
	parts := strings.SplitN(path, "/", 2)
	return parts[0]
}

func schemeHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// validate checks that required fields are present in a v2 config.
func validate(cfg *Config) error {
	if cfg.Global.Folder == "" {
		return fmt.Errorf("global.folder is required")
	}
	for name, acct := range cfg.Accounts {
		if acct.Provider == "" {
			return fmt.Errorf("account %q: provider is required", name)
		}
		if acct.URL == "" {
			return fmt.Errorf("account %q: url is required", name)
		}
		if acct.Username == "" {
			return fmt.Errorf("account %q: username is required", name)
		}
		if acct.Name == "" {
			return fmt.Errorf("account %q: name is required", name)
		}
		if acct.Email == "" {
			return fmt.Errorf("account %q: email is required", name)
		}
	}
	for name, source := range cfg.Sources {
		if source.Account == "" {
			return fmt.Errorf("source %q: account is required", name)
		}
		if _, ok := cfg.Accounts[source.Account]; !ok {
			return fmt.Errorf("source %q: references unknown account %q", name, source.Account)
		}
	}
	return nil
}
