package main

// EventSink is the one path every backend and shell event takes to the
// frontend. The GUI installs wailsSink in Startup; gitbox-server installs
// its RPC sink; tests install a recordingSink (events_test.go). Every event name passed to
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
