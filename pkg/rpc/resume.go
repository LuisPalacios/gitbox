package rpc

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"sync"
	"time"
)

// A persistent server outlives its connections (#247): gitbox-server run
// keeps one Server for its whole life, and each SSH session reaches it
// through a proxy. Three things make that work:
//
//   - The work a request starts keeps running when its connection ends.
//     Only $/cancelRequest, or the end of the server, cancels it.
//   - The outcome of a remembered request is kept for ResultTTL after it
//     finished, keyed by the client, the method and the request id, so a
//     client that reconnects and repeats the request gets that outcome
//     instead of a second effect, and never another client's.
//   - Every event carries a sequence number, and the events that end an
//     operation go into a Journal. A client that reconnects calls
//     $/resume with the number of the last event it saw and gets the ones
//     it missed.

// Persistent turns a Server into one that outlives its connections.
type Persistent struct {
	// Instance names this run of the server. A client replays only from
	// the instance it was connected to: a restarted server has new
	// sequence numbers and no memory of the old ones.
	Instance string
	// Journal keeps the events a reconnecting client may have missed. Nil
	// keeps none.
	Journal *Journal
	// Replay reports whether an event goes into the journal. Nil keeps
	// none.
	Replay func(name string) bool
}

// Peer is what the server knows about the other end of one connection.
type Peer struct {
	// ClientID identifies the client process, as the proxy received it.
	// Remembered requests are kept per client: two clients that pick the
	// same request id never see each other's outcomes.
	ClientID string
	// Started is set when the proxy of this connection had to start the
	// server, so it was not already running.
	Started bool
}

// DaemonHello is what a persistent server adds to its Hello.
type DaemonHello struct {
	Instance string `json:"instance"`
	// Resumed is set when the server was already running before this
	// connection came.
	Resumed bool `json:"resumed"`
	// LastSeq is the sequence number of the last event the server sent
	// before this connection.
	LastSeq uint64 `json:"lastSeq"`
}

// ResumePoint is where a reconnecting client left: the instance it was
// connected to and the last event it received.
type ResumePoint struct {
	Instance string
	After    uint64
}

// ResumeOutcome says what a resume on Dial did.
type ResumeOutcome struct {
	// Resumed is set when the server is the instance of the resume point.
	Resumed bool
	// Replayed is how many missed events were delivered.
	Replayed int
	// Gap is set when some missed events were no longer kept, so the
	// client must refresh what they would have told it.
	Gap bool
}

// resumeParams and resumeResult are the $/resume request and answer.
type resumeParams struct {
	Instance string `json:"instance"`
	After    uint64 `json:"after"`
}

type resumeResult struct {
	Resumed bool              `json:"resumed"`
	Events  []json.RawMessage `json:"events"`
	Gap     bool              `json:"gap"`
}

// eventParams is the params member of an event message.
type eventParams struct {
	Name    string `json:"name"`
	Payload []any  `json:"payload"`
	Seq     uint64 `json:"seq"`
}

// maxReplayBytes bounds the events one $/resume answer carries, well
// under MaxFrameBytes. Older events beyond it count as a gap.
const maxReplayBytes = MaxFrameBytes / 2

// Journal is a bounded ring of events, newest last, mirrored to a file so
// that what a server kept while nobody was attached can be read on the host.
// The file holds the events exactly as they went on the wire: what the
// server's handlers already prepared for a client, never more.
type Journal struct {
	mu       sync.Mutex
	capacity int
	entries  []journalEntry
	// evicted is the sequence number of the newest event dropped from the
	// ring: a resume after an earlier one has a gap.
	evicted uint64

	path  string
	f     *os.File
	w     *bufio.Writer
	lines int
}

type journalEntry struct {
	Seq   uint64          `json:"seq"`
	Name  string          `json:"name"`
	Event json.RawMessage `json:"event"`
}

// NewJournal returns a journal of at most capacity events. A path that is
// not empty gets a fresh file, mode 0600, with one JSON line per event; the
// file is rewritten to the ring when it grows to twice the capacity.
func NewJournal(capacity int, path string) (*Journal, error) {
	j := &Journal{capacity: max(capacity, 1), path: path}
	if path != "" {
		if err := j.open(path); err != nil {
			return nil, err
		}
	}
	return j, nil
}

