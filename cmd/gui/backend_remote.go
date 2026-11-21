package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// RemoteBackend is the Backend of a window that shows another host: every
// method is a call to the gitbox-server on that host (#233). The methods
// themselves are in backend_remote_methods.go, one line each.
//
// Everything the server sends is untrusted. Results are decoded into the
// method's own types, errors arrive only as UserErrors and are scrubbed
// again on this side, and events with a name this build does not know are
// dropped.
type RemoteBackend struct {
	client *rpc.Client
	sink   func() EventSink
	log    io.Writer

	mu      sync.Mutex
	lastErr error
}

var _ Backend = (*RemoteBackend)(nil)

// dialRemoteBackend runs the handshake with the server whose output is in
// and whose input is out, and returns its backend. sink says where the
// server's events go. The handshake fails unless the server is this same
// build.
func dialRemoteBackend(in io.Reader, out io.WriteCloser, sink func() EventSink, log io.Writer) (*RemoteBackend, error) {
	r := &RemoteBackend{sink: sink, log: log}
	client, err := rpc.Dial(in, out, rpc.ClientOptions{
		Version: version, Commit: commit,
		OnEvent: r.relayEvent,
		Log:     log,
	})
	if err != nil {
		return nil, err
	}
	r.client = client
	return r, nil
}

// The server loaded its own config and runs its own workers: there is
// nothing to start here.
func (r *RemoteBackend) Init(context.Context) {}
func (r *RemoteBackend) Start()               {}

// Close ends the connection, which is the server's signal to stop.
func (r *RemoteBackend) Close() { _ = r.client.Close() }

// ServerVersion is the server's build as it stated it in the handshake.
func (r *RemoteBackend) ServerVersion() string {
	hello := r.client.Server()
	return formatAppVersion(userText(hello.Version), userText(hello.Commit))
}

// LastError is the failure of the most recent call that had no error to
// return it in, or nil.
func (r *RemoteBackend) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastErr
}

func (r *RemoteBackend) logf(format string, args ...any) {
	if r.log != nil {
		fmt.Fprintf(r.log, "remote backend: "+format+"\n", args...)
	}
}

// relayEvent hands a server event to the window. The name must be one this
// build emits itself; anything else is dropped.
func (r *RemoteBackend) relayEvent(name string, payload []json.RawMessage) {
	if _, known := eventClass[name]; !known {
		r.logf("dropping unknown event %q", userText(name))
		return
	}
	items := make([]any, 0, len(payload))
	for _, raw := range payload {
		var item any
		if err := json.Unmarshal(raw, &item); err != nil {
			r.logf("dropping event %s: its payload cannot be decoded", name)
			return
		}
		items = append(items, item)
	}
	r.sink().Emit(name, items...)
}

// invoke calls a method on the server. A method that changes state on the
// host carries a request id, so a call that is sent twice takes effect once.
func (r *RemoteBackend) invoke(method string, result any, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), remoteCallTimeout(method))
	defer cancel()
	var opts []rpc.CallOption
	if methodClass[method].Mutating {
		opts = append(opts, rpc.WithRequestID(newRequestID()))
	}
	return r.client.Call(ctx, method, args, result, opts...)
}

// remoteCallTimeout is how long a call may take: the bound of the work it
// asks for, plus a margin for the round trip.
func remoteCallTimeout(method string) time.Duration {
	if d, ok := remoteWorkBounds[method]; ok {
		return d + remoteCallMargin
	}
	return remoteDefaultBound + remoteCallMargin
}

// newRequestID returns a random version 4 UUID.
func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// The four shapes a Backend method can have. The ones with no error to
// return keep the failure in LastError and answer with the zero value.

func (r *RemoteBackend) callVoid(method string, args ...any) {
	r.note(method, r.invoke(method, nil, args...))
}

func (r *RemoteBackend) callErr(method string, args ...any) error {
	return r.invoke(method, nil, args...)
}

func remoteValue[T any](r *RemoteBackend, method string, args ...any) T {
	var out T
	if err := r.invoke(method, &out, args...); err != nil {
		r.note(method, err)
		var zero T
		return zero
	}
	r.note(method, nil)
	return out
}

func remoteCall[T any](r *RemoteBackend, method string, args ...any) (T, error) {
	var out T
	if err := r.invoke(method, &out, args...); err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

func (r *RemoteBackend) note(method string, err error) {
	r.mu.Lock()
	r.lastErr = err
	r.mu.Unlock()
	if err != nil {
		r.logf("%s failed: %s", method, userMsg(err))
	}
}
