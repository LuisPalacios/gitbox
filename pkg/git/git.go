// Package git provides subprocess wrappers for Git operations.
package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// gitBin is the resolved path to the git binary. Resolved once on first use.
var (
	gitBin     string
	gitBinOnce sync.Once
)

// homebrewDirs are the Homebrew bin directories to prepend to PATH on macOS.
// On macOS, GUI apps (and some SSH sessions) inherit a minimal PATH that
// excludes Homebrew directories. Without these, git sub-commands like
// "git-credential-manager" silently fail because they aren't on PATH —
// even when we invoke the Homebrew git binary directly.
//
// Apple Silicon installs to /opt/homebrew/bin; Intel Macs use /usr/local/bin.
// Both are listed so the binary works on either architecture.
var homebrewDirs = []string{
	"/opt/homebrew/bin", // Apple Silicon Homebrew
	"/usr/local/bin",    // Intel Homebrew
}

// GitBin returns the path to the git binary.
//
// On macOS the system git (/usr/bin/git) ships WITHOUT Git Credential Manager
// and is often an outdated shim. We probe Homebrew install locations first so
// that GCM and other Homebrew-installed git extensions are available.
//
// IMPORTANT: Do NOT remove the Homebrew probing — the macOS system git does
// not include GCM, and using it breaks credential operations every time.
func GitBin() string {
	gitBinOnce.Do(func() {
		if runtime.GOOS == "darwin" {
			for _, dir := range homebrewDirs {
				candidate := filepath.Join(dir, "git")
				if _, err := os.Stat(candidate); err == nil {
					gitBin = candidate
					return
				}
			}
		}
		// Fallback: whatever is on PATH.
		if p, err := exec.LookPath("git"); err == nil {
			gitBin = p
		} else {
			gitBin = "git" // last resort, let exec fail with a clear error
		}
	})
	return gitBin
}

// Environ returns os.Environ() with Homebrew bin directories prepended to PATH
// on macOS. This ensures that git sub-commands (e.g. git-credential-manager)
// installed via Homebrew are found even when the parent process has a minimal
// PATH (GUI apps, LaunchAgents, restricted SSH sessions).
//
// On non-macOS platforms this returns os.Environ() unchanged.
//
// IMPORTANT: Always use git.Environ() (not bare os.Environ()) when setting
// cmd.Env for git subprocesses — otherwise GCM breaks on macOS.
func Environ() []string {
	env := os.Environ()
	if runtime.GOOS != "darwin" {
		return env
	}
	return ensureHomebrewPATH(env)
}

// ensureHomebrewPATH prepends Homebrew bin dirs to PATH if not already present.
func ensureHomebrewPATH(env []string) []string {
	for i, e := range env {
		if currentPath, ok := strings.CutPrefix(e, "PATH="); ok {
			var missing []string
			for _, dir := range homebrewDirs {
				if !strings.Contains(currentPath, dir) {
					missing = append(missing, dir)
				}
			}
			if len(missing) > 0 {
				env[i] = "PATH=" + strings.Join(missing, ":") + ":" + currentPath
			}
			return env
		}
	}
	// No PATH found at all — set one with Homebrew dirs.
	return append(env, "PATH="+strings.Join(homebrewDirs, ":"))
}

// CloneOpts configures a git clone operation.
type CloneOpts struct {
	Depth      int      // Shallow clone depth (0 = full clone)
	Branch     string   // Specific branch to clone
	Bare       bool     // Bare clone (for mirrors)
	Mirror     bool     // Mirror clone
	Quiet      bool     // Suppress stdout/stderr (capture instead of forward)
	ConfigArgs []string // Extra -c key=value args (e.g. "credential.helper=")
}

// RepoStatus holds the parsed output of git status.
type RepoStatus struct {
	Branch    string // Current branch name
	Upstream  string // Upstream tracking branch
	Ahead     int    // Commits ahead of upstream
	Behind    int    // Commits behind upstream
	Modified  int    // Modified files count
	Changed   int    // Tracked files with staged or unstaged changes
	Added     int    // Added (staged) files count
	Deleted   int    // Deleted files count
	Untracked int    // Untracked files count
	Conflicts int    // Conflicted files count
}

