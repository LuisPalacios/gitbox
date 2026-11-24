// Package ops holds gitbox's application-level operations: account
// lifecycle, credential switching, clone planning and discovery. Each
// operation works on a *config.Config plus the files it owns on disk
// (clones, SSH keys, credential stores).
//
// Operations never save the config and never lock: the caller serializes
// access to cfg and persists it afterwards. Operations that touch every
// clone of an account (ReconfigureClones) are separate so the caller can
// run them after a successful save.
package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// validAccountKey matches alphanumeric keys with inner hyphens.
var validAccountKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

// ValidateAccountKey reports whether key is usable as an account key.
func ValidateAccountKey(key string) error {
	if key == "" {
		return fmt.Errorf("account key cannot be empty")
	}
	if !validAccountKey.MatchString(key) {
		return fmt.Errorf("invalid key %q: use letters, numbers, and hyphens", key)
	}
	return nil
}

// keyJunk matches runs of characters an account key can't hold.
var keyJunk = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// SuggestAccountKey returns a valid, unused account key for provider and
// username, such as "github-alice", so the guided setup never asks for one.
// Characters a key can't hold become single hyphens, and a key already used
// by an account or a source gets a numeric suffix ("github-alice-2").
func SuggestAccountKey(cfg *config.Config, provider, username string) string {
	base := strings.Trim(keyJunk.ReplaceAllString(provider+"-"+username, "-"), "-")
	if base == "" {
		base = "account"
	}
	taken := func(k string) bool {
		_, acct := cfg.Accounts[k]
		_, src := cfg.Sources[k]
		return acct || src
	}
	key := base
	for n := 2; taken(key); n++ {
		key = fmt.Sprintf("%s-%d", base, n)
	}
	return key
}

// AddAccount adds an account plus a matching source (same key) so repos can
// be added right away. An empty DefaultCredentialType becomes "gcm", and the
// credential sub-object for that type is filled in.
func AddAccount(cfg *config.Config, key string, acct config.Account) error {
	if reservedCredentialKey(cfg, key) {
		return ErrCredentialTransitionPending
	}
	if acct.DefaultCredentialType == "" {
		acct.DefaultCredentialType = "gcm"
	}
	applyCredentialType(&acct, key, acct.DefaultCredentialType)

	if err := cfg.AddAccount(key, acct); err != nil {
		return err
	}
	src := config.Source{Account: key, Repos: make(map[string]config.Repo)}
	if err := cfg.AddSource(key, src); err != nil {
		_ = cfg.DeleteAccount(key)
		return err
	}
	return nil
}

// DiscardAccount removes a just-added account and its sources from the
// config without touching the disk, so the guided setup can start over with
// different details. Unlike DeleteAccount it never removes a folder: the
// source folder may predate the account and hold the user's files. It
// refuses once any of the account's sources has repos.
func DiscardAccount(cfg *config.Config, accountKey string) error {
	if acct, ok := cfg.Accounts[accountKey]; !ok {
		return fmt.Errorf("account %q %w", accountKey, config.ErrNotFound)
	} else if acct.CredentialTransition != nil {
		return ErrCredentialTransitionPending
	}
	for sourceKey, src := range cfg.Sources {
		if src.Account == accountKey && len(src.Repos) > 0 {
			return fmt.Errorf("account %q already has repos in source %q", accountKey, sourceKey)
		}
	}
	_, err := cfg.CascadeDeleteAccount(accountKey)
	return err
}

// sourceRoots returns every root a source uses, tilde-expanded and deduped:
// its own root, then any other root one of its repos lives in.
func sourceRoots(cfg *config.Config, sourceKey string) []string {
	roots := []string{cfg.SourceRoot(sourceKey)}
	for _, repo := range cfg.Sources[sourceKey].Repos {
		r := cfg.RepoRoot(sourceKey, repo)
		if !slices.ContainsFunc(roots, func(x string) bool { return config.SameRoot(x, r) }) {
			roots = append(roots, r)
		}
	}
	return roots
}

