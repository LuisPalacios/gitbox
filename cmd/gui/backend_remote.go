package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
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
	clientApp *App
	log       io.Writer
	// onTruncate runs when a list from the server was cut (capList).
	onTruncate func()
	// refuseUnserved refuses, before it reaches the wire, every method that
	// changes state on the host and that the server does not serve yet: the
	// credential methods (#242, #244). Client and server are the same
	// build, so servedMethods is exact. A remote window sets it.
	refuseUnserved bool

	mu      sync.Mutex
	client  *rpc.Client // nil while not connected
	lastErr error
	// gen counts connections: an event from a connection this backend
	// replaced or detached is dropped, so nothing it sends after the end
	// reaches the window.
	gen uint64
	// long are the calls goLong waits for in the background.
	long map[*longCall]struct{}
}

// longCall is one clone, pull, move or mirror setup in flight (goLong).
type longCall struct {
	done string // the event that ends it
	key  string // which operation, as doneKey says
	seen bool   // its done event arrived; guarded by RemoteBackend.mu
}

var _ Backend = (*RemoteBackend)(nil)

// newRemoteBackend returns a backend with no connection: every call fails
// with not_connected until connect succeeds. Events go to the client App.
func newRemoteBackend(app *App, log io.Writer) *RemoteBackend {
	return &RemoteBackend{clientApp: app, log: log}
}

// dialRemoteBackend runs the handshake with the server whose output is in
// and whose input is out, and returns its backend. The handshake fails
// unless the server is this same build.
func dialRemoteBackend(in io.Reader, out io.WriteCloser, sink func() EventSink, log io.Writer) (*RemoteBackend, error) {
	r := newRemoteBackend(&App{events: sink()}, log)
	if err := r.connect(in, out); err != nil {
		return nil, err
	}
	return r, nil
}

