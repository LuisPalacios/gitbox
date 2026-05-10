//go:build darwin

package main

import (
	"os"
	"os/exec"
)

// macOS lacks both PR_SET_PDEATHSIG and Job Objects, so we don't have an
// in-kernel "die when parent dies" mechanism. The parent-side Kill in
// app.go's Shutdown handler is the lifecycle guarantor here, and per
// issue #69 testing it works reliably on macOS — Wails fires Shutdown
// when the user X's the main window and the explicit Process.Kill on
// the tracked subprocess takes effect before the parent exits.

func linkChildLifetimeToParent(_ *os.Process) {}
func configureChildLifetime(_ *exec.Cmd)      {}
