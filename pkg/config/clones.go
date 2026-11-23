package config

import (
	"encoding/json"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/pathkey"
)

// IsIgnoredClone reports whether path is in Global.IgnoredClones.
func (c *Config) IsIgnoredClone(path string) bool {
	want := pathkey.Compare(path)
	for _, p := range c.Global.IgnoredClones {
		if pathkey.Compare(p) == want {
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
	want := pathkey.Compare(path)
	kept := c.Global.IgnoredClones[:0]
	for _, p := range c.Global.IgnoredClones {
		if pathkey.Compare(p) != want {
			kept = append(kept, p)
		}
	}
	c.Global.IgnoredClones = kept
	return true
}

// SamePath reports whether two clone paths name the same folder.
func SamePath(a, b string) bool {
	return pathkey.Compare(a) == pathkey.Compare(b)
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
	// The ordinary JSON copy excludes persistence-only extensions. Strings
	// are immutable, so copying each extension keeps the clone detached.
	out.Global.extra = c.Global.extra
	for key, account := range c.Accounts {
		cloned := out.Accounts[key]
		cloned.extra = account.extra
		out.Accounts[key] = cloned
	}
	out.SourceOrder = append([]string(nil), c.SourceOrder...)
	out.MirrorOrder = append([]string(nil), c.MirrorOrder...)
	for key, src := range c.Sources {
		dst := out.Sources[key]
		dst.RepoOrder = append([]string(nil), src.RepoOrder...)
		dst.extra = src.extra
		for repoKey, repo := range src.Repos {
			cloned := dst.Repos[repoKey]
			cloned.extra = repo.extra
			dst.Repos[repoKey] = cloned
		}
		out.Sources[key] = dst
	}
	for key, m := range c.Mirrors {
		dst := out.Mirrors[key]
		dst.RepoOrder = append([]string(nil), m.RepoOrder...)
		out.Mirrors[key] = dst
	}
	return &out, nil
}
