package main

import (
	"context"
	"strconv"
	"sync"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

const eventSchema = 1

type EventMetadata struct {
	Schema         int    `json:"schema"`
	ConfigRevision uint64 `json:"configRevision"`
	OperationID    string `json:"operationID"`
	Sequence       uint64 `json:"sequence"`
	Client         bool   `json:"client,omitempty"`
}

type Envelope[T any] struct {
	EventMetadata
	Payload T `json:"payload"`
}

// EventSink carries events to Wails, to the RPC server, or to a test.
type EventSink interface {
	Emit(name string, payload ...any)
}

type nopSink struct{}

func (nopSink) Emit(string, ...any) {}

type eventStream struct {
	mu       sync.Mutex
	revision uint64
	sequence uint64
	next     uint64
}

type repoEventDTO struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
}

type eventOperation struct {
	app      *App
	id       string
	revision uint64
	client   bool
}

func (a *App) newOperation() eventOperation {
	stream := &a.eventStream
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.next++
	return eventOperation{app: a, id: strconv.FormatUint(stream.next, 10), revision: stream.revision}
}

func (a *App) transport() EventSink {
	if a.events == nil {
		return nopSink{}
	}
	return a.events
}

func (op eventOperation) atRevision(revision uint64) eventOperation {
	op.revision = revision
	return op
}

func (op eventOperation) currentRevision() eventOperation {
	stream := &op.app.eventStream
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return op.atRevision(stream.revision)
}

func emit[T any](op eventOperation, name string, payload T) {
	stream := &op.app.eventStream
	stream.mu.Lock()
	defer stream.mu.Unlock()

	stream.sequence++
	op.app.transport().Emit(name, Envelope[T]{
		EventMetadata: EventMetadata{
			Schema: eventSchema, ConfigRevision: op.revision, OperationID: op.id,
			Sequence: stream.sequence, Client: op.client || eventClass[name].Class == classLocal,
		},
		Payload: payload,
	})
}

// currentRepoOperation advances the terminal event to the current revision
// when unrelated config edits left this operation's repository intact.
func (a *App) currentRepoOperation(op eventOperation, cfg *config.Config, source, repo string) eventOperation {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sameRefreshRepoLocked(cfg, source, repo) {
		return op.atRevision(a.configRevision)
	}
	return op
}

// publishCompletionStatus reports the working tree after the operation. The
// operation and the check never hold the config lock while Git runs.
func (a *App) publishCompletionStatus(ctx context.Context, op eventOperation, cfg *config.Config, source, repo, path string) {
	if !a.sameRefreshRepo(cfg, source, repo) {
		return
	}
	probe, cancel := context.WithTimeout(ctx, healReadTimeout)
	defer cancel()
	result := status.CheckContext(probe, path)
	result.Source, result.Repo, result.Account = source, repo, cfg.Sources[source].Account
	result.ErrorMsg = userText(result.ErrorMsg)
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil || !a.sameRefreshRepoLocked(cfg, source, repo) {
		return
	}
	// A fleet sweep that overlaps the mutation may contain pre-operation
	// status. Force its next pass to read afresh and expire stale cache.
	if a.statusRun != nil {
		a.statusRun.dirty = true
	}
	a.cachedStatus, a.cachedStatusConfig = nil, nil
	emit(op.atRevision(a.configRevision), "status:one", toStatusResult(result))
}
