package rpc

import (
	"bufio"
	"context"
	"io"
	"runtime"
	"testing"
	"testing/synctest"
	"time"
)

// Closing the client ends the connection on both sides and leaves no
// goroutine behind.
func TestClient_CloseLeavesNoGoroutines(t *testing.T) {
	runtime.GC()
	base := runtime.NumGoroutine()

	for range 5 {
		entered := make(chan struct{})
		srv := newServer(map[string]Handler{
			"Add": Func(func(a, b int) int { return a + b }),
			"Wait": Func(func(ctx context.Context) error {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			}),
		})
		l := connect(t, srv)
		var sum int
		if err := l.call("Add", []any{1, 2}, &sum); err != nil || sum != 3 {
			t.Fatalf("Add = %d, %v", sum, err)
		}
		srv.Emit("status:updated", 1)
		pending := make(chan error, 1)
		go func() { pending <- l.Client.Call(context.Background(), "Wait", nil, nil) }()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("the Wait handler did not start")
		}

		if err := l.Client.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err := <-l.served; err != nil {
			t.Errorf("Serve = %v after the client closed", err)
		}
		if err := <-pending; !IsCode(err, ErrCodeDisconnected) && !IsCode(err, ErrCodeCancelled) {
			t.Errorf("the pending call ended with %v, want disconnected", err)
		}
		if err := l.call("Add", []any{1, 2}, nil); !IsCode(err, ErrCodeDisconnected) {
			t.Errorf("a call after Close = %v, want disconnected", err)
		}
		select {
		case <-l.Client.Done():
		default:
			t.Error("Done must be closed after Close")
		}
	}

	if n, ok := goroutinesSettle(base, 2*time.Second); !ok {
		buf := make([]byte, 1<<16)
		t.Errorf("%d goroutines are left, %d before the test:\n%s", n, base, buf[:runtime.Stack(buf, true)])
	}
}

// silentServer does the handshake and then reads without ever answering: a
// link that looks open and is dead.
func silentServer(t *testing.T) (in io.Reader, out io.WriteCloser, stop func()) {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	go func() {
		reader := bufio.NewReader(c2sR)
		if err := serverHandshake(s2cW, reader, newServer(nil).Self); err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, reader)
	}()
	return s2cR, c2sW, func() { _ = c2sW.Close(); _ = s2cW.Close(); _ = c2sR.Close(); _ = s2cR.Close() }
}

// A server that stops answering is declared dead after a few missed pings,
// and the calls waiting on it fail as disconnected.
func TestClient_HeartbeatDeclaresADeadLink(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		in, out, stop := silentServer(t)
		deadCh := make(chan error, 1)
		client, err := Dial(in, out, ClientOptions{Version: testVersion, Commit: testCommit, OnDead: func(err error) { deadCh <- err }})
		if err != nil {
			t.Fatal(err)
		}
		started := time.Now()
		pending := make(chan error, 1)
		go func() { pending <- client.Call(context.Background(), "Anything", nil, nil) }()

		err = <-pending
		spent := time.Since(started)
		if !IsCode(err, ErrCodeDisconnected) {
			t.Errorf("the pending call ended with %v, want disconnected", err)
		}
		if limit := PingInterval*MaxMissedPings + PingInterval; spent > limit {
			t.Errorf("the link was declared dead after %v, want within %v", spent, limit)
		}
		if spent < PingInterval*(MaxMissedPings-1) {
			t.Errorf("the link was declared dead after only %v", spent)
		}
		if dead := <-deadCh; !IsCode(dead, ErrCodeDisconnected) {
			t.Errorf("OnDead got %v", dead)
		}
		if err := client.Call(context.Background(), "Anything", nil, nil); !IsCode(err, ErrCodeDisconnected) {
			t.Errorf("a call on a dead link = %v", err)
		}
		stop()
		_ = client.Close()
	})
}

// A healthy link stays up however long it idles.
func TestClient_HeartbeatKeepsAHealthyLink(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := connect(t, newServer(map[string]Handler{"Add": Func(func(a, b int) int { return a + b })}))
		time.Sleep(10 * time.Minute)
		synctest.Wait()
		var sum int
		if err := l.call("Add", []any{2, 2}, &sum); err != nil || sum != 4 {
			t.Errorf("after ten idle minutes: %d, %v", sum, err)
		}
		l.mu.Lock()
		dead := l.dead
		l.mu.Unlock()
		if dead != nil {
			t.Errorf("a healthy link was declared dead: %v", dead)
		}
		_ = l.Client.Close()
		<-l.served
	})
}

// A response nobody is waiting for is dropped; it cannot complete another
// call.
func TestClient_IgnoresResponsesItDidNotAskFor(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	go func() {
		reader := bufio.NewReader(c2sR)
		if err := serverHandshake(s2cW, reader, newServer(nil).Self); err != nil {
			return
		}
		for {
			var req message
			if readJSONFrame(reader, &req) != nil {
				_ = s2cW.Close()
				return
			}
			if req.Method != "Real" {
				if req.ID != nil && req.Method == "$/ping" {
					_ = writeJSONFrame(s2cW, response(req.ID, nil))
				}
				continue
			}
			forged := int64(9999)
			_ = writeJSONFrame(s2cW, response(&forged, []byte(`"forged"`)))
			_ = WriteFrame(s2cW, []byte(`{"jsonrpc":"2.0","method":"$/unknownNotification"}`))
			_ = WriteFrame(s2cW, []byte(`{"jsonrpc":"2.0","method":"event","params":"not an object"}`))
			_ = writeJSONFrame(s2cW, response(req.ID, []byte(`"real"`)))
		}
	}()
	client, err := Dial(s2cR, c2sW, ClientOptions{Version: testVersion, Commit: testCommit})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var got string
	if err := client.Call(ctx, "Real", nil, &got); err != nil || got != "real" {
		t.Errorf("Call = %q, %v; want the answer to its own request", got, err)
	}
}
