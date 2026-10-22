package workspace

import (
	"sort"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// DiscoverySnapshot copies only the inputs used to locate workspace files and
// resolve members. The caller must hold its config lock while taking the copy;
// Discover can then scan it without locking the live configuration.
func DiscoverySnapshot(cfg *config.Config) *config.Config {
	out := &config.Config{
		Global:      config.GlobalConfig{Roots: append([]string(nil), cfg.Global.Roots...)},
		Sources:     make(map[string]config.Source, len(cfg.Sources)),
		SourceOrder: append([]string(nil), cfg.SourceOrder...),
	}
	if len(out.SourceOrder) == 0 {
		for key := range cfg.Sources {
			out.SourceOrder = append(out.SourceOrder, key)
		}
		sort.Strings(out.SourceOrder)
	}
	for key, src := range cfg.Sources {
		dst := config.Source{Folder: src.Folder, Root: src.Root, Repos: make(map[string]config.Repo, len(src.Repos)), RepoOrder: append([]string(nil), src.RepoOrder...)}
		for repoKey, repo := range src.Repos {
			dst.Repos[repoKey] = config.Repo{IdFolder: repo.IdFolder, CloneFolder: repo.CloneFolder, Root: repo.Root, Container: repo.Container}
			if len(src.RepoOrder) == 0 {
				dst.RepoOrder = append(dst.RepoOrder, repoKey)
			}
		}
		if len(src.RepoOrder) == 0 {
			sort.Strings(dst.RepoOrder)
		}
		out.Sources[key] = dst
	}
	return out
}
