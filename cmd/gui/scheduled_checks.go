package main

import (
	"context"
	"time"
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
	var tasks []*scheduledTask
	for _, name := range []string{taskFetch, taskMirrors, taskPRs} {
		tasks = append(tasks, &scheduledTask{name: name, run: func(ctx context.Context) error {
			if a.remote != nil && !a.remote.connected() {
				return errTaskOffline
			}
			return a.be().RunFleetCheck(ctx, name)
		}})
	}
	if !a.remoteWindow() {
		tasks = append(tasks, a.hostHarnessTask(), &scheduledTask{name: taskUpdate, every: updateCheckInterval, next: time.Now(), run: a.checkForUpdate})
	}
	a.tasks.start(a.ctx, tasks)
}

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
	if v := a.GetPeriodicSync(); validPeriodicSync(v) {
		every, _ = time.ParseDuration(v)
	}
	a.tasks.setFleetInterval(every)
}

// GetScheduledTasks returns the tasks owned by this window.
func (a *App) GetScheduledTasks() []ScheduledTaskDTO { return a.tasks.snapshot() }

// RunTask requests a check now. Concurrent requests share the reservation
// with periodic runs. The caller that started it waits for actual completion.
func (a *App) RunTask(name string) (string, error) {
	if a.ctx != nil && a.windowMode != "terminals" {
		a.ensureWindowTasks()
	}
	return a.tasks.runNow(name)
}

// RunFleetCheck executes only the three read/check operations the window
// schedules on its fleet. Each inner git or provider request keeps its own
// bound; an entire fleet can take several such bounds to complete.
func (b localBackend) RunFleetCheck(ctx context.Context, name string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	a := b.a
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

func (r *RemoteBackend) RunFleetCheck(ctx context.Context, name string) error {
	return r.invokeContext(ctx, "RunFleetCheck", nil, name)
}
