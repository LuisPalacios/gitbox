package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

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