// CloneProgress holds a progress update from git clone.
type CloneProgress struct {
	Phase   string // e.g. "Receiving objects", "Resolving deltas"
	Percent int    // 0-100
}

// progressRe matches git progress lines like "Receiving objects:  45% (114/253)".
var progressRe = regexp.MustCompile(`^(remote: )?([\w ]+):\s+(\d+)%`)

// cloneArgs keeps each -c entry intact so Git receives helper strings verbatim.
func cloneArgs(url, dest string, opts CloneOpts) []string {
	args := []string{"clone", "--progress"}
	for _, c := range opts.ConfigArgs {
		args = append(args, "-c", c)
	}
	if opts.Mirror {
		args = append(args, "--mirror")
	} else if opts.Bare {
		args = append(args, "--bare")
	}
	if opts.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(opts.Depth))
	}
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
	}
	args = append(args, url, dest)

	return args
}

// CloneWithProgress runs git clone --progress and calls onProgress with updates.
// stderr is parsed for percentage lines; stdout is discarded. It stops when
// ctx is cancelled and leaves whatever git wrote at dest in place.
func CloneWithProgress(ctx context.Context, url, dest string, opts CloneOpts, onProgress func(CloneProgress)) error {
	args := cloneArgs(url, dest, opts)

	// Git progress uses \r for in-place updates within a line. The reader
	// splits what git prints to stderr, as it prints it, on both \r and \n.
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		parseProgress(pr, onProgress)
		_, _ = io.Copy(io.Discard, pr) // never block git on a reader that stopped
	}()
	res, err := Default.Run(ctx, Request{Dir: ".", Args: args, Stderr: pw})
	_ = pw.Close()
	<-done
	if err != nil {
		// What git printed only classifies the failure; it never reaches
		// the error text.
		return failure(ctx, "clone", err, res, res.Stderr)
	}
	return nil
}

// parseProgress reads git's stderr and extracts progress percentages.
func parseProgress(r io.Reader, onProgress func(CloneProgress)) {
	scanner := bufio.NewScanner(r)
	// Git uses \r for progress updates — split on both \r and \n.
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		for i, b := range data {
			if b == '\r' || b == '\n' {
				return i + 1, data[:i], nil
			}
		}
		if atEOF {
			return len(data), data, nil
		}
		return 0, nil, nil // need more data
	})

	for scanner.Scan() {
		line := scanner.Text()
		m := progressRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		phase := strings.TrimSpace(m[2])
		pct, _ := strconv.Atoi(m[3])
		onProgress(CloneProgress{Phase: phase, Percent: pct})
	}
}

// IsUpstreamGoneError reports whether a git fetch/pull/ls-remote error
// indicates that the upstream repository no longer exists (was deleted,
// renamed without a redirect, archived privately, or credentials lost the
// permission to see it). These all surface as "repository not found" or
// equivalent strings from the git client, so we match on the common
// substrings rather than on error types (each provider phrases it
// slightly differently but all converge on one of these).
func IsUpstreamGoneError(err error) bool {
	if err == nil {
		return false
	}
	return upstreamGoneText(strings.ToLower(diagnosticText(err)))
}

// PushMirror runs `git push --mirror <url>` from repoPath, pushing every
// ref and tag to the target URL. extraConfig, if non-nil, is passed as
// `-c <key=value>` args before the push subcommand — used to inject
// `credential.helper=` so an embedded user:PAT in the URL isn't
// overridden by a GCM-managed credential for the same host.
//
// Output is captured so the caller can surface provider-side errors
// (e.g. "remote: Repository is empty" vs. permission issues).
// It stops when ctx is cancelled, and the processes git started with it.
func PushMirror(ctx context.Context, repoPath, url string, extraConfig []string) (string, error) {
	args := make([]string, 0, len(extraConfig)*2+3)
	for _, kv := range extraConfig {
		args = append(args, "-c", kv)
	}
	args = append(args, "push", "--mirror", url)

	res, err := Default.Run(ctx, Request{Dir: repoPath, Args: args})
	out := res.combined()
	if err != nil {
		return string(out), failure(ctx, "push", err, res, out)
	}
	return string(out), nil
}

