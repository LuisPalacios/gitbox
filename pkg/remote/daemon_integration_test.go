//go:build sshd_integration

package remote_test

// The daemon of #247 against a real sshd: every session goes through
// gitbox-server proxy to the daemon of its build, as a remote window's do.
// The rest of the battery keeps serve --stdio, the path of a session the
// host serves without the daemon.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// daemonClient is a client whose sessions go through the daemon.
func (c *container) daemonClient(t *testing.T, clientID string) *session {
	t.Helper()
	s := c.client(t, clientID, true)
	s.opts.Daemon = true
	return s
}

// daemonDial is how one session of a test connects.
type daemonDial struct {
	events *eventLog
	resume *rpc.ResumePoint
	onDead func(error)
}

// dialDaemon starts a session and its handshake, and fails unless the
// daemon serves it.
func (s *session) dialDaemon(ctx context.Context, o daemonDial) (connected, error) {
	sess, err := remote.Dial(ctx, alias, s.opts)
	if err != nil {
		return connected{}, fmt.Errorf("Dial: %w", err)
	}
	conn := sess.Conn()
	opts := rpc.ClientOptions{Version: it.version, Commit: it.commit, Log: s.log, Resume: o.resume, OnDead: o.onDead}
	if o.events != nil {
		opts.OnEvent = o.events.record
	}
	client, err := rpc.Dial(conn, conn, opts)
	if err != nil {
		_ = sess.Close()
		return connected{}, fmt.Errorf("handshake: %w", err)
	}
	if hello := client.Server(); hello.Daemon == nil {
		_ = client.Close()
		_ = sess.Close()
		return connected{}, fmt.Errorf("the host served the session without the daemon (%q)", hello.Fallback)
	}
	return connected{sess, client}, nil
}

func (s *session) connectDaemon(t *testing.T, o daemonDial) connected {
	t.Helper()
	conn, err := s.dialDaemon(testCtx(t), o)
	if err != nil {
		s.dump(t)
		t.Fatal(err)
	}
	return conn
}

// reconnect connects again the way a remote window does after its link
// dropped: remote.DefaultReconnect's attempts, each bounded. before runs
// ahead of each attempt.
func (s *session) reconnect(t *testing.T, o daemonDial, before func(n int)) (connected, int) {
	t.Helper()
	var conn connected
	attempts := 0
	err := remote.DefaultReconnect.Run(testCtx(t), func(ctx context.Context, n int) error {
		attempts = n
		if before != nil {
			before(n)
		}
		actx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		var err error
		conn, err = s.dialDaemon(actx, o)
		return err
	})
	if err != nil {
		s.dump(t)
		t.Fatalf("no attempt connected again: %v", err)
	}
	return conn, attempts
}

// stateDir is the daemon's state folder of this session's build.
func (s *session) stateDir() string { return s.root + "/" + s.build + "/" + remote.StateDirName }

// repos is the set of repos of every payload of name, as "source/repo".
func (l *eventLog) repos(name string) map[string]bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]bool{}
	for _, raw := range l.payloads[name] {
		var p struct {
			Source string `json:"source"`
			Repo   string `json:"repo"`
		}
		if json.Unmarshal(raw, &p) == nil && p.Repo != "" {
			out[p.Source+"/"+p.Repo] = true
		}
	}
	return out
}

func (l *eventLog) count(name string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.payloads[name])
}

// fleetHostConfig is a host with n small repositories, all cloned, whose
// origin the git daemon of the container serves.
func fleetHostConfig(names []string) string {
	repos := make([]string, len(names))
	for i, n := range names {
		repos[i] = fmt.Sprintf("%q: {}", n)
	}
	return `{
  "version": 5,
  "global": {"roots": ["` + testHome + `/git"], "theme": "dark"},
  "accounts": {
    "acct": {"provider": "gitea", "url": "http://127.0.0.1:9418", "username": "alice", "name": "Alice", "email": "a@example.invalid", "default_credential_type": "gcm"}
  },
  "sources": {
    "acct": {"account": "acct", "repos": {` + strings.Join(repos, ", ") + `}}
  }
}
`
}

