package git

import (
	"context"
	"path/filepath"
	"strings"
)

// ConfigSnapshot keeps ordered, multi-value effective and direct local settings.
// Git expands includes for Values, so helper cancellation and inherited identity
// follow the same order as a later fetch. Local excludes includes: repairs must
// never mistake an inherited setting for one stored in the clone itself.
type ConfigSnapshot struct {
	Values  map[string][]string
	Local   map[string][]string
	Entries []ConfigEntry
}

// ConfigEntry retains the scope and ordering of an effective setting.
type ConfigEntry struct {
	Scope, Key, Value string
	origin            string
}

// ReadConfigSnapshot reads configuration during credential setup, including
// adoption. Its inspection overrides are excluded from the returned snapshot.
func ReadConfigSnapshot(repoPath string) (ConfigSnapshot, error) {
	profile, err := InspectionProfile()
	if err != nil {
		return ConfigSnapshot{}, err
	}
	return readConfigSnapshot(context.Background(), repoPath, profile)
}

// ReadConfigSnapshotContext reads a tracked clone's effective settings for heal.
func ReadConfigSnapshotContext(ctx context.Context, repoPath string) (ConfigSnapshot, error) {
	return readConfigSnapshot(ctx, repoPath, nil)
}

func readConfigSnapshot(ctx context.Context, repoPath string, profile []string) (ConfigSnapshot, error) {
	effective, err := outputContext(ctx, repoPath, append(profile, "config", "--null", "--show-scope", "--show-origin", "--list", "--includes")...)
	if err != nil {
		return ConfigSnapshot{}, err
	}
	local, err := outputContext(ctx, repoPath, append(profile, "config", "--null", "--show-scope", "--show-origin", "--list", "--local", "--no-includes")...)
	if err != nil {
		return ConfigSnapshot{}, err
	}
	localValues, localEntries := parseScopedConfigList(local, "")
	localOrigin := ""
	if len(localEntries) > 0 {
		localOrigin = localEntries[0].origin
	}
	values, entries := parseScopedConfigList(effective, localOrigin)
	// Git lists command-line -c settings last. Preserve values from files and
	// the caller's environment, including any identical earlier settings.
	for i := 1; i < len(profile); i += 2 {
		key, _, _ := strings.Cut(profile[i], "=")
		key = configKey(key)
		if entries := values[key]; len(entries) > 1 {
			values[key] = entries[:len(entries)-1]
		} else {
			delete(values, key)
		}
	}
	if profileCount := len(profile) / 2; profileCount <= len(entries) {
		entries = entries[:len(entries)-profileCount]
	}
	return ConfigSnapshot{Values: values, Local: localValues, Entries: entries}, nil
}

func parseScopedConfigList(raw, localOrigin string) (map[string][]string, []ConfigEntry) {
	values := make(map[string][]string)
	var entries []ConfigEntry
	for raw != "" {
		scope, rest, ok := strings.Cut(raw, "\x00")
		if !ok {
			break
		}
		origin, rest, ok := strings.Cut(rest, "\x00")
		if !ok {
			break
		}
		record, rest, ok := strings.Cut(rest, "\x00")
		if !ok {
			break
		}
		raw = rest
		key, value, _ := strings.Cut(record, "\n")
		key = configKey(key)
		values[key] = append(values[key], value)
		if scope == "local" && localOrigin != "" && origin != localOrigin {
			scope = "include"
		}
		entries = append(entries, ConfigEntry{Scope: scope, Key: key, Value: value, origin: origin})
	}
	return values, entries
}

// ResolvedOrigin reads the URL Git would fetch, applying insteadOf without a
// network connection or calling any credential helper.
func ResolvedOrigin(ctx context.Context, path string) (string, error) {
	url, err := outputContext(ctx, path, "remote", "get-url", "origin")
	return strings.TrimSuffix(url, "\n"), err
}

// CommonConfigPath returns the shared config file for a clone or linked worktree.
func CommonConfigPath(ctx context.Context, path string) (string, error) {
	dir, err := outputContext(ctx, path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Join(strings.TrimSuffix(dir, "\n"), "config"), nil
}

// Git section and variable names ignore case; subsection names retain it.
func configKey(key string) string {
	first, last := strings.IndexByte(key, '.'), strings.LastIndexByte(key, '.')
	if first < 0 {
		return strings.ToLower(key)
	}
	return strings.ToLower(key[:first]) + key[first:last+1] + strings.ToLower(key[last+1:])
}

// Get returns the last effective value of a single-value setting.
func (s ConfigSnapshot) Get(key string) string {
	values := s.All(key)
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

// All returns effective values in Git's configuration order.
func (s ConfigSnapshot) All(key string) []string { return s.Values[configKey(key)] }

// LocalAll returns values written directly in the clone's shared config file.
func (s ConfigSnapshot) LocalAll(key string) []string { return s.Local[configKey(key)] }
