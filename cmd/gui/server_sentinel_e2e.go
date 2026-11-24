//go:build gitboxserver && e2e_sentinel

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The e2e sentinel exists only in the server that scripts/remote-it.sh
// builds for the sshd integration tests (#239); no production build carries
// the tag. Every start of the binary, --version included, appends one line
// to $HOME/.gitbox-e2e/ran.log, so a test can prove that a step never
// executed the file.
func init() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".gitbox-e2e")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "ran.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%d %s\n", os.Getpid(), strings.Join(os.Args[1:], " "))
}

// The sshd tests set the daemon's fleet interval to a minute (#247); a
// production server takes only the intervals a window offers.
func init() {
	testFleetInterval = func(v string) (time.Duration, bool) {
		d, err := time.ParseDuration(v)
		return d, err == nil && d >= 10*time.Second
	}
}
