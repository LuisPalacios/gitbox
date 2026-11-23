package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"sync"
	"time"
)

// Times are monotonic milliseconds since creation of this window's App.
type startupTiming struct {
	mu              sync.Mutex
	start           time.Time
	marks           map[string]int64
	print, reported bool
}

func (t *startupTiming) mark(stage string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.start.IsZero() {
		return
	}
	if t.marks == nil {
		t.marks = map[string]int64{}
	}
	if stage == "finished" {
		if t.print && !t.reported {
			data, _ := json.Marshal(t.marks)
			fmt.Printf("startup-timing %s\n", data)
			t.reported = true
		}
		return
	}
	if _, ok := t.marks[stage]; !ok {
		t.marks[stage] = time.Since(t.start).Milliseconds()
	}
}

// MarkStartup records frontend milestones in the same clock as the config load.
func (a *App) MarkStartup(stage string) {
	switch stage {
	case "mount", "firstRow", "firstStatus", "firstRemote", "fullStatus", "finished":
		a.timing.mark(stage)
	}
}

// GetStartupTiming returns observed milestones; an absent mark has not occurred.
func (a *App) GetStartupTiming() map[string]int64 {
	a.timing.mu.Lock()
	defer a.timing.mu.Unlock()
	return maps.Clone(a.timing.marks)
}
