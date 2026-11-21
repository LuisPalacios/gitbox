package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/usererr"
)

// rawPeer speaks to a server by hand, to send what a well-behaved client
// never would.
type rawPeer struct {
	t      *testing.T
	in     *bufio.Reader
	out    *io.PipeWriter
	served chan error
}

func rawConnect(t *testing.T, srv *Server) *rawPeer {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	p := &rawPeer{t: t, in: bufio.NewReader(s2cR), out: c2sW, served: make(chan error, 1)}
	go func() {
		err := srv.Serve(context.Background(), c2sR, s2cW)
		_ = s2cW.Close()
		_ = c2sR.Close()
		p.served <- err
	}()
	if _, err := clientHandshake(c2sW, p.in, testVersion, testCommit); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	t.Cleanup(func() { _ = c2sW.Close() })
	return p
}

func (p *rawPeer) send(body string) {
	p.t.Helper()
	if err := WriteFrame(p.out, []byte(body)); err != nil {
		p.t.Fatalf("send: %v", err)
	}
}

func (p *rawPeer) recv() message {
	p.t.Helper()
	var m message
	if err := readJSONFrame(p.in, &m); err != nil {
		p.t.Fatalf("recv: %v", err)
	}
	return m
}

func TestServe_CallsMethodsByNameWithPositionalParams(t *testing.T) {
	type point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	var called atomic.Int32
	l := connect(t, newServer(map[string]Handler{
		"Add":     Func(func(a, b int) int { return a + b }),
		"Greet":   Func(func(name string, loud bool) (string, error) { return "hello " + name, nil }),
		"Move":    Func(func(p point, by []int) point { return point{p.X + by[0], p.Y + by[1]} }),
		"Touch":   Func(func() { called.Add(1) }),
		"Check":   Func(func(ok bool) error { return nil }),
		"WithCtx": Func(func(ctx context.Context, s string) string { return s + fmt.Sprint(ctx.Err() == nil) }),
		"Nothing": Func(func() []string { return nil }),
	}))

	var sum int
	if err := l.call("Add", []any{2, 40}, &sum); err != nil || sum != 42 {
		t.Errorf("Add = %d, %v", sum, err)
	}
	var greeting string
	if err := l.call("Greet", []any{"Ana", true}, &greeting); err != nil || greeting != "hello Ana" {
		t.Errorf("Greet = %q, %v", greeting, err)
	}
	var moved point
	if err := l.call("Move", []any{point{1, 2}, []int{10, 20}}, &moved); err != nil || moved != (point{11, 22}) {
		t.Errorf("Move = %+v, %v", moved, err)
	}
	if err := l.call("Touch", nil, nil); err != nil || called.Load() != 1 {
		t.Errorf("Touch: %v, called %d times", err, called.Load())
	}
	if err := l.call("Check", []any{true}, nil); err != nil {
		t.Errorf("Check: %v", err)
	}
	var withCtx string
	if err := l.call("WithCtx", []any{"alive="}, &withCtx); err != nil || withCtx != "alive=true" {
		t.Errorf("WithCtx = %q, %v", withCtx, err)
	}
	list := []string{"stale"}
	if err := l.call("Nothing", nil, &list); err != nil || list != nil {
		t.Errorf("Nothing = %v, %v; a nil result must decode as nil", list, err)
	}
}

func TestServe_ClosedListAndParameters(t *testing.T) {
	l := connect(t, newServer(map[string]Handler{
		"Add": Func(func(a, b int) int { return a + b }),
	}))
	wantCode(t, l.call("Missing", nil, nil), CodeMethodNotFound, ErrCodeUnsupported)
	wantCode(t, l.call("add", []any{1, 2}, nil), CodeMethodNotFound, ErrCodeUnsupported)
	wantCode(t, l.call("Add", []any{1}, nil), CodeInvalidParams, ErrCodeInvalidParams)
	wantCode(t, l.call("Add", []any{1, 2, 3}, nil), CodeInvalidParams, ErrCodeInvalidParams)
	wantCode(t, l.call("Add", []any{"one", 2}, nil), CodeInvalidParams, ErrCodeInvalidParams)
	var sum int
	if err := l.call("Add", []any{1, 2}, &sum); err != nil || sum != 3 {
		t.Errorf("the connection must survive bad requests: %d, %v", sum, err)
	}
}

