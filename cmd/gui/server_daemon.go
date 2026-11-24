package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// gitbox-server run (#247): the daemon of one build for one host user. It
// keeps one App and one persistent rpc.Server for its whole life, listens on
// the Unix socket of the build's state folder, and serves every proxy that
// connects. The work a client starts keeps running when its SSH session
// ends; the client gets the events it missed when it reconnects. The
// daemon runs the fleet checks at the interval the last client set, and
// ends when no client has been attached for its idle time and nothing is
// running, or on SIGTERM.

// daemonIdle is how long the daemon waits with no client before it ends.
const daemonIdle = 24 * time.Hour

// daemonIdleCheck is how often the daemon looks at its activity.
const daemonIdleCheck = time.Minute

// proxyHelloWait bounds the line a proxy sends before the protocol.
const proxyHelloWait = 5 * time.Second

// daemonConfig is one daemon. The entry point (daemonMain) fills it; tests
// build it with short paths and never change the working folder.
type daemonConfig struct {
	// socket is where to listen: relative to the working folder when the
	// absolute path is too long for a socket.
	socket string
	// stateDir is the absolute state folder, where the lock and the
	// journal live.
	stateDir string
	// idle is how long to wait with no client; zero means daemonIdle.
	idle time.Duration
	// cfgPath is the host's gitbox.json, empty for the default.
	cfgPath string
	log     io.Writer
	// selfHash is the SHA-256 of the binary, logged and never trusted: the
	// process says it about itself.
	selfHash string
	// peerUID reads the uid of a connection's peer; nil is remote.PeerUID.
	peerUID func(net.Conn) (int, error)
	// ready, when set, is closed once the daemon listens.
	ready chan<- struct{}
	// prepare, when set, sees the App and the server before the daemon
	// serves: tests add handlers there.
	prepare func(*App, *rpc.Server)
}

// replayedEvent names the events a reconnecting client gets again: those
// that end an operation.
func replayedEvent(name string) bool {
	return strings.HasSuffix(name, ":done") || strings.HasSuffix(name, ":alldone")
}

// runDaemon is the daemon. It returns nil when it ended on its own, when ctx
// ended, or at once when another daemon holds the state folder.
func runDaemon(ctx context.Context, d daemonConfig) error {
	logf := func(format string, args ...any) { fmt.Fprintf(d.log, "gitbox-server: "+format+"\n", args...) }
	lock, err := remote.LockDaemon(d.stateDir)
	if errors.Is(err, remote.ErrDaemonRunning) {
		return nil
	}
	if err != nil {
		logf("daemon: %s", remote.ReasonOf(err))
		return err
	}
	defer lock.Release()
	journal, err := rpc.NewJournal(rpc.MaxJournalEvents, filepath.Join(d.stateDir, remote.EventsLogName))
	if err != nil {
		logf("daemon: %s cannot be written", remote.EventsLogName)
		return err
	}
	defer journal.Close()
	listener, err := remote.ListenSocket(d.socket)
	if err != nil {
		logf("daemon: %s", remote.ReasonOf(err))
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	a := newServerApp(d.cfgPath)
	srv := newRPCServer(a, d.log)
	srv.Persistent = &rpc.Persistent{Instance: newInstanceID(), Journal: journal, Replay: replayedEvent}
	if d.prepare != nil {
		d.prepare(a, srv)
	}
	backend := a.be()
	backend.Init(ctx)
	a.tasks.start(ctx, append([]*scheduledTask{a.hostHarnessTask()}, a.fleetTasks()...))
	hash := d.selfHash
	if hash == "" {
		hash = "unknown"
	}
	logf("daemon: started, pid %d, instance %s, binary sha256 %s (as the process reports it)", os.Getpid(), srv.Persistent.Instance, hash)

	var conns sync.WaitGroup
	conns.Go(func() { acceptLoop(ctx, listener, srv, d, logf, &conns) })
	if d.ready != nil {
		close(d.ready)
	}
	idle := d.idle
	if idle <= 0 {
		idle = daemonIdle
	}
	started := time.Now()
	tick := time.NewTicker(min(daemonIdleCheck, max(idle/4, 10*time.Millisecond)))
	defer tick.Stop()
	for running := true; running; {
		select {
		case <-ctx.Done():
			logf("daemon: stopping")
			running = false
		case <-tick.C:
			act := srv.Activity()
			since := act.LastDetach
			if since.IsZero() {
				since = started
			}
			if act.Attached == 0 && act.InFlight == 0 && !a.tasks.busy() && time.Since(since) >= idle {
				logf("daemon: no client for %s and nothing running; stopping", idle.Round(time.Second))
				running = false
			}
		}
	}
	cancel()
	_ = listener.Close()
	conns.Wait()
	if !srv.Wait(rpc.ShutdownGrace) {
		logf("daemon: stopping with requests still running")
	}
	a.tasks.stop()
	backend.Close()
	_ = os.Remove(d.socket)
	return nil
}

// acceptLoop serves each connection of this user's proxies until the
// listener closes. A peer of another uid is refused before it can say
// anything.
func acceptLoop(ctx context.Context, l net.Listener, srv *rpc.Server, d daemonConfig, logf func(string, ...any), conns *sync.WaitGroup) {
	peerUID := d.peerUID
	if peerUID == nil {
		peerUID = remote.PeerUID
	}
	own := os.Getuid()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		uid, err := peerUID(c)
		if err != nil || uid != own {
			if err != nil {
				logf("daemon: refused a connection whose peer cannot be identified")
			} else {
				logf("daemon: refused a connection from uid %d", uid)
			}
			_ = c.Close()
			continue
		}
		conns.Go(func() {
			defer c.Close()
			stop := context.AfterFunc(ctx, func() { _ = c.Close() })
			defer stop()
			_ = c.SetReadDeadline(time.Now().Add(proxyHelloWait))
			clientID, started, err := remote.ReadProxyHello(c)
			if err != nil {
				logf("daemon: refused a connection that did not introduce itself")
				return
			}
			_ = c.SetReadDeadline(time.Time{})
			logf("daemon: serving client %s", clientID)
			_ = srv.ServeConn(ctx, c, c, rpc.Peer{ClientID: clientID, Started: started})
			logf("daemon: client %s detached", clientID)
		})
	}
}

// newInstanceID names one run of the daemon.
func newInstanceID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "d" + hex.EncodeToString(b[:])
}
