package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/remote"
)

// gitbox-server proxy (#247): what each SSH session runs once the start
// script has verified the binary. It reaches the daemon of its build
// through the state folder's socket, starting the daemon when none
// answers, introduces the connection with its client id, and relays stdin
// and stdout. When the session ends the proxy exits and the daemon stays.
//
// When the daemon cannot be used, the proxy serves the session in its own
// process, exactly like serve --stdio: the work of the session stops when
// its link drops. It says so in the server log and in the handshake, and
// leaves the reason in the build folder for gitbox-server --doctor.

// daemonStartWait bounds how long a proxy waits for a daemon it started.
const daemonStartWait = 10 * time.Second

// fallbackFile, in the build folder, holds the reason of the last session
// a proxy served without the daemon.
const fallbackFile = "daemon.fallback"

// proxyEnv is what a proxy needs from its process. proxyMain fills it;
// tests give it short paths and their own way to start a daemon.
type proxyEnv struct {
	clientID string
	// buildDir is the build folder, empty when the binary is not
	// installed under a server root.
	buildDir string
	// socket is the daemon's socket path, and enterErr why there is none:
	// the state folder could not be prepared or entered.
	socket   string
	enterErr error
	// spawn starts the daemon of the state folder.
	spawn     func(stateDir string) (<-chan error, error)
	startWait time.Duration
	// newApp builds the App of a session served without the daemon.
	newApp func() *App
}

// proxyMain is the entry point of gitbox-server proxy. Before anything
// else runs, it enters the state folder: the socket path, relative to it,
// stays within the system's limit however long the build name is. That is
// the only change of working folder, and it happens here alone.
func proxyMain(clientID string, stdin io.Reader, stdout, stderr io.Writer) int {
	env := proxyEnv{clientID: clientID, spawn: spawnDaemon, startWait: daemonStartWait,
		newApp: func() *App { return newServerApp("") }}
	if installedBuild != nil {
		env.buildDir = installedBuild()
	}
	if env.buildDir != "" && remote.DaemonSupported {
		env.socket, env.enterErr = enterStateDir(remote.StateDir(env.buildDir))
	}
	return runProxy(env, stdin, stdout, stderr)
}

// enterStateDir prepares the state folder and makes it this process's
// working folder. It returns the socket path relative to it.
func enterStateDir(dir string) (string, error) {
	if err := remote.PrepareStateDir(dir); err != nil {
		return "", err
	}
	if err := os.Chdir(dir); err != nil {
		return "", &remote.DaemonError{Reason: remote.ReasonStateUnsafe, Detail: "the state folder cannot be entered"}
	}
	return remote.SocketName, nil
}

// spawnDaemon starts gitbox-server run from the binary the start script
// just verified, in the state folder.
func spawnDaemon(stateDir string) (<-chan error, error) {
	exe := installedExe()
	if exe == "" {
		return nil, &remote.DaemonError{Reason: remote.ReasonStartFailed, Detail: "the server binary cannot be located"}
	}
	return remote.SpawnDetached(exe, []string{"run"}, stateDir)
}

// runProxy relays the session to the daemon, or serves it here when the
// daemon cannot be used.
func runProxy(env proxyEnv, stdin io.Reader, stdout, stderr io.Writer) int {
	// As in serve: a write to a closed session fails instead of killing
	// the process.
	signal.Ignore(syscall.SIGPIPE)
	log := stderr
	if serverLog != nil {
		log = serverLog(stderr)
	}
	logf := func(format string, args ...any) { fmt.Fprintf(log, "gitbox-server: "+format+"\n", args...) }

	conn, started, err := env.reach()
	if err != nil {
		reason := remote.ReasonOf(err)
		logf("proxy: the daemon is unavailable (%s); this session is served in this process, and its work stops if the link drops", reason)
		recordFallback(env.buildDir, reason)
		return serveSession(env.newApp(), env.clientID, reason, stdin, stdout, log)
	}
	defer conn.Close()
	if onServe != nil {
		defer onServe(env.clientID, log)()
	}
	if started {
		logf("proxy: started the daemon")
	} else {
		logf("proxy: joined the daemon")
	}
	logf("serving client %s", env.clientID)
	if err := remote.WriteProxyHello(conn, env.clientID, started); err != nil {
		logf("proxy: the daemon closed the connection")
		return serverExitFailed
	}
	relay(conn, stdin, stdout)
	return serverExitOK
}

