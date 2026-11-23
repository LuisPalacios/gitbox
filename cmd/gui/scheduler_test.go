package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

func TestSchedulerCadenceAndConcurrentRunNow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var r taskRegistry
		t.Cleanup(func() { r.stop(); r.wg.Wait() })
		release := make(chan struct{})
		var hits, fast atomic.Int32
		slow := &scheduledTask{name: taskFetch, run: func(ctx context.Context) error {
			hits.Add(1)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
		mirrors := &scheduledTask{name: taskMirrors, run: func(context.Context) error { fast.Add(1); return nil }}
		r.start(t.Context(), []*scheduledTask{slow, mirrors})
		r.setFleetInterval(5 * time.Minute)
		synctest.Wait()
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if hits.Load() != 1 || fast.Load() != 1 {
			t.Fatalf("at first period: fetch %d, mirrors %d", hits.Load(), fast.Load())
		}
		if result, err := r.runNow(taskFetch); result != "already-running" || err != nil {
			t.Fatalf("duplicate = %q, %v", result, err)
		}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		view := r.snapshot()
		if hits.Load() != 1 || fast.Load() != 2 || !view[0].InFlight || view[0].NextAt != 0 {
			t.Fatalf("second period: %d, %d, %+v", hits.Load(), fast.Load(), view)
		}
		close(release)
		synctest.Wait()
		if got := r.snapshot()[0]; got.InFlight || got.LastResult != "ok" || got.NextAt != time.Now().Add(5*time.Minute).UnixMilli() {
			t.Fatalf("completion: %+v", got)
		}
		time.Sleep(4 * time.Minute)
		synctest.Wait()
		if hits.Load() != 1 {
			t.Fatal("queued an extra run")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if hits.Load() != 2 || fast.Load() != 3 {
			t.Fatalf("after completion: fetch %d, mirrors %d", hits.Load(), fast.Load())
		}
	})
}

func TestSchedulerReloadAndDisable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := seedConfig(t, "gitbox.json", nil)
		a := windowOn(t, path)
		runs := 0
		a.tasks.start(t.Context(), []*scheduledTask{{name: taskFetch, run: func(context.Context) error { runs++; return nil }}})
		t.Cleanup(func() { a.tasks.stop(); a.tasks.wg.Wait() })
		if err := a.SetPeriodicSync("5m"); err != nil {
			t.Fatal(err)
		}
		if loadConfig(t, path).Global.PeriodicSync != "5m" {
			t.Fatal("interval not saved")
		}
		synctest.Wait()
		first := a.GetScheduledTasks()[0].NextAt
		time.Sleep(3 * time.Minute)
		if _, err := a.ReloadConfig(); err != nil {
			t.Fatal(err)
		}
		if got := a.GetScheduledTasks()[0].NextAt; got != first {
			t.Fatal("reload of same interval postponed the check")
		}
		external := loadConfig(t, path)
		external.Global.PeriodicSync = "15m"
		if err := config.Save(external, path); err != nil {
			t.Fatal(err)
		}
		if _, err := a.ReloadConfig(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Minute)
		synctest.Wait()
		if runs != 0 {
			t.Fatal("old interval remained armed")
		}
		if err := a.SetPeriodicSync("off"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Minute)
		synctest.Wait()
		if runs != 0 || a.GetScheduledTasks()[0].NextAt != 0 {
			t.Fatal("disabled timer fired")
		}
		result, err := a.RunTask(taskFetch)
		if err != nil || result != "finished" || runs != 1 {
			t.Fatalf("manual while disabled: %q, %v, %d", result, err, runs)
		}
	})
}

func TestSchedulerShutdownCancelsInFlightWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var r taskRegistry
		exited := false
		r.start(t.Context(), []*scheduledTask{{name: taskUpdate, next: time.Now(), run: func(ctx context.Context) error {
			<-ctx.Done()
			exited = true
			return ctx.Err()
		}}})
		synctest.Wait()
		r.stop()
		r.stop()
		r.wg.Wait()
		got := r.snapshot()[0]
		if !exited || got.InFlight || got.LastResult != "cancelled" {
			t.Fatalf("shutdown: %+v", got)
		}
		if _, err := r.runNow(taskUpdate); !errors.Is(err, errTaskStopped) {
			t.Fatalf("after shutdown: %v", err)
		}
	})
}

func TestSchedulerBootWithSavedInterval(t *testing.T) {
	for name, earlySnapshot := range map[string]bool{"snapshot first": true, "registry first": false} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a := &App{cfg: &config.Config{Global: config.GlobalConfig{PeriodicSync: "5m"}}}
				runs := 0
				start := func() {
					a.tasks.start(t.Context(), []*scheduledTask{{name: taskFetch, run: func(context.Context) error { runs++; return nil }}})
				}
				if !earlySnapshot {
					start()
				}
				if _, err := a.GetStartupSnapshot(); err != nil {
					t.Fatal(err)
				}
				if earlySnapshot {
					start()
				}
				t.Cleanup(func() { a.tasks.stop(); a.tasks.wg.Wait() })
				time.Sleep(4 * time.Minute)
				a.syncTaskInterval()
				time.Sleep(time.Minute)
				synctest.Wait()
				if runs != 1 {
					t.Fatalf("runs after five minutes = %d", runs)
				}
			})
		})
	}
}

func TestScheduledPRCheckWaitsForProviderResult(t *testing.T) {
	t.Setenv("GIT_TOKEN", "fixture-token")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		a, _ := prTestApp(t, 1, func(ctx context.Context, _, _, _ string, _ bool) (provider.AccountPRs, error) {
			select {
			case <-release:
				data := testAccountPRs(7)
				data.Complete = false
				return data, nil
			case <-ctx.Done():
				return provider.AccountPRs{}, ctx.Err()
			}
		})
		t.Cleanup(a.closeLocal)
		a.tasks.start(t.Context(), []*scheduledTask{{name: taskPRs, run: func(ctx context.Context) error { return a.be().RunFleetCheck(ctx, taskPRs) }}})
		done := make(chan error, 1)
		go func() { _, err := a.RunTask(taskPRs); done <- err }()
		synctest.Wait()
		if !a.GetScheduledTasks()[0].InFlight {
			t.Fatal("PR check finished while the provider was still running")
		}
		close(release)
		if err := <-done; !errors.Is(err, errTaskPartial) {
			t.Fatalf("incomplete provider listing: %v", err)
		}
		if s := a.GetScheduledTasks()[0]; s.InFlight || s.LastResult != "failed" {
			t.Fatalf("incomplete check = %+v", s)
		}
	})
}

func TestScheduledRemoteChecksStayOnConnectedHost(t *testing.T) {
	a := &App{ctx: t.Context(), windowMode: "remote"}
	a.remote = newRemoteSession(a, "fixture")
	a.backend = a.remote.backend
	a.ensureWindowTasks()
	t.Cleanup(func() { a.tasks.stop(); a.tasks.wg.Wait() })
	if got := len(a.GetScheduledTasks()); got != 3 {
		t.Fatalf("remote window has %d tasks; only the three fleet checks belong there", got)
	}
	if _, err := a.RunTask(taskFetch); !errors.Is(err, errTaskOffline) {
		t.Fatalf("disconnected remote tried to fetch: %v", err)
	}
	if got := a.GetScheduledTasks()[0].LastResult; got != "offline" {
		t.Fatalf("disconnected task says %q", got)
	}
	if _, err := a.RunTask(taskUpdate); !errors.Is(err, errTaskUnknown) {
		t.Fatalf("remote window has an updater: %v", err)
	}
}