// seedFleetHost serves n repositories with one commit each, clones them
// on the host where the config expects them, and writes that config.
func seedFleetHost(t *testing.T, c *container, n int) []string {
	t.Helper()
	docker(t, "exec", c.id, "sh", "-c", "set -e; for i in $(seq -w 1 "+strconv.Itoa(n)+"); do "+
		"git init -q -b main /tmp/w$i && git -C /tmp/w$i -c user.name=f -c user.email=f@example.invalid commit -q --allow-empty -m one && "+
		"git clone -q --bare /tmp/w$i /srv/git/org/r$i.git && rm -rf /tmp/w$i; done; chmod -R a+rX /srv/git")
	var names []string
	for i := 1; i <= n; i++ {
		names = append(names, fmt.Sprintf("org/r%0*d", len(strconv.Itoa(n)), i))
	}
	cfgDir := testHome + "/.config/gitbox"
	c.sh("mkdir -p " + cfgDir + " " + testHome + "/git/acct/org && echo " + base64.StdEncoding.EncodeToString([]byte(fleetHostConfig(names))) + " | base64 -d > " + cfgDir + "/gitbox.json" +
		" && git config --global url.git://127.0.0.1:9418/.insteadOf http://alice@127.0.0.1:9418/" +
		" && for r in " + strings.Join(names, " ") + "; do git clone -q http://alice@127.0.0.1:9418/$r.git " + testHome + "/git/acct/$r; done")
	return names
}

// statusRows is GetAllStatus reduced to what the comparison needs.
func statusRows(t *testing.T, conn connected) map[string]string {
	t.Helper()
	var rows []map[string]any
	if err := conn.client.Call(testCtx(t), "GetAllStatus", nil, &rows); err != nil {
		t.Fatalf("GetAllStatus: %v", err)
	}
	out := map[string]string{}
	for _, r := range rows {
		out[fmt.Sprint(r["source"], "/", r["repo"])] = fmt.Sprint(r["state"], " ahead ", r["ahead"], " behind ", r["behind"])
	}
	return out
}

