package rpc

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestEvents_ReachTheClientInOrder(t *testing.T) {
	gate := make(chan struct{})
	srv := newServer(nil)
	srv.Handlers = map[string]Handler{
		"Run": Func(func() string {
			srv.Emit("fetch:start", map[string]string{"source": "s", "repo": "r"})
			srv.Emit("status:updated", []int{1, 2, 3}, "second arg")
			srv.Emit("fetch:alldone")
			<-gate
			return "done"
		}),
	}
	srv.Emit("too:early") // before Serve: dropped
	l := connect(t, srv)

	result := make(chan error, 1)
	go func() { result <- l.call("Run", nil, nil) }()
	deadline := time.Now().Add(5 * time.Second)
	for len(l.Events()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	close(gate)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	want := []recordedEvent{
		{"fetch:start", `[{"repo":"r","source":"s"}]`},
		{"status:updated", `[[1,2,3],"second arg"]`},
		{"fetch:alldone", `[]`},
	}
	got := l.Events()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// Progress is worth its latest value: one event per operation per interval,
// and whatever is waiting goes out before the event that ends the operation.
func TestEvents_ProgressIsCoalesced(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv := newServer(nil)
		l := connect(t, srv)
		progress := func(repo string, pct int) {
			srv.Emit("clone:progress", map[string]any{"source": "s", "repo": repo, "percent": pct})
		}
		percents := func(repo string) (out []string) {
			for _, e := range l.Events() {
				if e.Name == "clone:progress" && strings.Contains(e.Payload, `"repo":"`+repo+`"`) {
					out = append(out, e.Payload[strings.Index(e.Payload, `"percent":`)+10:strings.Index(e.Payload, `,"repo"`)])
				}
			}
			return out
		}

		// A burst for one clone: the first goes out, the rest collapse
		// into the latest, which goes out when the interval is over.
		for pct := 1; pct <= 50; pct++ {
			progress("a", pct)
		}
		progress("b", 7) // another operation is not held back
		synctest.Wait()
		if got := percents("a"); fmt.Sprint(got) != "[1]" {
			t.Errorf("right after the burst clone a sent %v, want only the first", got)
		}
		if got := percents("b"); fmt.Sprint(got) != "[7]" {
			t.Errorf("clone b sent %v, want its own first event", got)
		}
		time.Sleep(ProgressInterval)
		synctest.Wait()
		if got := percents("a"); fmt.Sprint(got) != "[1 50]" {
			t.Errorf("after the interval clone a sent %v, want the first and the latest", got)
		}

		// The end of the operation flushes what was waiting, in order.
		progress("a", 60)
		progress("a", 99)
		srv.Emit("clone:done", map[string]any{"source": "s", "repo": "a"})
		synctest.Wait()
		events := l.Events()
		last, before := events[len(events)-1], events[len(events)-2]
		if last.Name != "clone:done" || before.Name != "clone:progress" || !strings.Contains(before.Payload, `"percent":99`) {
			t.Errorf("the operation ended with %v then %v; want the latest progress and then done", before, last)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if got := len(l.Events()); got != len(events) {
			t.Errorf("%d more events arrived after done", got-len(events))
		}
		_ = l.Client.Close()
		<-l.served
	})
}

// The queue is bounded. Progress never makes anything wait and is the first
// to go; responses and ordinary events are never dropped.
func TestOutbox_BoundsAndPriorities(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		box := newOutbox()
		defer box.close()

		// Fill it with one progress event per operation and the rest
		// ordinary items.
		for i := range MaxQueuedMessages {
			if i%4 == 0 {
				box.event("clone:progress", fmt.Sprintf("op-%d", i), []byte("progress"))
			} else {
				box.put([]byte("kept"))
			}
		}
		if len(box.queue) != MaxQueuedMessages {
			t.Fatalf("queue holds %d, want it full at %d", len(box.queue), MaxQueuedMessages)
		}

		// Progress into a full queue is dropped, not queued and not waited for.
		box.event("clone:progress", "op-new", []byte("progress"))
		if len(box.queue) != MaxQueuedMessages || box.dropped != 1 {
			t.Errorf("queue %d, dropped %d after progress into a full queue", len(box.queue), box.dropped)
		}

		// An ordinary item into a full queue takes the place of a progress item.
		progressBefore := countProgress(box)
		box.put([]byte("kept"))
		if len(box.queue) != MaxQueuedMessages || countProgress(box) != progressBefore-1 {
			t.Errorf("an ordinary item must evict one progress item: queue %d, progress %d -> %d", len(box.queue), progressBefore, countProgress(box))
		}

		// With no progress left to evict, the sender waits for room.
		for countProgress(box) > 0 {
			box.put([]byte("kept"))
		}
		waited := make(chan bool, 1)
		go func() { waited <- box.put([]byte("kept")) }()
		synctest.Wait()
		select {
		case <-waited:
			t.Fatal("a sender got through a full queue with nothing to evict")
		default:
		}
		if _, ok := box.take(); !ok {
			t.Fatal("take failed")
		}
		synctest.Wait()
		if ok := <-waited; !ok || len(box.queue) != MaxQueuedMessages {
			t.Errorf("after one item left, the waiting sender must get in: ok %v, queue %d", ok, len(box.queue))
		}
	})
}

