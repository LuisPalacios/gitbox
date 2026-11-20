//go:build gitboxserver

package main

import (
	"fmt"
	"os"
)

// main is the entry point of gitbox-server, the gitboxserver build of this
// package: the same backend code as GitboxApp, with no Wails and no window.
// It is a placeholder until remote mode's server lands (#237); it exists so
// the server build links and vets from the day the Wails imports were
// confined to !gitboxserver files (#235).
func main() {
	fmt.Fprintln(os.Stderr, "gitbox-server: not implemented yet")
	os.Exit(2)
}
