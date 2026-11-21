package git

import (
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

// procTree stops a command together with everything it started. Git runs
// credential helpers, ssh and hooks as children; stopping only git would
// leave them alive, holding its output open.
//
// On Windows the command is put in a Job Object right after it starts, and
// terminating the job ends every process in it. The job is not set to kill
// its processes when it closes: that would also end, after every command
// that finished well, what git leaves running on purpose (a background gc,
// the fsmonitor daemon). The tree is only ever stopped on cancellation.
type procTree struct {
	job windows.Handle
}

// prepare has nothing to do on Windows: the job is attached after Start.
func (*procTree) prepare(*exec.Cmd) {}

// attach puts the started command in a new job. If any step fails the tree
// falls back to stopping only the command itself.
func (t *procTree) attach(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	defer func() { _ = windows.CloseHandle(proc) }()
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return
	}
	t.job = job
}

// terminate stops the whole tree. Windows has no signal that lets a console
// program clean up, so this ends the processes at once.
func (t *procTree) terminate(cmd *exec.Cmd) error {
	if t.job != 0 {
		if err := windows.TerminateJobObject(t.job, 1); err == nil {
			return nil
		}
	}
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}

// kill stops whatever is left of the tree.
func (t *procTree) kill(cmd *exec.Cmd) { _ = t.terminate(cmd) }

// release frees the job handle. The processes in the job keep running.
func (t *procTree) release() {
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}
