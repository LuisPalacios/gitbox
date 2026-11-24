// sockprobe is what TestSocketPermissions runs as a second user of the
// container (#247): it connects to the daemon's socket, introduces itself
// as a proxy and reports in one word what happened.
//
//	denied    the socket's folder, or the socket, cannot be reached by this user
//
//	refused   nothing listens there, or another error, which follows
//	closed    the daemon closed the connection without a word
//	answered  the daemon spoke
//	silent    the daemon said nothing and kept the connection
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"time"
)

func main() {
	// Like the proxy, it enters the socket's folder and dials the name
	// alone: the full path of a tagged build passes the 108 bytes a socket
	// name allows.
	if err := os.Chdir(filepath.Dir(os.Args[1])); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			fmt.Println("denied")
		} else {
			fmt.Println("refused", err)
		}
		return
	}
	c, err := net.DialTimeout("unix", filepath.Base(os.Args[1]), 3*time.Second)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			fmt.Println("denied")
		} else {
			fmt.Println("refused", err)
		}
		return
	}
	defer c.Close()
	_, _ = fmt.Fprint(c, "gbx-proxy 1 intruder 0\n")
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := c.Read(make([]byte, 16))
	switch {
	case n > 0:
		fmt.Println("answered")
	case errors.Is(err, os.ErrDeadlineExceeded):
		fmt.Println("silent")
	default:
		fmt.Println("closed")
	}
}
