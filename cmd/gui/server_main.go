//go:build gitboxserver

package main

import "os"

// main is the entry point of gitbox-server, the gitboxserver build of this
// package: the same backend code as GitboxApp, with no Wails and no window.
// The server itself is in server.go.
func main() {
	os.Exit(serverMain(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
