package remote

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestReconnectPolicy(t *testing.T) {
	p := ReconnectPolicy{Backoff: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}}
	if p.Attempts() != 3 || DefaultReconnect.Attempts() != 3 {
		t.Fatalf("attempts %d and %d, want 3", p.Attempts(), DefaultReconnect.Attempts())
	}
	t.Run("stops at the first success", func(t *testing.T) {
		var seen []int
		started := time.Now()
		err := p.Run(t.Context(), func(_ context.Context, n int) error {
			seen = append(seen, n)
			if n == 2 {
				return nil
			}
			return errors.New("down")
		})
		if err != nil || len(seen) != 2 || seen[1] != 2 {
			t.Errorf("Run = %v after attempts %v, want success on the second", err, seen)
		}
		if took := time.Since(started); took < 30*time.Millisecond {
			t.Errorf("two attempts took %v, want at least both waits", took)
		}
	})
	t.Run("returns the last failure", func(t *testing.T) {
		last := errors.New("third")
		n := 0
		err := p.Run(t.Context(), func(_ context.Context, i int) error {
			n = i
			if i == 3 {
				return last
			}
			return errors.New("earlier")
		})
		if !errors.Is(err, last) || n != 3 {
			t.Errorf("Run = %v after %d attempts, want the third attempt's error", err, n)
		}
	})
	t.Run("ends with its context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		called := false
		err := p.Run(ctx, func(context.Context, int) error { called = true; return nil })
		if !errors.Is(err, context.Canceled) || called {
			t.Errorf("Run on a cancelled context = %v, called %v", err, called)
		}
	})
	if err := (ReconnectPolicy{}).Run(t.Context(), func(context.Context, int) error { return nil }); err == nil {
		t.Error("a policy with no attempt reported success")
	}
}

func TestProxyHello(t *testing.T) {
	for _, started := range []bool{false, true} {
		var b bytesBuffer
		if err := WriteProxyHello(&b, "client-a", started); err != nil {
			t.Fatal(err)
		}
		b.data = append(b.data, "protocol"...)
		id, gotStarted, err := ReadProxyHello(&b)
		if err != nil || id != "client-a" || gotStarted != started {
			t.Errorf("round trip = %q, %v, %v", id, gotStarted, err)
		}
		if string(b.data) != "protocol" {
			t.Errorf("ReadProxyHello consumed %q of what follows", "protocol"[:8-len(b.data)])
		}
	}
	if err := WriteProxyHello(&bytesBuffer{}, "bad id", false); err == nil {
		t.Error("an invalid client id was written")
	}
	for _, bad := range []string{
		"gbx-proxy 2 client-a 0\n", "gbx-proxy 1 client-a 2\n", "gbx-proxy 1 bad;id 0\n",
		"hello\n", "gbx-proxy 1 " + string(make([]byte, 200)), "gbx-proxy 1 client-a",
	} {
		if _, _, err := ReadProxyHello(&bytesBuffer{data: []byte(bad)}); err == nil {
			t.Errorf("ReadProxyHello accepted %q", bad)
		}
	}
}

// bytesBuffer is a reader and writer with no read-ahead of its own.
type bytesBuffer struct{ data []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) { b.data = append(b.data, p...); return len(p), nil }

func (b *bytesBuffer) Read(p []byte) (int, error) {
	if len(b.data) == 0 {
		return 0, errors.New("EOF")
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, nil
}
