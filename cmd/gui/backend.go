package main

import "context"

// Backend is the work half of the app: config, git, credentials and
// providers, on the host that has the data. The frontend never knows which
// implementation it talks to. localBackend runs the work in this process;
// remote mode (#233) adds an implementation that calls a gitbox-server over
// SSH. Client-only behaviour (window, updater, dialogs, launching local
// apps) stays outside, in the local shell (shell.go).
type Backend interface {
	// Init loads the host's config. Start launches background workers.
	// Close stops them. None is Wails-bound.
	Init(ctx context.Context)
	Start()
	Close()
}

// localBackend serves Backend from this process. It owns no state: every
// method calls the original body, which stays on *App.
type localBackend struct{ a *App }

func (b localBackend) Init(ctx context.Context) { b.a.initLocal(ctx) }
func (b localBackend) Start()                   { b.a.startLocal() }
func (b localBackend) Close()                   { b.a.closeLocal() }

// be returns the installed Backend, or the local one when none is set, so
// an App built as a bare struct (tests) keeps working.
func (a *App) be() Backend {
	if a.backend == nil {
		return localBackend{a}
	}
	return a.backend
}
