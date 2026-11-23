package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	taskFetch   = "fetch-all"
	taskMirrors = "mirror-status"
	taskPRs     = "pr-refresh"
	taskUpdate  = "update-check"
	taskHarness = "harness-detect"
)

var (
	errTaskUnknown = errors.New("unknown background task")
	errTaskStopped = errors.New("background checks are stopped")
	errTaskOffline = errors.New("remote window is disconnected")
	errTaskPartial = errors.New("background check was incomplete")
)

// ScheduledTaskDTO is a snapshot. Times are Unix milliseconds; zero means
// the task has no scheduled start (disabled or already in flight).
type ScheduledTaskDTO struct {
	Name       string `json:"name"`
	NextAt     int64  `json:"nextAt"`
	InFlight   bool   `json:"inFlight"`
	LastResult string `json:"lastResult"`
}

type scheduledTask struct {
	name   string
	every  time.Duration
	next   time.Time
	busy   bool
	result string
	run    func(context.Context) error
}

type taskRegistry struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	wake       chan struct{}
	tasks      []*scheduledTask
	wg         sync.WaitGroup
	fleetEvery time.Duration
	closed     bool
}

func (r *taskRegistry) start(ctx context.Context, tasks []*scheduledTask) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx != nil || r.closed {
		return
	}
	r.ctx, r.cancel = context.WithCancel(ctx)
	r.tasks = tasks
	r.setFleetIntervalLocked(r.fleetEvery)
	r.wake = make(chan struct{}, 1)
	r.wg.Go(r.loop)
}

func (r *taskRegistry) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *taskRegistry) loop() {
	for {
		r.mu.Lock()
		if r.ctx.Err() != nil {
			r.mu.Unlock()
			return
		}
		var earliest time.Time
		now := time.Now()
		for _, t := range r.tasks {
			if t.busy || t.next.IsZero() {
				continue
			}
			if !t.next.After(now) {
				r.launch(t, nil)
			} else if earliest.IsZero() || t.next.Before(earliest) {
				earliest = t.next
			}
		}
		r.mu.Unlock()
		var tick <-chan time.Time
		var timer *time.Timer
		if !earliest.IsZero() {
			timer = time.NewTimer(time.Until(earliest))
			tick = timer.C
		}
		select {
		case <-r.ctx.Done():
		case <-r.wake:
		case <-tick:
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// launch reserves the task under mu. Its next period starts when the work
// finishes, including any time spent waiting on the host's response.
func (r *taskRegistry) launch(t *scheduledTask, done chan<- error) {
	t.busy, t.next = true, time.Time{}
	r.wg.Go(func() {
		err := t.run(r.ctx)
		r.mu.Lock()
		t.busy = false
		switch {
		case r.ctx.Err() != nil:
			t.result = "cancelled"
		case errors.Is(err, errTaskOffline):
			t.result = "offline"
		case err != nil:
			t.result = "failed"
		default:
			t.result = "ok"
		}
		if r.ctx.Err() == nil && t.every > 0 {
			t.next = time.Now().Add(t.every)
		}
		r.signal()
		r.mu.Unlock()
		if done != nil {
			done <- err
		}
	})
}

// runNow shares the same reservation as a timer tick. A second caller
// learns that a run is active without adding another pass to a queue.
func (r *taskRegistry) runNow(name string) (string, error) {
	r.mu.Lock()
	if r.ctx == nil || r.ctx.Err() != nil {
		r.mu.Unlock()
		return "", errTaskStopped
	}
	for _, t := range r.tasks {
		if t.name != name {
			continue
		}
		if t.busy {
			r.mu.Unlock()
			return "already-running", nil
		}
		done := make(chan error, 1)
		r.launch(t, done)
		r.mu.Unlock()
		select {
		case err := <-done:
			return "finished", err
		case <-r.ctx.Done():
			return "", r.ctx.Err()
		}
	}
	r.mu.Unlock()
	return "", errTaskUnknown
}

func (r *taskRegistry) setFleetInterval(every time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fleetEvery = every
	r.setFleetIntervalLocked(every)
	r.signal()
}

func (r *taskRegistry) setFleetIntervalLocked(every time.Duration) {
	for _, t := range r.tasks {
		switch t.name {
		case taskFetch, taskMirrors, taskPRs:
			if t.every == every {
				continue
			}
			t.every, t.next = every, time.Time{}
			if every > 0 && !t.busy {
				t.next = time.Now().Add(every)
			}
		}
	}
}

func (r *taskRegistry) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *taskRegistry) snapshot() []ScheduledTaskDTO {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ScheduledTaskDTO, 0, len(r.tasks))
	for _, t := range r.tasks {
		s := ScheduledTaskDTO{Name: t.name, InFlight: t.busy, LastResult: t.result}
		if !t.next.IsZero() {
			s.NextAt = t.next.UnixMilli()
		}
		out = append(out, s)
	}
	return out
}
