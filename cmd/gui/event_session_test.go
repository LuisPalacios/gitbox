package main

import (
	"context"
	"encoding/json"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// statusEvent is a valid host envelope of status:updated with sequence seq.
func statusEvent(t *testing.T, seq uint64, session string) []json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(Envelope[[]StatusResult]{
		EventMetadata: EventMetadata{Schema: eventSchema, OperationID: "1", Sequence: seq, Session: session},
		Payload:       []StatusResult{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return []json.RawMessage{raw}
}

func backendGen(r *RemoteBackend) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gen
}

// waitNamed waits until sink holds n events called name.
func waitNamed(t *testing.T, sink *recordingSink, name string, n int) []recordedEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(sink.named(name)) < n && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := sink.named(name)
	if len(got) < n {
		t.Fatalf("%d %s events reached the window, want %d", len(got), name, n)
	}
	return got
}

// serveHost starts another server on the config at path and returns the
// ends a RemoteBackend connects with.
func serveHost(t *testing.T, path string) (*App, io.Reader, io.WriteCloser) {
	t.Helper()
	host := newServerApp(path)
	srv := newRPCServer(host, io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	host.be().Init(ctx)
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx, c2sR, s2cW)
		host.be().Close()
		_ = s2cW.Close()
		_ = c2sR.Close()
	}()
	t.Cleanup(func() {
		cancel()
		_ = c2sW.Close()
		<-done
	})
	return host, s2cR, c2sW
}

// An event that a connection still had queued when a detach or a new
// connection superseded it never reaches the window (#357).
func TestRelayDropsSupersededConnectionEvents(t *testing.T) {
	link := serveRemote(t, contractConfig(t), nil)
	r := link.Remote
	gen := backendGen(r)

	r.relayFrom(gen, "status:updated", statusEvent(t, 1, ""))
	if got := len(link.Events.named("status:updated")); got != 1 {
		t.Fatalf("the live connection's event: %d delivered, want 1", got)
	}

	r.detach()
	r.relayFrom(gen, "status:updated", statusEvent(t, 2, ""))
	if got := link.Events.named("status:updated"); len(got) != 1 {
		t.Errorf("an event of the detached connection reached the window: %+v", got[1:])
	}
}

// A connect and a detach end while the host sends events without pause, so
// one is nearly always being relayed (#409). The client holds its delivery
// lock while the relay hands an event to the window, and the relay takes
// relayMu, then mu: a connect or detach that waited for that lock while it
// held either one froze the window for good.
func TestRelayConnectAndDetachWhileEventsArrive(t *testing.T) {
	event := statusEvent(t, 1, "")[0]
	for range 50 {
		srv := &rpc.Server{
			ToUserError: toUserError,
			Self:        rpc.Hello{Version: version, Commit: commit, OS: "linux", Arch: "amd64", PID: 1},
			Log:         io.Discard,
			Persistent:  &rpc.Persistent{Instance: "daemon-1"},
		}
		ctx, cancel := context.WithCancel(context.Background())
		stop := make(chan struct{})
		go func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				srv.Emit("status:updated", event)
				runtime.Gosched()
			}
		}()
		c2sR, c2sW := io.Pipe()
		s2cR, s2cW := io.Pipe()
		go func() {
			_ = srv.Serve(ctx, c2sR, s2cW)
			_ = s2cW.Close()
			_ = c2sR.Close()
		}()
		sink := &recordingSink{}
		done := make(chan error, 1)
		go func() {
			r, err := dialRemoteBackend(s2cR, c2sW, func() EventSink { return sink }, io.Discard)
			if err == nil {
				for len(sink.named("status:updated")) == 0 {
					time.Sleep(time.Millisecond)
				}
				r.detach()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("connect: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("connect or detach never returned while events arrived")
		}
		close(stop)
		cancel()
	}
}

// Reconnecting the same backend delivers the new connection's events and
// none of the connection it replaced (#357).
func TestRelayReconnectDeliversNewConnection(t *testing.T) {
	link := serveRemote(t, contractConfig(t), nil)
	r := link.Remote
	old := backendGen(r)

	host, in, out := serveHost(t, contractConfig(t))
	if err := r.connect(in, out); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	r.relayFrom(old, "status:updated", statusEvent(t, 1, ""))
	emit(link.Host.newOperation(), "status:updated", []StatusResult{})
	emit(host.newOperation(), "status:one", StatusResult{Source: "acct", Repo: "repo"})

	waitNamed(t, link.Events, "status:one", 1)
	if got := link.Events.named("status:updated"); len(got) != 0 {
		t.Errorf("events of the replaced connection reached the window: %+v", got)
	}
}

// The relay stamps its own session on every event, whatever the host put
// in it; a stream stamps its session, and the default one is not on the
// wire (#357).
func TestEventsCarryTheirSession(t *testing.T) {
	link := serveRemote(t, contractConfig(t), nil)
	r := link.Remote
	r.session = "remote:r1"

	// A host that tags its events with another session.
	link.Host.eventStream.mu.Lock()
	link.Host.eventStream.session = "remote:forged"
	link.Host.eventStream.mu.Unlock()
	emit(link.Host.newOperation(), "status:updated", []StatusResult{})
	got := waitNamed(t, link.Events, "status:updated", 1)
	if s := got[0].meta.Session; s != "remote:r1" {
		t.Errorf("relayed host event session = %q, want remote:r1", s)
	}

	// An event the relay makes on this side, such as goLong's done.
	raw, _ := json.Marshal(cloneDoneDTO{Source: "acct", Repo: "repo", Error: "failed"})
	r.relayPayload("clone:done", Envelope[json.RawMessage]{EventMetadata: EventMetadata{Client: true, Session: "remote:forged"}, Payload: raw})
	done := waitNamed(t, link.Events, "clone:done", 1)
	if m := done[0].meta; m.Session != "remote:r1" || !m.Client {
		t.Errorf("relayed client event = %+v, want client and session remote:r1", m)
	}

	// A local stream stamps its own session; the default is omitted.
	sink := &recordingSink{}
	a := new(App)
	a.events = sink
	a.eventStream.session = "local"
	emit(a.newOperation(), "status:updated", []StatusResult{})
	a.eventStream.session = ""
	emit(a.newOperation(), "status:updated", []StatusResult{})
	local := sink.named("status:updated")
	if len(local) != 2 || local[0].meta.Session != "local" || local[1].meta.Session != "" {
		t.Fatalf("local sessions = %+v, want local then the default", local)
	}
	wire, err := json.Marshal(local[1].meta)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "session") {
		t.Errorf("the default session is on the wire: %s", wire)
	}
}
