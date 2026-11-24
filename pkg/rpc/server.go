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
	"sync/atomic"
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
	// Remember reports whether a request of method that carries a request id
	// is remembered and replayed on a repeat. Nil remembers every such
	// request. The app passes its mutating methods, so a read is never
	// answered from the cache (#294).
	Remember func(method string) bool
	// SecretParams marks the parameters that carry a secret (#242). The
	// server withholds the panic value of such a request and removes the
	// secret from its answer, success or error.
	SecretParams SecretParams
	// Persistent, when set, makes the server outlive its connections
	// (resume.go). Nil is a server for one session: the end of its
	// connection cancels everything the connection started.
	Persistent *Persistent

	// emitMu orders events: their sequence numbers, the journal and every
	// connection's queue see them in the same order.
	emitMu sync.Mutex
	seq    uint64

	mu         sync.Mutex
	conns      map[*serverConn]struct{} // registered while they serve
	attached   int                      // connections past the handshake
	lastDetach time.Time
	results    *resultStore // shared by the connections of a persistent server

	running  sync.WaitGroup // handlers of a persistent server
	inFlight atomic.Int64
}

// Emit sends an event to every connected client. It has the shape of the
// app's event sink. Each event gets the next sequence number; a persistent
// server also journals the events its Replay names, connected client or
// not. With no connection, an event is otherwise dropped.
func (s *Server) Emit(name string, payload ...any) {
	if payload == nil {
		payload = []any{}
	}
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	s.seq++
	params, err := json.Marshal(eventParams{Name: name, Payload: payload, Seq: s.seq})
	if err != nil {
		s.seq--
		s.logf("dropping event %q: it cannot be encoded", sanitizeToken(name))
		return
	}
	body, err := json.Marshal(message{JSONRPC: "2.0", Method: "event", Params: params})
	if err != nil {
		s.seq--
		return
	}
	if p := s.Persistent; p != nil && p.Journal != nil && p.Replay != nil && p.Replay(name) {
		p.Journal.add(s.seq, name, params)
	}
	key := operationKey(name, payload)
	for _, c := range s.connections() {
		if !c.box.event(name, key, body, s.Persistent == nil) {
			// A persistent server never waits for one slow client: that
			// would stall every other one. The client resumes when it
			// reconnects.
			s.logf("closing a connection that does not keep up with its events")
			c.abort()
		}
	}
}

func (s *Server) connections() []*serverConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*serverConn, 0, len(s.conns))
	for c := range s.conns {
		out = append(out, c)
	}
	return out
}

// register adds c to the connections that get events. Events from the next
// sequence number on reach it live; older ones only through $/resume.
func (s *Server) register(c *serverConn) {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns == nil {
		s.conns = make(map[*serverConn]struct{})
	}
	s.conns[c] = struct{}{}
	c.liveFrom = s.seq + 1
}

func (s *Server) unregister(c *serverConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, c)
}

// lastSeq is the number of the last event emitted.
func (s *Server) lastSeq() uint64 {
	s.emitMu.Lock()
	defer s.emitMu.Unlock()
	return s.seq
}

// sharedResults is the result store of a persistent server.
func (s *Server) sharedResults() *resultStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = newResultStore(ResultTTL)
	}
	return s.results
}

// Activity is what a persistent server's owner needs to decide whether it
// may stop.
type Activity struct {
	// Attached is the number of connections past their handshake.
	Attached int
	// InFlight is the number of requests still running, on a connection
	// or after it ended.
	InFlight int
	// LastDetach is when the last connection ended, zero before any did.
	LastDetach time.Time
}

// Activity reports the server's connections and running requests.
func (s *Server) Activity() Activity {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Activity{Attached: s.attached, InFlight: int(s.inFlight.Load()), LastDetach: s.lastDetach}
}

// Wait waits up to d for the requests a persistent server still runs and
// reports whether they all ended.
func (s *Server) Wait(d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
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
	return s.ServeConn(ctx, in, out, Peer{})
}

