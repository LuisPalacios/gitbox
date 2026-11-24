package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func (a *App) hostHarnessTask() *scheduledTask {
	return &scheduledTask{name: taskHarness, every: harnessSyncInterval, next: time.Now().Add(harnessInitialDelay), run: a.detectHarnesses}
}

func (a *App) ensureWindowTasks() {
	a.windowTasksOnce.Do(a.startWindowTasks)
}

// The window owns the fleet cadence, including for a remote fleet. A
// headless server registers only host tool detection.
func (a *App) startWindowTasks() {
	tasks := a.fleetTasks()
	if !a.remoteWindow() {
		tasks = append(tasks, a.hostHarnessTask(), &scheduledTask{name: taskUpdate, every: updateCheckInterval, next: time.Now(), run: a.checkForUpdate})
	}
	a.tasks.start(a.ctx, tasks)
}

// fleetTasks are fetch-all, mirror status and PR refresh, with no interval
// until one is set. A window runs them on its backend; the daemon of a host
// runs them on its own (#247).
func (a *App) fleetTasks() []*scheduledTask {
	var tasks []*scheduledTask
	for _, name := range fleetTaskNames {
		tasks = append(tasks, &scheduledTask{name: name, run: func(ctx context.Context) error {
			if a.remote != nil && !a.remote.connected() {
				return errTaskOffline
			}
			return a.be().RunFleetCheck(ctx, name)
		}})
	}
	return tasks
}

// fleetTaskNames are the checks that follow the periodic_sync interval.
var fleetTaskNames = []string{taskFetch, taskMirrors, taskPRs}

func (s *remoteSession) connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.State == remoteConnected
}

// Read the latest stored interval while holding the reschedule lock: a focus
// reload racing a preference save always installs the newest value.
func (a *App) syncTaskInterval() {
	a.taskPrefsMu.Lock()
	defer a.taskPrefsMu.Unlock()
	var every time.Duration
	v := a.GetPeriodicSync()
	if validPeriodicSync(v) {
		every, _ = time.ParseDuration(v)
	}
	// The daemon of a host runs the fleet checks itself, connected window or
	// not (#247): the window sends it the interval and keeps no timer.
	if a.remote != nil && a.remote.hostSchedules() {
		a.tasks.setFleetInterval(0)
		go a.remote.pushSchedule(v)
		return
	}
	a.tasks.setFleetInterval(every)
}

// GetScheduledTasks returns the tasks owned by this window.
func (a *App) GetScheduledTasks() []ScheduledTaskDTO { return a.tasks.snapshot() }

// RunTask requests a check now. Concurrent requests share the reservation
// with periodic runs. The caller that started it waits for actual completion.
func (a *App) RunTask(name string) (string, error) {
	if strings.HasPrefix(name, retryConnectionTask) {
		if a.remote != nil && !a.remote.connected() {
			return "", errTaskOffline
		}
		if err := a.be().RunFleetCheck(a.refreshContext(), name); err != nil {
			return "", err
		}
		return "finished", nil
	}
	if a.ctx != nil && a.windowMode != "terminals" {
		a.ensureWindowTasks()
	}
	return a.tasks.runNow(name)
}

// The account-scoped check is user-triggered and never registered with the scheduler.
const retryConnectionTask = "retry-connection/"

// RunFleetCheck handles scheduled fleet checks and explicit single-server retries.