// connect runs the handshake and attaches the connection.
func (r *RemoteBackend) connect(in io.Reader, out io.WriteCloser) error {
	r.mu.Lock()
	r.gen++
	gen := r.gen
	r.mu.Unlock()
	client, err := rpc.Dial(in, out, rpc.ClientOptions{
		Version: version, Commit: commit,
		OnEvent: func(name string, payload []json.RawMessage) {
			r.mu.Lock()
			live := r.gen == gen
			r.mu.Unlock()
			if live {
				r.relayEvent(name, payload)
			}
		},
		Log:          r.log,
		SecretParams: secretParams,
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
// leaves the backend answering not_connected. On the host that cancels the
// long calls in flight and their git processes (#295); here they fail as
// disconnected.
func (r *RemoteBackend) detach() {
	r.mu.Lock()
	c := r.client
	r.client = nil
	r.gen++
	r.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// goLong runs a clone, pull, move or mirror setup on the host and returns at
// once, as the local backend does: the server answers when the work is
// done, and its events tell the window how it went. Detaching ends it on
// both sides. When it fails before the server sent done (the
// connection dropped, or the call never left), goLong hands the window a
// done of its own with the error, through the same relay as the server's,
// so nothing in the window waits for an event that will not come. On the
// server the done event is queued before the answer, so a call that ends
// after done never adds a second one.
func (r *RemoteBackend) goLong(method, done string, failed func(msg string) any, args ...any) {
	call := &longCall{done: done, key: doneKey(done, failed(""))}
	r.mu.Lock()
	if r.long == nil {
		r.long = make(map[*longCall]struct{})
	}
	r.long[call] = struct{}{}
	r.mu.Unlock()
	go func() {
		err := r.invoke(method, nil, args...)
		r.mu.Lock()
		delete(r.long, call)
		seen := call.seen
		r.mu.Unlock()
		if err != nil {
			r.note(method, err)
			if raw, merr := json.Marshal(failed(userMsg(err))); merr == nil && !seen {
				r.relayPayload(done, Envelope[json.RawMessage]{
					EventMetadata: EventMetadata{Client: true},
					Payload:       raw,
				})
			}
		}
	}()
}

// doneKey names the operation a done event ends, from its payload: the
// repo for a clone or a pull, the mirror repo for a setup. A move has one
// key: the window runs one move at a time.
func doneKey(event string, payload any) string {
	b, err := json.Marshal(payload)
	if err != nil {
		return event
	}
	var p struct {
		Source    string `json:"source"`
		Repo      string `json:"repo"`
		MirrorKey string `json:"mirrorKey"`
		RepoKey   string `json:"repoKey"`
		Run       string `json:"run"`
	}
	_ = json.Unmarshal(b, &p)
	return event + "\x00" + p.Source + "\x00" + p.Repo + "\x00" + p.MirrorKey + "\x00" + p.RepoKey + "\x00" + p.Run
}

// sawDone marks the long calls that a relayed done event ends.
func (r *RemoteBackend) sawDone(event string, payload any) {
	key := doneKey(event, payload)
	r.mu.Lock()
	defer r.mu.Unlock()
	for c := range r.long {
		if c.done == event && c.key == key {
			c.seen = true
		}
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
	var event Envelope[json.RawMessage]
	if len(payload) != 1 || json.Unmarshal(payload[0], &event) != nil ||
		event.Schema != eventSchema || event.Client || event.Sequence == 0 || event.Sequence >= 1<<53 ||
		event.ConfigRevision >= 1<<53 || len(event.Payload) == 0 {
		r.logf("dropping event %s: invalid envelope", userText(name))
		return
	}
	if id, err := strconv.ParseUint(event.OperationID, 10, 64); err != nil || id == 0 || strconv.FormatUint(id, 10) != event.OperationID {
		r.logf("dropping event %s: invalid operation ID", userText(name))
		return
	}
	r.relayPayload(name, event)
}

func (r *RemoteBackend) relayPayload(name string, event Envelope[json.RawMessage]) {
	decode, known := relayedEvents[name]
	if info, ok := eventClass[name]; !known || !ok || info.Class != classRemote {
		r.logf("dropping event %q, which this window does not take from a server", userText(name))
		return
	}
	var item any
	if decode == nil && string(event.Payload) != "null" {
		r.logf("dropping event %s: unexpected payload", name)
		return
	}
	if decode != nil {
		var err error
		item, err = decode(r, event.Payload)
		if err != nil {
			r.logf("dropping event %s: its payload cannot be decoded", name)
			return
		}
		if longDoneEvents[name] && !event.Client {
			r.sawDone(name, item)
		}
	}
	if event.Client {
		op := r.clientApp.newOperation()
		op.client = true
		emit(op, name, item)
		return
	}
	r.clientApp.transport().Emit(name, Envelope[any]{EventMetadata: event.EventMetadata, Payload: item})
}

// longDoneEvents end a long call (goLong).
var longDoneEvents = map[string]bool{"clone:done": true, "pull:done": true, "pull:alldone": true, "move:done": true, "mirror:setup:done": true}

// Payloads that this build emits as maps, as structs with the same keys.
type fetchEventDTO struct {
	Source       string `json:"source"`
	Repo         string `json:"repo"`
	Error        string `json:"error,omitempty"`
	Code         string `json:"code,omitempty"`
	Host         string `json:"host,omitempty"`
	Skipped      bool   `json:"skipped,omitempty"`
	RefusedFast  bool   `json:"refusedFast,omitempty"`
	UpstreamGone bool   `json:"upstreamGone,omitempty"`
}

type fetchDoneDTO struct {
	fetchEventDTO
	RepairWarnings []string `json:"repairWarnings,omitzero"`
}

type fetchAllDoneDTO struct {
	Error string `json:"error,omitempty"`
}

type discoverDoneDTO struct {
	Problem    *ProblemDTO      `json:"problem,omitempty"`
	AccountKey string           `json:"accountKey"`
	Error      string           `json:"error,omitempty"`
	Repos      []DiscoverResult `json:"repos,omitempty"`
}

type mirrorDiscoverProgressDTO struct {
	Phase   string `json:"phase"`
	Account string `json:"account"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

type mirrorDiscoverDoneDTO struct {
	Error   string                   `json:"error,omitempty"`
	Results []mirror.DiscoveryResult `json:"results,omitempty"`
}

type networkStateDTO struct {
	Offline bool     `json:"offline"`
	Hosts   []string `json:"hosts"`
}

type repoRunDTO struct {
	Run    string `json:"run,omitempty"`
	Source string `json:"source"`
	Repo   string `json:"repo"`
}

type cloneProgressDTO struct {
	Run     string `json:"run,omitempty"`
	Source  string `json:"source"`
	Repo    string `json:"repo"`
	Phase   string `json:"phase"`
	Percent int    `json:"percent"`
}

// cloneDoneDTO is clone:done: the path of the new clone, or why it failed,
// with what heal.Repo fixed or warned about, or the unreachable server
// (unreachableDetails).
type cloneDoneDTO struct {
	Run      string   `json:"run,omitempty"`
	Source   string   `json:"source"`
	Repo     string   `json:"repo"`
	Path     string   `json:"path,omitempty"`
	Error    string   `json:"error,omitempty"`
	Code     string   `json:"code,omitempty"`
	Host     string   `json:"host,omitempty"`
	Skipped  bool     `json:"skipped,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Fixes    []string `json:"fixes,omitempty"`
}

// pullDoneDTO is pull:done. Skipped is a pull skip reason, or true for an
// unreachable server.
type pullDoneDTO struct {
	Run            string     `json:"run,omitempty"`
	BehindBefore   int        `json:"behindBefore"`
	Unchanged      bool       `json:"unchanged,omitempty"`
	RepairWarnings []string   `json:"repairWarnings,omitzero"`
	Source         string     `json:"source"`
	Repo           string     `json:"repo"`
	Error          string     `json:"error,omitempty"`
	Code           string     `json:"code,omitempty"`
	Host           string     `json:"host,omitempty"`
	Skipped        skipReason `json:"skipped,omitzero"`
	State          string     `json:"state,omitempty"`
	Modified       *int       `json:"modified,omitempty"`
	Untracked      *int       `json:"untracked,omitempty"`
	Ahead          *int       `json:"ahead,omitempty"`
	Behind         *int       `json:"behind,omitempty"`
}

// skipReason holds what pull:done says in skipped: true, false or an
// identifier such as "settingsChanged". Anything else does not decode.
type skipReason struct{ v any }

func (s skipReason) IsZero() bool { return s.v == nil }

func (s *skipReason) UnmarshalJSON(b []byte) error {
	var flag bool
	if json.Unmarshal(b, &flag) == nil {
		s.v = flag
		return nil
	}
	var reason string
	if err := json.Unmarshal(b, &reason); err != nil {
		return err
	}
	if reason == "" || identifier(reason) != reason {
		return fmt.Errorf("skipped is not an identifier")
	}
	s.v = reason
	return nil
}

func (s skipReason) MarshalJSON() ([]byte, error) {
	if s.v == nil {
		return []byte("false"), nil
	}
	return json.Marshal(s.v)
}

// payloadDecoder decodes one relayed payload; nil means the event carries
// none.
type payloadDecoder func(r *RemoteBackend, raw json.RawMessage) (any, error)

// relayedEvents are the events a remote window takes from its server: the
// P4 and P6 events of eventClass. TestRelayedEventsAreRemote keeps the two in step.
var relayedEvents = map[string]payloadDecoder{
	"status:one": func(_ *RemoteBackend, raw json.RawMessage) (any, error) {
		var row StatusResult
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		row.Error = userText(row.Error)
		return row, nil
	},
	"status:batch": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var batch StatusBatch
		if err := json.Unmarshal(raw, &batch); err != nil {
			return nil, err
		}
		batch.Items = capList(r, "status:batch", batch.Items, 8)
		return batch, nil
	},
	"status:updated": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var list []StatusResult
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		return capList(r, "status:updated", list, maxRemoteRepos), nil
	},
	"fetch:start": typedPayload[fetchEventDTO],
	"fetch:done": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var done fetchDoneDTO
		if err := json.Unmarshal(raw, &done); err != nil {
			return nil, err
		}
		done.RepairWarnings = remoteRepairNotes(r, done.RepairWarnings)
		return keepSentKeys(raw, done)
	},
	"fetch:allstart":           nil,
	"fetch:alldone":            typedPayload[fetchAllDoneDTO],
	"credentials:updated":      typedPayload[map[string]CredentialStatus],
	"discover:done":            typedPayload[discoverDoneDTO],
	"mirror:status":            typedPayload[[]MirrorStatusResult],
	"mirror:discover:progress": typedPayload[mirrorDiscoverProgressDTO],
	"mirror:discover:done":     typedPayload[mirrorDiscoverDoneDTO],
	"pr:refreshed":             typedPayload[PRAccountUpdateDTO],
	"network:state":            typedPayload[networkStateDTO],
	// The repository mutations (#295). Progress keeps the server's
	// coalescing: at most one per operation every rpc.ProgressInterval.
	"clone:start":    typedPayload[repoRunDTO],
	"clone:progress": typedPayload[cloneProgressDTO],
	"clone:done": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var done cloneDoneDTO
		if err := json.Unmarshal(raw, &done); err != nil {
			return nil, err
		}
		done.Warnings = capList(r, "clone warnings", done.Warnings, maxRemoteNotes)
		done.Fixes = capList(r, "clone fixes", done.Fixes, maxRemoteNotes)
		return keepSentKeys(raw, done)
	},
	"pull:start": typedPayload[repoRunDTO],
	"pull:allstart": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var start pullAllStartDTO
		if err := json.Unmarshal(raw, &start); err != nil {
			return nil, err
		}
		if identifier(start.Run) == "" || start.Pulls == nil || start.Clones == nil {
			return nil, fmt.Errorf("invalid fleet pull scope")
		}
		start.Pulls = capList(r, "pulls", start.Pulls, maxRemoteRepos)
		start.Clones = capList(r, "clones", start.Clones, maxRemoteRepos)
		return start, nil
	},
	"pull:alldone": typedPayload[pullAllDoneDTO],
	"pull:done": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var done pullDoneDTO
		if err := json.Unmarshal(raw, &done); err != nil {
			return nil, err
		}
		done.RepairWarnings = remoteRepairNotes(r, done.RepairWarnings)
		return keepSentKeys(raw, done)
	},
	"mirror:setup:done": typedPayload[MirrorSetupResult],
	"move:progress":     typedPayload[MoveProgressEventDTO],
	"move:done": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var done MoveResultDTO
		if err := json.Unmarshal(raw, &done); err != nil {
			return nil, err
		}
		done.Warnings = capList(r, "move warnings", done.Warnings, maxRemoteNotes)
		return done, nil
	},
	"reconcile:done": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var res ReconcileDTO
		if err := json.Unmarshal(raw, &res); err != nil {
			return nil, err
		}
		capReconcile(r, &res)
		return res, nil
	},
	// The host's AI harnesses, detected on the host (#243). A window runs
	// only the catalog's (harness.RemoteCommand), whatever this list says.
	"harnesses:updated": func(r *RemoteBackend, raw json.RawMessage) (any, error) {
		var list []config.AIHarnessEntry
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		return capList(r, "harnesses:updated", list, maxRemoteHarnesses), nil
	},
}

