// Package git provides subprocess wrappers for Git operations.
package git

import (
	"bufio"
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
		if strings.HasPrefix(e, "PATH=") {
			currentPath := strings.TrimPrefix(e, "PATH=")
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
	Added     int    // Added (staged) files count
	Deleted   int    // Deleted files count
	Untracked int    // Untracked files count
	Conflicts int    // Conflicted files count
}

// Clone runs git clone with the given options.
// When opts.Quiet is true, stdout/stderr are captured instead of forwarded.
func Clone(url, dest string, opts CloneOpts) error {
	args := []string{"clone"}
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
	if opts.Quiet {
		_, err := output(".", args...)
		return err
	}
	return run(".", args...)
}

// CloneProgress holds a progress update from git clone.
type CloneProgress struct {
	Phase   string // e.g. "Receiving objects", "Resolving deltas"
	Percent int    // 0-100
}

// progressRe matches git progress lines like "Receiving objects:  45% (114/253)".
var progressRe = regexp.MustCompile(`^(remote: )?([\w ]+):\s+(\d+)%`)

// CloneWithProgress runs git clone --progress and calls onProgress with updates.
// stderr is parsed for percentage lines; stdout is discarded.
func CloneWithProgress(url, dest string, opts CloneOpts, onProgress func(CloneProgress)) error {
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

	cmd := exec.Command(GitBin(), args...)
	cmd.Dir = "."
	cmd.Env = nonInteractiveEnv()
	cmd.Stdout = nil
	HideWindow(cmd)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}

	// Git progress uses \r for in-place updates within a line.
	// Read stderr splitting on both \r and \n.
	go parseProgress(stderr, onProgress)

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git clone: %w", err)
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

// Fetch runs git fetch --all in the given repo.
func Fetch(repoPath string) error {
	return run(repoPath, "fetch", "--all", "--prune")
}

// FetchQuiet runs git fetch --all --prune, capturing output instead of forwarding it.
func FetchQuiet(repoPath string) error {
	_, err := output(repoPath, "fetch", "--all", "--prune")
	return err
}

// FetchCaptured runs git fetch --all --prune and returns the combined
// stdout+stderr along with any error. GUI callers need this because
// git writes actionable diagnostics like "remote: Repository not found."
// to stderr, and plain Fetch/FetchQuiet either forward them to the
// terminal (run) or drop them after wrapping in a generic exec error
// (output). The captured text is what downstream classifiers match on
// (see IsUpstreamGoneError).
func FetchCaptured(repoPath string) (string, error) {
	cmd := exec.Command(GitBin(), "fetch", "--all", "--prune")
	cmd.Dir = repoPath
	cmd.Env = nonInteractiveEnv()
	HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git fetch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
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
	msg := strings.ToLower(err.Error())
	// GitHub / generic: "remote: Repository not found." / "fatal:
	//   repository '...' not found"
	// GitLab: "fatal: repository '...' not found" / "HTTP 404"
	// Gitea / Forgejo: "fatal: repository '...' not found" / HTTP 404
	// Bitbucket: "fatal: repository access denied" once deleted, plus 404
	// Any provider over HTTPS with hard 404: "the requested URL returned
	//   error: 404"
	return strings.Contains(msg, "repository not found") ||
		strings.Contains(msg, "repository '") && strings.Contains(msg, "' not found") ||
		strings.Contains(msg, "error: 404") ||
		strings.Contains(msg, "returned error: 404") ||
		strings.Contains(msg, "http 404") ||
		strings.Contains(msg, "repository access denied")
}

// Pull runs git pull --ff-only in the given repo.
func Pull(repoPath string) error {
	return run(repoPath, "pull", "--ff-only")
}

// PushMirror runs `git push --mirror <url>` from repoPath, pushing every
// ref and tag to the target URL. extraConfig, if non-nil, is passed as
// `-c <key=value>` args before the push subcommand — used to inject
// `credential.helper=` so an embedded user:PAT in the URL isn't
// overridden by a GCM-managed credential for the same host.
//
// Output is captured so the caller can surface provider-side errors
// (e.g. "remote: Repository is empty" vs. permission issues).
func PushMirror(repoPath, url string, extraConfig []string) (string, error) {
	args := make([]string, 0, len(extraConfig)*2+3)
	for _, kv := range extraConfig {
		args = append(args, "-c", kv)
	}
	args = append(args, "push", "--mirror", url)

	cmd := exec.Command(GitBin(), args...)
	cmd.Dir = repoPath
	cmd.Env = nonInteractiveEnv()
	HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git push --mirror: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// FetchTagsAndPrune runs `git fetch --prune --tags` in repoPath. Used as
// the first move-flow step so the upcoming push --mirror carries every
// ref (tags included) and stale remote branches are dropped. Output is
// captured (not forwarded) so the call is safe from a GUI process with
// no attached console — setting Stdout = os.Stdout fails with
// "The request is not supported" on Windows GUI subsystems.
func FetchTagsAndPrune(repoPath string) error {
	cmd := exec.Command(GitBin(), "fetch", "--prune", "--tags")
	cmd.Dir = repoPath
	cmd.Env = nonInteractiveEnv()
	HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch --prune --tags: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetUpstream configures the given local branch to track
// <remote>/<branch>. The remote must already have the branch fetched.
// Used after a move to re-bind the current branch to the new origin.
// Captures output for the same GUI-safety reason as FetchTagsAndPrune.
func SetUpstream(repoPath, branch, remote string) error {
	cmd := exec.Command(GitBin(), "branch", fmt.Sprintf("--set-upstream-to=%s/%s", remote, branch), branch)
	cmd.Dir = repoPath
	cmd.Env = nonInteractiveEnv()
	HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch --set-upstream-to=%s/%s %s: %w: %s", remote, branch, branch, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetRemoteURLCaptured is a GUI-safe variant of SetRemoteURL that
// captures stdout/stderr instead of inheriting them. Use this from
// code paths that can run under a Wails process (see FetchTagsAndPrune
// comment).
func SetRemoteURLCaptured(repoPath, remote, url string) error {
	cmd := exec.Command(GitBin(), "remote", "set-url", remote, url)
	cmd.Dir = repoPath
	cmd.Env = nonInteractiveEnv()
	HideWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git remote set-url %s: %w: %s", remote, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// PullQuiet runs git pull --ff-only, capturing output instead of forwarding it.
func PullQuiet(repoPath string) error {
	_, err := output(repoPath, "pull", "--ff-only")
	return err
}

// Status runs git status and parses the result.
func Status(repoPath string) (RepoStatus, error) {
	out, err := output(repoPath, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return RepoStatus{}, err
	}
	return parseStatus(out), nil
}

// FileChange describes a single changed file for human-readable display.
type FileChange struct {
	Kind string `json:"kind"` // "modified", "added", "deleted", "renamed", "conflict"
	Path string `json:"path"`
}

// DetailedStatus returns the list of changed and untracked files in a repo.
func DetailedStatus(repoPath string) (branch string, ahead, behind int, changed []FileChange, untracked []string, err error) {
	out, err := output(repoPath, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return "", 0, 0, nil, nil, err
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "# branch.head ") {
			branch = strings.TrimPrefix(line, "# branch.head ")
		} else if strings.HasPrefix(line, "# branch.ab ") {
			parts := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(parts) == 2 {
				ahead, _ = strconv.Atoi(strings.TrimPrefix(parts[0], "+"))
				behind, _ = strconv.Atoi(strings.TrimPrefix(parts[1], "-"))
			}
		} else if strings.HasPrefix(line, "1 ") {
			// Ordinary changed entry: 1 XY sub mH mI mW hH hI path
			parts := strings.SplitN(line, " ", 9)
			if len(parts) < 9 {
				continue
			}
			xy, path := parts[1], parts[8]
			changed = append(changed, FileChange{Kind: classifyXY(xy), Path: path})
		} else if strings.HasPrefix(line, "2 ") {
			// Renamed/copied entry: 2 XY sub mH mI mW hH hI X<score> path\torigPath
			parts := strings.SplitN(line, " ", 10)
			if len(parts) < 10 {
				continue
			}
			pathPart := parts[9]
			// path\torigPath — show as "origPath → path"
			if idx := strings.IndexByte(pathPart, '\t'); idx >= 0 {
				pathPart = pathPart[idx+1:] + " → " + pathPart[:idx]
			}
			changed = append(changed, FileChange{Kind: "renamed", Path: pathPart})
		} else if strings.HasPrefix(line, "u ") {
			// Unmerged (conflict): u XY sub m1 m2 m3 mW h1 h2 h3 path
			parts := strings.SplitN(line, " ", 11)
			if len(parts) >= 11 {
				changed = append(changed, FileChange{Kind: "conflict", Path: parts[10]})
			}
		} else if strings.HasPrefix(line, "? ") {
			untracked = append(untracked, strings.TrimPrefix(line, "? "))
		}
	}
	// Ensure non-nil slices so JSON serialization produces [] instead of null.
	if changed == nil {
		changed = []FileChange{}
	}
	if untracked == nil {
		untracked = []string{}
	}
	return branch, ahead, behind, changed, untracked, nil
}

func classifyXY(xy string) string {
	if len(xy) < 2 {
		return "modified"
	}
	// Prefer the working-tree status (2nd char), fall back to index (1st char).
	wt := xy[1]
	idx := xy[0]
	if wt == 'D' || idx == 'D' {
		return "deleted"
	}
	if wt == 'A' || idx == 'A' {
		return "added"
	}
	return "modified"
}

// RevCount returns the number of commits ahead/behind the upstream.
// Returns (0, 0, nil) if there's no upstream.
func RevCount(repoPath string) (ahead, behind int, err error) {
	out, err := output(repoPath, "rev-list", "--count", "--left-right", "HEAD...@{upstream}")
	if err != nil {
		// No upstream configured — not an error, just 0/0.
		return 0, 0, nil
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) != 2 {
		return 0, 0, nil
	}
	ahead, _ = strconv.Atoi(parts[0])
	behind, _ = strconv.Atoi(parts[1])
	return ahead, behind, nil
}

// RemoteURL returns the URL of the 'origin' remote.
func RemoteURL(repoPath string) (string, error) {
	out, err := output(repoPath, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ConfigGet reads a git config value from the given repo.
func ConfigGet(repoPath, key string) (string, error) {
	out, err := output(repoPath, "config", "--get", key)
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
	out, err := output(repoPath, "config", "--get-regexp", `^credential\..*\.username$`)
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Lines look like: "credential.https://github.com.username SW-Luis-Palacios"
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

// ConfigUnset removes a key from repo-level git config.
// Returns nil if the key does not exist.
func ConfigUnset(repoPath, key string) error {
	err := run(repoPath, "config", "--unset", key)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 {
			return nil
		}
	}
	return err
}

// ConfigUnsetAll removes ALL values of a multi-value key from repo-level git config.
// Returns nil if the key does not exist.
func ConfigUnsetAll(repoPath, key string) error {
	err := run(repoPath, "config", "--unset-all", key)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 {
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

// GlobalConfigGet reads a global git config value.
func GlobalConfigGet(key string) (string, error) {
	out, err := output(".", "config", "--global", "--get", key)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// GlobalConfigUnset removes a key from global git config.
// Returns nil if the key does not exist.
func GlobalConfigUnset(key string) error {
	err := run(".", "config", "--global", "--unset", key)
	if err != nil {
		// git config --unset exits with code 5 if the key is not found.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 {
			return nil
		}
	}
	return err
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

// SetRemoteURL sets the URL of a remote (typically "origin").
func SetRemoteURL(repoPath, remote, url string) error {
	return run(repoPath, "remote", "set-url", remote, url)
}

// Run executes a git command in the given directory (public wrapper).
func Run(dir string, args ...string) error {
	return run(dir, args...)
}

// RunWithInput executes a git command with data piped to stdin.
func RunWithInput(dir string, input string, args ...string) (string, error) {
	cmd := exec.Command(GitBin(), args...)
	cmd.Dir = dir
	cmd.Env = Environ() // Homebrew PATH for macOS — do not remove.
	cmd.Stdin = strings.NewReader(input)
	HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
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
	// Try origin/HEAD — set by git clone or git remote set-head.
	out, err := output(repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		ref := strings.TrimSpace(out)
		// "refs/remotes/origin/main" → "main"
		if parts := strings.Split(ref, "/"); len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	// Fallback: check if main or master exist locally.
	for _, candidate := range []string{"main", "master"} {
		if _, err := output(repoPath, "rev-parse", "--verify", "refs/heads/"+candidate); err == nil {
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
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
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
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
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
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
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
// 1. Tree comparison: if the branch tip's tree equals the base branch's tree,
//    the branch content is fully in the base (covers identical-state squash merges).
// 2. Synthetic ancestor: create a dangling commit with the branch's tree on the
//    merge base and check if it's an ancestor of the base branch (covers squash
//    merges where more commits landed on base after the squash).
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

	cmd := exec.Command(GitBin(), "merge-base", "--is-ancestor", synthetic, baseBranch)
	cmd.Dir = repoPath
	cmd.Env = nonInteractiveEnv()
	HideWindow(cmd)
	return cmd.Run() == nil
}

// --- Internal helpers ---

// nonInteractiveEnv returns Environ() with flags that prevent any interactive
// credential prompt (browser popup, terminal prompt). Git operations that need
// credentials will fail silently rather than blocking the GUI.
func nonInteractiveEnv() []string {
	return append(Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_ASKPASS=",
	)
}

// run executes a git command in the given directory.
func run(dir string, args ...string) error {
	cmd := exec.Command(GitBin(), args...)
	cmd.Dir = dir
	cmd.Env = nonInteractiveEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	HideWindow(cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// output executes a git command and returns its stdout.
func output(dir string, args ...string) (string, error) {
	cmd := exec.Command(GitBin(), args...)
	cmd.Dir = dir
	cmd.Env = nonInteractiveEnv()
	HideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// parseStatus parses git status --porcelain=v2 --branch output.
func parseStatus(out string) RepoStatus {
	var s RepoStatus
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Branch headers: # branch.oid, # branch.head, # branch.upstream, # branch.ab
		if strings.HasPrefix(line, "# branch.head ") {
			s.Branch = strings.TrimPrefix(line, "# branch.head ")
		} else if strings.HasPrefix(line, "# branch.upstream ") {
			s.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
		} else if strings.HasPrefix(line, "# branch.ab ") {
			parts := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			if len(parts) == 2 {
				s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(parts[0], "+"))
				s.Behind, _ = strconv.Atoi(strings.TrimPrefix(parts[1], "-"))
			}
		} else if strings.HasPrefix(line, "1 ") || strings.HasPrefix(line, "2 ") {
			// Changed entry (ordinary or renamed).
			// Format: 1 XY sub mH mI mW hH hI path
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				xy := parts[1]
				if len(xy) == 2 {
					indexStatus := xy[0]
					workStatus := xy[1]
					if indexStatus != '.' {
						s.Added++ // Staged change.
					}
					if workStatus == 'M' || workStatus == 'A' || workStatus == 'D' {
						s.Modified++ // Working tree change.
					}
					if workStatus == 'D' || indexStatus == 'D' {
						s.Deleted++
					}
				}
			}
		} else if strings.HasPrefix(line, "u ") {
			// Unmerged entry (conflict).
			s.Conflicts++
		} else if strings.HasPrefix(line, "? ") {
			// Untracked file.
			s.Untracked++
		}
	}
	return s
}
