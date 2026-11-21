package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// closeWait is how long Close waits for the server to end the connection
// after its input was closed.
const closeWait = 5 * time.Second

// cancelWait is how long a cancelled call waits for the server's answer to
// the cancellation before giving up on it.
const cancelWait = 2 * time.Second

// ClientOptions configure a connection.
type ClientOptions struct {
	// Version and Commit identify this build. The server accepts only its
	// own.
	Version, Commit string
	// OnEvent receives each event the server sends, in order. The payload
	// items are still encoded. It runs on the reader's goroutine: it must
	// not block and must not call back into the client.
	OnEvent func(name string, payload []json.RawMessage)
	// OnDead is called once, when the link is declared dead or the
	// connection ends.
	OnDead func(error)
	// Log receives one line per event worth a human's attention. Nil
	// discards them.
	Log io.Writer
}

// Client is the calling end of a connection. Everything the server sends is
// untrusted input: frames are bounded, responses are matched to calls that
// were really made, and an error arrives only as a UserError.
type Client struct {
	opts   ClientOptions
	server Hello
	out    io.WriteCloser

	writeMu sync.Mutex // serialises frames on out

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan message
	dead    error // set once; every later call fails with it

	done       chan struct{} // closed when the reader ends
	stopPing   chan struct{}
	stopOnce   sync.Once
	deadOnce   sync.Once
	pingClosed chan struct{}
}

// Dial runs the handshake over in (what the server writes) and out (what it
// reads), and returns a client ready for calls. It fails with a *Error whose
// code is banner_overflow or version_mismatch when the handshake says so.
func Dial(in io.Reader, out io.WriteCloser, opts ClientOptions) (*Client, error) {
	reader := bufio.NewReader(in)
	server, err := clientHandshake(out, reader, opts.Version, opts.Commit)
	if err != nil {
		switch {
		case errors.Is(err, ErrBannerOverflow):
			return nil, newError(CodeApplication, ErrCodeBannerOverflow, err.Error())
		case errors.Is(err, ErrVersionMismatch):
			return nil, newError(CodeApplication, ErrCodeVersionMismatch, err.Error())
		}
		return nil, newError(CodeApplication, ErrCodeDisconnected, "the connection ended during the handshake")
	}
	c := &Client{
		opts: opts, server: server, out: out,
		pending:  make(map[int64]chan message),
		done:     make(chan struct{}),
		stopPing: make(chan struct{}), pingClosed: make(chan struct{}),
	}
	go c.readLoop(reader)
	go c.heartbeat()
	return c, nil
}

// Server returns what the server said about itself in the handshake.
func (c *Client) Server() Hello { return c.server }

// Done is closed when the connection has ended.
func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) logf(format string, args ...any) {
	if c.opts.Log != nil {
		fmt.Fprintf(c.opts.Log, "rpc client: "+format+"\n", args...)
	}
}

// CallOption adjusts one call.
type CallOption func(*message)

// WithRequestID marks the call as one attempt at a mutation. If the same id
// is sent again in this session, the server answers with the stored outcome
// instead of repeating the effect.
func WithRequestID(id string) CallOption {
	return func(m *message) { m.Meta = &messageMeta{RequestID: id} }
}