func (b localBackend) RunFleetCheck(ctx context.Context, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	a := b.a
	if key, ok := strings.CutPrefix(name, retryConnectionTask); ok {
		a.mu.Lock()
		account, exists := a.cfg.Accounts[key]
		a.mu.Unlock()
		if !exists {
			return config.ErrNotFound
		}
		a.recheckDownServer(ctx, serverAddr(account))
		return ctx.Err()
	}
	switch name {
	case taskFetch:
		return a.fetchAllReposContext(ctx)
	case taskMirrors:
		a.mu.Lock()
		cfg := a.snapshotLocked()
		a.mu.Unlock()
		// Groups used to start independently from the dashboard. Keep a slow
		// server in one group from delaying status in every other group.
		results := make(chan error, len(cfg.Mirrors))
		for key := range cfg.Mirrors {
			go func() { results <- a.checkMirrorStatus(ctx, cfg, key) }()
		}
		var result error
		for range cfg.Mirrors {
			if err := <-results; err != nil {
				result = err
			}
		}
		return result
	case taskPRs:
		pass := a.queuePRs(true)
		if pass == nil {
			return a.refreshContext().Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pass.done:
			return pass.err
		}
	default:
		return errTaskUnknown
	}
}

// RunFleetCheck runs a check on the host. On a daemon the window's end
// never cancels it (#247): closing the window, Disconnect or a dropped
// link only detaches, and the check finishes on the host. Like a clone or
// a pull, the call then waits for the next connection and asks again with
// the same request id, which joins the run instead of starting another.
func (r *RemoteBackend) RunFleetCheck(ctx context.Context, name string) error {
	if !r.isPersistent() {
		return r.invokeContext(ctx, "RunFleetCheck", nil, name)
	}
	return r.invokeAcross(context.WithoutCancel(ctx), "RunFleetCheck", nil, true, name)
}

// testFleetInterval, set only in the e2e_sentinel build of the server,
// accepts the short intervals the sshd tests need (#247).
var testFleetInterval func(string) (time.Duration, bool)

// fleetInterval reads a periodic_sync value: off, or one of the intervals
// a window offers.
func fleetInterval(v string) (time.Duration, bool) {
	switch {
	case v == "" || v == "off":
		return 0, true
	case validPeriodicSync(v):
		d, err := time.ParseDuration(v)
		return d, err == nil
	case testFleetInterval != nil:
		return testFleetInterval(v)
	}
	return 0, false
}

// errFleetInterval refuses an interval no window offers.
var errFleetInterval = errors.New("invalid periodic sync interval")

// SetFleetSchedule sets the interval of the fleet checks that a host's
// daemon runs on its own, connected client or not (#247). A window sends
// its periodic_sync preference when it connects and whenever it changes;
// with several windows, the last one wins. A server for one session has no
// such checks, and the call changes nothing there.
func (b localBackend) SetFleetSchedule(interval string) error {
	every, ok := fleetInterval(interval)
	if !ok {
		return errFleetInterval
	}
	b.a.tasks.setFleetInterval(every)
	return nil
}

// FleetSchedule is the state of those checks.
func (b localBackend) FleetSchedule() []ScheduledTaskDTO {
	out := []ScheduledTaskDTO{}
	for _, t := range b.a.tasks.snapshot() {
		if slices.Contains(fleetTaskNames, t.Name) {
			out = append(out, t)
		}
	}
	return out
}

func (r *RemoteBackend) SetFleetSchedule(interval string) error {
	return r.callErr("SetFleetSchedule", interval)
}

// FleetSchedule keeps only the fleet tasks, once each, with a result this
// build knows: the host is untrusted.
func (r *RemoteBackend) FleetSchedule() []ScheduledTaskDTO {
	out := []ScheduledTaskDTO{}
	for _, t := range remoteValue[[]ScheduledTaskDTO](r, "FleetSchedule") {
		if !slices.Contains(fleetTaskNames, t.Name) || slices.ContainsFunc(out, func(o ScheduledTaskDTO) bool { return o.Name == t.Name }) {
			continue
		}
		if !slices.Contains(taskResults, t.LastResult) {
			t.LastResult = ""
		}
		if t.NextAt < 0 || t.NextAt >= 1<<53 {
			t.NextAt = 0
		}
		out = append(out, t)
	}
	return out
}

// taskResults are the LastResult values of a task.
var taskResults = []string{"", "ok", "failed", "offline", "cancelled"}
