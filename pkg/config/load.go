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

// Repair describes a single recoverable fix that LoadWithRepair applied to the
// parsed config. Kind is a short machine-readable tag; Detail is the
// user-facing explanation the GUI renders in the repair confirmation.
type Repair struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
}

// LoadWithRepair is Load plus a recovery pass for well-understood integrity
// failures. Today it drops mirror entries whose account_src or account_dst
// references an account that no longer exists — the dangling reference that
// turns the GUI's delete-account bug into total config loss (see issue #60).
//
// Contract:
//   - Unrecoverable errors (I/O, malformed JSON, schema mismatch, missing
//     global.folder, missing account required fields, dangling source
//     account ref, invalid workspace type) still surface as an error.
//   - Recoverable repairs are returned alongside the repaired config so the
//     GUI can show the user what was dropped before saving back.
//   - Strict Load remains unchanged and is still what the CLI / tests use.
func LoadWithRepair(path string) (*Config, []Repair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parsing v2 config: %w", err)
	}
	if cfg.Version != 2 {
		return nil, nil, fmt.Errorf("expected version 2, got %d", cfg.Version)
	}

	var repairs []Repair

	// Repair pass: drop mirror entries whose account refs are dangling.
	for name, m := range cfg.Mirrors {
		_, srcOk := cfg.Accounts[m.AccountSrc]
		_, dstOk := cfg.Accounts[m.AccountDst]
		if srcOk && dstOk {
			continue
		}
		var missing []string
		if !srcOk {
			missing = append(missing, fmt.Sprintf("account_src=%q", m.AccountSrc))
		}
		if !dstOk {
			missing = append(missing, fmt.Sprintf("account_dst=%q", m.AccountDst))
		}
		repairs = append(repairs, Repair{
			Kind:    "dangling_mirror",
			Subject: name,
			Detail:  fmt.Sprintf("mirror %q referenced missing %s; dropped", name, strings.Join(missing, ", ")),
		})
		delete(cfg.Mirrors, name)
	}

	// After repairs, run the strict validator — anything still broken is out
	// of scope for auto-repair and the caller must surface it.
	if err := validate(&cfg); err != nil {
		return nil, repairs, err
	}

	extractKeyOrder(data, &cfg)

	for key, w := range cfg.Workspaces {
		if deduped := dedupWorkspaceMembers(w.Members); len(deduped) != len(w.Members) {
			w.Members = deduped
			cfg.Workspaces[key] = w
		}
	}

	return &cfg, repairs, nil
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

	// Defensive: collapse any duplicate workspace members that may have been
	// hand-edited into the JSON or persisted by an older buggy code path.
	// In-memory only — the next save will write back the deduped form.
	for key, w := range cfg.Workspaces {
		if deduped := dedupWorkspaceMembers(w.Members); len(deduped) != len(w.Members) {
			w.Members = deduped
			cfg.Workspaces[key] = w
		}
	}

	return &cfg, nil
}

// dedupWorkspaceMembers preserves member order while collapsing duplicates by
// (source, repo). Defensive: hand-edited or legacy workspace entries can carry
// the same clone twice.
func dedupWorkspaceMembers(members []WorkspaceMember) []WorkspaceMember {
	if len(members) < 2 {
		return members
	}
	seen := make(map[WorkspaceMember]bool, len(members))
	out := members[:0]
	for _, m := range members {
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
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

	// Extract workspace key order by tokenizing. Members inside each workspace
	// are an ordered array, so no per-workspace order extraction is needed.
	cfg.WorkspaceOrder = extractMapKeyOrder(data, "workspaces")
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
	for name, w := range cfg.Workspaces {
		switch w.Type {
		case WorkspaceTypeCode, WorkspaceTypeTmuxinator:
		default:
			return fmt.Errorf("workspace %q: type must be %q or %q", name, WorkspaceTypeCode, WorkspaceTypeTmuxinator)
		}
		if w.Layout != "" {
			if w.Type != WorkspaceTypeTmuxinator {
				return fmt.Errorf("workspace %q: layout is only valid for tmuxinator workspaces", name)
			}
			switch w.Layout {
			case WorkspaceLayoutWindows, WorkspaceLayoutSplit:
			default:
				return fmt.Errorf("workspace %q: layout must be %q or %q", name, WorkspaceLayoutWindows, WorkspaceLayoutSplit)
			}
		}
	}
	return nil
}
