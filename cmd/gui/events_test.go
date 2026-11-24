package main

import "sync"

func (e Envelope[T]) eventParts() (EventMetadata, any) {
	return e.EventMetadata, e.Payload
}

type recordedEvent struct {
	name string
	args []any
	meta EventMetadata
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
	event := recordedEvent{name: name, args: args}
	if len(args) == 1 {
		if envelope, ok := args[0].(interface{ eventParts() (EventMetadata, any) }); ok {
			meta, payload := envelope.eventParts()
			event.meta, event.args = meta, nil
			if payload != nil {
				event.args = []any{payload}
			}
		}
	}
	e.events = append(e.events, event)
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
