package remote

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// The daemon of a build on the host (#247). gitbox-server run keeps one
// process per remote user and build; each SSH session starts
// gitbox-server proxy, which reaches it through a Unix socket in the state
// folder of the build and relays the session's stdin and stdout:
//
//	<root>/<build>/state/          mode 0700, owned by the user
//	<root>/<build>/state/rpc.sock  the daemon's socket, mode 0600
//	<root>/<build>/state/daemon.lock  held (flock) while the daemon runs
//	<root>/<build>/state/daemon.pid   its process id
//	<root>/<build>/state/events.log   the events it keeps for reconnects
//
// No TCP port is opened. This file holds what every platform shares; the
// mechanics are in daemon_unix.go, and a Windows host has none yet (#246).

// Names in the state folder.
const (
	StateDirName  = "state"
	SocketName    = "rpc.sock"
	PIDName       = "daemon.pid"
	LockName      = "daemon.lock"
	EventsLogName = "events.log"
)

// StateDir is the state folder of a build folder.
func StateDir(buildDir string) string { return filepath.Join(buildDir, StateDirName) }

// Why a proxy could not use the daemon and served its session itself. They
// are identifiers: the handshake and the doctor report carry them.
const (
	ReasonUnsupported  = "unsupported"   // the host system has no daemon yet
	ReasonStateUnsafe  = "state_unsafe"  // the state folder is not the user's alone
	ReasonStartFailed  = "start_failed"  // the daemon could not be started
	ReasonUnreachable  = "unreachable"   // it started but its socket did not answer
	ReasonNotInstalled = "not_installed" // the binary is not under a server root
)

// DaemonError is a failure to reach or run the daemon. Reason is one of the
// Reason values; Detail uses this package's own words and the state folder,
// never what another program printed.
type DaemonError struct {
	Reason string
	Detail string
}

func (e *DaemonError) Error() string { return e.Detail }

func daemonError(reason, format string, args ...any) *DaemonError {
	return &DaemonError{Reason: reason, Detail: fmt.Sprintf(format, args...)}
}

// ReasonOf is the Reason of err, or ReasonStartFailed for any other error.
func ReasonOf(err error) string {
	if e, ok := errors.AsType[*DaemonError](err); ok {
		return e.Reason
	}
	return ReasonStartFailed
}

// ErrDaemonRunning means another daemon holds the lock of the state folder.
var ErrDaemonRunning = errors.New("a daemon already runs for this state folder")

// DaemonStopWait bounds how long a stop waits for a daemon to end after
// SIGTERM before it kills it.
const DaemonStopWait = 10 * time.Second

// The proxy introduces each connection with one line, before the protocol:
//
//	gbx-proxy 1 <client id> <0|1>\n
//
// The last field says whether this proxy started the daemon.
const (
	proxyHelloPrefix = "gbx-proxy 1 "
	maxProxyHello    = 128
)

// WriteProxyHello writes the line that introduces a proxy's connection.
func WriteProxyHello(w io.Writer, clientID string, started bool) error {
	if !clientIDRE.MatchString(clientID) {
		return errors.New("invalid client id")
	}
	flag := "0"
	if started {
		flag = "1"
	}
	_, err := io.WriteString(w, proxyHelloPrefix+clientID+" "+flag+"\n")
	return err
}

// ReadProxyHello reads that line, one byte at a time so that nothing of the
// protocol after it is consumed.
func ReadProxyHello(r io.Reader) (clientID string, started bool, err error) {
	var line []byte
	b := make([]byte, 1)
	for {
		if len(line) >= maxProxyHello {
			return "", false, errors.New("the proxy line is too long")
		}
		if _, err := io.ReadFull(r, b); err != nil {
			return "", false, err
		}
		if b[0] == '\n' {
			break
		}
		line = append(line, b[0])
	}
	rest, ok := strings.CutPrefix(string(line), proxyHelloPrefix)
	if !ok {
		return "", false, errors.New("not a proxy line")
	}
	id, flag, ok := strings.Cut(rest, " ")
	if !ok || !clientIDRE.MatchString(id) || (flag != "0" && flag != "1") {
		return "", false, errors.New("a malformed proxy line")
	}
	return id, flag == "1", nil
}