// Call invokes method with positional params and decodes the result into
// result, which may be nil. It fails with a *Error. When ctx ends first, the
// server is told to cancel the request.
func (c *Client) Call(ctx context.Context, method string, params []any, result any, opts ...CallOption) error {
	if params == nil {
		params = []any{}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return newError(CodeInvalidParams, ErrCodeInvalidParams, "the parameters cannot be encoded")
	}

	c.mu.Lock()
	if c.dead != nil {
		err := c.dead
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := c.nextID
	reply := make(chan message, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}

	req := message{JSONRPC: "2.0", ID: &id, Method: method, Params: raw}
	for _, opt := range opts {
		opt(&req)
	}
	if err := c.write(req); err != nil {
		forget()
		return c.fail(err)
	}

	select {
	case m, ok := <-reply:
		return c.finish(m, ok, result)
	case <-ctx.Done():
	}

	// The caller gave up: tell the server, and take its answer if it comes
	// soon. Either way the call is over.
	p, _ := json.Marshal(map[string]int64{"id": id})
	_ = c.write(message{JSONRPC: "2.0", Method: "$/cancelRequest", Params: p})
	timer := time.NewTimer(cancelWait)
	defer timer.Stop()
	select {
	case m, ok := <-reply:
		return c.finish(m, ok, result)
	case <-timer.C:
		forget()
		return newError(CodeCancelled, ErrCodeCancelled, "")
	}
}

// finish turns a response into the outcome of a call. ok is false when the
// connection died before the response came.
func (c *Client) finish(m message, ok bool, result any) error {
	if !ok {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.dead
	}
	if m.Error != nil {
		return m.Error.asError()
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(m.Result, result); err != nil {
		return newError(CodeInternalError, ErrCodeInternal, "the server's answer has an unexpected shape")
	}
	return nil
}

func (c *Client) write(m message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteFrame(c.out, body)
}

// fail declares the link dead after a failure to write.
func (c *Client) fail(cause error) error {
	c.markDead(newError(CodeApplication, ErrCodeDisconnected, "the connection to the server was lost"))
	c.logf("write failed: %v", cause)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dead
}

// markDead fails every pending call and all later ones. The first cause
// wins.
func (c *Client) markDead(err *Error) {
	c.deadOnce.Do(func() {
		c.mu.Lock()
		c.dead = err
		pending := c.pending
		c.pending = make(map[int64]chan message)
		c.mu.Unlock()
		for _, reply := range pending {
			close(reply)
		}
		c.stopOnce.Do(func() { close(c.stopPing) })
		if c.opts.OnDead != nil {
			c.opts.OnDead(err)
		}
	})
}

func (c *Client) readLoop(r *bufio.Reader) {
	defer close(c.done)
	for {
		body, err := ReadFrame(r)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.logf("closing the connection: %v", err)
			}
			c.markDead(newError(CodeApplication, ErrCodeDisconnected, "the connection to the server ended"))
			return
		}
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			c.logf("closing the connection: a frame is not valid JSON")
			c.markDead(newError(CodeApplication, ErrCodeDisconnected, "the server sent something that is not the protocol"))
			return
		}
		switch {
		case m.Method == "event":
			c.deliverEvent(m.Params)
		case m.Method != "":
			// A notification this client does not know: ignore it.
		case m.ID != nil:
			c.mu.Lock()
			reply := c.pending[*m.ID]
			delete(c.pending, *m.ID)
			c.mu.Unlock()
			if reply != nil {
				reply <- m
			}
			// A response to a call nobody made, or made and abandoned, is
			// dropped.
		}
	}
}

func (c *Client) deliverEvent(params json.RawMessage) {
	if c.opts.OnEvent == nil {
		return
	}
	var ev struct {
		Name    string            `json:"name"`
		Payload []json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(params, &ev); err != nil || ev.Name == "" {
		c.logf("dropping an event that cannot be decoded")
		return
	}
	c.opts.OnEvent(ev.Name, ev.Payload)
}

// heartbeat checks the link every PingInterval. A ping still unanswered when
// the next one is due counts as missed; MaxMissedPings in a row declare the
// link dead, which fails every pending call.
func (c *Client) heartbeat() {
	defer close(c.pingClosed)
	ticker := time.NewTicker(PingInterval)
	defer ticker.Stop()
	missed := 0
	var outstanding chan error
	send := func() {
		ch := make(chan error, 1)
		outstanding = ch
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), PingInterval*time.Duration(MaxMissedPings+1))
			defer cancel()
			ch <- c.ping(ctx)
		}()
	}
	send()
	for {
		select {
		case <-c.stopPing:
			return
		case <-ticker.C:
		}
		select {
		case err := <-outstanding:
			if err == nil {
				missed = 0
			} else {
				missed++
			}
			send()
		default:
			missed++ // the last ping is still unanswered
		}
		if missed >= MaxMissedPings {
			c.markDead(newError(CodeApplication, ErrCodeDisconnected, "the server stopped answering"))
			return
		}
	}
}

// ping is a call that never asks the server to cancel: an unanswered ping is
// information, not work to stop.
func (c *Client) ping(ctx context.Context) error {
	c.mu.Lock()
	if c.dead != nil {
		err := c.dead
		c.mu.Unlock()
		return err
	}
	c.nextID++
	id := c.nextID
	reply := make(chan message, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	if err := c.write(message{JSONRPC: "2.0", ID: &id, Method: "$/ping"}); err != nil {
		return c.fail(err)
	}
	select {
	case m, ok := <-reply:
		return c.finish(m, ok, nil)
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}

// Close ends the connection: it closes the server's input, which is the
// server's signal to stop, and waits for the server to end its output.
// Pending calls fail as disconnected.
func (c *Client) Close() error {
	c.stopOnce.Do(func() { close(c.stopPing) })
	err := c.out.Close()
	select {
	case <-c.done:
	case <-time.After(closeWait):
		c.markDead(newError(CodeApplication, ErrCodeDisconnected, "the connection was closed"))
	}
	<-c.pingClosed
	return err
}
