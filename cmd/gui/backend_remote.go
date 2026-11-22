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
	sink func() EventSink
	log  io.Writer

	mu      sync.Mutex
	client  *rpc.Client // nil while not connected
	lastErr error
}

var _ Backend = (*RemoteBackend)(nil)

// newRemoteBackend returns a backend with no connection: every call fails
// with not_connected until connect succeeds. sink says where the server's
// events go.
func newRemoteBackend(sink func() EventSink, log io.Writer) *RemoteBackend {
	return &RemoteBackend{sink: sink, log: log}
}

// dialRemoteBackend runs the handshake with the server whose output is in
// and whose input is out, and returns its backend. The handshake fails
// unless the server is this same build.
func dialRemoteBackend(in io.Reader, out io.WriteCloser, sink func() EventSink, log io.Writer) (*RemoteBackend, error) {
	r := newRemoteBackend(sink, log)
	if err := r.connect(in, out); err != nil {
		return nil, err
	}
	return r, nil
}

// connect runs the handshake and attaches the connection.
func (r *RemoteBackend) connect(in io.Reader, out io.WriteCloser) error {
	client, err := rpc.Dial(in, out, rpc.ClientOptions{
		Version: version, Commit: commit,
		OnEvent: r.relayEvent,
		Log:     r.log,
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	old := r.client
	r.client = client
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// detach ends the connection, which is the server's signal to stop, and
// leaves the backend answering not_connected.
func (r *RemoteBackend) detach() {
	r.mu.Lock()
	c := r.client
	r.client = nil
	r.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

func (r *RemoteBackend) current() *rpc.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client
}

// The server loaded its own config and runs its own workers: there is
// nothing to start here.
func (r *RemoteBackend) Init(context.Context) {}
func (r *RemoteBackend) Start()               {}

// Close ends the connection.
func (r *RemoteBackend) Close() { r.detach() }

// Hello is what the server said about itself in the handshake, or the zero
// value while not connected.
func (r *RemoteBackend) Hello() rpc.Hello {
	if c := r.current(); c != nil {
		return c.Server()
	}
	return rpc.Hello{}
}

// ServerVersion is the server's build as it stated it in the handshake.
func (r *RemoteBackend) ServerVersion() string {
	hello := r.Hello()
	if hello.Version == "" && hello.Commit == "" {
		return ""
	}
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

// relayEvent hands a server event to the window. The name must be a REMOTE
// event this build emits itself; anything else is dropped, LOCAL events
// such as remote:state or update:quit included, which only this process
// may emit.
func (r *RemoteBackend) relayEvent(name string, payload []json.RawMessage) {
	if info, known := eventClass[name]; !known || info.Class != classRemote {
		r.logf("dropping event %q, which a server may not send", userText(name))
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
// Before the connection and after it ends, every call fails with
// not_connected; a method the server does not serve yet fails with
// not_implemented_remote.
func (r *RemoteBackend) invoke(method string, result any, args ...any) error {
	client := r.current()
	if client == nil {
		return &remoteError{remoteNotConnected, "not connected to the remote"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteCallTimeout(method))
	defer cancel()
	var opts []rpc.CallOption
	if methodClass[method].Mutating {
		opts = append(opts, rpc.WithRequestID(newRequestID()))
	}
	err := client.Call(ctx, method, args, result, opts...)
	if rpc.IsCode(err, rpc.ErrCodeUnsupported) {
		return &remoteError{remoteNotImplemented, method + " is not available in remote mode yet"}
	}
	return err
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