// open starts the file at path afresh.
func (j *Journal) open(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	j.f, j.w = f, bufio.NewWriter(f)
	return nil
}

// add keeps one event.
func (j *Journal) add(seq uint64, name string, event json.RawMessage) {
	j.mu.Lock()
	defer j.mu.Unlock()
	e := journalEntry{Seq: seq, Name: name, Event: event}
	j.entries = append(j.entries, e)
	if over := len(j.entries) - j.capacity; over > 0 {
		j.evicted = j.entries[over-1].Seq
		j.entries = slices.Delete(j.entries, 0, over)
	}
	if j.w == nil {
		return
	}
	if j.lines >= 2*j.capacity {
		j.compactLocked()
		return
	}
	j.writeLocked(e)
	_ = j.w.Flush()
}

func (j *Journal) writeLocked(e journalEntry) {
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	_, _ = j.w.Write(append(line, '\n'))
	j.lines++
}

// compactLocked rewrites the file to hold the ring and nothing else.
func (j *Journal) compactLocked() {
	_ = j.f.Close()
	j.f, j.w, j.lines = nil, nil, 0
	tmp := j.path + ".new"
	if j.open(tmp) != nil {
		return
	}
	for _, e := range j.entries {
		j.writeLocked(e)
	}
	_ = j.w.Flush()
	if os.Rename(tmp, j.path) != nil {
		_ = j.f.Close()
		_ = os.Remove(tmp)
		j.f, j.w = nil, nil
	}
}

// since returns the events after seq after and before seq before, oldest
// first, within budget bytes. gap reports that some of them are gone.
func (j *Journal) since(after, before uint64, budget int) (events []json.RawMessage, gap bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	gap = after < j.evicted
	used := 0
	for _, e := range slices.Backward(j.entries) {
		if e.Seq <= after {
			break
		}
		if e.Seq >= before {
			continue
		}
		if used+len(e.Event) > budget {
			gap = true
			break
		}
		used += len(e.Event)
		events = append(events, e.Event)
	}
	slices.Reverse(events)
	return events, gap
}

// Close closes the file. The ring stays readable.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.f == nil {
		return nil
	}
	_ = j.w.Flush()
	err := j.f.Close()
	j.f, j.w = nil, nil
	return err
}

// resultStore keeps the outcomes of remembered requests: at most
// MaxRememberedRequests, and with a ttl, none longer than ttl after it
// finished. A request still running is never dropped for its age.
type resultStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]*remembered
	order   []string // oldest first
}

func newResultStore(ttl time.Duration) *resultStore {
	return &resultStore{ttl: ttl, now: time.Now, entries: make(map[string]*remembered)}
}

// resultKey is the key of a remembered request: who asked, what, and with
// which id.
func resultKey(clientID, method, requestID string) string {
	return clientID + "\x00" + method + "\x00" + requestID
}

// take returns the entry of key and false when it exists, or a new entry,
// stored under key, and true when the caller must run the request.
func (r *resultStore) take(key string) (*remembered, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked()
	if prior := r.entries[key]; prior != nil {
		return prior, false
	}
	entry := &remembered{done: make(chan struct{})}
	r.entries[key] = entry
	r.order = append(r.order, key)
	if len(r.order) > MaxRememberedRequests {
		delete(r.entries, r.order[0])
		r.order = r.order[1:]
	}
	return entry, true
}

// finish records when entry ended; forget removes it instead, so a repeat
// runs again.
func (r *resultStore) finish(key string, entry *remembered, forget bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.finished = r.now()
	if forget && r.entries[key] == entry {
		delete(r.entries, key)
		if i := slices.Index(r.order, key); i >= 0 {
			r.order = slices.Delete(r.order, i, i+1)
		}
	}
}

func (r *resultStore) expireLocked() {
	if r.ttl <= 0 {
		return
	}
	now := r.now()
	r.order = slices.DeleteFunc(r.order, func(key string) bool {
		e := r.entries[key]
		if e != nil && !e.finished.IsZero() && now.Sub(e.finished) > r.ttl {
			delete(r.entries, key)
			return true
		}
		return false
	})
}
