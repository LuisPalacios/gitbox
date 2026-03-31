package config

import (
	"encoding/json"
	"fmt"
	"os"
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

// Parse parses JSON configuration data in v2 format.
func Parse(data []byte) (*Config, error) {
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
		Mirrors map[string]json.RawMessage `json:"mirrors"`
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

	// Extract mirror key order by tokenizing.
	cfg.MirrorOrder = extractMapKeyOrder(data, "mirrors")

	// Extract repo key order per mirror.
	for mirrorName, mirrorRaw := range raw.Mirrors {
		repoOrder := extractMapKeyOrderFromObject(mirrorRaw, "repos")
		if len(repoOrder) > 0 {
			m := cfg.Mirrors[mirrorName]
			m.RepoOrder = repoOrder
			cfg.Mirrors[mirrorName] = m
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
	for name, m := range cfg.Mirrors {
		if m.AccountSrc == "" {
			return fmt.Errorf("mirror %q: account_src is required", name)
		}
		if m.AccountDst == "" {
			return fmt.Errorf("mirror %q: account_dst is required", name)
		}
		if m.AccountSrc == m.AccountDst {
			return fmt.Errorf("mirror %q: account_src and account_dst must be different", name)
		}
		if _, ok := cfg.Accounts[m.AccountSrc]; !ok {
			return fmt.Errorf("mirror %q: references unknown account %q (account_src)", name, m.AccountSrc)
		}
		if _, ok := cfg.Accounts[m.AccountDst]; !ok {
			return fmt.Errorf("mirror %q: references unknown account %q (account_dst)", name, m.AccountDst)
		}
		for repoName, repo := range m.Repos {
			switch repo.Direction {
			case "push", "pull":
			default:
				return fmt.Errorf("mirror %q repo %q: direction must be \"push\" or \"pull\"", name, repoName)
			}
			switch repo.Origin {
			case "src", "dst":
			default:
				return fmt.Errorf("mirror %q repo %q: origin must be \"src\" or \"dst\"", name, repoName)
			}
		}
	}
	return nil
}
