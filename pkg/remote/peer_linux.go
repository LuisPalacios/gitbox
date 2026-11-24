package remote

import "golang.org/x/sys/unix"

// peerUID reads the peer's credentials with SO_PEERCRED.
func peerUID(fd int) (int, error) {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return -1, err
	}
	return int(cred.Uid), nil
}
