//go:build !gitboxserver

package main

import (
	"context"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// wailsSink forwards events to the Wails runtime of one window.
type wailsSink struct{ ctx context.Context }

func (s wailsSink) Emit(name string, payload ...any) {
	wailsrt.EventsEmit(s.ctx, name, payload...)
}
