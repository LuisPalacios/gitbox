//go:build linux || darwin

package remote

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// DaemonSupported reports whether this system runs the daemon.
const DaemonSupported = true

// PrepareStateDir creates the state folder, mode 0700, and checks that it
// is a real folder of this user. A folder of this user with a wider mode is
// narrowed to 0700; anything else is refused with ReasonStateUnsafe.
func PrepareStateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return daemonError(ReasonStateUnsafe, "the state folder %s cannot be created", dir)
	}
	return checkStateDir(dir, true)
}

// CheckStateDir checks the state folder as PrepareStateDir does, without
// creating or changing it. A missing folder is not an error.
func CheckStateDir(dir string) error {
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return checkStateDir(dir, false)
}

func checkStateDir(dir string, fix bool) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return daemonError(ReasonStateUnsafe, "the state folder %s cannot be read", dir)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 || !ok || int(st.Uid) != os.Getuid() {
		return daemonError(ReasonStateUnsafe, "the state folder %s is not a folder of this user", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if !fix {
			return daemonError(ReasonStateUnsafe, "the state folder %s is open to other users", dir)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return daemonError(ReasonStateUnsafe, "the state folder %s cannot be narrowed to mode 0700", dir)
		}
	}
	return nil
}

// DaemonLock is the lock a running daemon holds on its state folder.
type DaemonLock struct {
	f   *os.File
	dir string
}

// LockDaemon takes the lock of the state folder and writes daemon.pid. It
// fails with ErrDaemonRunning when another daemon holds it. The kernel
// releases the lock when the process ends, however it ends.
func LockDaemon(dir string) (*DaemonLock, error) {
	f, err := os.OpenFile(filepath.Join(dir, LockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, daemonError(ReasonStartFailed, "the daemon lock in %s cannot be opened", dir)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrDaemonRunning
		}
		return nil, daemonError(ReasonStartFailed, "the daemon lock in %s cannot be taken", dir)
	}
	pid := filepath.Join(dir, PIDName)
	tmp := pid + ".new"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil || os.Rename(tmp, pid) != nil {
		_ = os.Remove(tmp)
		_ = f.Close()
		return nil, daemonError(ReasonStartFailed, "daemon.pid cannot be written in %s", dir)
	}
	return &DaemonLock{f: f, dir: dir}, nil
}

// Release removes daemon.pid and gives the lock up.
func (l *DaemonLock) Release() {
	_ = os.Remove(filepath.Join(l.dir, PIDName))
	_ = l.f.Close()
}

// DaemonAlive reports whether a daemon holds the lock of the state folder,
// and its process id when daemon.pid names one.
func DaemonAlive(dir string) (pid int, alive bool) {
	f, err := os.Open(filepath.Join(dir, LockName))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0, false
	}
	data, err := os.ReadFile(filepath.Join(dir, PIDName))
	if err != nil {
		return 0, true
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || n <= 1 {
		return 0, true
	}
	return n, true
}

// StopDaemon stops the daemon of the state folder: SIGTERM, then SIGKILL
// when it still holds its lock after wait. It reports whether a daemon was
// running. The pid is trusted only while the lock is held, so a reused pid
// is never signalled.
func StopDaemon(dir string, wait time.Duration) (bool, error) {
	pid, alive := DaemonAlive(dir)
	if !alive {
		return false, nil
	}
	if pid == 0 {
		return true, daemonError(ReasonStartFailed, "the daemon of %s names no process", dir)
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if waitUnlocked(dir, wait) {
		return true, nil
	}
	if _, still := DaemonAlive(dir); still {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if waitUnlocked(dir, 2*time.Second) {
		return true, nil
	}
	return true, daemonError(ReasonStartFailed, "the daemon of %s did not stop", dir)
}

func waitUnlocked(dir string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if _, alive := DaemonAlive(dir); !alive {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ListenSocket listens on the Unix socket at path, mode 0600. The caller
// holds the daemon lock, so a socket left there by a daemon that died is
// removed first. A path longer than the system allows (104 bytes on macOS,
// 108 on Linux) must be given relative to the current folder, which only
// the entry point of a process may change.
func ListenSocket(path string) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, daemonError(ReasonStartFailed, "an old socket cannot be removed")
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, daemonError(ReasonStartFailed, "the daemon socket cannot be opened")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		return nil, daemonError(ReasonStartFailed, "the daemon socket cannot be restricted to this user")
	}
	return l, nil
}

// DialSocket connects to the Unix socket at path, relative like
// ListenSocket's when it is long.
func DialSocket(path string, timeout time.Duration) (net.Conn, error) {
	c, err := net.DialTimeout("unix", path, timeout)
	if err != nil {
		return nil, daemonError(ReasonUnreachable, "the daemon socket does not answer")
	}
	return c, nil
}

// PeerUID is the user id of the process at the other end of a Unix socket
// connection.
func PeerUID(c net.Conn) (int, error) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return -1, errors.New("not a Unix socket connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return -1, err
	}
	uid := -1
	var serr error
	if err := raw.Control(func(fd uintptr) { uid, serr = peerUID(int(fd)) }); err != nil {
		return -1, err
	}
	return uid, serr
}

// SpawnDetached starts exe with args in a session of its own, in dir, with
// no terminal and stdio on the null device, so it outlives the SSH session
// that started it. The returned channel gets its exit status; nothing needs
// to read it.
func SpawnDetached(exe string, args []string, dir string) (<-chan error, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	git.HideWindow(cmd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, daemonError(ReasonStartFailed, "the daemon could not be started")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	return done, nil
}
