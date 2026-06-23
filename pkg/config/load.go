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
		return nil, nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Version != 2 && cfg.Version != CurrentVersion {
		return nil, nil, fmt.Errorf("unsupported config version %d (expected 2 or %d)", cfg.Version, CurrentVersion)
	}
	migrated := cfg.Version == 2

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

	if migrated {
		// v2→v3: bump version and discard the regenerable workspace cache.
		// Silent (no Repair entry) so it doesn't trigger the repair dialog;
		// it persists on the next save.
		cfg.Version = CurrentVersion
		cfg.Workspaces = nil
		cfg.WorkspaceOrder = nil
	} else {
		sanitizeWorkspaces(&cfg, data)
		for key, w := range cfg.Workspaces {
			if deduped := dedupWorkspaceMembers(w.Members); len(deduped) != len(w.Members) {
				w.Members = deduped
				cfg.Workspaces[key] = w
			}
		}
	}

	MigrateLegacyTerminals(&cfg.Global)

	return &cfg, repairs, nil
}

// Parse parses JSON configuration data. It accepts the current v3 format and
// transparently migrates v2 files (see migrateV2toV3). Any other version is an
// error. The migration is in-memory only — the next Save persists the v3 shape.
func Parse(data []byte) (*Config, error) {
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	switch probe.Version {
	case CurrentVersion:
		return parseConfig(data, false)
	case 2:
		return parseConfig(data, true)
	default:
		return nil, fmt.Errorf("unsupported config version %d (expected 2 or %d)", probe.Version, CurrentVersion)
	}
}

// parseConfig unmarshals, optionally migrates v2→v3, validates, and finalizes
// the config. When migrate is true the workspaces section is discarded wholesale
// (it is a regenerable discovered cache) and the version is bumped to v3.
func parseConfig(data []byte, migrate bool) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if migrate {
		cfg.Version = CurrentVersion
	}
	if cfg.Version != CurrentVersion {
		return nil, fmt.Errorf("expected version %d, got %d", CurrentVersion, cfg.Version)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}

	// Extract JSON key order for sources and repos (and, for v3 files, workspaces).
	extractKeyOrder(data, &cfg)

	if migrate {
		// v2→v3: workspaces are now a read-only discovered cache. Drop any v2
		// entries (user-created or tmuxinator) wholesale; discovery rebuilds the
		// cache on next launch. New v3 keys (extra_folders, nested_scan_depth,
		// repo.container) are absent in v2 and default cleanly.
		cfg.Workspaces = nil
		cfg.WorkspaceOrder = nil
	} else {
		// v3 strict load: defend against hand-edited entries — keep only
		// discovered codeWorkspace entries.
		sanitizeWorkspaces(&cfg, data)
		// Collapse duplicate members that may have been hand-edited in.
		for key, w := range cfg.Workspaces {
			if deduped := dedupWorkspaceMembers(w.Members); len(deduped) != len(w.Members) {
				w.Members = deduped
				cfg.Workspaces[key] = w
			}
		}
	}

	// Auto-migrate legacy v2.0 flat global.terminals[] into the three-array
	// shape (terminal_apps + shells + terminal_profiles). In-memory; the next
	// save persists the new shape and drops the legacy block.
	MigrateLegacyTerminals(&cfg.Global)

	return &cfg, nil
}

// sanitizeWorkspaces enforces the read-only-cache invariant on the workspaces
// section: only discovered VSCode .code-workspace entries survive. Legacy
// tmuxinator workspaces and user-created (non-discovered) entries are dropped.
// The pre-slim "type"/"discovered" fields are read from the raw JSON because
// the Workspace struct no longer carries "type". In-memory only.
func sanitizeWorkspaces(cfg *Config, data []byte) {
	if len(cfg.Workspaces) == 0 {
		return
	}
	var raw struct {
		Workspaces map[string]struct {
			Type       string `json:"type"`
			Discovered bool   `json:"discovered"`
		} `json:"workspaces"`
	}
	_ = json.Unmarshal(data, &raw)
	for key := range cfg.Workspaces {
		meta, ok := raw.Workspaces[key]
		// Drop tmuxinator entries and anything not marked discovered. Entries
		// with no recorded type but discovered==true are kept (the new shape).
		if !ok || !meta.Discovered || (meta.Type != "" && meta.Type != "codeWorkspace") {
			delete(cfg.Workspaces, key)
		}
	}
	// Drop removed keys from the order slice, preserving the rest.
	if len(cfg.WorkspaceOrder) > 0 {
		kept := cfg.WorkspaceOrder[:0]
		for _, k := range cfg.WorkspaceOrder {
			if _, ok := cfg.Workspaces[k]; ok {
				kept = append(kept, k)
			}
		}
		cfg.WorkspaceOrder = kept
	}
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
	switch cfg.Global.Language {
	case "", "en", "es":
	default:
		return fmt.Errorf("global.language must be \"en\" or \"es\"")
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
	// Workspaces are a read-only discovered cache (VSCode .code-workspace only).
	// No structural validation: any legacy shape is sanitized in parseV2.
	return nil
}
