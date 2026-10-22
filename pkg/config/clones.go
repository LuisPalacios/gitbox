package config

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// IsIgnoredClone reports whether path is in Global.IgnoredClones.
func (c *Config) IsIgnoredClone(path string) bool {
	want := clonePathKey(path)
	for _, p := range c.Global.IgnoredClones {
		if clonePathKey(p) == want {
			return true
		}
	}
	return false
}

// IgnoreClone adds path to Global.IgnoredClones (ignore) or removes it.
// Returns true when the list changed.
func (c *Config) IgnoreClone(path string, ignore bool) bool {
	path = strings.TrimSpace(path)
	if path == "" || c.IsIgnoredClone(path) == ignore {
		return false
	}
	if ignore {
		c.Global.IgnoredClones = append(c.Global.IgnoredClones, path)
		return true
	}
	want := clonePathKey(path)
	kept := c.Global.IgnoredClones[:0]
	for _, p := range c.Global.IgnoredClones {
		if clonePathKey(p) != want {
			kept = append(kept, p)
		}
	}
	c.Global.IgnoredClones = kept
	return true
}

// SamePath reports whether two clone paths name the same folder.
func SamePath(a, b string) bool {
	return clonePathKey(a) == clonePathKey(b)
}

// clonePathKey normalizes a clone path for comparison, like rootKey.
func clonePathKey(p string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(ExpandTilde(strings.TrimSpace(p)))))
}

// Clone returns a deep copy of the config, key orders included, so a scan
// can read it without holding the caller's lock.
func (c *Config) Clone() (*Config, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	out.SourceOrder = append([]string(nil), c.SourceOrder...)
	out.MirrorOrder = append([]string(nil), c.MirrorOrder...)
	out.WorkspaceOrder = append([]string(nil), c.WorkspaceOrder...)
	for key, src := range c.Sources {
		dst := out.Sources[key]
		dst.RepoOrder = append([]string(nil), src.RepoOrder...)
		out.Sources[key] = dst
	}
	for key, m := range c.Mirrors {
		dst := out.Mirrors[key]
		dst.RepoOrder = append([]string(nil), m.RepoOrder...)
		out.Mirrors[key] = dst
	}
	return &out, nil
}
