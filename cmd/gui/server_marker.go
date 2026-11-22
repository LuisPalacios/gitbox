//go:build gitboxserver

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
)

// The in-use marker of a running gitbox-server and the garbage collection
// that follows its start (#239). A marker says "this build is running for
// that client"; garbage collection keeps every build with a live one.

func init() {
	onServe = func(clientID string, log io.Writer) func() {
		return startInUse(installedExe(), clientID, log, time.Now)
	}
}

// installedExe is the path of the running binary with links resolved, or
// empty.
func installedExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe
}

// gcWait bounds how long a server that is stopping waits for its garbage
// collection.
const gcWait = 10 * time.Second

// startInUse writes this server's marker next to its binary, refreshes it
// every remote.MarkerRefresh, and starts a garbage collection of the server
// root. The returned function stops the refresh and deletes this server's
// own marker, and no other. A binary that is not installed under a server
// root, or a server with no client id, does nothing.
func startInUse(exe, clientID string, log io.Writer, now func() time.Time) func() {
	root, build, ok := remote.Locate(exe)
	if !ok || clientID == "" {
		return func() {}
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(log, "gitbox-server: "+format+"\n", args...)
	}
	owner := remote.Marker{PID: os.Getpid(), ClientID: clientID, StartedAt: now()}
	path, err := remote.WriteMarker(filepath.Join(root, build), owner)
	if err != nil {
		logf("the in-use marker could not be written")
		return func() {}
	}

	stop, refreshed := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(refreshed)
		tick := time.NewTicker(remote.MarkerRefresh)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				t := now()
				_ = os.Chtimes(path, t, t)
			}
		}
	}()

	collected := make(chan struct{})
	go func() {
		defer close(collected)
		if err := remote.CollectGarbage(remote.GC{Root: root, Current: build, Owner: owner, Now: now(), Logf: logf}); err != nil {
			logf("gc: failed")
		}
	}()

	return func() {
		close(stop)
		<-refreshed
		select {
		case <-collected:
		case <-time.After(gcWait):
		}
		_ = os.Remove(path)
	}
}
