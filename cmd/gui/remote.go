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
// DomReady through pkg/remote to a gitbox-server it installs and verifies.
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
	// RemoteEditors are the editor commands that open folders of the host
	// (pkg/remote.RemoteEditors); the window disables every other (#243).
	RemoteEditors []string `json:"remoteEditors"`
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
}

// RemotesDTO is what the Remotes section of Settings shows. Enabled is false
// unless remote mode is on, by the flag or the experimental_remote key.
type RemotesDTO struct {
	Enabled bool             `json:"enabled"`
	Remotes []RemoteEntryDTO `json:"remotes"`
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
}

func newRemoteSession(a *App, alias string) *remoteSession {
	s := &remoteSession{a: a, alias: alias, clientID: newClientID(), dial: dialSSH}
	s.log = remote.NewLog(func(line string) { a.sink().Emit("remote:log", userText(line)) })
	s.backend = newRemoteBackend(a.sink, s.log)
	s.backend.onTruncate = s.noteTruncated
	s.backend.readOnly = true // until P5 (#240)
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
	s.mu.Lock()
	if gen != s.gen {
		s.mu.Unlock()
		return
	}
	change(&s.state)
	s.state.RemoteEditors = remote.RemoteEditorCommands()
	st := s.state
	s.mu.Unlock()
	s.a.sink().Emit("remote:state", st)
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
	s.a.sink().Emit("remote:state", st)
}

func (s *remoteSession) info() RemoteStateDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.RemoteEditors = remote.RemoteEditorCommands()
	return st
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
	fail := func(err error) {
		code, hint, detail := remoteFailure(err)
		s.log.Printf("connect: failed: %s (%s)", detail, code)
		s.set(gen, func(st *RemoteStateDTO) {
			*st = RemoteStateDTO{State: remoteFailed, Code: code, Hint: hint, Detail: detail, Alias: s.alias}
		})
	}
	s.log.Printf("connect: %s, client %s, build %s", s.alias, s.clientID, formatAppVersion(version, commit))
	o, err := s.a.remoteOptions(s.alias, s.clientID, s.log)
	if err != nil {
		fail(err)
		return
	}
	o.OnInstall = func() { s.set(gen, func(st *RemoteStateDTO) { st.State = remoteInstalling }) }
	conn, err := s.dial(ctx, s.alias, o)
	if err != nil {
		fail(err)
		return
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
		fail(err)
		return
	}
	s.mu.Lock()
	if gen != s.gen {
		s.mu.Unlock()
		s.backend.detach()
		_ = conn.Close()
		return
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
	s.set(gen, func(st *RemoteStateDTO) {
		*st = RemoteStateDTO{State: remoteConnected, Alias: s.alias,
			ServerOS: userText(hello.OS), ServerArch: userText(hello.Arch), ServerVersion: s.backend.ServerVersion()}
	})
	go s.watch(gen, conn)
}

// watch turns the end of a connection that nobody closed into a failure.
func (s *remoteSession) watch(gen int, conn remoteConn) {
	var linkDead <-chan struct{}
	if c := s.backend.current(); c != nil {
		linkDead = c.Done()
	}
	select {
	case <-conn.Done():
	case <-linkDead:
	}
	s.mu.Lock()
	current := gen == s.gen
	if current {
		s.conn = nil
	}
	s.mu.Unlock()
	if !current {
		return
	}
	err := conn.Err()
	if err == nil {
		err = &remoteError{remoteServerCrashed, "the server ended the connection"}
	}
	s.backend.detach()
	_ = conn.Close()
	code, hint, detail := remoteFailure(err)
	s.log.Printf("connect: the connection ended: %s (%s)", detail, code)
	s.set(gen, func(st *RemoteStateDTO) {
		*st = RemoteStateDTO{State: remoteFailed, Code: code, Hint: hint, Detail: detail, Alias: s.alias}
	})
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
}

// remoteEnabled reports whether remote mode is on: the flag or the
// experimental_remote key.
func (a *App) remoteEnabled() bool {
	return a.experimentalRemote || a.clientPrefs().Get().ExperimentalRemote
}

// ListRemotes is what the Remotes section of Settings shows.
func (a *App) ListRemotes() RemotesDTO {
	out := RemotesDTO{Enabled: a.remoteEnabled(), Remotes: []RemoteEntryDTO{}}
	for _, r := range a.clientPrefs().Get().Remotes {
		out.Remotes = append(out.Remotes, RemoteEntryDTO{Alias: r.Alias, ServerDir: r.ServerDir})
	}
	return out
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
// no --parent-pid and this window's Shutdown leaves it running. A second
// Connect reaches the single-instance lock of that alias, which focuses the
// open window.
func (a *App) ConnectRemote(alias string) RemoteResultDTO {
	if !a.remoteEnabled() {
		return resultOf(&remoteError{remoteNotEnabled, "remote mode is off on this machine"})
	}
	if _, ok := findRemote(a.clientPrefs().Get().Remotes, alias); !ok {
		return resultOf(&remoteError{remoteUnknownAlias, fmt.Sprintf("no remote named %s is configured", alias)})
	}
	exe, err := os.Executable()
	if err != nil {
		return resultOf(fmt.Errorf("the GitboxApp binary cannot be located"))
	}
	args := remoteWindowArgs(alias, a.testMode, a.cfgPath)
	cmd := exec.Command(exe, args...)
	// No git.HideWindow: the child is a Wails GUI, and SW_HIDE would start
	// its window hidden (see OpenTerminalsManagerWindow).
	if err := cmd.Start(); err != nil {
		return resultOf(fmt.Errorf("the remote window could not be started"))
	}
	go func() { _, _ = cmd.Process.Wait() }()
	return RemoteResultDTO{}
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
}

// parseRemoteFlags reads --remote <alias> (or --remote=<alias>),
// --experimental-remote, --test-mode and --prefs-file=<path>. It fails on a
// --remote without --experimental-remote, and on --prefs-file without
// --test-mode and --remote.
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
	case f.prefsFile != "" && (!f.testMode || !remoteSeen):
		return f, errors.New("--prefs-file is only for a remote window in test mode (--remote, --test-mode)")
	}
	return f, nil
}
