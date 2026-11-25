package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/cmd/gui/serverbin"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/remote"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// Remote mode on the client (#233, #239). A remote window is its own
// process, GitboxApp --remote <alias> --experimental-remote: it loads only
// the client preferences, shows its window at once, and connects from
// domReady through pkg/remote to a gitbox-server it installs and verifies.
// Once connected it shows the host's data read-only (P4, #240). The main window lists
// the remotes in Settings, adds and removes them, uninstalls the server and
// starts the remote windows.

// The states of a remote window, as remote:state reports them.
const (
	remoteConnecting   = "connecting"
	remoteInstalling   = "installing"
	remoteConnected    = "connected"
	remoteFailed       = "failed"
	remoteDisconnected = "disconnected"
)

// remoteConnectTimeout bounds one connect, install included.
const remoteConnectTimeout = 3 * time.Minute

// remoteOpTimeout bounds uninstall, reinstall and doctor.
const remoteOpTimeout = 2 * time.Minute

// RemoteStateDTO is the state of a remote window: the remote:state payload
// and what RemoteInfo returns. Code and Hint say why it failed; the view
// shows the translated hint, and the code appears only in the log.
type RemoteStateDTO struct {
	State         string `json:"state"`
	Code          string `json:"code,omitempty"`
	Hint          string `json:"hint,omitempty"`
	Detail        string `json:"detail,omitempty"`
	Alias         string `json:"alias"`
	ServerOS      string `json:"serverOS,omitempty"`
	ServerArch    string `json:"serverArch,omitempty"`
	ServerVersion string `json:"serverVersion,omitempty"`
	// Truncated says the server sent a list longer than a window takes, so
	// what it shows is incomplete (#240).
	Truncated bool `json:"truncated,omitempty"`
	// Waiting says, while connecting, that ssh has not logged in after a
	// few seconds: "agent", or "1password" when the alias uses 1Password's
	// agent, whose approval request shows only in its menu bar or tray icon.
	Waiting string `json:"waiting,omitempty"`
	// RemoteEditors are the editor commands that open folders of the host
	// (pkg/remote.RemoteEditors); the window disables every other (#243).
	RemoteEditors []string `json:"remoteEditors"`
	// Editor is the editor chosen for this remote in its entry, empty for
	// Auto: the window then enables only that editor (#287).
	Editor string `json:"editor,omitempty"`
	// Attempt and Attempts say, while connecting again after the link to
	// the host's daemon dropped, which attempt this is (#247).
	Attempt  int `json:"attempt,omitempty"`
	Attempts int `json:"attempts,omitempty"`
	// Resumed says, once connected, that the connection resumed the
	// host's daemon where the last one left, with every event it missed:
	// the window keeps what it shows and refreshes status instead of
	// reloading everything (#247).
	Resumed bool `json:"resumed,omitempty"`
}

