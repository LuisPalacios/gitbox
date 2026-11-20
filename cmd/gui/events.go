package main

import "sync"

// EventSink is the one path every backend and shell event takes to the
// frontend. The GUI installs wailsSink in Startup; gitbox-server installs
// its RPC sink; tests install a recordingSink. Every event name passed to
// Emit must be a string literal registered in classification.go.
type EventSink interface {
	Emit(name string, payload ...any)
}

// nopSink drops events. It is the default for an App built without a
// runtime (tests, early startup).
type nopSink struct{}

func (nopSink) Emit(string, ...any) {}

// sink returns the installed EventSink, or nopSink when none is set.
func (a *App) sink() EventSink {
	if a.events == nil {
		return nopSink{}
	}
	return a.events
}

type recordedEvent struct {
	name string
	args []any
}

// recordingSink keeps every emitted event in order, for tests.
type recordingSink struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (e *recordingSink) Emit(name string, args ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, recordedEvent{name, args})
}

func (e *recordingSink) named(name string) []recordedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	var found []recordedEvent
	for _, event := range e.events {
		if event.name == name {
			found = append(found, event)
		}
	}
	return found
}
