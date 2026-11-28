package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The persistent server of #247: requests that outlive their connection,
// results kept per client, and the replay of missed events.

func persistentServer(t *testing.T, handlers map[string]Handler, journal *Journal) *Server {
	t.Helper()
	srv := newServer(handlers)
	srv.Persistent = &Persistent{Instance: "inst-1", Journal: journal, Replay: func(name string) bool { return strings.HasSuffix(name, ":done") }}
	return srv
}

// peerLink is one connection of peer to srv, served under the server's
// life ctx. close ends the connection the way a proxy does when its SSH
// session ends.
type peerLink struct {
	*link
	cut func()
}

func connectPeer(t *testing.T, ctx context.Context, srv *Server, peer Peer, opts ClientOptions) *peerLink {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	l := &link{t: t, Server: srv, toServer: c2sW, served: make(chan error, 1)}
	go func() {
		err := srv.ServeConn(ctx, c2sR, s2cW, peer)
		_ = s2cW.Close()
		_ = c2sR.Close()
		l.served <- err
	}()
	opts.Version, opts.Commit = testVersion, testCommit
	opts.OnEvent = func(name string, payload []json.RawMessage) {
		data, _ := json.Marshal(payload)
		l.mu.Lock()
		l.events = append(l.events, recordedEvent{name, string(data)})
		l.mu.Unlock()
	}
	client, err := Dial(s2cR, c2sW, opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	l.Client = client
	var once sync.Once
	cut := func() {
		once.Do(func() {
			_ = client.Close()
			<-l.served
		})
	}
	t.Cleanup(cut)
	return &peerLink{l, cut}
}

func TestPersistent_RequestOutlivesItsConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var runs atomic.Int32
	started, gate := make(chan struct{}, 1), make(chan struct{})
	srv := persistentServer(t, map[string]Handler{
		"Clone": Func(func(ctx context.Context) (int, error) {
			runs.Add(1)
			started <- struct{}{}
			select {
			case <-gate:
				return 42, nil
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}),
	}, nil)
	peer := Peer{ClientID: "client-a"}
	first := connectPeer(t, ctx, srv, peer, ClientOptions{})
	errs := make(chan error, 1)
	go func() { errs <- first.call("Clone", nil, nil, WithRequestID("r1")) }()
	<-started
	first.cut()
	wantCode(t, <-errs, CodeApplication, ErrCodeDisconnected)
	if got := srv.Activity(); got.Attached != 0 || got.InFlight != 1 {
		t.Fatalf("after the cut: %+v, want no connection and the clone still running", got)
	}

	second := connectPeer(t, ctx, srv, peer, ClientOptions{})
	result := make(chan int, 1)
	go func() {
		var n int
		if err := second.call("Clone", nil, &n, WithRequestID("r1")); err != nil {
			t.Errorf("the repeat after the reconnect = %v", err)
		}
		result <- n
	}()
	close(gate)
	if n := <-result; n != 42 || runs.Load() != 1 {
		t.Errorf("the repeat returned %d after %d runs, want the first run's 42 and one run", n, runs.Load())
	}
	if !srv.Wait(5 * time.Second) {
		t.Error("the clone did not end")
	}
}

// TestPersistent_ResultsAreKeptPerClient: two clients of one daemon pick
// the same request id. Each gets its own outcome, and a repeat replays only
// its own client's.
func TestPersistent_ResultsAreKeptPerClient(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var effects atomic.Int32
	srv := persistentServer(t, map[string]Handler{
		"Bump": Func(func() int { return int(effects.Add(1)) }),
	}, nil)
	a := connectPeer(t, ctx, srv, Peer{ClientID: "client-a"}, ClientOptions{})
	b := connectPeer(t, ctx, srv, Peer{ClientID: "client-b"}, ClientOptions{})
	var fromA, fromB, againA, againB int
	if err := a.call("Bump", nil, &fromA, WithRequestID("same")); err != nil {
		t.Fatal(err)
	}
	if err := b.call("Bump", nil, &fromB, WithRequestID("same")); err != nil {
		t.Fatal(err)
	}
	if err := a.call("Bump", nil, &againA, WithRequestID("same")); err != nil {
		t.Fatal(err)
	}
	if err := b.call("Bump", nil, &againB, WithRequestID("same")); err != nil {
		t.Fatal(err)
	}
	if fromA != 1 || fromB != 2 || againA != 1 || againB != 2 || effects.Load() != 2 {
		t.Errorf("a got %d then %d, b got %d then %d, after %d effects; want 1, 1, 2, 2 and two effects",
			fromA, againA, fromB, againB, effects.Load())
	}
}

func TestResultStore_KeepsFinishedResultsForTheirTTL(t *testing.T) {
	now := time.Unix(1000, 0)
	store := newResultStore(ResultTTL)
	store.now = func() time.Time { return now }
	entry, first := store.take("k")
	if !first {
		t.Fatal("a new key must be run")
	}
	now = now.Add(time.Hour)
	if _, first := store.take("k"); first {
		t.Fatal("a request still running was dropped for its age")
	}
	store.finish("k", entry, false)
	now = now.Add(ResultTTL - time.Second)
	if _, first := store.take("k"); first {
		t.Fatal("a result was dropped before its TTL")
	}
	now = now.Add(2 * time.Second)
	if _, first := store.take("k"); !first {
		t.Fatal("a result outlived its TTL")
	}
}

func TestResume_ReplaysTheMissedDoneEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	journal, err := NewJournal(MaxJournalEvents, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := persistentServer(t, nil, journal)
	peer := Peer{ClientID: "client-a", Started: true}
	first := connectPeer(t, ctx, srv, peer, ClientOptions{})
	if d := first.Client.Server().Daemon; d == nil || d.Instance != "inst-1" || d.Resumed {
		t.Fatalf("hello of the first connection: %+v, want the instance, not resumed", d)
	}
	srv.Emit("fetch:done", "seen")
	waitEvents(t, first.link, 1)
	point := first.Client.ResumePoint()
	first.cut()

	srv.Emit("fetch:done", "missed-1")
	srv.Emit("fetch:progress", "not journaled")
	srv.Emit("fetch:done", "missed-2")

	second := connectPeer(t, ctx, srv, Peer{ClientID: "client-a"}, ClientOptions{Resume: &point})
	if d := second.Client.Server().Daemon; d == nil || !d.Resumed {
		t.Errorf("hello after the reconnect: %+v, want resumed", d)
	}
	if got := second.Client.Resumed(); !got.Resumed || got.Replayed != 2 || got.Gap {
		t.Errorf("resume = %+v, want two replayed events and no gap", got)
	}
	srv.Emit("fetch:done", "live")
	got := waitEvents(t, second.link, 3)
	want := []string{`["missed-1"]`, `["missed-2"]`, `["live"]`}
	for i, e := range got {
		if e.Payload != want[i] {
			t.Errorf("event %d = %s, want %s", i, e.Payload, want[i])
		}
	}
}

// TestResume_EndDeliveryCountsWhatWasDelivered: after EndDelivery the
// client hands OnEvent nothing more, and its point counts exactly what
// OnEvent got, so the next connection replays the rest (#409).
func TestResume_EndDeliveryCountsWhatWasDelivered(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	journal, err := NewJournal(MaxJournalEvents, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := persistentServer(t, map[string]Handler{
		"Ping": Func(func(context.Context) (int, error) { return 1, nil }),
	}, journal)
	peer := Peer{ClientID: "client-a", Started: true}
	first := connectPeer(t, ctx, srv, peer, ClientOptions{})
	srv.Emit("fetch:done", "seen")
	waitEvents(t, first.link, 1)
	point := first.Client.EndDelivery()
	srv.Emit("fetch:done", "after the end")
	// The reply follows the event on the wire: once it is back, the
	// reader has passed the event.
	if err := first.call("Ping", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := first.Events(); len(got) != 1 {
		t.Errorf("OnEvent got %d events, want only the one before EndDelivery", len(got))
	}
	if again := first.Client.ResumePoint(); again != point {
		t.Errorf("the point moved after EndDelivery: %+v, then %+v", point, again)
	}
	first.cut()

	second := connectPeer(t, ctx, srv, peer, ClientOptions{Resume: &point})
	if got := waitEvents(t, second.link, 1); got[0].Payload != `["after the end"]` {
		t.Errorf("the next connection replayed %s, want the event held back", got[0].Payload)
	}
}

func TestResume_AnotherInstanceReplaysNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	journal, _ := NewJournal(MaxJournalEvents, "")
	srv := persistentServer(t, nil, journal)
	srv.Emit("fetch:done", "before")
	l := connectPeer(t, ctx, srv, Peer{ClientID: "c"}, ClientOptions{Resume: &ResumePoint{Instance: "old-instance", After: 0}})
	if got := l.Client.Resumed(); got.Resumed || got.Replayed != 0 {
		t.Errorf("resume from another instance = %+v, want nothing", got)
	}
	time.Sleep(50 * time.Millisecond)
	if n := len(l.Events()); n != 0 {
		t.Errorf("%d events replayed from another instance", n)
	}
}

func TestResume_ReportsAGap(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	journal, _ := NewJournal(2, "")
	srv := persistentServer(t, nil, journal)
	for i := range 4 {
		srv.Emit("fetch:done", i)
	}
	l := connectPeer(t, ctx, srv, Peer{ClientID: "c"}, ClientOptions{Resume: &ResumePoint{Instance: "inst-1", After: 0}})
	if got := l.Client.Resumed(); !got.Resumed || !got.Gap || got.Replayed != 2 {
		t.Errorf("resume past the ring = %+v, want the two kept events and a gap", got)
	}
}

// TestResume_LiveEventsWaitForTheReplay: events emitted while a client
// resumes reach it after the replay, in the order they were emitted.
func TestResume_LiveEventsWaitForTheReplay(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	journal, _ := NewJournal(MaxJournalEvents, "")
	srv := persistentServer(t, nil, journal)
	next := 0
	emit := func() { srv.Emit("fetch:done", next); next++ }
	for range 20 {
		emit()
	}
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			default:
				emit()
				time.Sleep(time.Millisecond)
			}
		}
	}()
	l := connectPeer(t, ctx, srv, Peer{ClientID: "c"}, ClientOptions{Resume: &ResumePoint{Instance: "inst-1", After: 0}})
	time.Sleep(50 * time.Millisecond)
	close(stop)
	<-stopped
	total := next
	got := waitEvents(t, l.link, total)
	for i, e := range got {
		if e.Payload != "["+itoa(i)+"]" {
			t.Fatalf("event %d is %s: the replay and the live events are out of order", i, e.Payload)
		}
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func waitEvents(t *testing.T, l *link, n int) []recordedEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got := l.Events(); len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d events, want %d", len(l.Events()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestJournal_File: events.log holds the journaled events as they went on
// the wire, mode 0600, and stays within twice the ring.
func TestJournal_File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.log")
	if err := os.WriteFile(path, []byte("left by an earlier run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, err := NewJournal(3, path)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	srv := persistentServer(t, nil, journal)
	for i := range 20 {
		srv.Emit("fetch:done", map[string]int{"n": i})
		srv.Emit("fetch:start", i)
	}
	// Windows has no POSIX modes, and a Windows host runs no daemon.
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("events.log: %v, %v; want mode 0600", info, err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []journalEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e journalEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("a line is not an entry: %q", sc.Text())
		}
		lines = append(lines, e)
	}
	if len(lines) == 0 || len(lines) > 6 {
		t.Fatalf("%d lines, want between 1 and twice the ring", len(lines))
	}
	last := lines[len(lines)-1]
	var ev eventParams
	if err := json.Unmarshal(last.Event, &ev); err != nil || last.Name != "fetch:done" || ev.Name != "fetch:done" || ev.Seq != last.Seq {
		t.Errorf("the last line %+v does not hold the event as sent", last)
	}
	if !strings.Contains(string(last.Event), `"n":19`) {
		t.Errorf("the last line is %s, want the newest event", last.Event)
	}
}

// TestPersistent_SlowClientIsDropped: a client that stops reading never
// stalls the events of a persistent server; its connection is closed.
func TestPersistent_SlowClientIsDropped(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	srv := persistentServer(t, nil, nil)
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	served := make(chan error, 1)
	go func() { served <- srv.ServeConn(ctx, c2sR, s2cW, Peer{ClientID: "slow"}) }()
	// The handshake, then nobody reads.
	if _, err := clientHandshake(c2sW, bufio.NewReader(s2cR), testVersion, testCommit); err != nil {
		t.Fatal(err)
	}
	emitted := make(chan struct{})
	go func() {
		for i := range MaxQueuedMessages + 50 {
			srv.Emit("fetch:done", i)
		}
		close(emitted)
	}()
	select {
	case <-emitted:
	case <-time.After(10 * time.Second):
		t.Fatal("Emit waited for a client that does not read")
	}
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("the slow connection was not closed")
	}
	_ = s2cR.Close()
	_ = c2sW.Close()
}

func TestPersistent_DetachLeavesNoGoroutines(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	journal, _ := NewJournal(MaxJournalEvents, "")
	srv := persistentServer(t, map[string]Handler{"Ping": Func(func() int { return 1 })}, journal)
	cycle := func() {
		l := connectPeer(t, ctx, srv, Peer{ClientID: "c"}, ClientOptions{Resume: &ResumePoint{Instance: "inst-1"}})
		if err := l.call("Ping", nil, nil, WithRequestID("p")); err != nil {
			t.Fatal(err)
		}
		srv.Emit("fetch:done", 1)
		l.cut()
	}
	cycle()
	base := runtime.NumGoroutine()
	for range 20 {
		cycle()
	}
	if n, ok := goroutinesSettle(base, 5*time.Second); !ok {
		t.Errorf("%d goroutines after 20 attach and detach cycles, %d before", n, base)
	}
}
