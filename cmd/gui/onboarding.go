package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/ops"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// OnboardingRootPreviewDTO describes the selected folder before onboarding writes anything.
// Sampled means the bounded directory read may have missed additional clones.
type OnboardingRootPreviewDTO struct {
	Abs            string `json:"abs"`
	Exists         bool   `json:"exists"`
	IsDir          bool   `json:"isDir"`
	Writable       bool   `json:"writable"`
	ExistingClones int    `json:"existingClones"`
	Sampled        bool   `json:"sampled"`
}

func (a *App) PreviewRoot(path string) (OnboardingRootPreviewDTO, error) {
	return a.be().PreviewRoot(path)
}

func (b localBackend) PreviewRoot(path string) (OnboardingRootPreviewDTO, error) {
	if path == "" {
		path = "~/00.git"
		if b.a.testMode {
			path = filepath.Join(filepath.Dir(filepath.Dir(b.a.cfgPath)), "git")
		}
	}
	abs, err := hostPath(path)
	if err != nil {
		return OnboardingRootPreviewDTO{}, err
	}
	out := OnboardingRootPreviewDTO{Abs: abs}
	for p := abs; ; p = filepath.Dir(p) {
		info, err := os.Stat(p)
		if err == nil {
			if p == abs {
				out.Exists, out.IsDir = true, info.IsDir()
			}
			out.Writable = info.IsDir() && onboardingFolderWritable(p)
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || p == filepath.Dir(p) {
			return out, nil
		}
	}
	if out.IsDir {
		budget := 256
		out.ExistingClones, out.Sampled = countOnboardingClones(abs, 3, &budget)
	}
	return out, nil
}

// Sample up to the usual root/account/owner/repo layout. Skip hidden folders,
// symlinks and working trees. The entry budget covers the entire sample.
func countOnboardingClones(dir string, depth int, budget *int) (int, bool) {
	if marker, err := os.Lstat(filepath.Join(dir, ".git")); err == nil && (marker.IsDir() || marker.Mode().IsRegular()) {
		return 1, false
	}
	if depth == 0 || *budget == 0 {
		return 0, true
	}
	f, err := os.Open(dir)
	if err != nil {
		return 0, true
	}
	entries, err := f.ReadDir(*budget + 1)
	_ = f.Close()
	sampled := err != nil && !errors.Is(err, io.EOF)
	if len(entries) > *budget {
		entries = entries[:*budget]
		sampled = true
	}
	*budget -= len(entries)
	count := 0
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		n, partial := countOnboardingClones(filepath.Join(dir, e.Name()), depth-1, budget)
		count += n
		sampled = sampled || partial
	}
	return count, sampled
}

func (a *App) PreviewClonePath(source, repo string) (string, error) {
	return a.be().PreviewClonePath(source, repo)
}

func (b localBackend) PreviewClonePath(source, repo string) (string, error) {
	if !validOnboardingRepo(repo) {
		return "", errors.New("invalid project name")
	}
	b.a.mu.Lock()
	defer b.a.mu.Unlock()
	key, err := ops.ResolveSourceKey(b.a.cfg, source)
	if err != nil {
		return "", err
	}
	return status.RepoPath(b.a.cfg, key, repo, b.a.cfg.Sources[key].Repos[repo]), nil
}

func validOnboardingRepo(repo string) bool {
	return strings.Contains(repo, "/") && fs.ValidPath(repo) && !strings.ContainsAny(repo, "\\:\x00\r\n") && !slices.Contains(strings.Split(repo, "/"), ".git")
}

// AdoptFirstProject confirms one freshly inspected clone. Other eligible
// clones remain available for the dashboard's normal reconciliation.
func (a *App) AdoptFirstProject(account, repo, path string) (bool, error) {
	return a.be().AdoptFirstProject(account, repo, path)
}

func (b localBackend) AdoptFirstProject(account, repo, path string) (bool, error) {
	abs, err := hostPath(path)
	if err != nil {
		return false, err
	}
	if !validOnboardingRepo(repo) {
		return false, errors.New("invalid project name")
	}
	a := b.a
	a.reconcileMu.Lock()
	defer a.reconcileMu.Unlock()
	a.mu.Lock()
	if err := a.saveRefused(); err != nil {
		a.mu.Unlock()
		return false, err
	}
	cfg, err := a.cfg.Clone()
	a.mu.Unlock()
	if err != nil {
		return false, err
	}
	key, err := ops.ResolveSourceKey(cfg, account)
	if err != nil {
		return false, err
	}
	src := cfg.Sources[key]
	if src.Account != account {
		return false, errors.New("the selected account has changed")
	}
	root, err := filepath.EvalSymlinks(cfg.SourceRoot(key))
	if err != nil {
		return false, errors.New("the projects folder is unavailable")
	}
	physical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return false, errors.New("the selected folder is unavailable")
	}
	rel, err := filepath.Rel(root, physical)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return false, errors.New("the selected clone is outside the projects folder")
	}
	gitDir, err := os.Lstat(filepath.Join(physical, ".git"))
	if err != nil || !gitDir.IsDir() {
		return false, errors.New("the selected folder is not a project")
	}
	if prior, ok := src.Repos[repo]; ok {
		prev, err := filepath.EvalSymlinks(status.RepoPath(cfg, key, repo, prior))
		if err == nil && prev == physical {
			raw, err := git.RemoteURL(physical)
			host, org, name, parseErr := git.ParseRemoteURL(raw)
			if err != nil || parseErr != nil || org+"/"+name != repo || !adopt.HostMatches(cfg.Accounts[account], host) {
				return false, errors.New("the project identity changed")
			}
			return a.firstProjectIdentityReady(cfg, key, repo), nil
		}
		return false, errors.New("the project is already tracked in another folder")
	}
	rows, err := adopt.FindOrphansUnder(cfg, physical)
	if err != nil {
		return false, err
	}
	if len(rows) != 1 || rows[0].Path != physical || rows[0].RepoKey != repo || !slices.Contains(rows[0].Candidates, account) {
		return false, errors.New("the selected project no longer matches this account")
	}
	var source string
	saved, err := a.commitConfig(func(live *config.Config) error {
		if !config.EqualIgnoringUIState(cfg, live) {
			return ErrStalePlan
		}
		var addErr error
		source, addErr = reconcile.Adopt(live, rows[0], account)
		return addErr
	})
	if err != nil {
		return false, err
	}
	return a.firstProjectIdentityReady(saved, source, repo), nil
}

// A saved clone can still need identity setup. A retry checks that setup
// again, including when the first save succeeded but repair was interrupted.
func (a *App) firstProjectIdentityReady(cfg *config.Config, source, repo string) bool {
	report := a.localSession.healRepo(cfg, source, repo)
	return report.Path != "" && report.Skipped == "" && len(report.Warnings) == 0
}