// FetchTagsAndPrune runs `git fetch --prune --tags` in repoPath. Used
// as the first move-flow step so the upcoming push --mirror carries
// every ref (tags included) and stale remote branches are dropped. It
// stops when ctx is cancelled.
func FetchTagsAndPrune(ctx context.Context, repoPath string) error {
	return runContext(ctx, repoPath, "fetch", "--prune", "--tags")
}

// PullQuietContext runs git pull --ff-only, capturing output instead of
// forwarding it, and stops when ctx is cancelled: the runner asks git to stop
// with an interrupt, so git removes its lock files.
func PullQuietContext(ctx context.Context, repoPath string) error {
	res, err := Default.Run(ctx, Request{Dir: repoPath, Args: []string{"pull", "--ff-only"}})
	if err != nil {
		return failure(ctx, "pull", err, res, res.Stderr)
	}
	return nil
}

// StatusContext runs git status and parses the result, and stops when ctx is
// cancelled. I disable optional locks to prevent background reads from
// refreshing the index.
func StatusContext(ctx context.Context, repoPath string) (RepoStatus, error) {
	status, err := readStatus(ctx, repoPath)
	if err != nil {
		return RepoStatus{}, err
	}
	return status.summary(), nil
}

// FileChange describes a single changed file for human-readable display.
type FileChange struct {
	Kind string `json:"kind"` // "modified", "added", "deleted", "renamed", "conflict"
	Path string `json:"path"`
}

// DetailedStatus returns the list of changed and untracked files in a repo.
// I disable optional locks to leave the index untouched when opening repo details.
func DetailedStatus(repoPath string) (branch string, ahead, behind int, changed []FileChange, untracked []string, err error) {
	status, err := readStatus(context.Background(), repoPath)
	if err != nil {
		return "", 0, 0, nil, nil, err
	}
	changed, untracked = status.details()
	return status.branch.Branch, status.branch.Ahead, status.branch.Behind, changed, untracked, nil
}