func TestServe_MalformedRequests(t *testing.T) {
	p := rawConnect(t, newServer(map[string]Handler{"Add": Func(func(a, b int) int { return a + b })}))

	p.send(`[{"jsonrpc":"2.0","id":1,"method":"Add","params":[1,2]}]`)
	if m := p.recv(); m.Error == nil || m.Error.Code != CodeInvalidRequest || m.ID != nil {
		t.Errorf("a batch got %+v, want -32600 with no id", m)
	}
	p.send(`{"jsonrpc":"2.0","id":2,`)
	if m := p.recv(); m.Error == nil || m.Error.Code != CodeParseError {
		t.Errorf("broken JSON got %+v, want -32700", m)
	}
	p.send(`{"jsonrpc":"1.0","id":3,"method":"Add","params":[1,2]}`)
	if m := p.recv(); m.Error == nil || m.Error.Code != CodeInvalidRequest {
		t.Errorf("a wrong protocol version got %+v, want -32600", m)
	}
	p.send(`{"jsonrpc":"2.0","id":4,"method":"Add","params":{"a":1,"b":2}}`)
	if m := p.recv(); m.Error == nil || m.Error.Code != CodeInvalidParams {
		t.Errorf("named params got %+v, want -32602", m)
	}
	// Notifications the server does not know are ignored, and the
	// connection goes on.
	p.send(`{"jsonrpc":"2.0","method":"$/unknown","params":[]}`)
	p.send(`{"jsonrpc":"2.0","method":"Add","params":[1,2]}`)
	p.send(`{"jsonrpc":"2.0","id":5,"method":"Add","params":[20,22]}`)
	if m := p.recv(); m.ID == nil || *m.ID != 5 || string(m.Result) != "42" {
		t.Errorf("after the noise the server answered %+v", m)
	}
	p.send(`{"jsonrpc":"2.0","id":6,"method":"$/ping"}`)
	if m := p.recv(); m.ID == nil || *m.ID != 6 || m.Error != nil {
		t.Errorf("ping got %+v", m)
	}
}

