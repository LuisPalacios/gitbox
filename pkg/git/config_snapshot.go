package git

import (
	"strings"
)

// ConfigSnapshot keeps ordered, multi-value effective and direct local settings.
// Git expands includes for Values, so helper cancellation and inherited identity
// follow the same order as a later fetch. Local excludes includes: repairs must
// never mistake an inherited setting for one stored in the clone itself.
type ConfigSnapshot struct {
	Values map[string][]string
	Local  map[string][]string
}

// ReadConfigSnapshot reads a clone's configuration in two Git processes.
func ReadConfigSnapshot(repoPath string) (ConfigSnapshot, error) {
	effective, err := output(repoPath, "config", "--null", "--list", "--includes")
	if err != nil {
		return ConfigSnapshot{}, err
	}
	local, err := output(repoPath, "config", "--null", "--list", "--local", "--no-includes")
	if err != nil {
		return ConfigSnapshot{}, err
	}
	return ConfigSnapshot{Values: parseConfigList(effective), Local: parseConfigList(local)}, nil
}

func parseConfigList(raw string) map[string][]string {
	values := make(map[string][]string)
	for record := range strings.SplitSeq(raw, "\x00") {
		if record == "" {
			continue
		}
		key, value, _ := strings.Cut(record, "\n")
		key = configKey(key)
		values[key] = append(values[key], value)
	}
	return values
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