// DeleteAccount removes the account, its credentials and the config entries
// that depend on it. It leaves clones in place unless removeFolders is set.
// When a folder cannot be deleted, the account and its credentials stay so
// the user can resolve the failure and retry.
func DeleteAccount(cfg *config.Config, accountKey string, removeFolders bool) (removed []string, errs []error) {
	if _, ok := cfg.Accounts[accountKey]; !ok {
		_, err := cfg.CascadeDeleteAccount(accountKey)
		return nil, []error{err}
	}
	if cfg.Accounts[accountKey].CredentialTransition != nil {
		return nil, []error{ErrCredentialTransitionPending}
	}
	impact := cfg.AccountDeletionImpact(accountKey)
	if removeFolders {
		type clone struct{ source, repo, path string }
		var clones []clone
		for _, sourceKey := range impact.Sources {
			for repoKey, repo := range cfg.Sources[sourceKey].Repos {
				clones = append(clones, clone{sourceKey, repoKey, status.RepoPath(cfg, sourceKey, repoKey, repo)})
			}
		}
		// Process explicitly tracked child clones before their parent.
		slices.SortFunc(clones, func(a, b clone) int {
			return strings.Compare(b.path, a.path)
		})
		for _, c := range clones {
			ok, err := removeCloneFolder(cfg, c.source, c.repo, c.path)
			if err != nil {
				errs = append(errs, &CloneFolderError{err})
			} else if ok {
				removed = append(removed, c.path)
			}
		}
	}
	if len(errs) > 0 {
		return removed, errs
	}

	// A new account with the same key must not inherit the old token or
	// SSH key (#242, #244). The credential-change flow handles GCM entries.
	if err := credential.DeleteToken(accountKey); err != nil {
		return removed, []error{err}
	}
	if _, err := removeSSHKey(cfg, accountKey); err != nil {
		return removed, []error{err}
	}

	if !removeFolders {
		for _, sourceKey := range impact.Sources {
			repos := cfg.Sources[sourceKey].Repos
			for _, repoKey := range slices.Sorted(maps.Keys(repos)) {
				cfg.IgnoreClone(status.RepoPath(cfg, sourceKey, repoKey, repos[repoKey]), true)
			}
		}
	}
	_, err := cfg.CascadeDeleteAccount(accountKey)
	if err != nil {
		return removed, []error{err}
	}
	return removed, nil
}

// StopTrackingRepo forgets the repo and keeps reconcile from adopting the
// clone again. An ignored path can be restored from Unmatched clones.
func StopTrackingRepo(cfg *config.Config, sourceKey, repoKey string) error {
	repo := cfg.Sources[sourceKey].Repos[repoKey]
	path := status.RepoPath(cfg, sourceKey, repoKey, repo)
	if err := cfg.DeleteRepo(sourceKey, repoKey); err != nil {
		return err
	}
	cfg.IgnoreClone(path, true)
	return nil
}

// DeleteRepoClone removes the clone folder before forgetting the repo.
func DeleteRepoClone(cfg *config.Config, sourceKey, repoKey string) error {
	repo, ok := cfg.Sources[sourceKey].Repos[repoKey]
	if !ok {
		return cfg.DeleteRepo(sourceKey, repoKey)
	}
	path := status.RepoPath(cfg, sourceKey, repoKey, repo)
	if _, err := removeCloneFolder(cfg, sourceKey, repoKey, path); err != nil {
		return err
	}
	return cfg.DeleteRepo(sourceKey, repoKey)
}

// CloneFolderError identifies a failed account folder deletion. Unwrap
// exposes the original filesystem error, including its path.
type CloneFolderError struct{ err error }

func (e *CloneFolderError) Error() string { return e.err.Error() }
func (e *CloneFolderError) Unwrap() error { return e.err }