// typedPayload decodes raw into T and encodes it again. For an object it
// keeps only the keys the server sent that T knows, so a key the emitting
// side leaves out stays out.
func typedPayload[T any](_ *RemoteBackend, raw json.RawMessage) (any, error) {
	var typed T
	if err := json.Unmarshal(raw, &typed); err != nil {
		return nil, err
	}
	return keepSentKeys(raw, typed)
}

// keepSentKeys encodes typed, the decoded raw, keeping only the keys raw
// had.
func keepSentKeys(raw json.RawMessage, typed any) (any, error) {
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
	maxRemoteRepos     = 10_000
	maxRemoteAccounts  = 1_000
	maxRemoteHarnesses = 1_000
	// maxRemoteNotes caps the warnings and fixes of one clone or move.
	maxRemoteNotes = 100
)

func remoteRepairNotes(r *RemoteBackend, notes []string) []string {
	notes = capList(r, "repair notes", notes, maxRemoteNotes)
	for i, note := range notes {
		notes[i] = userText(note)
	}
	return notes
}

// capReconcile cuts every list of a reconcile pass to maxRemoteRepos.
func capReconcile(r *RemoteBackend, res *ReconcileDTO) {
	res.Repairs = capList(r, "clone repairs", res.Repairs, maxRemoteRepos)
	for i := range res.Repairs {
		row := &res.Repairs[i]
		row.Fixed = remoteRepairNotes(r, row.Fixed)
		row.Warnings = remoteRepairNotes(r, row.Warnings)
	}
	res.Unmatched = capList(r, "unmatched clones", res.Unmatched, maxRemoteRepos)
	res.Skipped = capList(r, "skipped clones", res.Skipped, maxRemoteRepos)
	res.Ignored = capList(r, "ignored clones", res.Ignored, maxRemoteRepos)
	res.Incomplete = capList(r, "incomplete clones", res.Incomplete, maxRemoteRepos)
	res.WorkspaceChoices = capList(r, "workspace choices", res.WorkspaceChoices, maxRemoteRepos)
	res.Placeholders = capList(r, "workspace folders", res.Placeholders, maxRemoteRepos)
	res.WorkspaceMembers = capList(r, "workspace members", res.WorkspaceMembers, maxRemoteRepos)
	res.WorkspaceUnknown = capList(r, "unknown workspaces", res.WorkspaceUnknown, maxRemoteRepos)
	res.WorkspaceDiagnostics = capList(r, "workspace diagnostics", res.WorkspaceDiagnostics, maxRemoteRepos)
}

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
	dto.PathConflicts = capList(r, "folder conflicts", dto.PathConflicts, maxRemoteRepos)
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
//
// A method in remoteUnbounded has no deadline of its own.
func (r *RemoteBackend) invoke(method string, result any, args ...any) error {
	return r.invokeContext(context.Background(), method, result, args...)
}

func (r *RemoteBackend) invokeContext(ctx context.Context, method string, result any, args ...any) error {
	client := r.current()
	if client == nil {
		return &remoteError{remoteNotConnected, "not connected to the remote"}
	}
	info, _ := classOf(method)
	if r.refuseUnserved && info.Mutating && !servedSet[method] {
		return &remoteError{remoteReadOnly, method + " is not available on a remote yet"}
	}
	cancel := func() {}
	if !remoteUnbounded[method] {
		ctx, cancel = context.WithTimeout(ctx, remoteCallTimeout(method))
	}
	defer cancel()
	var opts []rpc.CallOption
	if info.Mutating {
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