// RemoteURL inspects the origin of a discovered clone with command-scoped settings.
func RemoteURL(repoPath string) (string, error) {
	out, err := inspectOutput(repoPath, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CredentialUsernames returns every value of repo-local git config keys that
// match `credential.<url>.username`. Returns nil if none are set. Errors from
// git (including "no match", which exits 1) are swallowed — a repo without
// credential usernames is a normal case, not a failure.
func CredentialUsernames(repoPath string) []string {
	out, err := inspectOutput(repoPath, "config", "--get-regexp", `^credential\..*\.username$`)
	if err != nil {
		return nil
	}
	var names []string
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Lines look like: "credential.https://github.com.username SW-First-Last"
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		names = append(names, fields[len(fields)-1])
	}
	return names
}

// ConfigSet sets a git config value in the given repo.
func ConfigSet(repoPath, key, value string) error {
	return run(repoPath, "config", key, value)
}

// ConfigUnsetAll removes ALL values of a multi-value key from repo-level git config.
// Returns nil if the key does not exist.
func ConfigUnsetAll(repoPath, key string) error {
	err := run(repoPath, "config", "--unset-all", key)
	if err != nil {
		if exitCode(err) == 5 {
			return nil
		}
	}
	return err
}

// ConfigAdd appends a value to a multi-value key in repo-level git config.
func ConfigAdd(repoPath, key, value string) error {
	return run(repoPath, "config", "--add", key, value)
}

// GlobalConfigSet sets a global git config value (git config --global).
func GlobalConfigSet(key, value string) error {
	return run(".", "config", "--global", key, value)
}

// GlobalConfigReplaceAll sets a global git config key to a single value,
// replacing every existing value. Plain GlobalConfigSet fails on a key that
// already holds several values (exit 5), as credential.helper often does.
func GlobalConfigReplaceAll(key, value string) error {
	return run(".", "config", "--global", "--replace-all", key, value)
}

// GlobalConfigGet reads a global git config value.
func GlobalConfigGet(key string) (string, error) {
	out, err := output(".", "config", "--global", "--get", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// GlobalConfigGetAll reads every value of a (possibly multi-valued) global git
// config key, in the order git stores them. Empty values are preserved — they
// matter for credential.helper, where an empty entry resets the helper chain.
// Returns an empty slice (nil error) when the key is absent.
func GlobalConfigGetAll(key string) ([]string, error) {
	out, err := output(".", "config", "--global", "--get-all", key)
	if err != nil {
		// git config --get-all exits with code 1 when the key does not exist.
		if exitCode(err) == 1 {
			return nil, nil
		}
		return nil, err
	}
	// git prints each value followed by a newline; strip the single trailing
	// newline, then split. An all-empty result means one empty-string value.
	out = strings.TrimSuffix(out, "\n")
	return strings.Split(out, "\n"), nil
}

// GlobalConfigUnset removes a key from global git config.
// Returns nil if the key does not exist.
func GlobalConfigUnset(key string) error {
	err := run(".", "config", "--global", "--unset", key)
	if err != nil {
		// git config --unset exits with code 5 if the key is not found.
		if exitCode(err) == 5 {
			return nil
		}
	}
	return err
}

// IsLinkedWorktree reports whether path is a linked worktree of another
// clone: its .git is a file pointing at the main clone's git directory.
func IsLinkedWorktree(path string) bool {
	data, err := os.ReadFile(filepath.Join(path, ".git"))
	return err == nil && strings.HasPrefix(string(data), "gitdir: ")
}

// IsRepo checks if the given path contains a .git directory or is a bare repo.
func IsRepo(path string) bool {
	gitDir := filepath.Join(path, ".git")
	if info, err := os.Stat(gitDir); err == nil {
		return info.IsDir()
	}
	// Check if it's a bare repo (has HEAD file directly).
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err == nil {
		if _, err := os.Stat(filepath.Join(path, "refs")); err == nil {
			return true
		}
	}
	return false
}

// IsIncompleteClone reports a clone that git stopped before it finished:
// one whose HEAD is git's placeholder for a head not known yet, or one
// whose HEAD names no commit while the clone never got its fetch refspec.
// A clone of an empty repository has the refspec, so it is complete. A
// clone killed outright leaves such a folder behind; one that git was asked
// to stop removes its own folder. It reads files only.
func IsIncompleteClone(path string) bool {
	gitDir := filepath.Join(path, ".git")
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return false
	}
	ref, symbolic := strings.CutPrefix(strings.TrimSpace(string(head)), "ref: ")
	if !symbolic {
		return false // a detached HEAD names a commit
	}
	if ref == "refs/heads/.invalid" {
		return true
	}
	if _, err := os.Stat(filepath.Join(gitDir, "reftable")); err == nil {
		return false // refs this reader does not parse
	}
	if _, err := os.Stat(filepath.Join(gitDir, filepath.FromSlash(ref))); err == nil {
		return false
	}
	if packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")); err == nil && strings.Contains(string(packed), " "+ref+"\n") {
		return false
	}
	cfg, err := os.ReadFile(filepath.Join(gitDir, "config"))
	return err == nil && !strings.Contains(string(cfg), "fetch = ")
}

// SetRemoteURL sets the URL of a remote (typically "origin").
func SetRemoteURL(repoPath, remote, url string) error {
	return SetRemoteURLContext(context.Background(), repoPath, remote, url)
}

// SetRemoteURLContext is SetRemoteURL that stops when ctx is cancelled.
func SetRemoteURLContext(ctx context.Context, repoPath, remote, url string) error {
	return runContext(ctx, repoPath, "remote", "set-url", remote, url)
}

// CurrentBranch returns the current branch name.
func CurrentBranch(repoPath string) (string, error) {
	out, err := output(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ─── Branch sweep ─────────────────────────────────────────────

// SweepResult holds the outcome of scanning for stale branches.
type SweepResult struct {
	DefaultBranch string   `json:"defaultBranch"`
	CurrentBranch string   `json:"currentBranch"`
	Merged        []string `json:"merged"`   // merged into default branch
	Gone          []string `json:"gone"`     // upstream tracking ref deleted
	Squashed      []string `json:"squashed"` // squash-merged into default (different commits, same changes)
}

// DefaultBranch returns the default branch name (e.g. "main", "master").
// It checks origin/HEAD first, then falls back to probing main/master locally.
func DefaultBranch(repoPath string) (string, error) {
	return DefaultBranchContext(context.Background(), repoPath)
}

// DefaultBranchContext is DefaultBranch that stops when ctx is cancelled.
func DefaultBranchContext(ctx context.Context, repoPath string) (string, error) {
	// Try origin/HEAD — set by git clone or git remote set-head.
	out, err := outputContext(ctx, repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		// "refs/remotes/origin/main" → "main"
		if parts := strings.Split(ref, "/"); len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fallback: check if main or master exist locally.
	for _, candidate := range []string{"main", "master"} {
		if _, err := outputContext(ctx, repoPath, "rev-parse", "--verify", "refs/heads/"+candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot determine default branch")
}

// SweepBranches identifies stale local branches without deleting anything.
// A branch is stale if it is merged into the default branch or its upstream
// tracking ref has been deleted. The current branch and default branch are
// never included.
func SweepBranches(repoPath string) (SweepResult, error) {
	defBranch, err := DefaultBranch(repoPath)
	if err != nil {
		return SweepResult{}, err
	}
	curBranch, err := CurrentBranch(repoPath)
	if err != nil {
		return SweepResult{}, err
	}

	merged, err := mergedBranches(repoPath, defBranch)
	if err != nil {
		return SweepResult{}, err
	}
	gone, err := goneBranches(repoPath)
	if err != nil {
		return SweepResult{}, err
	}

	// Build sets for de-duplication.
	protect := map[string]bool{defBranch: true, curBranch: true}
	goneSet := make(map[string]bool, len(gone))
	for _, b := range gone {
		goneSet[b] = true
	}
	mergedSet := make(map[string]bool, len(merged))
	for _, b := range merged {
		mergedSet[b] = true
	}

	// Filter gone and merged: remove protected, de-dup.
	var filteredGone []string
	for _, b := range gone {
		if protect[b] {
			continue
		}
		filteredGone = append(filteredGone, b)
	}
	var filteredMerged []string
	for _, b := range merged {
		if protect[b] || goneSet[b] {
			continue
		}
		filteredMerged = append(filteredMerged, b)
	}

	// Detect squash-merged branches: changes are in default but via
	// different commits (squash-and-merge or rebase-and-merge on the server).
	// Only check branches not already categorized as merged or gone.
	allBranches, _ := listLocalBranches(repoPath)
	var filteredSquashed []string
	for _, b := range allBranches {
		if protect[b] || goneSet[b] || mergedSet[b] {
			continue
		}
		if isSquashMerged(repoPath, defBranch, b) {
			filteredSquashed = append(filteredSquashed, b)
		}
	}

	return SweepResult{
		DefaultBranch: defBranch,
		CurrentBranch: curBranch,
		Merged:        filteredMerged,
		Gone:          filteredGone,
		Squashed:      filteredSquashed,
	}, nil
}

// DeleteStaleBranches deletes branches listed in a SweepResult.
// Gone and squashed branches use -D (force); merged branches use -d (safe).
// Returns names of successfully deleted branches and any errors.
func DeleteStaleBranches(repoPath string, result SweepResult) (deleted []string, errs []error) {
	for _, b := range result.Gone {
		if err := deleteBranch(repoPath, b, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", b, err))
		} else {
			deleted = append(deleted, b)
		}
	}
	for _, b := range result.Merged {
		if err := deleteBranch(repoPath, b, false); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", b, err))
		} else {
			deleted = append(deleted, b)
		}
	}
	for _, b := range result.Squashed {
		if err := deleteBranch(repoPath, b, true); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", b, err))
		} else {
			deleted = append(deleted, b)
		}
	}
	return deleted, errs
}

// mergedBranches lists branches merged into the given base branch.
func mergedBranches(repoPath, base string) ([]string, error) {
	out, err := output(repoPath, "branch", "--merged", base, "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	var branches []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		b := strings.TrimSpace(line)
		if b != "" {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

// goneBranches lists branches whose upstream tracking ref no longer exists.
func goneBranches(repoPath string) ([]string, error) {
	out, err := output(repoPath, "for-each-ref",
		"--format=%(refname:short) %(upstream) %(upstream:track)",
		"refs/heads/")
	if err != nil {
		return nil, err
	}
	var branches []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "branchname refs/remotes/origin/branchname [gone]"
		// A branch is "gone" if it has an upstream but the track status is [gone].
		if strings.Contains(line, "[gone]") {
			name := strings.Fields(line)[0]
			branches = append(branches, name)
		}
	}
	return branches, nil
}

// deleteBranch deletes a local branch. If force is true, uses -D; otherwise -d.
func deleteBranch(repoPath, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return run(repoPath, "branch", flag, branch)
}

// listLocalBranches returns all local branch names.
func listLocalBranches(repoPath string) ([]string, error) {
	out, err := output(repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	var branches []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		b := strings.TrimSpace(line)
		if b != "" {
			branches = append(branches, b)
		}
	}
	return branches, nil
}

// isSquashMerged checks if a branch was squash-merged (or rebase-merged) into
// the base branch. The commits differ but the changes are identical.
//
// Two-step check:
//  1. Tree comparison: if the branch tip's tree equals the base branch's tree,
//     the branch content is fully in the base (covers identical-state squash merges).
//  2. Synthetic ancestor: create a dangling commit with the branch's tree on the
//     merge base and check if it's an ancestor of the base branch (covers squash
//     merges where more commits landed on base after the squash).
func isSquashMerged(repoPath, baseBranch, branch string) bool {
	// Fast path: if both trees are identical, branch is fully incorporated.
	baseTree, err := output(repoPath, "rev-parse", baseBranch+"^{tree}")
	if err != nil {
		return false
	}
	branchTree, err := output(repoPath, "rev-parse", branch+"^{tree}")
	if err != nil {
		return false
	}
	if strings.TrimSpace(baseTree) == strings.TrimSpace(branchTree) {
		return true
	}

	// Slow path: synthetic commit-tree + is-ancestor check.
	mbOut, err := output(repoPath, "merge-base", baseBranch, branch)
	if err != nil {
		return false
	}
	mergeBase := strings.TrimSpace(mbOut)
	tree := strings.TrimSpace(branchTree)

	commitOut, err := output(repoPath, "commit-tree", tree, "-p", mergeBase, "-m", "_")
	if err != nil {
		return false
	}
	synthetic := strings.TrimSpace(commitOut)

	_, err = Default.Run(context.Background(), Request{Dir: repoPath, Args: []string{"merge-base", "--is-ancestor", synthetic, baseBranch}})
	return err == nil
}

// --- Internal helpers ---

// nonInteractiveEnv returns Environ() with flags that prevent any interactive
// credential prompt (browser popup, terminal prompt). Git operations that need
// credentials will fail silently rather than blocking the GUI.
//
// It also makes git speak English. The window never shows what git prints;
// the classifiers in errors.go read it, and they know git's English
// messages. Without this a translated git (a Spanish system, say) would turn
// every failure into "unknown".
func nonInteractiveEnv() []string {
	return append(Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_ASKPASS=",
		"LC_ALL=en_US.UTF-8",
		"LANG=en_US.UTF-8",
		"LANGUAGE=en",
	)
}

// run executes a git command in the given directory and discards what it
// prints. The error is a *GitError: it names the subcommand and why it
// failed, and never carries the arguments or what git printed.
func run(dir string, args ...string) error {
	return runContext(context.Background(), dir, args...)
}

// runContext is run that stops when ctx is cancelled.
func runContext(ctx context.Context, dir string, args ...string) error {
	res, err := Default.Run(ctx, Request{Dir: dir, Args: args})
	if err != nil {
		return failure(ctx, opName(args), err, res, res.combined())
	}
	return nil
}

// output executes a git command and returns its stdout.
func output(dir string, args ...string) (string, error) {
	return outputContext(context.Background(), dir, args...)
}

// errOutputTruncated is the cause of the error for output that hit the
// runner's bound.
var errOutputTruncated = errors.New("output exceeds the limit")

// outputContext executes a git command under ctx and returns its stdout.
// Output that hit the runner's bound is an error: what the caller would
// parse is incomplete, and half a status or half a config must never be
// taken for the whole.
func outputContext(ctx context.Context, dir string, args ...string) (string, error) {
	res, err := Default.Run(ctx, Request{Dir: dir, Args: args})
	if err != nil {
		return "", failure(ctx, opName(args), err, res, res.Stderr)
	}
	if res.Truncated {
		e := failure(ctx, opName(args), errOutputTruncated, res, nil)
		e.Detail = "output too large"
		return "", e
	}
	return string(res.Stdout), nil
}
