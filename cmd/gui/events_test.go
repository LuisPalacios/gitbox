package main

import "sync"

type recordedEvent struct {
	name string
	args []any
}

// recordingSink keeps every emitted event in order, for tests.
type recordingSink struct {
	mu      sync.Mutex
	events  []recordedEvent
	changed chan struct{}
}

func (e *recordingSink) Emit(name string, args ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, recordedEvent{name, args})
	if e.changed != nil {
		close(e.changed)
		e.changed = nil
	}
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
