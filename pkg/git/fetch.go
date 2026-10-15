package git

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// refusedFastRE matches curl's connect failure with its elapsed time, e.g.
// "Failed to connect to host port 443 after 5 ms".
var refusedFastRE = regexp.MustCompile(`(?i)failed to connect to \S+ port \d+ after (\d+) ms`)

// IsRefusedFast reports whether a git network error is a connect the OS
// refused within a second, rather than a timeout or a server-side error.
// On macOS, Local Network privacy refuses an app's first connections to
// LAN hosts for a moment right after launch, so one retry a few seconds
// later usually succeeds.
func IsRefusedFast(err error) bool {
	if err == nil {
		return false
	}
	m := refusedFastRE.FindStringSubmatch(err.Error())
	if m == nil {
		return false
	}
	ms, convErr := strconv.Atoi(m[1])
	return convErr == nil && ms < 1000
}

// FetchCapturedContext lets refresh shutdown cancel an in-flight network transfer.
func FetchCapturedContext(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, GitBin(), "fetch", "--all", "--prune")
	cmd.Dir = repoPath
	cmd.Env = nonInteractiveEnv()
	cmd.WaitDelay = time.Second
	HideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git fetch: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
