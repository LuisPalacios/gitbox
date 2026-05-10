//go:build linux

package main

// watchParent is a no-op on Linux — the Manager subprocess already gets
// a kernel-guaranteed SIGKILL via PR_SET_PDEATHSIG (set in
// process_lifecycle_linux.go before the spawn). The watcher would just
// add a polling loop on top of an already-instant mechanism.
func watchParent(_ int) {}
