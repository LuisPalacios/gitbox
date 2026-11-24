package remote

import "golang.org/x/sys/unix"

// peerUID reads the peer's credentials with LOCAL_PEERCRED, which is what
// getpeereid uses.
func peerUID(fd int) (int, error) {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return -1, err
	}
	return int(cred.Uid), nil
}