// RemoteResultDTO is the outcome of a remote action. Empty Error means it
// worked; Hint is the catalog key that explains a failure.
type RemoteResultDTO struct {
	Error string `json:"error,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// RemoteUninstallDTO is the outcome of RemoteUninstall. When NeedsConfirm is
// set, nothing was deleted: OtherClients other clients have a live server
// there, and the caller asks before it calls again with force.
type RemoteUninstallDTO struct {
	RemoteResultDTO
	NeedsConfirm bool `json:"needsConfirm"`
	OtherClients int  `json:"otherClients"`
	Done         bool `json:"done"`
	RootKept     bool `json:"rootKept"`
}

// RemoteDoctorDTO is the outcome of RemoteRunDoctor.
type RemoteDoctorDTO struct {
	RemoteResultDTO
	Report *serverDoctorReport `json:"report,omitempty"`
}

// RemoteEntryDTO is one configured remote.
type RemoteEntryDTO struct {
	Alias     string `json:"alias"`
	ServerDir string `json:"serverDir,omitempty"`
	// Editor is the editor chosen for this remote, empty for Auto (#287).
	Editor string `json:"editor,omitempty"`
	// Open says a window of this remote is running on this machine, and
	// State is the connection state it last recorded (#330).
	Open  bool   `json:"open"`
	State string `json:"state,omitempty"`
	// Waiting is what its login waits for while it connects: "agent" or
	// "1password" when the ssh agent may be asking for approval.
	Waiting string `json:"waiting,omitempty"`
	// The last-known facts of the host, from its last connection (#330).
	ServerOS      string `json:"serverOS,omitempty"`
	ServerArch    string `json:"serverArch,omitempty"`
	ServerVersion string `json:"serverVersion,omitempty"`
	LastConnected string `json:"lastConnected,omitempty"`
}

// RemotesDTO is what the host switcher and the Remote hosts window show
// (#330). Enabled is false unless remote mode is on, by the flag or the
// experimental_remote key. Current is the alias this window shows, empty in
// the local window.
type RemotesDTO struct {
	Enabled bool             `json:"enabled"`
	Current string           `json:"current,omitempty"`
	Remotes []RemoteEntryDTO `json:"remotes"`
	// RemoteEditors are the editor commands a remote can be set to
	// (pkg/remote.RemoteEditors).
	RemoteEditors []string `json:"remoteEditors"`
}

// remoteConn is a started gitbox-server: what a dialer returns.
type remoteConn interface {
	Conn() io.ReadWriteCloser
	Done() <-chan struct{}
	Err() error
	Close() error
}

// remoteDialer starts the server on alias. dialSSH is the real one; tests
// replace it.
type remoteDialer func(ctx context.Context, alias string, o remote.Options) (remoteConn, error)

// dialSSH checks that this build can run remote mode, then starts the
// server over the system ssh.
func dialSSH(ctx context.Context, alias string, o remote.Options) (remoteConn, error) {
	if err := thisServerBuild().check(); err != nil {
		return nil, err
	}
	s, err := remote.Dial(ctx, alias, o)
	if err != nil {
		return nil, err
	}
	o.Log.Printf("probe: host %s/%s, home %s, banner %d bytes", s.Probe().OS, s.Probe().Arch, s.Probe().Home, s.Probe().Banner)
	return s, nil
}

// serverTarget hands pkg/remote the embedded server for a host.
func serverTarget(goos, goarch string) (remote.Target, error) {
	t, ok := serverbin.Lookup(goos, goarch)
	if !ok {
		return remote.Target{}, &remote.Error{Code: remote.CodeServerTargetMissing,
			Detail: fmt.Sprintf("this GitboxApp carries no gitbox-server for %s/%s", goos, goarch)}
	}
	return remote.Target{SHA256: t.SHA256, Size: t.Size, Load: func() ([]byte, error) { return serverbin.Binary(t) }}, nil
}

// newClientID returns a random id for this client process.
func newClientID() string { return "c" + strings.ReplaceAll(newRequestID(), "-", "")[:16] }

// remoteSession is the connection of a remote window.
type remoteSession struct {
	a        *App
	alias    string
	clientID string
	log      *remote.Log
	backend  *RemoteBackend
	dial     remoteDialer

	mu     sync.Mutex
	state  RemoteStateDTO
	conn   remoteConn
	probe  remote.Probe // what the host said before the server started
	gen    int          // each connect and disconnect starts a new generation
	cancel context.CancelFunc
	// policy paces the attempts after the link to a daemon dropped.
	policy remote.ReconnectPolicy
	// presence tells the other processes of this machine that this
	// window is open and how its connection is (#330). Nil when it could
	// not be taken.
	presence *config.WindowPresence
}

func newRemoteSession(a *App, alias string) *remoteSession {
	s := &remoteSession{a: a, alias: alias, clientID: newClientID(), dial: dialSSH, policy: remote.DefaultReconnect}
	s.log = remote.NewLog(func(line string) { emit(a.newOperation(), "remote:log", userText(line)) })
	s.backend = newRemoteBackend(a, s.log)
	s.backend.onTruncate = s.noteTruncated
	s.backend.refuseUnserved = true // the unserved mutations (#294)
	s.state = RemoteStateDTO{State: remoteDisconnected, Alias: alias}
	return s
}

// options are the pkg/remote options of this remote, read from the client
// preferences now, so an edit of server_dir applies to the next connect.
func (a *App) remoteOptions(alias, clientID string, log *remote.Log) (remote.Options, error) {
	entry, ok := findRemote(a.clientPrefs().Get().Remotes, alias)
	if !ok {
		return remote.Options{}, &remoteError{remoteUnknownAlias, fmt.Sprintf("no remote named %s is configured", alias)}
	}
	o := remote.Options{
		ServerDir: entry.ServerDir, ClientID: clientID, Version: version, Commit: commit,
		Target: serverTarget, Log: log,
		ControlDir: filepath.Join(config.ConfigRoot(), config.V2ConfigDir),
		// Every session goes through the host's daemon (#247).
		Daemon: true,
	}
	return o, nil
}

func findRemote(remotes []config.RemoteEntry, alias string) (config.RemoteEntry, bool) {
	for _, r := range remotes {
		if r.Alias == alias {
			return r, true
		}
	}
	return config.RemoteEntry{}, false
}

// set changes the state, unless gen is stale, and reports it.
func (s *remoteSession) set(gen int, change func(*RemoteStateDTO)) {
	editor := s.chosenEditor()
	s.mu.Lock()
	if gen != s.gen {
		s.mu.Unlock()
		return
	}
	change(&s.state)
	s.state.RemoteEditors = remote.RemoteEditorCommands()
	s.state.Editor = editor
	st := s.state
	s.mu.Unlock()
	_ = s.presence.SetState(config.PresenceState{State: st.State, Waiting: st.Waiting})
	emit(s.a.newOperation(), "remote:state", st)
}

// holdPresence marks this window open for the other processes of this
// machine (#330). A failure only costs the other windows that knowledge.
func (s *remoteSession) holdPresence(dir string) {
	p, err := config.HoldWindow(dir, s.alias)
	if err != nil {
		s.log.Printf("window: presence not recorded: %s", userMsg(err))
		return
	}
	s.presence = p
	st := s.info()
	_ = p.SetState(config.PresenceState{State: st.State, Waiting: st.Waiting})
}

// rememberFacts saves what the host reported to this remote's entry, for
// the Remote hosts window (#330). A failed save is only logged.
func (s *remoteSession) rememberFacts(st RemoteStateDTO) {
	f := config.RemoteFacts{ServerOS: st.ServerOS, ServerArch: st.ServerArch, ServerVersion: st.ServerVersion, LastConnected: time.Now()}
	if err := s.a.clientPrefs().Update(func(p *config.ClientPrefs) { p.SetRemoteFacts(s.alias, f) }); err != nil {
		s.log.Printf("connect: the host's facts were not saved: %s", userMsg(err))
	}
}

// noteTruncated marks the state incomplete, once per connection.
func (s *remoteSession) noteTruncated() {
	s.mu.Lock()
	if s.state.Truncated {
		s.mu.Unlock()
		return
	}
	s.state.Truncated = true
	st := s.state
	s.mu.Unlock()
	emit(s.a.newOperation(), "remote:state", st)
}

func (s *remoteSession) info() RemoteStateDTO {
	editor := s.chosenEditor()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.RemoteEditors = remote.RemoteEditorCommands()
	st.Editor = editor
	return st
}

// chosenEditor is the editor of this window's remote entry, empty for Auto
// or when the entry is gone. The preferences re-read the shared file, so a
// choice made in the main window reaches an open remote window.
func (s *remoteSession) chosenEditor() string {
	if e, ok := findRemote(s.a.clientPrefs().Get().Remotes, s.alias); ok {
		return e.Editor
	}
	return ""
}

// waitingFor is the Waiting value of a slow login with agent.
func waitingFor(agent string) string {
	if agent == remote.AgentOnePassword {
		return "1password"
	}
	return "agent"
}

// failure is the code, hint and text of a connection failure.
func remoteFailure(err error) (code, hint, detail string) {
	ue := toUserError(err)
	code = strings.TrimPrefix(ue.Code, "remote.")
	switch {
	case code == string(rpc.ErrCodeVersionMismatch), code == string(rpc.ErrCodeBannerOverflow):
	case strings.HasPrefix(ue.Code, "remote."):
	default:
		code = string(remoteServerCrashed)
	}
	return code, "error.remote." + code, ue.String()
}

// start connects, unless a connection is already up or under way.
func (s *remoteSession) start() {
	s.mu.Lock()
	switch s.state.State {
	case remoteConnecting, remoteInstalling, remoteConnected:
		s.mu.Unlock()
		return
	}
	s.gen++
	gen := s.gen
	ctx, cancel := context.WithTimeout(context.Background(), remoteConnectTimeout)
	s.cancel = cancel
	s.mu.Unlock()
	s.set(gen, func(st *RemoteStateDTO) {
		*st = RemoteStateDTO{State: remoteConnecting, Alias: s.alias}
	})
	go s.run(ctx, cancel, gen)
}

func (s *remoteSession) run(ctx context.Context, cancel context.CancelFunc, gen int) {
	defer cancel()
	if err := s.connectOnce(ctx, gen); err != nil {
		s.fail(gen, "connect: failed", err)
	}
}

// fail reports a connection that failed or ended, with what says why.
func (s *remoteSession) fail(gen int, what string, err error) {
	code, hint, detail := remoteFailure(err)
	s.log.Printf("%s: %s (%s)", what, detail, code)
	s.set(gen, func(st *RemoteStateDTO) {
		*st = RemoteStateDTO{State: remoteFailed, Code: code, Hint: hint, Detail: detail, Alias: s.alias}
	})
}

// connectOnce starts the server, attaches the backend and watches the
// connection. A stale gen means a disconnect came meanwhile: the new
// connection is closed, and that is no failure.
func (s *remoteSession) connectOnce(ctx context.Context, gen int) error {
	s.log.Printf("connect: %s, client %s, build %s", s.alias, s.clientID, formatAppVersion(version, commit))
	o, err := s.a.remoteOptions(s.alias, s.clientID, s.log)
	if err != nil {
		return err
	}
	o.OnInstall = func() { s.set(gen, func(st *RemoteStateDTO) { st.State = remoteInstalling; st.Waiting = "" }) }
	o.OnSlowLogin = func(agent string) {
		s.set(gen, func(st *RemoteStateDTO) {
			if st.State == remoteConnecting {
				st.Waiting = waitingFor(agent)
			}
		})
	}
	conn, err := s.dial(ctx, s.alias, o)
	if err != nil {
		return err
	}
	// The handshake is bounded by the same deadline.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	rwc := conn.Conn()
	err = s.backend.connect(rwc, rwc)
	stopped := stop()
	if err != nil || !stopped {
		_ = conn.Close()
		if err == nil {
			err = &remote.Error{Code: remote.CodeSSHTimeout, Detail: "the server did not answer in time"}
		}
		return err
	}
	s.mu.Lock()
	if gen != s.gen {
		s.mu.Unlock()
		s.backend.detach()
		_ = conn.Close()
		return nil
	}
	s.conn = conn
	s.probe = remote.Probe{}
	if p, ok := conn.(interface{ Probe() remote.Probe }); ok {
		s.probe = p.Probe()
	}
	s.mu.Unlock()
	s.backend.Init(ctx)
	hello := s.backend.Hello()
	s.log.Printf("connect: connected to %s %s/%s, build %s", s.alias, userText(hello.OS), userText(hello.Arch), s.backend.ServerVersion())
	s.logSession(hello)
	outcome, had := s.backend.resumeOutcome()
	connected := RemoteStateDTO{State: remoteConnected, Alias: s.alias,
		ServerOS: userText(hello.OS), ServerArch: userText(hello.Arch), ServerVersion: s.backend.ServerVersion(),
		Resumed: had && outcome.Resumed && !outcome.Gap}
	s.set(gen, func(st *RemoteStateDTO) { *st = connected })
	s.rememberFacts(connected)
	s.a.syncTaskInterval()
	go s.watch(gen, conn)
	return nil
}

// logSession says in the connection log how the host serves this session
// (#247): through its daemon, resumed or not, or in a process of its own.
func (s *remoteSession) logSession(hello rpc.Hello) {
	switch {
	case hello.Fallback != "":
		s.log.Printf("connect: the host serves this session without the daemon (%s): work stops if the link drops", userText(hello.Fallback))
		return
	case hello.Daemon == nil:
		return
	case hello.Daemon.Resumed:
		s.log.Printf("connect: joined the host's daemon")
	default:
		s.log.Printf("connect: the host started its daemon")
	}
	switch outcome, had := s.backend.resumeOutcome(); {
	case outcome.Resumed && outcome.Gap:
		s.log.Printf("connect: resumed with %d missed events; older ones were no longer kept, and the window reloads everything", outcome.Replayed)
	case outcome.Resumed:
		s.log.Printf("connect: resumed with %d missed events", outcome.Replayed)
	case had:
		s.log.Printf("connect: the host's daemon restarted; what it did meanwhile is not replayed, and the window reloads everything")
	}
}

// hostSchedules reports whether the host's daemon runs the fleet checks of
// this window (#247): the window is connected to one.
func (s *remoteSession) hostSchedules() bool {
	return s.connected() && s.backend.isPersistent()
}

// pushSchedule sends the periodic_sync preference to the host's daemon.
func (s *remoteSession) pushSchedule(interval string) {
	if err := s.backend.SetFleetSchedule(interval); err != nil {
		s.log.Printf("schedule: the host did not take the interval: %s", userMsg(err))
	}
}

// watch turns the end of a connection that nobody closed into a failure.
// The end is the ssh session ending, or the link dying: its reader ended,
// or the heartbeat declared it dead. After the link to a daemon dropped it
// first connects again, up to the policy's attempts: the daemon kept
// running what the window started, and the new connection gets what the
// window missed.
func (s *remoteSession) watch(gen int, conn remoteConn) {
	linkDead := s.backend.linkDone()
	select {
	case <-conn.Done():
	case <-linkDead:
	}
	// From here until the next connection to a daemon the window has none,
	// and its calls wait for the next one instead of failing. A Disconnect
	// that came first wins: it moved to another generation.
	daemon := s.backend.isPersistent()
	s.mu.Lock()
	current := gen == s.gen
	if current {
		s.conn = nil
		if daemon {
			s.backend.setReconnecting(true)
		}
	}
	s.mu.Unlock()
	if !current {
		return
	}
	if !daemon {
		err := endError(conn, linkEndGrace)
		s.backend.detach()
		_ = conn.Close()
		s.backend.markLost()
		s.fail(gen, "connect: the connection ended", err)
		return
	}
	// The window says so at once, before it lets the connection go and
	// waits for the first attempt.
	attempts := s.policy.Attempts()
	s.set(gen, func(st *RemoteStateDTO) {
		*st = RemoteStateDTO{State: remoteConnecting, Alias: s.alias, Attempt: 1, Attempts: attempts}
	})
	// A link the heartbeat declared dead may still have its ssh running:
	// closing it may wait for ssh to end, which must not delay the attempts.
	go func() { _ = conn.Close() }()
	s.backend.detach()
	code, _, detail := remoteFailure(endError(conn, 0))
	s.log.Printf("connect: the link dropped: %s (%s); connecting again", detail, code)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.mu.Lock()
	if gen != s.gen {
		s.mu.Unlock()
		return
	}
	s.cancel = cancel
	s.mu.Unlock()
	err := s.policy.Run(ctx, func(ctx context.Context, n int) error {
		if n > 1 {
			s.set(gen, func(st *RemoteStateDTO) {
				*st = RemoteStateDTO{State: remoteConnecting, Alias: s.alias, Attempt: n, Attempts: attempts}
			})
		}
		actx, acancel := context.WithTimeout(ctx, remoteConnectTimeout)
		defer acancel()
		err := s.connectOnce(actx, gen)
		if err != nil {
			s.log.Printf("connect: attempt %d of %d failed: %s", n, attempts, userMsg(err))
		}
		return err
	})
	s.mu.Lock()
	stale := gen != s.gen
	s.mu.Unlock()
	if err == nil || stale {
		return
	}
	s.backend.markLost()
	s.fail(gen, "connect: gave up connecting again", err)
}

// linkEndGrace is how long the end of a link waits for its ssh session to
// end too, before it counts as a host that stopped answering.
const linkEndGrace = 2 * time.Second

// endError is why a connection ended, waiting up to wait for its session
// to end. The session's own error is meaningful only once it ended; a link
// that died while its ssh still runs stopped answering.
func endError(conn remoteConn, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-conn.Done():
	case <-timer.C:
		return &remote.Error{Code: remote.CodeSSHTimeout, Detail: "the host stopped answering"}
	}
	if err := conn.Err(); err != nil {
		return err
	}
	return &remoteError{remoteServerCrashed, "the server ended the connection"}
}

// disconnect ends the connection, or the connect under way.
func (s *remoteSession) disconnect() {
	s.mu.Lock()
	s.gen++
	gen := s.gen
	conn, cancel := s.conn, s.cancel
	s.conn, s.cancel = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.backend.setReconnecting(false)
	s.backend.detach()
	if conn != nil {
		_ = conn.Close()
		s.log.Printf("disconnect: done")
	}
	s.set(gen, func(st *RemoteStateDTO) { *st = RemoteStateDTO{State: remoteDisconnected, Alias: s.alias} })
}

// ─── Bound methods (all LOCAL, see classification.go) ────────────────────

func resultOf(err error) RemoteResultDTO {
	if err == nil {
		return RemoteResultDTO{}
	}
	ue := toUserError(err)
	hint := ue.Hint
	if code, ok := strings.CutPrefix(ue.Code, "remote."); ok {
		hint = "error.remote." + code
	}
	return RemoteResultDTO{Error: ue.String(), Hint: hint}
}

var errNotRemoteWindow = errors.New("this window does not show a remote")

// RemoteInfo is the state of this remote window.
func (a *App) RemoteInfo() RemoteStateDTO {
	if a.remote == nil {
		return RemoteStateDTO{State: remoteDisconnected}
	}
	return a.remote.info()
}

// RemoteLog is the connection log of this remote window, oldest line first.
func (a *App) RemoteLog() []string {
	if a.remote == nil {
		return []string{}
	}
	lines := a.remote.log.Lines()
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = userText(l)
	}
	return out
}

// RemoteDisconnect ends this window's connection.
func (a *App) RemoteDisconnect() {
	if a.remote != nil {
		a.remote.disconnect()
	}
}

// RemoteReconnect connects again after a disconnect or a failure.
func (a *App) RemoteReconnect() {
	if a.remote != nil {
		a.remote.start()
	}
}

// RemoteReinstall deletes this build's folder on the host and connects
// again, which installs it anew. The window asks before it calls this.
func (a *App) RemoteReinstall() RemoteResultDTO {
	s := a.remote
	if s == nil {
		return resultOf(errNotRemoteWindow)
	}
	s.disconnect()
	err := thisServerBuild().check()
	if err == nil {
		var o remote.Options
		if o, err = a.remoteOptions(s.alias, s.clientID, s.log); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), remoteOpTimeout)
			err = remote.Reinstall(ctx, s.alias, o)
			cancel()
		}
	}
	if err != nil {
		s.log.Printf("reinstall: failed: %s", userMsg(err))
		return resultOf(err)
	}
	s.start()
	return RemoteResultDTO{}
}

// RemoteUninstall removes the server from a remote. In the remote window of
// that alias it disconnects first. When another client has a live server
// there, it stops and asks, unless force is set.
func (a *App) RemoteUninstall(alias string, force bool) RemoteUninstallDTO {
	log, clientID := remote.NewLog(nil), newClientID()
	if s := a.remote; s != nil && s.alias == alias {
		s.disconnect()
		log, clientID = s.log, s.clientID
	}
	o, err := a.remoteOptions(alias, clientID, log)
	if err != nil {
		return RemoteUninstallDTO{RemoteResultDTO: resultOf(err)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteOpTimeout)
	defer cancel()
	res, err := remote.Uninstall(ctx, alias, o, force)
	if err != nil {
		log.Printf("uninstall: failed: %s", userMsg(err))
		return RemoteUninstallDTO{RemoteResultDTO: resultOf(err)}
	}
	return RemoteUninstallDTO{
		NeedsConfirm: !res.Done && res.OtherClients > 0, OtherClients: res.OtherClients,
		Done: res.Done, RootKept: res.RootKept,
	}
}

// RemoteRunDoctor runs gitbox-server --doctor on this window's remote, over
// an exec of its own, and adds the report to the connection log.
func (a *App) RemoteRunDoctor() RemoteDoctorDTO {
	s := a.remote
	if s == nil {
		return RemoteDoctorDTO{RemoteResultDTO: resultOf(errNotRemoteWindow)}
	}
	err := thisServerBuild().check()
	var raw []byte
	if err == nil {
		var o remote.Options
		if o, err = a.remoteOptions(s.alias, s.clientID, s.log); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), remoteOpTimeout)
			raw, err = remote.Doctor(ctx, s.alias, o)
			cancel()
		}
	}
	if err != nil {
		s.log.Printf("doctor: failed: %s", userMsg(err))
		return RemoteDoctorDTO{RemoteResultDTO: resultOf(err)}
	}
	var report serverDoctorReport
	if err := json.Unmarshal(raw, &report); err != nil {
		s.log.Printf("doctor: the report cannot be read")
		return RemoteDoctorDTO{RemoteResultDTO: resultOf(&remoteError{remoteServerCrashed, "the doctor report cannot be read"})}
	}
	report.sanitize()
	s.log.Printf("doctor: %s %s/%s, git %q, home %s", report.Version, report.OS, report.Arch, report.GitVersion, report.Home)
	s.log.Printf("doctor: config %s, folder writable %v, programs can run %v, %d credential findings",
		report.ConfigPath, report.ConfigDirWritable, report.ConfigDirExec, len(report.CredentialFindings))
	for _, r := range report.Roots {
		s.log.Printf("doctor: root %s: %s", r.Path, r.State)
	}
	if d := report.Daemon; d != nil {
		fallback := d.LastFallback
		if fallback == "" {
			fallback = "none"
		}
		s.log.Printf("doctor: daemon state %s, running %v, last fallback %s", d.State, d.Running, fallback)
	}
	return RemoteDoctorDTO{Report: &report}
}

// sanitize scrubs every string of a report that came from a host.
func (r *serverDoctorReport) sanitize() {
	for _, f := range []*string{&r.Version, &r.Commit, &r.OS, &r.Arch, &r.Home, &r.ConfigPath, &r.GitVersion} {
		*f = userText(*f)
	}
	if len(r.CredentialFindings) > 100 {
		r.CredentialFindings = r.CredentialFindings[:100]
	}
	data, err := json.Marshal(r.CredentialFindings)
	if err != nil || len(data) > 64<<10 {
		r.CredentialFindings = nil
	}
	if len(r.Roots) > 100 {
		r.Roots = r.Roots[:100]
	}
	for i := range r.Roots {
		r.Roots[i].Path = userText(r.Roots[i].Path)
		if !slices.Contains(doctorRootStates, r.Roots[i].State) {
			r.Roots[i].State = doctorRootUnreadable
		}
	}
	if d := r.Daemon; d != nil {
		if !slices.Contains(doctorDaemonStates, d.State) {
			d.State = doctorDaemonUnsafe
		}
		if !slices.Contains(fallbackReasons, d.LastFallback) {
			d.LastFallback = ""
		}
	}
}

// remoteEnabled reports whether remote mode is on: the flag or the
// experimental_remote key.
func (a *App) remoteEnabled() bool {
	return a.experimentalRemote || a.clientPrefs().Get().ExperimentalRemote
}

// prefsPath is the client's own gitbox.json.
func (a *App) prefsPath() string {
	if a.cfgPath != "" {
		return a.cfgPath
	}
	return config.DefaultV2Path()
}

// freshPrefs reads the client preferences from the file, so a remote that
// another window added or removed shows here at once (#330).
func (a *App) freshPrefs() config.ClientPrefs {
	if f, ok := a.clientPrefs().(*filePrefs); ok {
		f.Reload()
		return f.Get()
	}
	if cfg, err := config.Load(a.prefsPath()); err == nil {
		return config.ExtractPrefs(cfg)
	}
	return a.clientPrefs().Get()
}

// ListRemotes is what the host switcher and the Remote hosts window show
// (#330): every configured remote, whether a window of it is open and how
// its connection is, and what the host reported last.
func (a *App) ListRemotes() RemotesDTO {
	a.touchRemotesWindow()
	out := RemotesDTO{Enabled: a.remoteEnabled(), Current: a.remoteAlias, Remotes: []RemoteEntryDTO{}, RemoteEditors: remote.RemoteEditorCommands()}
	dir := config.WindowsDir(a.prefsPath())
	for _, r := range a.freshPrefs().Remotes {
		open, state := config.WindowOpen(dir, r.Alias)
		if r.Alias == a.remoteAlias && a.remote != nil {
			st := a.remote.info()
			open, state = true, config.PresenceState{State: st.State, Waiting: st.Waiting}
		}
		out.Remotes = append(out.Remotes, RemoteEntryDTO{Alias: r.Alias, ServerDir: r.ServerDir, Editor: r.Editor,
			Open: open, State: state.State, Waiting: state.Waiting, ServerOS: r.ServerOS, ServerArch: r.ServerArch, ServerVersion: r.ServerVersion, LastConnected: r.LastConnected})
	}
	return out
}

// SSHHostDTO is one Host line of this computer's ssh configuration: the
// alias that adding it uses, and every name on the line as written (#330).
type SSHHostDTO struct {
	Alias string   `json:"alias"`
	Names []string `json:"names"`
}

// The Remote hosts window hides when it is closed, so it opens again at
// once (#330). While it shows, its view reads ListRemotes every minute; once
// no read has come for remotesIdleTimeout it is hidden, and its process
// ends.
var remotesIdleTimeout = 10 * time.Minute

// quitRemotesWindow ends the Remote hosts window's process. runRemotesWindow
// sets it, since the server build carries no Wails; tests replace it.
var quitRemotesWindow = func(*App) {}

// touchRemotesWindow restarts the idle countdown of the Remote hosts
// window. Other windows have none.
func (a *App) touchRemotesWindow() {
	if a.windowMode != "remotes" {
		return
	}
	a.remotesIdleMu.Lock()
	defer a.remotesIdleMu.Unlock()
	if a.remotesIdle != nil {
		a.remotesIdle.Stop()
	}
	a.remotesIdle = time.AfterFunc(remotesIdleTimeout, func() { quitRemotesWindow(a) })
}

// ListSSHHosts suggests the Host lines of this computer's ssh configuration
// whose alias is not a remote yet, one per line, for the add form of the
// Remote hosts window (#330). It never fails: no configuration means no
// suggestions.
func (a *App) ListSSHHosts() []SSHHostDTO {
	out := []SSHHostDTO{}
	home, err := os.UserHomeDir()
	if err != nil {
		return out
	}
	added := a.freshPrefs().Remotes
	for _, h := range remote.ConfigHosts(home) {
		if _, dup := findRemote(added, h.Alias); !dup {
			out = append(out, SSHHostDTO{Alias: h.Alias, Names: h.Names})
		}
	}
	return out
}

// SetRemoteEditor chooses the editor a remote's window opens folders in
// (#287): the command name of an editor that opens remote folders
// (pkg/remote.RemoteEditors), or empty for Auto. It saves only this
// computer's gitbox.json.
func (a *App) SetRemoteEditor(alias, editor string) RemoteResultDTO {
	editor = strings.TrimSpace(editor)
	if _, ok := findRemote(a.clientPrefs().Get().Remotes, alias); !ok {
		return resultOf(&remoteError{remoteUnknownAlias, fmt.Sprintf("no remote named %s is configured", alias)})
	}
	if editor != "" && (remote.EditorName(editor) != editor || !remote.EditorSupportsRemote(editor)) {
		return resultOf(&remoteError{remoteEditorUnsupported, "this editor cannot open folders on remote hosts"})
	}
	return resultOf(a.clientPrefs().Update(func(p *config.ClientPrefs) {
		for i := range p.Remotes {
			if p.Remotes[i].Alias == alias {
				p.Remotes[i].Editor = editor
			}
		}
	}))
}

// GetRemoteTint is the color that tints remote windows (#331): the saved
// preset, or the default blue.
func (a *App) GetRemoteTint() string {
	return config.RemoteTintOf(a.clientPrefs().Get().RemoteTint)
}

// SetRemoteTint saves the color of remote windows in this computer's
// gitbox.json, never on a host. Only a preset is accepted; the default blue
// is saved as no key at all. Open remote windows pick it up through
// watchRemoteTint.
func (a *App) SetRemoteTint(color string) RemoteResultDTO {
	color = strings.ToLower(strings.TrimSpace(color))
	if !slices.Contains(config.RemoteTintPresets, color) {
		return resultOf(&remoteError{remoteTintUnsupported, "this color is not one of the remote window colors"})
	}
	if color == config.DefaultRemoteTint {
		color = ""
	}
	return resultOf(a.clientPrefs().Update(func(p *config.ClientPrefs) { p.RemoteTint = color }))
}

// remoteTintPoll is how often a remote window looks for a new tint.
var remoteTintPoll = time.Second

// watchRemoteTint tells this remote window's frontend when the tint saved on
// this computer changes, whichever process saved it (#331). It only stats
// the client's gitbox.json each poll, and re-reads it when its time or size
// changed. It ends with ctx.
func (a *App) watchRemoteTint(ctx context.Context) {
	path := a.prefsPath()
	last := a.GetRemoteTint()
	stat := func() (time.Time, int64) {
		if fi, err := os.Stat(path); err == nil {
			return fi.ModTime(), fi.Size()
		}
		return time.Time{}, -1
	}
	mod, size := stat()
	tick := time.NewTicker(remoteTintPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		m, s := stat()
		if m.Equal(mod) && s == size {
			continue
		}
		mod, size = m, s
		if r, ok := a.clientPrefs().(interface{ Reload() }); ok {
			r.Reload()
		}
		if tint := a.GetRemoteTint(); tint != last {
			last = tint
			emit(a.newOperation(), "remote:tint", tint)
		}
	}
}

// AddRemote checks an ssh_config alias with ssh -G, which does not connect,
// and saves it to this machine's gitbox.json. ssh resolves any name, so the
// check catches a broken ssh config, not an unknown host.
func (a *App) AddRemote(alias, serverDir string) RemoteResultDTO {
	if !a.remoteEnabled() {
		return resultOf(&remoteError{remoteNotEnabled, "remote mode is off on this machine"})
	}
	alias, serverDir = strings.TrimSpace(alias), strings.TrimSpace(serverDir)
	remotes := a.clientPrefs().Get().Remotes
	entry := config.RemoteEntry{Alias: alias, ServerDir: serverDir}
	if err := config.ValidateRemotes(append(slices.Clone(remotes), entry)); err != nil {
		return resultOf(err)
	}
	if serverDir != "" {
		if err := remote.ValidateServerDir(serverDir); err != nil {
			return resultOf(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	check := a.checkAlias
	if check == nil {
		check = func(ctx context.Context, alias string) error { return remote.CheckAlias(ctx, alias, remote.Options{}) }
	}
	if err := check(ctx, alias); err != nil {
		return resultOf(err)
	}
	err := a.clientPrefs().Update(func(p *config.ClientPrefs) {
		if _, dup := findRemote(p.Remotes, alias); !dup {
			p.Remotes = append(p.Remotes, entry)
		}
	})
	return resultOf(err)
}

// RemoveRemote forgets a remote. It does not touch the host: Uninstall
// does that.
func (a *App) RemoveRemote(alias string) RemoteResultDTO {
	if _, ok := findRemote(a.clientPrefs().Get().Remotes, alias); !ok {
		return resultOf(&remoteError{remoteUnknownAlias, fmt.Sprintf("no remote named %s is configured", alias)})
	}
	return resultOf(a.clientPrefs().Update(func(p *config.ClientPrefs) {
		p.Remotes = slices.DeleteFunc(p.Remotes, func(r config.RemoteEntry) bool { return r.Alias == alias })
	}))
}

// ConnectRemote opens the window of a remote: a new GitboxApp process,
// spawned like the terminals manager but independent of this one. It gets
// no --parent-pid and this window's shutdown leaves it running. A second
// Connect reaches the single-instance lock of that alias, which focuses the
// open window.
func (a *App) ConnectRemote(alias string) RemoteResultDTO {
	if !a.remoteEnabled() {
		return resultOf(&remoteError{remoteNotEnabled, "remote mode is off on this machine"})
	}
	if _, ok := findRemote(a.clientPrefs().Get().Remotes, alias); !ok {
		return resultOf(&remoteError{remoteUnknownAlias, fmt.Sprintf("no remote named %s is configured", alias)})
	}
	if err := startWindow(remoteWindowArgs(alias, a.testMode, a.cfgPath)); err != nil {
		return resultOf(fmt.Errorf("the remote window could not be started"))
	}
	return RemoteResultDTO{}
}

// ShowRemoteLog brings the open window of a remote to the front with its
// connection log shown (#330). The log lives in that window's process, so
// a remote without an open window has none to show.
func (a *App) ShowRemoteLog(alias string) RemoteResultDTO {
	if !a.remoteEnabled() {
		return resultOf(&remoteError{remoteNotEnabled, "remote mode is off on this machine"})
	}
	if _, ok := findRemote(a.freshPrefs().Remotes, alias); !ok {
		return resultOf(&remoteError{remoteUnknownAlias, fmt.Sprintf("no remote named %s is configured", alias)})
	}
	if open, _ := config.WindowOpen(config.WindowsDir(a.prefsPath()), alias); !open {
		return resultOf(&remoteError{remoteWindowClosed, "this remote has no open window"})
	}
	if err := startWindow(append(remoteWindowArgs(alias, a.testMode, a.cfgPath), showLogFlag)); err != nil {
		return resultOf(fmt.Errorf("the remote window could not be reached"))
	}
	return RemoteResultDTO{}
}

// OpenRemotesWindow opens the Remote hosts window (#330): a process of its
// own, like a remote window, so it outlives the window that opened it. A
// second call reaches its single-instance lock, which focuses it.
func (a *App) OpenRemotesWindow() RemoteResultDTO {
	if !a.remoteEnabled() {
		return resultOf(&remoteError{remoteNotEnabled, "remote mode is off on this machine"})
	}
	if err := startWindow(remotesWindowArgs(a.testMode, a.cfgPath)); err != nil {
		return resultOf(fmt.Errorf("the Remote hosts window could not be started"))
	}
	return RemoteResultDTO{}
}

// ShowLocalWindow brings the local window to the front, or starts it, from
// a remote window or the Remote hosts window (#330): a plain launch reaches
// the local window's single-instance lock.
func (a *App) ShowLocalWindow() RemoteResultDTO {
	var args []string
	if a.testMode {
		args = append(args, "--test-mode")
	}
	if err := startWindow(args); err != nil {
		return resultOf(fmt.Errorf("the local window could not be started"))
	}
	return RemoteResultDTO{}
}

// startWindow starts this binary with args, independent of this process.
// No git.HideWindow: the child is a Wails GUI, and SW_HIDE would start its
// window hidden (see OpenTerminalsManagerWindow).
var startWindow = func(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _, _ = cmd.Process.Wait() }()
	return nil
}

// showLogFlag asks the open window of a remote to show its connection log.
const showLogFlag = "--show-log"

// remotesWindowArgs are the arguments of the Remote hosts window's process.
// In test mode it reads the remotes of the test window that opened it.
func remotesWindowArgs(testMode bool, cfgPath string) []string {
	args := []string{"--remotes-window", "--experimental-remote"}
	if testMode && cfgPath != "" {
		args = append(args, "--test-mode", "--prefs-file="+cfgPath)
	}
	return args
}

// remoteWindowArgs are the arguments of a remote window's process. In test
// mode it reads the remotes from the test config of this window.
func remoteWindowArgs(alias string, testMode bool, cfgPath string) []string {
	args := []string{"--remote", alias, "--experimental-remote"}
	if testMode && cfgPath != "" {
		args = append(args, "--test-mode", "--prefs-file="+cfgPath)
	}
	return args
}

// remoteFlags are the command-line flags of a remote window.
type remoteFlags struct {
	alias        string
	prefsFile    string
	experimental bool
	testMode     bool
	// remotesWindow runs the Remote hosts window (#330).
	remotesWindow bool
	// showLog asks a remote window to show its connection log (#330).
	showLog bool
}

// parseRemoteFlags reads --remote <alias> (or --remote=<alias>),
// --remotes-window, --show-log, --experimental-remote, --test-mode and
// --prefs-file=<path>. It fails on a --remote or --remotes-window without
// --experimental-remote, on both at once, on --show-log without --remote,
// and on --prefs-file without --test-mode and one of the two.
func parseRemoteFlags(args []string) (remoteFlags, error) {
	var f remoteFlags
	remoteSeen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--remote":
			remoteSeen = true
			if i+1 < len(args) {
				f.alias = args[i+1]
				i++
			}
		case strings.HasPrefix(arg, "--remote="):
			remoteSeen = true
			f.alias = strings.TrimPrefix(arg, "--remote=")
		case arg == "--remotes-window":
			f.remotesWindow = true
		case arg == showLogFlag:
			f.showLog = true
		case arg == "--experimental-remote":
			f.experimental = true
		case arg == "--test-mode":
			f.testMode = true
		case strings.HasPrefix(arg, "--prefs-file="):
			f.prefsFile = strings.TrimPrefix(arg, "--prefs-file=")
		}
	}
	switch {
	case remoteSeen && f.alias == "":
		return f, errors.New("--remote needs the ssh_config alias of a remote")
	case remoteSeen && config.ValidateRemotes([]config.RemoteEntry{{Alias: f.alias}}) != nil:
		return f, fmt.Errorf("--remote %q is not a valid ssh_config alias", f.alias)
	case remoteSeen && !f.experimental:
		return f, errors.New("--remote needs --experimental-remote: remote mode is experimental")
	case remoteSeen && f.remotesWindow:
		return f, errors.New("--remote and --remotes-window open different windows")
	case f.remotesWindow && !f.experimental:
		return f, errors.New("--remotes-window needs --experimental-remote: remote mode is experimental")
	case f.showLog && !remoteSeen:
		return f, errors.New("--show-log is only for a remote window (--remote)")
	case f.prefsFile != "" && (!f.testMode || (!remoteSeen && !f.remotesWindow)):
		return f, errors.New("--prefs-file is only for a remote or Remote hosts window in test mode (--remote or --remotes-window, --test-mode)")
	}
	return f, nil
}