// ServeConn is Serve for one connection of peer. On a persistent server
// ctx is the server's life: the requests of the connection run under it and
// keep running when in ends, and Serve returns without waiting for them. A
// persistent server drops a connection that does not keep up with its
// events by closing in and out, those of them that are an io.Closer.
func (s *Server) ServeConn(ctx context.Context, in io.Reader, out io.Writer, peer Peer) error {
	if s.ToUserError == nil {
		return errors.New("rpc: Server.ToUserError is not set")
	}
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn := &serverConn{s: s, ctx: connCtx, cancel: cancel, peer: peer, box: newOutbox(),
		cancels: make(map[int64]context.CancelFunc)}
	conn.handlerCtx, conn.handlerWG = connCtx, &conn.handlers
	if s.Persistent != nil {
		conn.handlerCtx, conn.handlerWG, conn.results = ctx, &s.running, s.sharedResults()
	} else {
		conn.results = newResultStore(0)
	}
	for _, stream := range []any{in, out} {
		if closer, ok := stream.(io.Closer); ok {
			conn.closers = append(conn.closers, closer)
		}
	}
	box := conn.box
	// The connection gets events before the handshake ends: an event
	// emitted the moment the client takes the connection as made is
	// queued, and goes out after the verdict.
	s.register(conn)
	defer s.unregister(conn)

	reader := bufio.NewReader(in)
	self := s.Self
	if p := s.Persistent; p != nil {
		self.Daemon = &DaemonHello{Instance: p.Instance, Resumed: !peer.Started, LastSeq: conn.liveFrom - 1}
	}
	if err := serverHandshake(out, reader, self); err != nil {
		box.close()
		s.logf("handshake failed: %v", err)
		return err
	}
	s.mu.Lock()
	s.attached++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.attached--
		s.lastDetach = time.Now()
		s.mu.Unlock()
	}()

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

	readErr := conn.readLoop(reader)

	// The input ended: stop everything and give the handlers a moment. A
	// persistent server's handlers go on; their answers have nowhere to go,
	// but what they remember is there for a repeat.
	cancel()
	if s.Persistent == nil {
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
	cancel   context.CancelFunc
	peer     Peer
	box      *outbox
	handlers sync.WaitGroup
	closers  []io.Closer // what abort closes
	// liveFrom is the sequence number of the first event the connection
	// got live.
	liveFrom uint64

	// handlerCtx and handlerWG are what requests run under: the
	// connection's, or the persistent server's.
	handlerCtx context.Context
	handlerWG  *sync.WaitGroup
	results    *resultStore

	mu        sync.Mutex
	cancels   map[int64]context.CancelFunc // requests in flight
	abortOnce sync.Once
}

// abort drops the connection: its queue closes, and its streams when they
// can, so neither the reader nor the writer stays blocked.
func (c *serverConn) abort() {
	c.abortOnce.Do(func() {
		c.box.close()
		c.cancel()
		for _, closer := range c.closers {
			_ = closer.Close()
		}
	})
}

// remembered is the outcome of a mutation, kept to answer a repeat.
type remembered struct {
	done     chan struct{}
	result   json.RawMessage
	err      *Error
	finished time.Time // guarded by the store's lock
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
	case "$/resume":
		if c.s.Persistent != nil {
			c.resume(req)
			return
		}
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
	ctx, cancel := context.WithCancel(c.handlerCtx)
	c.mu.Lock()
	if _, busy := c.cancels[id]; busy {
		c.mu.Unlock()
		cancel()
		c.send(errorResponse(req.ID, newError(CodeInvalidRequest, ErrCodeInvalidRequest, "the request id is already in use")))
		return
	}
	c.cancels[id] = cancel
	c.mu.Unlock()

	c.s.inFlight.Add(1)
	c.handlerWG.Go(func() {
		defer func() {
			c.mu.Lock()
			delete(c.cancels, id)
			c.mu.Unlock()
			cancel()
			c.s.inFlight.Add(-1)
		}()

		type outcome struct {
			result json.RawMessage
			err    *Error
		}
		done := make(chan outcome, 1)
		c.handlerWG.Go(func() { // closing waits for the handler itself, too
			result, err := c.runOnce(ctx, req, handler)
			done <- outcome{result, err}
		})

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
	})
}

