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
		a.beforeClose(context.Background())
		if got := loadConfig(t, path).Global; !reflect.DeepEqual(got.Window, ws(10)) {
			t.Fatalf("BeforeClose: window %+v, want x=10", got.Window)
		}
		a.windowGeom = geom(20)
		a.beforeClose(context.Background())
		if got := loadConfig(t, path).Global; !reflect.DeepEqual(got.Window, ws(20)) {
			t.Errorf("a second BeforeClose: window %+v, want x=20", got.Window)
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
		a.beforeClose(context.Background())
		a.windowGeom = geom(20)
		a.beforeClose(context.Background())

		got := loadConfig(t, localPath).Global
		if !reflect.DeepEqual(got.Window, ws(1)) {
			t.Errorf("the remote window wrote the local window's geometry: %+v", got.Window)
		}
		if r := got.Remotes[0]; r.Window != nil {
			t.Errorf("the remote window wrote another remote's entry: %+v", r)
		}
		if r := got.Remotes[1]; !reflect.DeepEqual(r.Window, ws(20)) {
			t.Errorf("remotes[1]: window %+v, want x=20", r.Window)
		}
		if stamp(t, backendPath) != before {
			t.Errorf("window geometry touched the backend's file")
		}
	})
}