// A frame that breaks the framing rules ends the connection.
func TestServe_MalformedFrameClosesTheConnection(t *testing.T) {
	var log bytes.Buffer
	srv := newServer(nil)
	srv.Log = &log
	p := rawConnect(t, srv)
	if _, err := p.out.Write([]byte("Content-Length: nope\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-p.served:
		if !errors.Is(err, ErrMalformedFrame) {
			t.Errorf("Serve = %v, want the framing error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after a malformed frame")
	}
	if !strings.Contains(log.String(), "closing the connection") {
		t.Errorf("the log does not say why: %q", log.String())
	}
}

// Errors cross only as what the server's conversion made of them: the text
// of the Go error never does.
func TestServe_ErrorsCrossAsUserErrors(t *testing.T) {
	const secret = "CANARY-TOKEN-237"
	srv := newServer(map[string]Handler{
		"Fail": Func(func() error { return errors.New("fetch https://user:" + secret + "@host/x.git failed") }),
		"Both": Func(func() (string, error) { return "partial " + secret, errors.New("boom " + secret) }),
	})
	srv.ToUserError = func(error) usererr.UserError {
		return usererr.UserError{Code: "git.offline", Hint: "error.git.offline", Detail: "git fetch: offline"}
	}
	p := rawConnect(t, srv)
	for id, method := range map[int]string{1: "Fail", 2: "Both"} {
		p.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q}`, id, method))
		m := p.recv()
		raw, _ := json.Marshal(m)
		if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "user:") {
			t.Errorf("%s leaked the error text or the partial result: %s", method, raw)
		}
		if m.Error == nil || m.Error.Code != CodeApplication || m.Error.Message != "error.git.offline" ||
			m.Error.Data == nil || m.Error.Data.Code != "git.offline" || m.Error.Data.Detail != "git fetch: offline" {
			t.Errorf("%s answered %s, want the converted error with its hint as message", method, raw)
		}
		if len(m.Result) != 0 {
			t.Errorf("%s sent a result next to its error: %s", method, m.Result)
		}
	}
}

func TestServe_PanicsBecomeInternalErrors(t *testing.T) {
	var log syncBuffer
	srv := newServer(map[string]Handler{
		"Boom": Func(func() string { panic("secret-in-panic") }),
		"Fine": Func(func() string { return "ok" }),
	})
	srv.Log = &log
	l := connect(t, srv)

	err := l.call("Boom", nil, nil)
	wantCode(t, err, CodeInternalError, ErrCodeInternal)
	if strings.Contains(fmt.Sprintf("%+v %v", err, err), "secret-in-panic") {
		t.Errorf("the panic value crossed the wire: %v", err)
	}
	var s string
	if err := l.call("Fine", nil, &s); err != nil || s != "ok" {
		t.Errorf("the connection must survive a panic: %q, %v", s, err)
	}
	if got := log.String(); !strings.Contains(got, "panic in Boom") || !strings.Contains(got, "secret-in-panic") {
		t.Errorf("the server log must hold the panic and its stack: %q", got)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A cancelled request is answered at once, whether or not its handler
// notices.
func TestServe_CancelRequest(t *testing.T) {
	stubborn := make(chan struct{})
	defer close(stubborn)
	var noticed atomic.Bool
	l := connect(t, newServer(map[string]Handler{
		"Wait": Func(func(ctx context.Context) error {
			<-ctx.Done()
			noticed.Store(true)
			return ctx.Err()
		}),
		"Stubborn": Func(func() string { <-stubborn; return "late" }),
		"Add":      Func(func(a, b int) int { return a + b }),
	}))

	for _, method := range []string{"Wait", "Stubborn"} {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- l.Client.Call(ctx, method, nil, nil) }()
		time.Sleep(50 * time.Millisecond) // let the request reach its handler
		started := time.Now()
		cancel()
		select {
		case err := <-done:
			wantCode(t, err, CodeCancelled, ErrCodeCancelled)
			if spent := time.Since(started); spent > time.Second {
				t.Errorf("%s: the cancellation took %v, want under a second", method, spent)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: no answer to the cancellation", method)
		}
	}
	if !noticed.Load() {
		t.Error("the handler that watches its context must see the cancellation")
	}
	var sum int
	if err := l.call("Add", []any{1, 1}, &sum); err != nil || sum != 2 {
		t.Errorf("the connection must go on after cancellations: %d, %v", sum, err)
	}
}

// The end of the input is the server's only shutdown signal: it cancels the
// handlers and Serve returns.
func TestServe_EndOfInputStopsTheServer(t *testing.T) {
	var cancelled atomic.Bool
	l := connect(t, newServer(map[string]Handler{
		"Wait": Func(func(ctx context.Context) error {
			<-ctx.Done()
			cancelled.Store(true)
			return ctx.Err()
		}),
	}))
	pending := make(chan error, 1)
	go func() { pending <- l.Client.Call(context.Background(), "Wait", nil, nil) }()
	time.Sleep(50 * time.Millisecond)

	started := time.Now()
	if err := l.toServer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-l.served:
		if err != nil {
			t.Errorf("Serve = %v, want a clean end", err)
		}
	case <-time.After(ShutdownGrace + 2*time.Second):
		t.Fatal("Serve did not return after the input ended")
	}
	if spent := time.Since(started); spent > 2*time.Second {
		t.Errorf("Serve took %v to return with a handler that obeys its context", spent)
	}
	if !cancelled.Load() {
		t.Error("the handler in flight was not cancelled")
	}
	select {
	case err := <-pending:
		if err == nil {
			t.Error("the pending call must fail when the connection ends")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the pending call never returned")
	}
}

// A handler that ignores the cancellation cannot hold the server open.
func TestServe_ShutdownDoesNotWaitForever(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		srv := newServer(map[string]Handler{"Stubborn": Func(func() { <-release })})
		c2sR, c2sW := io.Pipe()
		s2cR, s2cW := io.Pipe()
		served := make(chan error, 1)
		go func() { served <- srv.Serve(context.Background(), c2sR, s2cW) }()
		reader := bufio.NewReader(s2cR)
		if _, err := clientHandshake(c2sW, reader, testVersion, testCommit); err != nil {
			t.Fatal(err)
		}
		if err := WriteFrame(c2sW, []byte(`{"jsonrpc":"2.0","id":1,"method":"Stubborn"}`)); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()

		started := time.Now()
		_ = c2sW.Close()
		go func() { _, _ = io.Copy(io.Discard, reader) }() // the peer keeps reading
		if err := <-served; err != nil {
			t.Errorf("Serve = %v", err)
		}
		if spent := time.Since(started); spent != ShutdownGrace {
			t.Errorf("Serve returned after %v, want exactly the %v grace", spent, ShutdownGrace)
		}
		close(release)
		_ = s2cW.Close()
		synctest.Wait()
	})
}

// A repeated request id gets the stored outcome, not a second effect.
func TestServe_RepeatedMutationRunsOnce(t *testing.T) {
	var effects atomic.Int32
	gate := make(chan struct{})
	l := connect(t, newServer(map[string]Handler{
		"AddRoot": Func(func(path string) (int, error) {
			n := int(effects.Add(1))
			if path == "/fails" {
				return 0, errors.New("root refused")
			}
			return n, nil
		}),
		"Slow": Func(func() int { <-gate; return int(effects.Add(1)) }),
	}))

	var first, second, other int
	if err := l.call("AddRoot", []any{"/a"}, &first, WithRequestID("req-1")); err != nil {
		t.Fatal(err)
	}
	if err := l.call("AddRoot", []any{"/a"}, &second, WithRequestID("req-1")); err != nil {
		t.Fatal(err)
	}
	if first != 1 || second != 1 || effects.Load() != 1 {
		t.Errorf("results %d and %d after %d effects; the repeat must return the stored result", first, second, effects.Load())
	}
	if err := l.call("AddRoot", []any{"/a"}, &other, WithRequestID("req-2")); err != nil || other != 2 {
		t.Errorf("a new request id must run again: %d, %v", other, err)
	}
	// Without an id every call runs.
	if err := l.call("AddRoot", []any{"/a"}, &other); err != nil || other != 3 {
		t.Errorf("a call without an id must always run: %d, %v", other, err)
	}

	// A failure is remembered too.
	errA := l.call("AddRoot", []any{"/fails"}, nil, WithRequestID("req-fail"))
	errB := l.call("AddRoot", []any{"/fails"}, nil, WithRequestID("req-fail"))
	if errA == nil || errB == nil || errA.Error() != errB.Error() || effects.Load() != 4 {
		t.Errorf("errors %v and %v after %d effects; the failure must be replayed", errA, errB, effects.Load())
	}

	// A repeat that arrives while the first attempt runs waits for it.
	results := make(chan int, 2)
	for range 2 {
		go func() {
			var n int
			_ = l.call("Slow", nil, &n, WithRequestID("req-slow"))
			results <- n
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(gate)
	a, b := <-results, <-results
	if a != b || effects.Load() != 5 {
		t.Errorf("concurrent repeats got %d and %d after %d effects, want one effect and the same result", a, b, effects.Load())
	}
}

func TestServe_RemembersABoundedNumberOfRequests(t *testing.T) {
	var effects atomic.Int32
	l := connect(t, newServer(map[string]Handler{"Bump": Func(func() int { return int(effects.Add(1)) })}))
	for i := range MaxRememberedRequests + 1 {
		if err := l.call("Bump", nil, nil, WithRequestID(fmt.Sprintf("req-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	// The newest is remembered; the oldest was forgotten and runs again.
	before := effects.Load()
	_ = l.call("Bump", nil, nil, WithRequestID(fmt.Sprintf("req-%d", MaxRememberedRequests)))
	if effects.Load() != before {
		t.Error("the newest request id must still be remembered")
	}
	_ = l.call("Bump", nil, nil, WithRequestID("req-0"))
	if effects.Load() != before+1 {
		t.Error("the oldest request id must have been forgotten")
	}
}

func TestServe_ResultTooLargeIsAnError(t *testing.T) {
	l := connect(t, newServer(map[string]Handler{
		"Huge": Func(func() string { return strings.Repeat("x", MaxFrameBytes) }),
	}))
	wantCode(t, l.call("Huge", nil, nil), CodeInternalError, ErrCodeInternal)
}

func TestFunc_RejectsShapesItCannotServe(t *testing.T) {
	for name, fn := range map[string]any{
		"not a function":      42,
		"variadic":            func(...int) {},
		"three results":       func() (int, int, error) { return 0, 0, nil },
		"second is not error": func() (int, string) { return 0, "" },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Func accepted it")
				}
			}()
			Func(fn)
		})
	}
	// A nil params member counts as no parameters.
	h := Func(func() int { return 7 })
	for _, params := range []string{"", "null", "[]"} {
		if v, err := h(context.Background(), json.RawMessage(params)); err != nil || v != 7 {
			t.Errorf("params %q: %v, %v", params, v, err)
		}
	}
}