// removeCloneFolder returns whether it removed a folder. A *os.PathError
// carries the failing path and underlying filesystem error.
func removeCloneFolder(cfg *config.Config, sourceKey, repoKey, path string) (bool, error) {
	path = filepath.Clean(path)
	refuse := func(reason string) error {
		return &os.PathError{Op: "delete clone", Path: path, Err: errors.New(reason)}
	}
	if !filepath.IsAbs(path) || path == filepath.Dir(path) {
		return false, refuse("the clone path must be an absolute folder below the filesystem root")
	}
	repo := cfg.Sources[sourceKey].Repos[repoKey]
	if status.RepoRootOffline(cfg, sourceKey, repo) {
		return false, refuse("the clone root is offline")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && pathkey.Of(path).Access == pathkey.Missing {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || !git.IsRepo(path) || git.IsLinkedWorktree(path) {
		return false, refuse("the path is not a standalone Git clone directory")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	if unsafeClonePlacement(cfg, sourceKey, repoKey, path, resolved) {
		return false, refuse("the path overlaps a configured folder or resolves outside its expected location")
	}

	// Read the stored URL rather than applying global Git URL rewrites.
	res, err := git.Run(context.Background(), git.Request{Dir: resolved, Args: []string{"config", "--local", "--get", "remote.origin.url"}})
	if err != nil {
		return false, refuse("the clone's origin cannot be verified")
	}
	host, owner, name, err := git.ParseRemoteURL(string(res.Stdout))
	if err != nil || owner+"/"+name != repoKey || !adopt.HostMatches(cfg.Accounts[cfg.Sources[sourceKey].Account], host) {
		return false, refuse("the clone's origin no longer matches the tracked project")
	}

	gitDir := filepath.Join(resolved, ".git")
	adminDir := resolved
	if marker, err := os.Lstat(gitDir); err == nil {
		if !marker.IsDir() {
			return false, refuse("the clone uses a separate Git directory")
		}
		adminDir = gitDir
	} else if !os.IsNotExist(err) {
		return false, err
	}
	worktrees, err := os.ReadDir(filepath.Join(adminDir, "worktrees"))
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if len(worktrees) > 0 {
		return false, refuse("the clone has linked worktrees")
	}
	err = filepath.WalkDir(resolved, func(child string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if child == resolved {
			return nil
		}
		if child == gitDir {
			return fs.SkipDir
		}
		if entry.Name() == ".git" || (entry.IsDir() && git.IsRepo(child)) {
			return refuse("the clone contains a nested Git repository")
		}
		return nil
	})
	if err != nil {
		return false, err
	}

	// Confine recursive removal to the verified physical parent. Confirm
	// that the target still names the directory inspected above.
	parent, err := os.OpenRoot(filepath.Dir(resolved))
	if err != nil {
		return false, err
	}
	defer parent.Close()
	name = filepath.Base(resolved)
	current, err := parent.Lstat(name)
	if err != nil {
		return false, err
	}
	if !os.SameFile(info, current) {
		return false, refuse("the folder changed during confirmation")
	}
	if err := parent.RemoveAll(name); err != nil {
		if pathErr, ok := errors.AsType[*os.PathError](err); ok && !filepath.IsAbs(pathErr.Path) {
			return false, &os.PathError{Op: pathErr.Op, Path: filepath.Join(filepath.Dir(path), pathErr.Path), Err: pathErr.Err}
		}
		return false, err
	}
	return true, nil
}

func unsafeClonePlacement(cfg *config.Config, sourceKey, repoKey, path, resolved string) bool {
	resolvedKey := pathkey.Compare(resolved)
	repo := cfg.Sources[sourceKey].Repos[repoKey]
	root := cfg.RepoRoot(sourceKey, repo)
	for _, candidate := range append(slices.Clone(cfg.Global.Roots), root) {
		if candidate == "" {
			continue
		}
		candidate = config.ExpandTilde(candidate)
		if key := pathkey.Compare(candidate); key == resolvedKey || lexicallyInside(resolvedKey, key) {
			return true
		}
		if lexicallyInside(candidate, path) {
			rel, _ := filepath.Rel(candidate, path)
			physicalRoot, err := filepath.EvalSymlinks(candidate)
			if err != nil || !samePhysicalPlacement(physicalRoot, rel, resolved) ||
				!lexicallyInside(pathkey.Compare(physicalRoot), resolvedKey) {
				return true
			}
		}
	}
	if repo.CloneFolder == "" && !lexicallyInside(root, path) {
		return true
	}
	for otherSource, src := range cfg.Sources {
		for _, root := range sourceRoots(cfg, otherSource) {
			key := pathkey.Compare(filepath.Join(root, src.EffectiveFolder(otherSource)))
			if key == resolvedKey || lexicallyInside(resolvedKey, key) {
				return true
			}
		}
		for otherRepo, repo := range src.Repos {
			if sourceKey == otherSource && repoKey == otherRepo {
				continue
			}
			other := status.RepoPath(cfg, otherSource, otherRepo, repo)
			key := pathkey.Of(other)
			if key.Cmp == resolvedKey || (key.Access != pathkey.Missing && lexicallyInside(resolvedKey, key.Cmp)) {
				return true
			}
		}
	}
	return false
}

func samePhysicalPlacement(root, rel, resolved string) bool {
	// Walk from the canonical root to reject aliases below it. On Windows
	// Lstat marks directory junctions with ModeSymlink as well.
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		root = filepath.Join(root, part)
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return config.SameRoot(root, resolved)
}

func lexicallyInside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// ClonedRepoPaths returns the on-disk path of every cloned repo that belongs
// to the account, keyed by "sourceKey/repoKey".
func ClonedRepoPaths(cfg *config.Config, accountKey string) map[string]string {
	paths := map[string]string{}
	for sourceKey, src := range cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		for repoKey, repo := range src.Repos {
			path := status.RepoPath(cfg, sourceKey, repoKey, repo)
			if git.IsRepo(path) {
				paths[sourceKey+"/"+repoKey] = path
			}
		}
	}
	return paths
}

// CountClonedRepos returns how many of the account's repos are cloned.
func CountClonedRepos(cfg *config.Config, accountKey string) int {
	return len(ClonedRepoPaths(cfg, accountKey))
}

// ReconfigureClones heals every cloned repo of the account so its identity,
// origin URL and credential config match the current account settings.
// Returns one report per clone that changed or warned.
func ReconfigureClones(cfg *config.Config, accountKey string) []heal.Report {
	return ReconfigureClonesWith(cfg, accountKey, func(cfg *config.Config, sourceKey, repoKey string) heal.Report {
		return heal.RepoContext(context.Background(), cfg, sourceKey, repoKey)
	})
}

// ReconfigureClonesWith lets a config owner serialize each repair with
// other operations on the same clone.
func ReconfigureClonesWith(cfg *config.Config, accountKey string, repair func(*config.Config, string, string) heal.Report) []heal.Report {
	var reports []heal.Report
	for _, sourceKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[sourceKey]
		if src.Account != accountKey {
			continue
		}
		for _, repoKey := range src.OrderedRepoKeys() {
			if r := repair(cfg, sourceKey, repoKey); r.HasWork() {
				reports = append(reports, r)
			}
		}
	}
	return reports
}

// HostnameFromURL extracts the hostname from a provider URL. A value without
// a parseable host is returned with any http(s) scheme stripped.
func HostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	for _, prefix := range []string{"https://", "http://"} {
		if after, ok := strings.CutPrefix(rawURL, prefix); ok {
			return after
		}
	}
	return rawURL
}
