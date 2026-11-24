package rpc

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// outbox is the server's outgoing queue. It is bounded, and it treats what
// it holds unequally:
//
//   - Responses and ordinary events are never dropped. When the queue is
//     full their sender waits.
//   - Progress events (names that end in ":progress") are worth only their
//     latest value. At most one per operation goes out every
//     ProgressInterval, and when the queue is full they are the first to go.
type outbox struct {
	mu     sync.Mutex
	space  *sync.Cond // signalled when an item leaves or the box closes
	items  *sync.Cond // signalled when an item arrives or the box closes
	queue  []outItem
	closed bool

	// Per operation: when its last progress event was queued, and the one
	// waiting for its turn.
	lastProgress map[string]time.Time
	pending      map[string]outItem
	timers       map[string]*time.Timer
	dropped      int
}

const eventOperationPrefix = "operationID="

type outItem struct {
	body     []byte
	progress bool
	name     string // event name, for progress items
}

func newOutbox() *outbox {
	o := &outbox{
		lastProgress: make(map[string]time.Time),
		pending:      make(map[string]outItem),
		timers:       make(map[string]*time.Timer),
	}
	o.space = sync.NewCond(&o.mu)
	o.items = sync.NewCond(&o.mu)
	return o
}

// put queues an item that must not be lost, waiting for room if needed. It
// reports false when the box is closed.
func (o *outbox) put(body []byte) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.putLocked(outItem{body: body}, true)
}

// putLocked queues item. When the queue is full and holds no progress to
// drop, it waits for room, or with wait unset reports false at once.
func (o *outbox) putLocked(item outItem, wait bool) bool {
	for len(o.queue) >= MaxQueuedMessages && !o.closed {
		if o.evictProgressLocked() {
			break
		}
		if !wait {
			return false
		}
		o.space.Wait()
	}
	if o.closed {
		return false
	}
	o.queue = append(o.queue, item)
	o.items.Signal()
	return true
}

// evictProgressLocked removes the oldest queued progress item, if any.
func (o *outbox) evictProgressLocked() bool {
	for i, it := range o.queue {
		if it.progress {
			o.queue = append(o.queue[:i], o.queue[i+1:]...)
			o.dropped++
			return true
		}
	}
	return false
}

// event queues an event. key identifies the operation it belongs to. An
// event that must not be lost waits for room in a full queue; with wait
// unset event reports false instead, and the event is not queued.
func (o *outbox) event(name, key string, body []byte, wait bool) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return true
	}
	if !strings.HasSuffix(name, ":progress") {
		// An event that closes an operation must come after the last
		// progress of that operation: send what was waiting first.
		if strings.HasPrefix(key, eventOperationPrefix) {
			if strings.HasSuffix(name, ":done") {
				o.flushKeyLocked(key)
				delete(o.lastProgress, key)
			}
		} else {
			o.flushPrefixLocked(eventPrefix(name))
		}
		return o.putLocked(outItem{body: body}, wait) || o.closed
	}
	item := outItem{body: body, progress: true, name: name}
	since := time.Since(o.lastProgress[key])
	if _, waiting := o.pending[key]; !waiting && since >= ProgressInterval {
		o.sendProgressLocked(key, item)
		return true
	}
	// Too soon after the last one: keep only the latest and send it when
	// the interval is over.
	if _, waiting := o.pending[key]; waiting {
		o.dropped++
	}
	o.pending[key] = item
	if o.timers[key] == nil {
		o.timers[key] = time.AfterFunc(ProgressInterval-since, func() { o.flush(key) })
	}
	return true
}

// sendProgressLocked queues a progress item, unless the queue is full: a
// progress event never makes anything wait.
func (o *outbox) sendProgressLocked(key string, item outItem) {
	o.lastProgress[key] = time.Now()
	if len(o.queue) >= MaxQueuedMessages {
		o.dropped++
		return
	}
	o.queue = append(o.queue, item)
	o.items.Signal()
}

// flush sends the progress event waiting for key.
func (o *outbox) flush(key string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.flushKeyLocked(key)
}

func (o *outbox) flushKeyLocked(key string) {
	item, ok := o.pending[key]
	if t := o.timers[key]; t != nil {
		t.Stop()
		delete(o.timers, key)
	}
	if !ok || o.closed {
		return
	}
	delete(o.pending, key)
	o.sendProgressLocked(key, item)
}

// flushPrefixLocked sends every waiting progress event of the operations
// whose event names start with prefix.
func (o *outbox) flushPrefixLocked(prefix string) {
	for key, item := range o.pending {
		if eventPrefix(item.name) == prefix {
			o.flushKeyLocked(key)
		}
	}
}

// eventPrefix is the part of an event name before its last colon:
// "clone:progress" and "clone:done" share "clone".
func eventPrefix(name string) string {
	if i := strings.LastIndex(name, ":"); i >= 0 {
		return name[:i]
	}
	return name
}

// take waits for the next item. It reports false when the box is closed and
// empty.
func (o *outbox) take() ([]byte, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for len(o.queue) == 0 && !o.closed {
		o.items.Wait()
	}
	if len(o.queue) == 0 {
		return nil, false
	}
	item := o.queue[0]
	o.queue = o.queue[1:]
	o.space.Signal()
	return item.body, true
}

// close stops the box: senders stop waiting, and take drains what is left.
func (o *outbox) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	for key, t := range o.timers {
		t.Stop()
		delete(o.timers, key)
	}
	o.space.Broadcast()
	o.items.Broadcast()
}

// operationKey lets progress and completion share one operation ID. Other
// callers of the RPC package can still key progress by their entity fields.
func operationKey(name string, payload []any) string {
	key := name
	if len(payload) == 0 {
		return key
	}
	data, err := json.Marshal(payload[0])
	if err != nil {
		return key
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return key
	}
	if id := fields["operationID"]; len(id) != 0 {
		return eventOperationPrefix + string(id) + "|" + eventPrefix(name)
	}
	for _, f := range [...]string{"source", "repo", "accountKey", "account", "mirror", "mirrorKey", "key"} {
		if v, ok := fields[f]; ok {
			key += "|" + f + "=" + string(v)
		}
	}
	return key
}
