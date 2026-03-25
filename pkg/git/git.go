// Package git provides subprocess wrappers for Git operations.
package git

import (
	"bufio"
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

// GitBin returns the path to the git binary.
// On macOS, GUI apps inherit a minimal PATH that excludes /opt/homebrew/bin,
// so Homebrew's git (and GCM) won't be found. We probe common locations first.
func GitBin() string {
	gitBinOnce.Do(func() {
		if runtime.GOOS == "darwin" {
			for _, candidate := range []string{
				"/opt/homebrew/bin/git", // Apple Silicon Homebrew
				"/usr/local/bin/git",    // Intel Homebrew
			} {
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

// CloneOpts configures a git clone operation.
type CloneOpts struct {
	Depth  int    // Shallow clone depth (0 = full clone)
	Branch string // Specific branch to clone
	Bare   bool   // Bare clone (for mirrors)
	Mirror bool   // Mirror clone
	Quiet  bool   // Suppress stdout/stderr (capture instead of forward)
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
	cmd.Stdout = nil

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

// Pull runs git pull --ff-only in the given repo.
func Pull(repoPath string) error {
	return run(repoPath, "pull", "--ff-only")
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

// ConfigSet sets a git config value in the given repo.
func ConfigSet(repoPath, key, value string) error {
	return run(repoPath, "config", key, value)
}

// GlobalConfigSet sets a global git config value (git config --global).
func GlobalConfigSet(key, value string) error {
	return run(".", "config", "--global", key, value)
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
	cmd.Stdin = strings.NewReader(input)
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

// --- Internal helpers ---

// run executes a git command in the given directory.
func run(dir string, args ...string) error {
	cmd := exec.Command(GitBin(), args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// output executes a git command and returns its stdout.
func output(dir string, args ...string) (string, error) {
	cmd := exec.Command(GitBin(), args...)
	cmd.Dir = dir
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
