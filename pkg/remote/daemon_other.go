//go:build !linux && !darwin

package remote

import (
	"net"
	"time"
)

// A host other than Linux and macOS has no daemon yet: Windows hosts
// follow #246. Every call fails with ReasonUnsupported, and a proxy serves
// its session itself.

// DaemonSupported reports whether this system runs the daemon.
const DaemonSupported = false

func unsupported() error {
	return daemonError(ReasonUnsupported, "this system runs no gitbox-server daemon")
}

// PrepareStateDir fails: there is no daemon here.
func PrepareStateDir(string) error { return unsupported() }

// CheckStateDir has nothing to check.
func CheckStateDir(string) error { return nil }

// DaemonLock is never taken here.
type DaemonLock struct{}

// LockDaemon fails: there is no daemon here.
func LockDaemon(string) (*DaemonLock, error) { return nil, unsupported() }

// Release does nothing.
func (*DaemonLock) Release() {}

// DaemonAlive finds no daemon.
func DaemonAlive(string) (int, bool) { return 0, false }

// StopDaemon finds no daemon to stop.
func StopDaemon(string, time.Duration) (bool, error) { return false, nil }

// ListenSocket fails: there is no daemon here.
func ListenSocket(string) (net.Listener, error) { return nil, unsupported() }

// DialSocket fails: there is no daemon here.
func DialSocket(string, time.Duration) (net.Conn, error) { return nil, unsupported() }

// PeerUID fails: there is no daemon here.
func PeerUID(net.Conn) (int, error) { return -1, unsupported() }

// SpawnDetached fails: there is no daemon here.
func SpawnDetached(string, []string, string) (<-chan error, error) { return nil, unsupported() }
