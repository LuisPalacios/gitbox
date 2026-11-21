package rpc

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/usererr"
)

// ShutdownGrace is how long Serve waits, after the input ended, for the
// handlers still running to notice that their context was cancelled.
const ShutdownGrace = 5 * time.Second

// Server serves a closed list of methods over one connection.
//
// The peer is untrusted: a method that is not on the list does not exist,
// parameters are decoded into the handler's own types and nothing else, and
// every bound in limits.go applies.
type Server struct {
	// Handlers is the closed list. Build each with Func or by hand.
	Handlers map[string]Handler
	// ToUserError turns an error a handler returned into the only form that
	// may cross the wire. It must not let a secret through. Required.
	ToUserError func(error) usererr.UserError
	// Self is what the server says about itself in the handshake. Version
	// and Commit must match the client's exactly.
	Self Hello
	// Log receives one line per event worth a human's attention: a refused
	// handshake, a malformed frame, a panic. Nil discards them.
	Log io.Writer

	mu  sync.Mutex
	out *outbox // set while Serve runs
}

// Emit sends an event to the client. It has the shape of the app's event
// sink. Before Serve runs, and after it ends, events are dropped.
func (s *Server) Emit(name string, payload ...any) {
	out := s.outbox()
	if out == nil {
		return
	}
	if payload == nil {
		payload = []any{}
	}
	params, err := json.Marshal(struct {
		Name    string `json:"name"`
		Payload []any  `json:"payload"`
	}{name, payload})
	if err != nil {
		s.logf("dropping event %q: it cannot be encoded", sanitizeToken(name))
		return
	}
	body, err := json.Marshal(message{JSONRPC: "2.0", Method: "event", Params: params})
	if err != nil {
		return
	}
	out.event(name, operationKey(name, payload), body)
}

func (s *Server) outbox() *outbox {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out
}

func (s *Server) setOutbox(o *outbox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out = o
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		fmt.Fprintf(s.Log, "gitbox-server: "+format+"\n", args...)
	}
}

// Serve runs the handshake and then answers requests read from in, writing
// to out, until in ends. The end of in is the only shutdown signal there is:
// Serve then cancels the context of every handler, waits ShutdownGrace at
// most for them, and returns nil. It returns an error when the handshake
// fails or the connection breaks.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if s.ToUserError == nil {
		return errors.New("rpc: Server.ToUserError is not set")
	}
	reader := bufio.NewReader(in)
	if err := serverHandshake(out, reader, s.Self); err != nil {
		s.logf("handshake failed: %v", err)
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	box := newOutbox()
	s.setOutbox(box)
	defer s.setOutbox(nil)

	// The writer is the only goroutine that writes to out.
	writerDone := make(chan error, 1)
	go func() {
		for {
			body, ok := box.take()
			if !ok {
				writerDone <- nil
				return
			}
			if err := WriteFrame(out, body); err != nil {
				cancel()
				box.close()
				writerDone <- err
				return
			}
		}
	}()

	conn := &serverConn{s: s, ctx: ctx, box: box, cancels: make(map[int64]context.CancelFunc), remembered: make(map[string]*remembered)}
	readErr := conn.readLoop(reader)

	// The input ended: stop everything and give the handlers a moment.
	cancel()
	handlersDone := make(chan struct{})
	go func() {
		conn.handlers.Wait()
		close(handlersDone)
	}()
	select {
	case <-handlersDone:
	case <-time.After(ShutdownGrace):
		s.logf("closing with handlers still running")
	}
	box.close()
	writeErr := <-writerDone
	if readErr != nil {
		return readErr
	}
	return writeErr
}

// serverConn is the state of one connection.
type serverConn struct {
	s        *Server
	ctx      context.Context
	box      *outbox
	handlers sync.WaitGroup

	mu         sync.Mutex
	cancels    map[int64]context.CancelFunc // requests in flight
	remembered map[string]*remembered       // by request id of the mutation
	order      []string                     // remembered ids, oldest first
}

// remembered is the outcome of a mutation, kept to answer a repeat.
type remembered struct {
	done   chan struct{}
	result json.RawMessage
	err    *Error
}

func (c *serverConn) send(m message) {
	body, err := json.Marshal(m)
	if err != nil {
		body, _ = json.Marshal(errorResponse(m.ID, newError(CodeInternalError, ErrCodeInternal, "")))
	}
	if len(body) > MaxFrameBytes {
		body, _ = json.Marshal(errorResponse(m.ID, newError(CodeInternalError, ErrCodeInternal, "the result is too large")))
	}
	c.box.put(body)
}

// readLoop reads requests until the input ends. A clean end returns nil.
func (c *serverConn) readLoop(r *bufio.Reader) error {
	for {
		body, err := ReadFrame(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			c.s.logf("closing the connection: %v", err)
			return err
		}
		c.handle(body)
	}
}