func countProgress(o *outbox) (n int) {
	for _, it := range o.queue {
		if it.progress {
			n++
		}
	}
	return n
}

func TestOperationKey(t *testing.T) {
	a := operationKey("clone:progress", []any{map[string]any{"source": "s", "repo": "a", "percent": 10}})
	a2 := operationKey("clone:progress", []any{map[string]any{"source": "s", "repo": "a", "percent": 90}})
	b := operationKey("clone:progress", []any{map[string]any{"source": "s", "repo": "b", "percent": 10}})
	other := operationKey("move:progress", []any{map[string]any{"source": "s", "repo": "a"}})
	if a != a2 {
		t.Errorf("two progress events of one clone must share a key: %q, %q", a, a2)
	}
	if a == b || a == other {
		t.Errorf("different operations must not share a key: %q, %q, %q", a, b, other)
	}
	if operationKey("x:progress", nil) != "x:progress" || operationKey("x:progress", []any{42}) != "x:progress" {
		t.Error("a payload that names no operation falls back to the event name")
	}
}

// An event the server emits while the handshake is still in flight is not
// lost: it goes out after the verdict. A client takes the connection as made
// the moment it reads the verdict, and whatever the server emits from then
// on must reach it.
func TestProgressEnvelopeClosesOnlyItsOperation(t *testing.T) {
	box := newOutbox()
	defer box.close()
	key := func(name, id string) string {
		return operationKey(name, []any{map[string]string{"operationID": id}})
	}
	first, second := key("clone:progress", "1"), key("clone:progress", "2")
	// Queue both pending updates by seeding their previous progress at the
	// current time. Completion should flush the first without affecting the second.
	box.lastProgress[first], box.lastProgress[second] = time.Now(), time.Now()
	box.pending[first] = outItem{progress: true, name: "clone:progress", body: []byte("first progress")}
	box.pending[second] = outItem{progress: true, name: "clone:progress", body: []byte("second progress")}
	box.event("clone:done", key("clone:done", "1"), []byte("first done"))
	if len(box.queue) != 2 || string(box.queue[0].body) != "first progress" || string(box.queue[1].body) != "first done" {
		t.Fatalf("first operation closed out of order: %v", box.queue)
	}
	if _, kept := box.lastProgress[first]; kept {
		t.Fatal("closed operation kept its throttling timestamp")
	}
	if _, queued := box.pending[second]; !queued {
		t.Fatal("closing the first operation flushed progress of the second")
	}
}

func TestEvents_EmittedDuringTheHandshakeArrive(t *testing.T) {
	srv := newServer(nil)
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	served := make(chan error, 1)
	go func() {
		served <- srv.Serve(context.Background(), c2sR, s2cW)
		_ = s2cW.Close()
	}()
	reader := bufio.NewReader(s2cR)

	// The client's half by hand, stopping after the server's greeting.
	if err := skipToMagic(reader); err != nil {
		t.Fatal(err)
	}
	var hello Hello
	if err := readJSONFrame(reader, &hello); err != nil {
		t.Fatal(err)
	}
	srv.Emit("early:done", "kept")

	if err := writeJSONFrame(c2sW, clientHello{Version: testVersion, Commit: testCommit}); err != nil {
		t.Fatal(err)
	}
	var v verdict
	if err := readJSONFrame(reader, &v); err != nil || !v.OK {
		t.Fatalf("verdict %+v, %v", v, err)
	}
	type frame struct {
		body []byte
		err  error
	}
	next := make(chan frame, 1)
	go func() {
		body, err := ReadFrame(reader)
		next <- frame{body, err}
	}()
	select {
	case f := <-next:
		if f.err != nil || !strings.Contains(string(f.body), `"early:done"`) || !strings.Contains(string(f.body), `"kept"`) {
			t.Errorf("the first frame after the verdict is %q, %v; want the event", f.body, f.err)
		}
	case <-time.After(5 * time.Second):
		t.Error("the event emitted during the handshake never arrived")
	}
	_ = c2sW.Close()
	if err := <-served; err != nil {
		t.Errorf("Serve = %v", err)
	}

	// A handshake that fails releases an emitter that was waiting for room
	// in the queue, and later events are dropped without waiting.
	in2R, in2W := io.Pipe()
	out2R, out2W := io.Pipe()
	go func() { served <- srv.Serve(context.Background(), in2R, out2W) }()
	reader2 := bufio.NewReader(out2R)
	if err := skipToMagic(reader2); err != nil {
		t.Fatal(err)
	}
	if err := readJSONFrame(reader2, &hello); err != nil {
		t.Fatal(err)
	}
	emitted := make(chan struct{})
	go func() {
		for range 2 * MaxQueuedMessages {
			srv.Emit("late:done")
		}
		close(emitted)
	}()
	select {
	case <-emitted:
		t.Fatal("the queue took twice its bound without waiting: the test proves nothing")
	case <-time.After(200 * time.Millisecond):
	}
	_ = in2W.Close()
	if err := <-served; err == nil {
		t.Fatal("a handshake the client abandons must fail")
	}
	select {
	case <-emitted:
	case <-time.After(5 * time.Second):
		t.Error("an emitter stayed blocked after the handshake failed")
	}
}
