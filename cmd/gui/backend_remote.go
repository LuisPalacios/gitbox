package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/mirror"
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
	// onTruncate runs when a list from the server was cut (capList).
	onTruncate func()
	// readOnly refuses every method that changes state on the host before
	// it reaches the wire (#240). A remote window sets it until P5.
	readOnly bool

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

// relayEvent hands a server event to the window. Only the events of the
// relay table pass: an unknown name, a LOCAL event such as remote:state or
// update:quit, which only this process may emit, and an event of a later
// phase are dropped with a log line. Each payload is decoded into the shape
// this build emits and encoded again, so a field this build does not know,
// or one of the wrong type, never reaches the window.
func (r *RemoteBackend) relayEvent(name string, payload []json.RawMessage) {
	decode, known := relayedEvents[name]
	if info, ok := eventClass[name]; !known || !ok || info.Class != classRemote {
		r.logf("dropping event %q, which this window does not take from a server", userText(name))
		return
	}
	if len(payload) > 1 || (len(payload) == 0) != (decode == nil) {
		r.logf("dropping event %s: it carries %d payloads", name, len(payload))
		return
	}
	var items []any
	if decode != nil {
		item, err := decode(r, payload[0])
		if err != nil {
			r.logf("dropping event %s: its payload cannot be decoded", name)
			return
		}
		items = append(items, item)
	}
	r.sink().Emit(name, items...)
}

// Payloads that this build emits as maps, as structs with the same keys.
type fetchEventDTO struct {
	Source       string `json:"source"`
	Repo         string `json:"repo"`
	Error        string `json:"error"`
	Code         string `json:"code"`
	Host         string `json:"host"`
	Skipped      bool   `json:"skipped"`
	RefusedFast  bool   `json:"refusedFast"`
	UpstreamGone bool   `json:"upstreamGone"`
}

type fetchAllDoneDTO struct {
	Error string `json:"error"`
}

type discoverDoneDTO struct {
	AccountKey string           `json:"accountKey"`
	Error      string           `json:"error"`
	Repos      []DiscoverResult `json:"repos"`
}

type mirrorDiscoverProgressDTO struct {
	Phase   string `json:"phase"`
	Account string `json:"account"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

type mirrorDiscoverDoneDTO struct {
	Error   string                   `json:"error"`
	Results []mirror.DiscoveryResult `json:"results"`
}

type networkStateDTO struct {
	Offline bool     `json:"offline"`
	Hosts   []string `json:"hosts"`
}

// payloadDecoder decodes one relayed payload; nil means the event carries
// none.
type payloadDecoder func(r *RemoteBackend, raw json.RawMessage) (any, error)

// relayedEvents are the events a remote window takes from its server: the
// P4 events of eventClass. TestRelayedEventsAreRemote keeps the two in step.
var relayedEvents = map[string]payloadDecoder{
	"status:updated": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var list []StatusResult
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		return capList(r, "status:updated", list, maxRemoteRepos), nil
	},
	"fetch:start":              typedPayload[fetchEventDTO],
	"fetch:done":               typedPayload[fetchEventDTO],
	"fetch:allstart":           nil,
	"fetch:alldone":            typedPayload[fetchAllDoneDTO],
	"credentials:updated":      typedPayload[map[string]CredentialStatus],
	"discover:done":            typedPayload[discoverDoneDTO],
	"mirror:status":            typedPayload[[]MirrorStatusResult],
	"mirror:discover:progress": typedPayload[mirrorDiscoverProgressDTO],
	"mirror:discover:done":     typedPayload[mirrorDiscoverDoneDTO],
	"pr:refreshed":             typedPayload[PRAccountUpdateDTO],
	"network:state":            typedPayload[networkStateDTO],
}

// typedPayload decodes raw into T and encodes it again. For an object it
// keeps only the keys the server sent that T knows, so a key the emitting
// side leaves out stays out.
func typedPayload[T any](_ *RemoteBackend, raw json.RawMessage) (any, error) {
	var typed T
	if err := json.Unmarshal(raw, &typed); err != nil {
		return nil, err
	}
	clean, err := json.Marshal(typed)
	if err != nil {
		return nil, err
	}
	var sent map[string]json.RawMessage
	if json.Unmarshal(raw, &sent) == nil && sent != nil {
		var out map[string]any
		if err := json.Unmarshal(clean, &out); err != nil {
			return nil, err
		}
		for key := range out {
			if _, ok := sent[key]; !ok {
				delete(out, key)
			}
		}
		return out, nil
	}
	var out any
	err = json.Unmarshal(clean, &out)
	return out, err
}

// The longest lists a remote window takes from its server (#240). A longer
// one is cut, with a log line, and the window says it is incomplete.
const (
	maxRemoteRepos    = 10_000
	maxRemoteAccounts = 1_000
)

// capList cuts list to limit items.
func capList[T any](r *RemoteBackend, what string, list []T, limit int) []T {
	if len(list) <= limit {
		return list
	}
	r.truncated(what, len(list), limit)
	return list[:limit]
}

// capConfig keeps the first maxRemoteAccounts accounts, by key, and the
// first maxRemoteRepos repos of the sources left, in key order.
func capConfig(r *RemoteBackend, dto *ConfigDTO) {
	if len(dto.Accounts) > maxRemoteAccounts {
		keys := slices.Sorted(maps.Keys(dto.Accounts))
		r.truncated("accounts", len(keys), maxRemoteAccounts)
		for _, key := range keys[maxRemoteAccounts:] {
			delete(dto.Accounts, key)
		}
		for key, src := range dto.Sources {
			if _, ok := dto.Accounts[src.Account]; !ok {
				delete(dto.Sources, key)
			}
		}
	}
	total := 0
	for _, src := range dto.Sources {
		total += len(src.Repos)
	}
	if total <= maxRemoteRepos {
		return
	}
	r.truncated("repos", total, maxRemoteRepos)
	left := maxRemoteRepos
	for _, key := range slices.Sorted(maps.Keys(dto.Sources)) {
		src := dto.Sources[key]
		repos := slices.Sorted(maps.Keys(src.Repos))
		if len(repos) > left {
			for _, repo := range repos[left:] {
				delete(src.Repos, repo)
			}
			src.RepoOrder = slices.DeleteFunc(src.RepoOrder, func(repo string) bool { _, ok := src.Repos[repo]; return !ok })
			dto.Sources[key] = src
		}
		left = max(0, left-len(src.Repos))
	}
}

// truncated logs a cut list and tells the window.
func (r *RemoteBackend) truncated(what string, had, kept int) {
	r.logf("the server sent %d %s; the window shows the first %d", had, what, kept)
	if r.onTruncate != nil {
		r.onTruncate()
	}
}

// invoke calls a method on the server. A method that changes state on the
// host carries a request id, so a call that is sent twice takes effect once.
// Before the connection and after it ends, every call fails with
// not_connected; on a read-only backend a mutation fails with read_only
// without being sent; a method the server does not serve yet fails with
// not_implemented_remote.
func (r *RemoteBackend) invoke(method string, result any, args ...any) error {
	client := r.current()
	if client == nil {
		return &remoteError{remoteNotConnected, "not connected to the remote"}
	}
	if r.readOnly && methodClass[method].Mutating {
		return &remoteError{remoteReadOnly, method + " is not available on a read-only remote"}
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
