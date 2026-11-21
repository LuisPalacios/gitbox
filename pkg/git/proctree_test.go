package git

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// procTreeEnv turns the test binary into a stand-in for git that starts a
// child, the way git starts a credential helper or ssh. TestMain reads it.
const procTreeEnv = "GITBOX_TEST_PROCTREE"

func TestMain(m *testing.M) {
	switch os.Getenv(procTreeEnv) {
	case "parent":
		// Start a child that shares this process's stderr, announce it, and
		// hang: a git stuck on a helper that never answers.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), procTreeEnv+"=child")
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "start:", err)
			os.Exit(3)
		}
		fmt.Fprintf(os.Stderr, "child %d\n", child.Process.Pid)
		time.Sleep(5 * time.Minute)
		os.Exit(0)
	case "detach":
		// Start a child that keeps none of this process's handles, announce
		// it, and finish well: a git that leaves a daemon behind on purpose.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), procTreeEnv+"=child")
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "start:", err)
			os.Exit(3)
		}
		fmt.Fprintf(os.Stderr, "child %d\n", child.Process.Pid)
		os.Exit(0)
	case "child":
		time.Sleep(5 * time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// standIn runs the test binary in the given mode through the real runner
// and returns the pid of the child it started, as soon as it is announced.
func standIn(t *testing.T, ctx context.Context, mode string) (childPID <-chan int, done <-chan error) {
	t.Helper()
	pr, pw := io.Pipe()
	pids := make(chan int, 1)
	go func() {
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			if rest, ok := strings.CutPrefix(scanner.Text(), "child "); ok {
				if pid, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
					pids <- pid
				}
			}
		}
		_, _ = io.Copy(io.Discard, pr)
	}()
	finished := make(chan error, 1)
	go func() {
		_, err := execRunner{bin: os.Args[0]}.Run(ctx, Request{
			EnvExact: append(os.Environ(), procTreeEnv+"="+mode),
			Stderr:   pw,
		})
		_ = pw.Close()
		finished <- err
	}()
	return pids, finished
}

// waitGone waits for a process to disappear.
func waitGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return !processAlive(pid)
}

// Cancelling a command must stop what the command started too. The stand-in
// hangs with a child that holds its output open, as git does on a helper
// that never answers.
func TestCancelStopsTheWholeProcessTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pids, done := standIn(t, ctx, "parent")

	var child int
	select {
	case child = <-pids:
	case err := <-done:
		t.Fatalf("the stand-in ended before starting its child: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("the stand-in never announced its child")
	}
	t.Cleanup(func() { endProcess(child) })
	if !processAlive(child) {
		t.Fatal("the child must be running before the cancel")
	}

	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a cancelled command must report an error")
		}
	case <-time.After(runnerWaitDelay + 10*time.Second):
		t.Fatal("the runner did not return after the cancel")
	}
	if spent := time.Since(started); spent > runnerWaitDelay+5*time.Second {
		t.Errorf("the runner took %v to return after the cancel", spent)
	}
	if !waitGone(child, 5*time.Second) {
		t.Errorf("child %d survived the cancel: only the command itself was stopped", child)
	}
}

// What a command leaves running on purpose survives when the command ends
// well: the tree is only stopped on cancellation.
func TestFinishedCommandLeavesItsBackgroundWorkAlone(t *testing.T) {
	pids, done := standIn(t, context.Background(), "detach")

	var child int
	select {
	case child = <-pids:
	case <-time.After(20 * time.Second):
		t.Fatal("the stand-in never announced its child")
	}
	t.Cleanup(func() { endProcess(child) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the stand-in must finish well: %v", err)
		}
	case <-time.After(runnerWaitDelay + 10*time.Second):
		t.Fatal("the runner did not return")
	}
	time.Sleep(300 * time.Millisecond)
	if !processAlive(child) {
		t.Errorf("child %d was stopped although the command finished well", child)
	}
}
