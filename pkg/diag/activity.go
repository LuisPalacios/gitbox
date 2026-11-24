package diag

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

type OperationCode string

const (
	Status    OperationCode = "status"
	Fetch     OperationCode = "fetch"
	FetchAll  OperationCode = "fetch-all"
	Clone     OperationCode = "clone"
	Pull      OperationCode = "pull"
	RepoProbe OperationCode = "repo-exists"
)

type Operation struct {
	Code       OperationCode `json:"code"`
	AgeMS      int64         `json:"ageMs"`
	DurationMS int64         `json:"durationMs"`
	QueueDepth *int          `json:"queueDepth,omitempty"`
	Failed     bool          `json:"failed"`
	ErrorCode  string        `json:"errorCode,omitempty"`
	RepoID     string        `json:"repoId,omitempty"`
}

const (
	activityWindow = time.Hour
	maxOperations  = 1000
)

type observation struct {
	Operation
	finished time.Time
}

// Activity retains the most recent 1000 operation completions in memory.
// It never stores paths, raw errors or command output.
type Activity struct {
	mu   sync.Mutex
	list []observation
	next int
}

func (a *Activity) Record(code OperationCode, started, finished time.Time, queueDepth *int, path string, err error) {
	switch code {
	case Status, Fetch, FetchAll, Clone, Pull, RepoProbe:
	default:
		return
	}
	entry := observation{
		finished: finished,
		Operation: Operation{
			Code:       code,
			DurationMS: max(0, finished.Sub(started).Milliseconds()),
			Failed:     err != nil,
			ErrorCode:  errorCode(err),
		},
	}
	if queueDepth != nil {
		depth := max(0, *queueDepth)
		entry.QueueDepth = &depth
	}
	if entry.ErrorCode != "" && path != "" {
		digest := sha256.Sum256([]byte(path))
		entry.RepoID = hex.EncodeToString(digest[:8])
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.list) < maxOperations {
		a.list = append(a.list, entry)
		return
	}
	a.list[a.next] = entry
	a.next = (a.next + 1) % maxOperations
}

func (a *Activity) recent(now time.Time) []Operation {
	out := make([]Operation, 0)
	if a == nil {
		return out
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.list {
		entry := a.list[(a.next+i)%len(a.list)]
		age := now.Sub(entry.finished)
		if age < 0 || age > activityWindow {
			continue
		}
		op := entry.Operation
		op.AgeMS = age.Milliseconds()
		if op.QueueDepth != nil {
			depth := *op.QueueDepth
			op.QueueDepth = &depth
		}
		out = append(out, op)
	}
	return out
}

func errorCode(err error) string {
	var ge *git.GitError
	if errors.As(err, &ge) && ge != nil {
		switch ge.Code {
		case git.CodeAuthFailed, git.CodeNotFastForward, git.CodeNoUpstream, git.CodeOffline,
			git.CodeTimeout, git.CodeCancelled, git.CodeNotARepo, git.CodeLockHeld,
			git.CodeRepoNotFound, git.CodeUnsafeConfig, git.CodeUnknown:
			return "git." + string(ge.Code)
		default:
			return "git.unknown"
		}
	}
	var pe *provider.ProviderError
	if errors.As(err, &pe) && pe != nil {
		switch pe.Class {
		case provider.ClassUnauthorized, provider.ClassScopeDenied, provider.ClassForbidden,
			provider.ClassRateLimited, provider.ClassNotFound, provider.ClassServerError,
			provider.ClassOffline, provider.ClassTimeout, provider.ClassUnknown:
			return "provider." + string(pe.Class)
		default:
			return "provider.unknown"
		}
	}
	return ""
}