// reach connects to the daemon, starting it when none answers. started
// says whether this proxy had to start it.
func (env proxyEnv) reach() (c net.Conn, started bool, err error) {
	switch {
	case !remote.DaemonSupported:
		return nil, false, &remote.DaemonError{Reason: remote.ReasonUnsupported, Detail: "this system runs no daemon"}
	case env.buildDir == "":
		return nil, false, &remote.DaemonError{Reason: remote.ReasonNotInstalled, Detail: "the server is not installed under a server root"}
	case env.enterErr != nil:
		return nil, false, env.enterErr
	}
	if c, err := remote.DialSocket(env.socket, time.Second); err == nil {
		return c, false, nil
	}
	exited, err := env.spawn(remote.StateDir(env.buildDir))
	if err != nil {
		return nil, false, err
	}
	deadline := time.Now().Add(env.startWait)
	for {
		if c, err := remote.DialSocket(env.socket, time.Second); err == nil {
			return c, true, nil
		}
		// A daemon that ended at once found another holding the lock: that
		// one is about to listen.
		select {
		case <-exited:
			exited = nil
		default:
		}
		if time.Now().After(deadline) {
			return nil, false, &remote.DaemonError{Reason: remote.ReasonUnreachable, Detail: "the daemon did not answer in time"}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// relay copies the session's stdin to the daemon and the daemon's answers
// to its stdout. The end of stdin ends the connection's input, which tells
// the daemon the client detached; relay returns when the daemon has closed
// its side.
func relay(c net.Conn, stdin io.Reader, stdout io.Writer) {
	go func() {
		_, _ = io.Copy(c, stdin)
		if cw, ok := c.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = c.Close()
		}
	}()
	_, _ = io.Copy(stdout, c)
}

// recordFallback leaves the reason of a session served without the daemon
// in the build folder.
func recordFallback(buildDir, reason string) {
	if buildDir == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(buildDir, fallbackFile), []byte(reason+"\n"), 0o600)
}

// daemonMain is the entry point of gitbox-server run. Like proxyMain it
// enters the state folder first, the only change of working folder.
func daemonMain(idle time.Duration, stderr io.Writer) int {
	buildDir := ""
	if installedBuild != nil {
		buildDir = installedBuild()
	}
	if buildDir == "" || !remote.DaemonSupported {
		fmt.Fprintln(stderr, "gitbox-server: run needs a server installed under a server root on Linux or macOS")
		return serverExitFailed
	}
	stateDir := remote.StateDir(buildDir)
	socket, err := enterStateDir(stateDir)
	if err != nil {
		fmt.Fprintf(stderr, "gitbox-server: run: %s\n", remote.ReasonOf(err))
		return serverExitFailed
	}
	signal.Ignore(syscall.SIGPIPE, syscall.SIGHUP)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log := stderr
	if serverLog != nil {
		log = serverLog(stderr)
	}
	err = runDaemon(ctx, daemonConfig{socket: socket, stateDir: stateDir, idle: idle, log: log, selfHash: fileSHA256(installedExe())})
	if err != nil {
		return serverExitFailed
	}
	return serverExitOK
}

// fileSHA256 is the hex SHA-256 of a file, or empty.
func fileSHA256(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// parseRunArgs reads the arguments of run: nothing, or --idle <duration>
// of at least a second.
func parseRunArgs(args []string) (time.Duration, bool) {
	switch {
	case len(args) == 0:
		return daemonIdle, true
	case len(args) == 2 && args[0] == "--idle":
		d, err := time.ParseDuration(strings.TrimSpace(args[1]))
		return d, err == nil && d >= time.Second
	}
	return 0, false
}

// doctorDaemon is the daemon part of gitbox-server --doctor.
type doctorDaemon struct {
	// State is one of the doctorDaemon* values.
	State   string `json:"state"`
	Running bool   `json:"running"`
	// LastFallback is the reason of the last session a proxy served
	// without the daemon, a remote.Reason* value.
	LastFallback string `json:"lastFallback,omitempty"`
}

const (
	doctorDaemonOK           = "ok"
	doctorDaemonAbsent       = "absent"
	doctorDaemonUnsafe       = "unsafe"
	doctorDaemonUnsupported  = "unsupported"
	doctorDaemonNotInstalled = "not_installed"
)

// doctorDaemonStates is every State a client accepts, and fallbackReasons
// every LastFallback.
var (
	doctorDaemonStates = []string{doctorDaemonOK, doctorDaemonAbsent, doctorDaemonUnsafe, doctorDaemonUnsupported, doctorDaemonNotInstalled}
	fallbackReasons    = []string{remote.ReasonUnsupported, remote.ReasonStateUnsafe, remote.ReasonStartFailed, remote.ReasonUnreachable, remote.ReasonNotInstalled}
)

// daemonDoctor reads the daemon's state for the build folder, without
// changing anything.
func daemonDoctor(buildDir string) doctorDaemon {
	switch {
	case !remote.DaemonSupported:
		return doctorDaemon{State: doctorDaemonUnsupported}
	case buildDir == "":
		return doctorDaemon{State: doctorDaemonNotInstalled}
	}
	var d doctorDaemon
	if data, err := os.ReadFile(filepath.Join(buildDir, fallbackFile)); err == nil {
		if reason := strings.TrimSpace(string(data)); identifier(reason) == reason {
			d.LastFallback = reason
		}
	}
	state := remote.StateDir(buildDir)
	switch _, err := os.Lstat(state); {
	case err != nil:
		d.State = doctorDaemonAbsent
		return d
	case remote.CheckStateDir(state) != nil:
		d.State = doctorDaemonUnsafe
		return d
	}
	d.State = doctorDaemonOK
	_, d.Running = remote.DaemonAlive(state)
	return d
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
