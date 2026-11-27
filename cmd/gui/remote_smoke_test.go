//go:build remote_smoke && !gitboxserver

package main

// The remote smoke (#239): the client half of remote mode, run in-process
// against a real host over the user's own ssh, with no window. It drives
// the same App methods the Settings section and the remote window call:
// add the remote, connect (installing the server), read the log, run the
// doctor, disconnect and uninstall.
//
// It needs a build that carries its server, stamped like the app:
//
//	eval "$(scripts/stamp.sh)" && scripts/build-server.sh
//	pkg=github.com/LuisPalacios/gitbox/cmd/gui
//	GITBOX_SMOKE_ALIAS=<ssh alias> GITBOX_SMOKE_SERVER_DIR='~/.cache/<scratch>' \
//	  GITBOX_SMOKE_OS=<host GOOS, default linux> go test -tags serverbin,remote_smoke -run TestRemoteSmoke -v \
//	  -ldflags "-X $pkg.version=$VERSION -X $pkg.commit=$COMMIT" ./cmd/gui/
//
// A test binary takes the stamp under the package's import path, not as
// main.version.
//
// The config, the client preferences and the ssh control sockets live in
// a temporary folder; the user's gitbox.json and ssh files are only read.

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRemoteSmoke(t *testing.T) {
	alias, serverDir := os.Getenv("GITBOX_SMOKE_ALIAS"), os.Getenv("GITBOX_SMOKE_SERVER_DIR")
	if alias == "" || !strings.HasPrefix(serverDir, "~/.cache/") {
		t.Fatal("set GITBOX_SMOKE_ALIAS and a scratch GITBOX_SMOKE_SERVER_DIR under ~/.cache/")
	}
	wantOS := os.Getenv("GITBOX_SMOKE_OS")
	if wantOS == "" {
		wantOS = "linux"
	}
	// A short folder: ssh control socket paths are limited to about 100
	// bytes.
	short, err := os.MkdirTemp("/tmp", "gbxsmoke")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	t.Setenv("XDG_CONFIG_HOME", short)
	if err := thisServerBuild().check(); err != nil {
		t.Fatalf("this test binary cannot run remote mode: %v", err)
	}

	// The main window: add the remote.
	path := seedConfig(t, "gitbox.json", nil)
	main := newTestApp(withCfg(loadConfig(t, path)), withCfgPath(path), withCfgLoaded())
	if res := main.AddRemote(alias, serverDir); res.Error != "" {
		t.Fatalf("AddRemote: %+v", res)
	}
	if got := main.ListRemotes(); len(got.Remotes) != 1 || got.Remotes[0].ServerDir != serverDir {
		t.Fatalf("ListRemotes = %+v", got)
	}
	t.Logf("step 1 add: %s with server_dir %s saved to the test config", alias, serverDir)

	// The remote window, built the way runRemoteWindow builds it.
	win := NewApp()
	win.prefs = newFilePrefs(path)
	win.cfgPath, win.remoteAlias, win.windowMode = path, alias, "remote"
	win.remote = newRemoteSession(win, alias)
	win.backend = selectBackend(win)
	sink := &recordingSink{}
	win.events = sink

	t0 := time.Now()
	win.RemoteReconnect()
	deadline := time.Now().Add(remoteConnectTimeout)
	for st := win.RemoteInfo(); st.State != remoteConnected; st = win.RemoteInfo() {
		if st.State == remoteFailed || time.Now().After(deadline) {
			t.Fatalf("connect: %+v\n%s", st, strings.Join(win.RemoteLog(), "\n"))
		}
		time.Sleep(100 * time.Millisecond)
	}
	st := win.RemoteInfo()
	var seen []string
	for _, e := range sink.named("remote:state") {
		seen = append(seen, e.args[0].(RemoteStateDTO).State)
	}
	if st.ServerOS != wantOS || st.ServerVersion != formatAppVersion(version, commit) {
		t.Errorf("connected to %+v", st)
	}
	if !slices.Contains(seen, remoteInstalling) {
		t.Errorf("states %v: the first connect to a scratch server_dir must install", seen)
	}
	t.Logf("step 2 connect: %s in %s, states %v, server %s %s/%s", st.State, time.Since(t0).Round(time.Millisecond), seen, st.ServerVersion, st.ServerOS, st.ServerArch)

	if cfg, err := win.GetConfig(); err != nil {
		t.Errorf("GetConfig over the connection: %v", err)
	} else {
		t.Logf("step 2 rpc: GetConfig answered with %d accounts from the host", len(cfg.Accounts))
	}

	log := win.RemoteLog()
	for _, want := range []string{"install: uploaded file hash", "result ok", "matches the pin", "serving client " + win.remote.clientID} {
		if !slices.ContainsFunc(log, func(l string) bool { return strings.Contains(l, want) }) {
			t.Errorf("the connection log lacks %q", want)
		}
	}
	t.Logf("step 3 log: %d lines, %d remote:log events", len(log), len(sink.named("remote:log")))
	for _, l := range log {
		if strings.Contains(l, "install:") || strings.Contains(l, "start:") || strings.Contains(l, "gc:") {
			t.Logf("  %s", l)
		}
	}

	doc := win.RemoteRunDoctor()
	if doc.Error != "" || doc.Report == nil || doc.Report.OS != wantOS || doc.Report.Commit != commit {
		t.Fatalf("doctor: %+v", doc)
	}
	t.Logf("step 4 doctor: %s %s/%s, git %q, config folder writable %v, exec %v",
		doc.Report.Version, doc.Report.OS, doc.Report.Arch, doc.Report.GitVersion, doc.Report.ConfigDirWritable, doc.Report.ConfigDirExec)
	for _, r := range doc.Report.Roots {
		t.Logf("  root %s: %s", r.Path, r.State)
	}

	win.RemoteDisconnect()
	if st := win.RemoteInfo(); st.State != remoteDisconnected {
		t.Errorf("after disconnect: %+v", st)
	}
	if _, err := win.GetConfig(); !codeIs(err, "not_connected") {
		t.Errorf("GetConfig after disconnect = %v", err)
	}
	t.Logf("step 5 disconnect: %s", win.RemoteInfo().State)

	// The server's marker goes when it exits; give it a moment, then
	// uninstall, which must find no other live client.
	var res RemoteUninstallDTO
	for range 20 {
		if res = win.RemoteUninstall(alias, false); !res.NeedsConfirm {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if res.Error != "" || !res.Done || res.RootKept || res.OtherClients != 0 {
		t.Fatalf("uninstall: %+v\n%s", res, strings.Join(win.RemoteLog(), "\n"))
	}
	t.Logf("step 6 uninstall: done %v, root kept %v, other clients %d", res.Done, res.RootKept, res.OtherClients)
}