// TestKillSSHDuringFetchAll kills the client's ssh once 30 % of a fetch
// across 50 repositories is done. The daemon finishes the run, the client
// is connected again within 30 seconds, the fetch:done events it got
// before the cut and with the resume cover every repository, and the
// status it reads equals what a fresh client reads.
func TestKillSSHDuringFetchAll(t *testing.T) {
	c := startContainer(t, ctOpts{netAdmin: true, gitDaemon: true})
	names := seedFleetHost(t, c, 50)
	// Every round trip to the git daemon takes 100 ms more, so the run
	// lasts several seconds.
	docker(t, "exec", c.id, "tc", "qdisc", "add", "dev", "lo", "root", "netem", "delay", "100ms")
	s := c.daemonClient(t, "client-fetch")

	before := &eventLog{}
	cut := make(chan struct{})
	var once sync.Once
	before.onEvent = func(name string, _ json.RawMessage) {
		if name == "fetch:done" && before.count("fetch:done") >= 15 {
			once.Do(func() { close(cut) })
		}
	}
	conn := s.connectDaemon(t, daemonDial{events: before})
	go func() { _ = conn.client.Call(testCtx(t), "FetchAllRepos", nil, nil) }()
	select {
	case <-cut:
	case <-time.After(90 * time.Second):
		t.Fatalf("the fetch never reached 30 %%: %d done", before.count("fetch:done"))
	}
	remote.KillSession(conn.sess)
	cutAt := time.Now()
	point := conn.client.ResumePoint()
	conn.close()
	if before.count("fetch:alldone") != 0 {
		t.Fatal("the run ended before the cut; the test proves nothing")
	}

	after := &eventLog{}
	conn2, _ := s.reconnect(t, daemonDial{events: after, resume: &point}, nil)
	defer conn2.close()
	if took := time.Since(cutAt); took > 30*time.Second {
		t.Errorf("connected again after %v, want within 30 s", took)
	}
	if !conn2.client.Resumed().Resumed {
		t.Fatal("the new session did not resume from the daemon it left")
	}
	eventually(t, "the run to end", func() bool { return after.count("fetch:alldone") == 1 })
	seen := before.repos("fetch:done")
	for r := range after.repos("fetch:done") {
		seen[r] = true
	}
	for _, n := range names {
		if !seen["acct/"+n] {
			t.Errorf("no fetch:done reached the client for %s", n)
		}
	}
	if msg := doneError(t, after.last("fetch:alldone")); msg != "" {
		t.Errorf("the run failed: %s", msg)
	}
	fresh := c.daemonClient(t, "client-fresh").connectDaemon(t, daemonDial{})
	defer fresh.close()
	if got, want := statusRows(t, conn2), statusRows(t, fresh); len(got) != len(names) || !mapsEqual(got, want) {
		t.Errorf("the reconnected status:\n%v\na fresh client's:\n%v", got, want)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestResumeInFlightClone drops the connection during a clone. After the
// reconnect, the retry with the same request id returns the daemon's
// result, and git ran one clone: trace2 records every git process.
func TestResumeInFlightClone(t *testing.T) {
	c := startContainer(t, ctOpts{netAdmin: true, gitDaemon: true})
	seedCloneHost(c)
	trace := testHome + "/trace2"
	c.sh("mkdir -p " + trace + " && git config --global trace2.eventTarget " + trace)
	docker(t, "exec", c.id, "tc", "qdisc", "add", "dev", "lo", "root", "netem", "rate", "40mbit")
	s := c.daemonClient(t, "client-clone")

	log := &eventLog{}
	cut := make(chan struct{})
	var once sync.Once
	log.onEvent = func(name string, raw json.RawMessage) {
		var p struct {
			Phase   string `json:"phase"`
			Percent int    `json:"percent"`
		}
		if name == "clone:progress" && json.Unmarshal(raw, &p) == nil && strings.HasPrefix(p.Phase, "Receiving") && p.Percent > 30 {
			once.Do(func() { close(cut) })
		}
	}
	const id = "00000000-0000-4000-8000-000000000701"
	conn := s.connectDaemon(t, daemonDial{events: log})
	first := make(chan error, 1)
	go func() {
		first <- conn.client.Call(testCtx(t), "CloneRepo", []any{"acct", "org/fixture", ""}, nil, rpc.WithRequestID(id))
	}()
	select {
	case <-cut:
	case err := <-first:
		t.Fatalf("the clone ended before 30 %%: %v", err)
	case <-time.After(90 * time.Second):
		t.Fatal("the clone never passed 30 %")
	}
	remote.KillSession(conn.sess)
	point := conn.client.ResumePoint()
	<-first
	conn.close()
	if c.gitProcesses() == "" {
		t.Fatal("git stopped with the cut: the daemon must keep the clone running")
	}

	log2 := &eventLog{}
	conn2, _ := s.reconnect(t, daemonDial{events: log2, resume: &point}, nil)
	defer conn2.close()
	if err := conn2.client.Call(testCtx(t), "CloneRepo", []any{"acct", "org/fixture", ""}, nil, rpc.WithRequestID(id)); err != nil {
		t.Fatalf("the retry with the same request id = %v", err)
	}
	eventually(t, "clone:done", func() bool { return log2.last("clone:done") != nil })
	if msg := doneError(t, log2.last("clone:done")); msg != "" {
		t.Fatalf("the clone failed: %s", msg)
	}
	c.sh("git -C " + fixtureClone + " rev-parse --verify -q HEAD")
	clones := c.sh("cat " + trace + "/* | grep '\"event\":\"cmd_name\"' | grep -c '\"name\":\"clone\"' || true")
	if clones != "1" {
		t.Errorf("git ran %s clones, want one", clones)
	}
}

// TestPeriodicSyncWhileDisconnected sets the daemon's interval to one
// minute (the e2e build accepts it), leaves for three minutes and comes
// back: the daemon fetched on its own, at least twice, and kept the runs.
func TestPeriodicSyncWhileDisconnected(t *testing.T) {
	t.Parallel()
	c := startContainer(t, ctOpts{gitDaemon: true})
	seedFleetHost(t, c, 2)
	s := c.daemonClient(t, "client-periodic")
	conn := s.connectDaemon(t, daemonDial{})
	if err := conn.client.Call(testCtx(t), "SetFleetSchedule", []any{"1m"}, nil); err != nil {
		t.Fatalf("SetFleetSchedule: %v", err)
	}
	point := conn.client.ResumePoint()
	conn.close()

	time.Sleep(3*time.Minute + 15*time.Second)
	runs := c.sh("grep -c '\"name\":\"fetch:alldone\"' " + s.stateDir() + "/" + remote.EventsLogName + " || true")
	if n, _ := strconv.Atoi(runs); n < 2 {
		t.Fatalf("events.log holds %s fetch-all runs, want at least 2:\n%s", runs, c.sh("tail -n 20 "+s.root+"/logs/"+s.build+".log || true"))
	}
	log := &eventLog{}
	conn2 := s.connectDaemon(t, daemonDial{events: log, resume: &point})
	defer conn2.close()
	if got := conn2.client.Resumed(); !got.Resumed || log.count("fetch:alldone") < 2 {
		t.Errorf("the resume = %+v with %d fetch-all runs, want the runs that happened meanwhile", got, log.count("fetch:alldone"))
	}
}

// pgrepF lists the test user's processes whose command line holds text.
// One character goes in brackets, so the pattern no longer matches the
// command line of the shell that runs pgrep.
func pgrepF(text string) string {
	return "pgrep -u test -f " + q(text[:1]+"["+text[1:2]+"]"+text[2:]) + " || true"
}

// TestNoZombieDaemons: garbage collection of an old version stops its
// daemon, and after the uninstall no gitbox-server of the user is left.
func TestNoZombieDaemons(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.daemonClient(t, "client-zombie")
	s.connectDaemon(t, daemonDial{}).close()
	if c.sh(pgrepF("gitbox-server run")) == "" {
		t.Fatal("no daemon ran: the test proves nothing")
	}

	// An old version of the server, with its daemon running and no client.
	pin8 := "00000000"
	if strings.HasPrefix(it.pin, pin8) {
		pin8 = "11111111"
	}
	old := it.version + "-" + it.commit + "-" + pin8
	oldDir := s.root + "/" + old
	c.sh("umask 077 && mkdir -p " + oldDir + " && cp " + s.final + " " + oldDir + "/gitbox-server")
	docker(t, "exec", "-d", "-u", "test", "-e", "HOME="+testHome, c.id, oldDir+"/gitbox-server", "run")
	eventually(t, "the old daemon to start", func() bool { return c.exists(oldDir + "/state/daemon.pid") })

	conn := s.connectDaemon(t, daemonDial{})
	eventually(t, "the old version to be collected", func() bool { return !c.exists(oldDir) })
	if left := c.sh(pgrepF(oldDir)); left != "" {
		t.Errorf("the old version's daemon survived its collection: %s", left)
	}
	if !s.logHas("gc: stopped the daemon of " + old) {
		s.dump(t)
		t.Error("the log does not say the old daemon was stopped")
	}
	conn.close()

	res, err := remote.Uninstall(testCtx(t), alias, s.opts, false)
	if err != nil || !res.Done {
		t.Fatalf("Uninstall = %+v, %v", res, err)
	}
	eventually(t, "every gitbox-server to end", func() bool { return c.sh("pgrep -u test gitbox-server || true") == "" })
}

// TestSocketPermissions: the socket is the user's, mode 0600, in a 0700
// folder; another user cannot reach it, and with the modes opened on
// purpose the daemon still refuses that user by its uid.
func TestSocketPermissions(t *testing.T) {
	c := startContainer(t, ctOpts{})
	s := c.daemonClient(t, "client-socket")
	conn := s.connectDaemon(t, daemonDial{})
	defer conn.close()
	state := s.stateDir()
	sock := state + "/" + remote.SocketName
	if got := c.sh("stat -c '%U %a' " + q(sock)); got != "test 600" {
		t.Errorf("the socket is %q, want owner test and mode 600", got)
	}
	if got := c.mode(state); got != "700" {
		t.Errorf("the state folder has mode %s, want 700", got)
	}

	probe := filepath.Join(t.TempDir(), "sockprobe")
	build := exec.Command("go", "build", "-o", probe, "./testdata/sockprobe")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the probe: %v\n%s", err, out)
	}
	c.install(probe, "/tmp/sockprobe")
	docker(t, "exec", c.id, "chmod", "755", "/tmp/sockprobe")
	asGit := func() string { return docker(t, "exec", "-u", "git", c.id, "/tmp/sockprobe", sock) }
	if got := asGit(); got != "denied" {
		t.Errorf("another user reached the socket: %s", got)
	}

	// Open every folder on the way and the socket itself: only the peer
	// check stands.
	folders := []string{testHome, s.root, s.root + "/" + s.build, state}
	saved := make([]string, len(folders))
	for i, f := range folders {
		saved[i] = c.mode(f)
	}
	c.sh("chmod 711 " + q(testHome) + " " + q(s.root) + " " + q(s.root+"/"+s.build) + " && chmod 755 " + q(state) + " && chmod 666 " + q(sock))
	got := asGit()
	for i, f := range folders {
		c.sh("chmod " + saved[i] + " " + q(f))
	}
	c.sh("chmod 600 " + q(sock))
	if got != "closed" {
		t.Errorf("with the modes open, another user's connection was %s, want closed", got)
	}
	logFile := s.root + "/logs/" + s.build + ".log"
	eventually(t, "the refusal in the server log", func() bool {
		return strings.Contains(c.sh("cat "+q(logFile)+" || true"), "daemon: refused a connection from uid 1001")
	})
	if strings.Contains(c.sh("cat "+q(logFile)), "intruder") {
		t.Error("the daemon logged what the refused peer sent")
	}
	var path string
	if err := conn.client.Call(testCtx(t), "GetConfigPath", nil, &path); err != nil {
		t.Errorf("the daemon stopped serving its own user: %v", err)
	}
}

// TestHeartbeatDeadLink drops every packet with tc. The client declares
// the link dead within three missed pings and a margin, then connects
// again once the link is back.
func TestHeartbeatDeadLink(t *testing.T) {
	t.Parallel()
	c := startContainer(t, ctOpts{netAdmin: true})
	s := c.daemonClient(t, "client-heartbeat")
	var dead atomic.Int64
	conn := s.connectDaemon(t, daemonDial{onDead: func(error) { dead.CompareAndSwap(0, time.Now().UnixNano()) }})
	point := conn.client.ResumePoint()
	docker(t, "exec", c.id, "tc", "qdisc", "add", "dev", "eth0", "root", "netem", "loss", "100%")
	dropped := time.Now()
	limit := time.Duration(rpc.MaxMissedPings)*rpc.PingInterval + 15*time.Second
	if !waitFor(limit, func() bool { return dead.Load() != 0 }) {
		t.Fatalf("the link was not declared dead within %v", limit)
	}
	took := time.Unix(0, dead.Load()).Sub(dropped)
	t.Logf("declared dead after %v", took.Round(time.Second))
	point = conn.client.ResumePoint()
	conn.close()

	restored := false
	conn2, attempts := s.reconnect(t, daemonDial{resume: &point}, func(n int) {
		if n == 2 && !restored {
			docker(t, "exec", c.id, "tc", "qdisc", "del", "dev", "eth0", "root")
			restored = true
		}
	})
	defer conn2.close()
	if attempts < 2 || !conn2.client.Resumed().Resumed {
		t.Errorf("connected again on attempt %d, resumed %+v; want a failed attempt while the link was down, then the same daemon", attempts, conn2.client.Resumed())
	}
}

// TestConfigChangedWithTheDaemon: two clients of one build share its
// daemon, so their saves never conflict with each other; a change another
// process makes to the host's gitbox.json still makes the next save fail
// with config.changed, and the retry keeps everything (#294).
func TestConfigChangedWithTheDaemon(t *testing.T) {
	c := startContainer(t, ctOpts{})
	cfgPath := seedMutationHost(c)
	a := c.daemonClient(t, "client-a").connectDaemon(t, daemonDial{})
	defer a.close()
	b := c.daemonClient(t, "client-b").connectDaemon(t, daemonDial{})
	defer b.close()
	if a.client.Server().Daemon.Instance != b.client.Server().Daemon.Instance {
		t.Fatal("two clients of one build reached two daemons")
	}
	call := func(conn connected, id int, key string) error {
		ctx, cancel := context.WithTimeout(testCtx(t), 30*time.Second)
		defer cancel()
		return conn.client.Call(ctx, "AddAccount", addAccountParams(key), nil, rpc.WithRequestID(fmt.Sprintf("00000000-0000-4000-8000-%012d", id)))
	}
	if err := call(a, 801, "froma"); err != nil {
		t.Fatalf("the first client's add = %v", err)
	}
	if err := call(b, 802, "fromb"); err != nil {
		t.Fatalf("the second client's add through the same daemon = %v", err)
	}
	// GitboxApp on the host saves a domain change.
	c.sh("sed -i 's/\"name\": \"Alice\"/\"name\": \"Alice B\"/' " + cfgPath)
	err := call(a, 803, "fromc")
	if e, ok := errors.AsType[*rpc.Error](err); !ok || e.Data.Code != "config.changed" {
		t.Fatalf("the add after another process saved = %v, want config.changed", err)
	}
	if err := call(a, 804, "fromc"); err != nil {
		t.Fatalf("the retry after the reload = %v", err)
	}
	got := hostAccounts(t, c, cfgPath)
	for _, key := range []string{"acct", "froma", "fromb", "fromc"} {
		if !slices.Contains(got, key) {
			t.Errorf("accounts %v, want %s kept", got, key)
		}
	}
	if !strings.Contains(c.sh("cat "+cfgPath), "Alice B") {
		t.Error("the other process's change was lost")
	}
}
