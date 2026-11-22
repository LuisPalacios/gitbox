//go:build !gitboxserver

package main

import (
	"context"
	"reflect"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestWindowGeometryTarget(t *testing.T) {
	geom := func(x int) func() (int, int, int, int, bool) {
		return func() (int, int, int, int, bool) { return x, x + 1, 900, 700, true }
	}
	ws := func(x int) *config.WindowState { return &config.WindowState{X: x, Y: x + 1, Width: 900, Height: 700} }

	t.Run("local window", func(t *testing.T) {
		path := seedConfig(t, "gitbox.json", nil)
		a := &App{cfg: loadConfig(t, path), cfgPath: path, cfgLoaded: true}

		a.windowGeom = geom(10)
		a.BeforeClose(context.Background())
		if got := loadConfig(t, path).Global; !reflect.DeepEqual(got.Window, ws(10)) || got.CompactWindow != nil {
			t.Fatalf("BeforeClose in full view: window %+v, compact %+v", got.Window, got.CompactWindow)
		}

		a.windowGeom = geom(20)
		if target := a.SetViewMode("compact"); target != nil {
			t.Errorf("SetViewMode(compact) returned %+v with no compact window saved", target)
		}
		a.windowGeom = geom(30)
		a.BeforeClose(context.Background())
		got := loadConfig(t, path).Global
		if got.ViewMode != "compact" || !reflect.DeepEqual(got.Window, ws(20)) || !reflect.DeepEqual(got.CompactWindow, ws(30)) {
			t.Errorf("after switching to compact: view %q, window %+v, compact %+v", got.ViewMode, got.Window, got.CompactWindow)
		}
		if target := a.SetViewMode("full"); target == nil || target.X != 20 {
			t.Errorf("SetViewMode(full) = %+v, want the saved full window", target)
		}
	})

	t.Run("remote window", func(t *testing.T) {
		backendPath := seedConfig(t, "backend.json", nil)
		localPath := seedConfig(t, "local.json", func(c *config.Config) {
			c.Global.Window = ws(1)
			c.Global.Remotes = []config.RemoteEntry{{Alias: "other"}, {Alias: "my-server"}}
		})
		a := newTestApp(fileBackend(t, backendPath), localPath)
		a.remoteAlias = "my-server"
		before := stamp(t, backendPath)

		a.windowGeom = geom(10)
		a.BeforeClose(context.Background())
		a.windowGeom = geom(20)
		a.SetViewMode("compact")
		a.windowGeom = geom(30)
		a.BeforeClose(context.Background())

		got := loadConfig(t, localPath).Global
		if !reflect.DeepEqual(got.Window, ws(1)) || got.CompactWindow != nil {
			t.Errorf("the remote window wrote the local window's slots: %+v, %+v", got.Window, got.CompactWindow)
		}
		if r := got.Remotes[0]; r.Window != nil || r.CompactWindow != nil {
			t.Errorf("the remote window wrote another remote's entry: %+v", r)
		}
		if r := got.Remotes[1]; !reflect.DeepEqual(r.Window, ws(20)) || !reflect.DeepEqual(r.CompactWindow, ws(30)) {
			t.Errorf("remotes[1]: window %+v, compact %+v; want x=20 and x=30", r.Window, r.CompactWindow)
		}
		if stamp(t, backendPath) != before {
			t.Errorf("window geometry touched the backend's file")
		}
	})
}
