package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/usererr"
)

const (
	testVersion = "v9.9.9"
	testCommit  = "0123456789abcdef0123456789abcdef01234567"
)

// plainUserError is the conversion the tests give the server: the error's
// text as the detail. Real servers scrub; these tests check the protocol.
func plainUserError(err error) usererr.UserError {
	return usererr.UserError{Code: "test.failed", Hint: "error.test.failed", Detail: err.Error()}
}

// link is a client and a server joined by two in-memory pipes, the way
// GitboxApp and gitbox-server are joined by the server's stdin and stdout.
type link struct {
	t      *testing.T
	Client *Client
	Server *Server
	// toServer is what the client writes; closing it is the server's EOF.
	toServer *io.PipeWriter
	served   chan error

	mu     sync.Mutex
	events []recordedEvent
	dead   error
}

type recordedEvent struct {
	Name    string
	Payload string // the payload array, as JSON
}

func newServer(handlers map[string]Handler) *Server {
	return &Server{
		Handlers:    handlers,
		ToUserError: plainUserError,
		Self:        Hello{Version: testVersion, Commit: testCommit, OS: "testos", Arch: "testarch", PID: 4242},
	}
}

// connect serves srv and dials it. The link is closed when the test ends.
func connect(t *testing.T, srv *Server) *link {
	t.Helper()
	l, err := tryConnect(t, srv, ClientOptions{Version: testVersion, Commit: testCommit})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return l
}

func tryConnect(t *testing.T, srv *Server, opts ClientOptions) (*link, error) {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	l := &link{t: t, Server: srv, toServer: c2sW, served: make(chan error, 1)}
	go func() {
		err := srv.Serve(context.Background(), c2sR, s2cW)
		_ = s2cW.Close() // a process that exits closes its output
		_ = c2sR.Close()
		l.served <- err
	}()
	opts.OnEvent = func(name string, payload []json.RawMessage) {
		data, _ := json.Marshal(payload)
		l.mu.Lock()
		l.events = append(l.events, recordedEvent{name, string(data)})
		l.mu.Unlock()
	}
	opts.OnDead = func(err error) {
		l.mu.Lock()
		l.dead = err
		l.mu.Unlock()
	}
	client, err := Dial(s2cR, c2sW, opts)
	if err != nil {
		_ = c2sW.Close()
		_ = s2cR.Close()
		<-l.served
		return nil, err
	}
	l.Client = client
	t.Cleanup(func() { _ = client.Close() })
	return l, nil
}

func (l *link) Events() []recordedEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]recordedEvent(nil), l.events...)
}

// call is Call with a generous deadline, for tests that expect an answer.
func (l *link) call(method string, params []any, result any, opts ...CallOption) error {
	l.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return l.Client.Call(ctx, method, params, result, opts...)
}

func wantCode(t *testing.T, err error, rpcCode int, userCode string) {
	t.Helper()
	e, ok := errors.AsType[*Error](err)
	if !ok || e.Code != rpcCode || e.Data.Code != userCode {
		t.Errorf("err = %#v, want rpc code %d with user code %q", err, rpcCode, userCode)
	}
}

// goroutinesSettle waits until the number of goroutines is back to base.
func goroutinesSettle(base int, within time.Duration) (int, bool) {
	deadline := time.Now().Add(within)
	for {
		n := runtime.NumGoroutine()
		if n <= base {
			return n, true
		}
		if time.Now().After(deadline) {
			return n, false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