func (c *serverConn) handle(body []byte) {
	if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		c.send(errorResponse(nil, newError(CodeInvalidRequest, ErrCodeInvalidRequest, "batches are not supported")))
		return
	}
	var req message
	if err := json.Unmarshal(body, &req); err != nil {
		c.send(errorResponse(nil, newError(CodeParseError, ErrCodeInvalidRequest, "the request is not valid JSON")))
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		c.send(errorResponse(req.ID, newError(CodeInvalidRequest, ErrCodeInvalidRequest, "not a JSON-RPC 2.0 request")))
		return
	}

	switch req.Method {
	case "$/cancelRequest":
		var p struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(req.Params, &p) == nil {
			c.mu.Lock()
			cancel := c.cancels[p.ID]
			c.mu.Unlock()
			if cancel != nil {
				cancel()
			}
		}
		return
	case "$/ping":
		if req.ID != nil {
			c.send(response(req.ID, nil))
		}
		return
	}

	if req.ID == nil {
		return // a notification the server does not know: nothing to answer
	}
	handler, ok := c.s.Handlers[req.Method]
	if !ok {
		c.send(errorResponse(req.ID, newError(CodeMethodNotFound, ErrCodeUnsupported, "")))
		return
	}

	id := *req.ID
	ctx, cancel := context.WithCancel(c.ctx)
	c.mu.Lock()
	if _, busy := c.cancels[id]; busy {
		c.mu.Unlock()
		cancel()
		c.send(errorResponse(req.ID, newError(CodeInvalidRequest, ErrCodeInvalidRequest, "the request id is already in use")))
		return
	}
	c.cancels[id] = cancel
	c.mu.Unlock()

	c.handlers.Add(1)
	go func() {
		defer c.handlers.Done()
		defer func() {
			c.mu.Lock()
			delete(c.cancels, id)
			c.mu.Unlock()
			cancel()
		}()

		type outcome struct {
			result json.RawMessage
			err    *Error
		}
		done := make(chan outcome, 1)
		c.handlers.Add(1) // closing waits for the handler itself, too
		go func() {
			defer c.handlers.Done()
			result, err := c.runOnce(ctx, req, handler)
			done <- outcome{result, err}
		}()

		select {
		case o := <-done:
			if o.err != nil {
				c.send(errorResponse(req.ID, o.err))
			} else {
				c.send(response(req.ID, o.result))
			}
		case <-ctx.Done():
			// Cancelled by the client, or the connection is closing. The
			// answer goes out now; whatever the handler returns later is
			// dropped.
			c.send(errorResponse(req.ID, newError(CodeCancelled, ErrCodeCancelled, "")))
		}
	}()
}

// runOnce runs the handler, unless the request repeats a mutation the server
// remembers: then it returns that outcome, waiting for it if the first
// attempt is still running.
func (c *serverConn) runOnce(ctx context.Context, req message, handler Handler) (json.RawMessage, *Error) {
	if req.Meta == nil || req.Meta.RequestID == "" {
		return c.run(ctx, req, handler)
	}
	id := req.Meta.RequestID
	c.mu.Lock()
	if prior := c.remembered[id]; prior != nil {
		c.mu.Unlock()
		select {
		case <-prior.done:
			return prior.result, prior.err
		case <-ctx.Done():
			return nil, newError(CodeCancelled, ErrCodeCancelled, "")
		}
	}
	entry := &remembered{done: make(chan struct{})}
	c.remembered[id] = entry
	c.order = append(c.order, id)
	if len(c.order) > MaxRememberedRequests {
		delete(c.remembered, c.order[0])
		c.order = c.order[1:]
	}
	c.mu.Unlock()

	entry.result, entry.err = c.run(ctx, req, handler)
	close(entry.done)
	return entry.result, entry.err
}

// run calls the handler and turns what it returns into what may cross the
// wire. A panic becomes an internal error with no detail; the stack goes to
// the server's log.
func (c *serverConn) run(ctx context.Context, req message, handler Handler) (result json.RawMessage, rpcErr *Error) {
	defer func() {
		if p := recover(); p != nil {
			c.s.logf("panic in %s: %v\n%s", sanitizeToken(req.Method), p, debug.Stack())
			result, rpcErr = nil, newError(CodeInternalError, ErrCodeInternal, "")
		}
	}()
	value, err := handler(ctx, req.Params)
	if err != nil {
		if pe, ok := errors.AsType[*paramsError](err); ok {
			return nil, newError(CodeInvalidParams, ErrCodeInvalidParams, pe.detail)
		}
		return nil, &Error{Code: CodeApplication, Data: c.s.ToUserError(err)}
	}
	data, err := json.Marshal(value)
	if err != nil {
		c.s.logf("the result of %s cannot be encoded", sanitizeToken(req.Method))
		return nil, newError(CodeInternalError, ErrCodeInternal, "")
	}
	return data, nil
}