// runOnce runs the handler, unless the request repeats a mutation the server
// remembers: then it returns that outcome, waiting for it if the first
// attempt is still running.
func (c *serverConn) runOnce(ctx context.Context, req message, handler Handler) (json.RawMessage, *Error) {
	if req.Meta == nil || req.Meta.RequestID == "" || (c.s.Remember != nil && !c.s.Remember(req.Method)) {
		return c.run(ctx, req, handler)
	}
	// The method is part of the key: an id repeated on another method is a
	// different request (#294). So is the client: on a persistent server
	// two clients may pick the same id (#247).
	key := resultKey(c.peer.ClientID, req.Method, req.Meta.RequestID)
	entry, first := c.results.take(key)
	if !first {
		select {
		case <-entry.done:
			return entry.result, entry.err
		case <-ctx.Done():
			return nil, newError(CodeCancelled, ErrCodeCancelled, "")
		}
	}

	entry.result, entry.err = c.run(ctx, req, handler)
	// A cancelled attempt that failed did not finish: a repeat must run
	// again, not replay the cancellation (#294). A success that finished
	// despite a late cancel stays remembered, so a retry never repeats its
	// effect. Waiters already parked still get the outcome.
	c.results.finish(key, entry, entry.err != nil && (entry.err.Code == CodeCancelled || ctx.Err() != nil))
	close(entry.done)
	return entry.result, entry.err
}

// resume answers $/resume: the journaled events after the client's last
// one and before the first this connection got live, when the client was
// connected to this instance.
func (c *serverConn) resume(req message) {
	var p resumeParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		if req.ID != nil {
			c.send(errorResponse(req.ID, newError(CodeInvalidParams, ErrCodeInvalidParams, "")))
		}
		return
	}
	if req.ID == nil {
		return
	}
	res := resumeResult{Events: []json.RawMessage{}}
	persistent := c.s.Persistent
	if p.Instance != "" && p.Instance == persistent.Instance {
		res.Resumed = true
		if persistent.Journal != nil {
			res.Events, res.Gap = persistent.Journal.since(p.After, c.liveFrom, maxReplayBytes)
			if res.Events == nil {
				res.Events = []json.RawMessage{}
			}
		} else {
			res.Gap = p.After+1 < c.liveFrom
		}
	}
	data, err := json.Marshal(res)
	if err != nil {
		c.send(errorResponse(req.ID, newError(CodeInternalError, ErrCodeInternal, "")))
		return
	}
	c.send(response(req.ID, data))
}

// run calls the handler and turns what it returns into what may cross the
// wire. A panic becomes an internal error with no detail; the stack goes to
// the server's log, and so does the panic value unless the request carries
// a secret. The secrets of a request never come back in its answer.
func (c *serverConn) run(ctx context.Context, req message, handler Handler) (result json.RawMessage, rpcErr *Error) {
	secrets := c.s.SecretParams.values(req.Method, req.Params)
	defer func() {
		if p := recover(); p != nil {
			var value any = p
			if len(c.s.SecretParams[req.Method]) > 0 {
				value = secretMark
			}
			c.s.logf("panic in %s: %v\n%s", sanitizeToken(req.Method), value, debug.Stack())
			result, rpcErr = nil, newError(CodeInternalError, ErrCodeInternal, "")
		}
	}()
	value, err := handler(ctx, req.Params)
	if err != nil {
		if pe, ok := errors.AsType[*paramsError](err); ok {
			return nil, newError(CodeInvalidParams, ErrCodeInvalidParams, pe.detail)
		}
		return nil, &Error{Code: CodeApplication, Data: scrubUserError(c.s.ToUserError(err), secrets)}
	}
	data, err := json.Marshal(value)
	if err != nil {
		c.s.logf("the result of %s cannot be encoded", sanitizeToken(req.Method))
		return nil, newError(CodeInternalError, ErrCodeInternal, "")
	}
	return scrubJSON(data, secrets), nil
}
