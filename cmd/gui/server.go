package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// This file is gitbox-server: the backend of this package served over
// JSON-RPC on stdin and stdout, for a GitboxApp on another machine (#233).
// Its main() is in server_main.go, which only the gitboxserver build
// compiles; everything else lives here, untagged, so the tests of the
// ordinary build exercise it.

// servedMethods is the closed list of Backend methods the server answers. A
// method that is not here does not exist for the client. The list grows one
// phase at a time: P4 (#240) serves every read, the REMOTE methods it owns
// and the read halves of the MIXED ones (the two browser methods only
// resolve a link: the client opens it); P6 (#243) adds AccountFolder, the
// read half of the account launchers, and the AI harness detection, which
// runs where the harnesses run; P5 adds the config mutations (#294) and the
// repository mutations (#295), each with its row in mutationIdempotence;
// P5b (#242) adds the token credentials, and serves the GCM setup only to
// refuse it (hostCredentialHandlers); P7 (#244) adds the SSH key methods,
// whose keys stay on the host, and the agent report. Each method decodes
// its parameters into their Go types and checks what it is given
// (hostPath, #241).
var servedMethods = []string{
	"AccountDeletionImpact",
	"AccountFolder",
	"AcknowledgeConfigError",
	"AddAccount",
	"AddDiscoveredRepos",
	"AddDiscoveredReposToFolder",
	"AddMirrorGroup",
	"AddMirrorRepo",
	"AddRoot",
	"AdoptClones",
	"AdoptFirstProject",
	"ApplyDiscoveredMirrors",
	"BeginStatus",
	"ChangeCredentialType",
	"CheckGlobalGCMConfig",
	"CheckGlobalGitignore",
	"CheckGlobalIdentity",
	"CheckMirrorCredentials",
	"CheckMoveReadiness",
	"CloneRepo",
	"ConfirmSweep",
	"CreateNewRepo",
	"CredentialDelete",
	"CredentialRegenerateSSH",
	"CredentialSSHAgentReport",
	"CredentialSetupGCM",
	"CredentialSetupSSH",
	"CredentialStoreToken",
	"CredentialVerify",
	"DeleteAccount",
	"DeleteMirrorGroup",
	"DeleteMirrorRepo",
	"DeleteRepo",
	"DiscardAccount",
	"Discover",
	"DiscoverMirrors",
	"DoctorPrecheck",
	"DoctorRun",
	"FetchAllRepos",
	"FetchRepo",
	"FixGlobalGCMConfig",
	"GetAllStatus",
	"GetCheckGlobalGitignore",
	"GetCollapsed",
	"GetConfig",
	"GetConfigLoadError",
	"GetConfigPath",
	"GetMirrorStatus",
	"GetNestedScanDepth",
	"GetOS",
	"GetPRSettings",
	"GetPRsForRepo",
	"GetRepoDetail",
	"GetStartupSnapshot",
	"GetTokenGuide",
	"IgnoreClone",
	"InitRoot",
	"InstallGlobalGitignore",
	"IsFirstRun",
	"IsGlobalGCMConfigNeeded",
	"ListAccountOrgs",
	"ListConfigBackups",
	"ListMoveDestinationOwners",
	"ListRemoteRepos",
	"ListRoots",
	"MoveRepo",
	"OpenAccountInBrowser",
	"OpenRepoInBrowser",
	"PreflightMove",
	"PreviewClonePath",
	"PreviewRoot",
	"PreviewSweep",
	"PullRepo",
	"RecommendedGlobalGitignoreBody",
	"Reconcile",
	"RefreshAIHarnesses",
	"RefreshAllPRs",
	"RefreshStatus",
	"ReloadConfig",
	"RemoveGlobalIdentity",
	"RemoveRoot",
	"RenameAccount",
	"RepairConfig",
	"RepoNesting",
	"RepoWorkspaceFiles",
	"RestoreFromBackup",
	"RunFleetCheck",
	"ScanFolderForClones",
	"SetCheckGlobalGitignore",
	"SetDefaultRoot",
	"SetNestedScanDepth",
	"SetPRBadgesEnabled",
	"SetPRIncludeDrafts",
	"SetRepoContainer",
	"SetRepoWorkspace",
	"SetupMirrorRepo",
	"SuggestAccountKey",
	"SyncAIHarnesses",
	"UpdateAccount",
	"ValidateRemotePath",
}

// hostBackend is the local backend as the server exposes it. The config it
// sends carries no client preferences: those belong to the machine that
// shows the window, and the client lays its own over what it receives.
type hostBackend struct{ localBackend }

func withoutClientPrefs(dto ConfigDTO) ConfigDTO {
	scratch := config.Config{Global: dto.Global}
	config.StripClientPrefs(&scratch)
	dto.Global = scratch.Global
	return dto
}

func (b hostBackend) GetConfig() (ConfigDTO, error) {
	dto, err := b.localBackend.GetConfig()
	return withoutClientPrefs(dto), err
}

func (b hostBackend) ReloadConfig() (ConfigDTO, error) {
	dto, err := b.localBackend.ReloadConfig()
	return withoutClientPrefs(dto), err
}

func (b hostBackend) RedetectProfiles() (ConfigDTO, error) {
	dto, err := b.localBackend.RedetectProfiles()
	return withoutClientPrefs(dto), err
}

func (b hostBackend) GetStartupSnapshot() (StartupSnapshot, error) {
	snap, err := b.localBackend.GetStartupSnapshot()
	snap.Config = withoutClientPrefs(snap.Config)
	return snap, err
}

// newServerApp builds the App a server runs: no window, and a config whose
// client preferences are the host's own and never the server's to change
// (saveConfig takes them from the file before every save, under the guard
// of #241). cfgPath empty means the host's default gitbox.json.
func newServerApp(cfgPath string) *App {
	a := NewApp()
	a.cfgPath = cfgPath
	return a
}

// servedSet is servedMethods as a set, for the client's per-method gate.
var servedSet = func() map[string]bool {
	m := make(map[string]bool, len(servedMethods))
	for _, name := range servedMethods {
		m[name] = true
	}
	return m
}()

// mutationIdempotence says, for every served method that changes the host,
// what a duplicate does (#294). Within a session the server replays a
// repeated request id; across sessions, after a reconnect, idempotence comes
// from the domain. TestServedMutationsHaveIdempotence keeps it complete.
var mutationIdempotence = map[string]string{
	"AcknowledgeConfigError":     "clears a pending load error; a second call finds none and does nothing",
	"AddAccount":                 "an existing key returns config.exists and changes nothing",
	"AddDiscoveredRepos":         "a repo already in the source is skipped; the others are added",
	"AddDiscoveredReposToFolder": "a repo already in the source is skipped; the others are added",
	"AddMirrorGroup":             "an existing key returns config.exists and changes nothing",
	"AddMirrorRepo":              "an existing repo in the group returns config.exists",
	"AddRoot":                    "an existing root is a no-op",
	"AdoptClones":                "a clone already adopted is tracked, so it is no longer offered; the pass that follows finds nothing to adopt",
	"AdoptFirstProject":          "the same project stays at its selected path and its identity is checked again",
	"ApplyDiscoveredMirrors":     "a mirror repo already in its group is skipped; the others are added",
	"ChangeCredentialType":       "the type the account already has changes nothing; GCM is always refused and changes nothing",
	"CloneRepo":                  "a clone already on disk fails with the clone error and changes nothing",
	"ConfirmSweep":               "a branch already deleted is no longer stale, so a second sweep deletes nothing",
	"CreateNewRepo":              "an existing repository on the provider fails with the provider's error and changes nothing",
	"CredentialDelete":           "a credential already gone is a no-op",
	"CredentialRegenerateSSH":    "each call makes a new key; a repeated request id replays the first answer, and after a reconnect a retry makes another key whose public half the window shows",
	"CredentialSetupGCM":         "always refused on a host; changes nothing",
	"CredentialSetupSSH":         "an existing key and Host entry are kept; a second call only tests the connection again",
	"CredentialStoreToken":       "storing the same token again writes the same file",
	"DeleteAccount":              "a missing account returns config.not_found",
	"DeleteMirrorGroup":          "a missing group returns config.not_found",
	"DeleteMirrorRepo":           "a missing repo returns config.not_found",
	"DeleteRepo":                 "a missing repo returns config.not_found",
	"DiscardAccount":             "a missing account returns config.not_found",
	"FixGlobalGCMConfig":         "writes only what is missing; a second call finds nothing to fix",
	"IgnoreClone":                "setting the same ignore state again changes nothing",
	"InitRoot":                   "an existing root is kept and becomes the default; the mode is kept",
	"InstallGlobalGitignore":     "the managed block is written once; a second call finds it in place",
	"MoveRepo":                   "a moved repo is no longer in its source, so a second move fails in preflight and changes nothing",
	"PullRepo":                   "a second fast-forward pull finds nothing to pull",
	"Reconcile":                  "a second pass finds what the first adopted already tracked",
	"RemoveGlobalIdentity":       "a missing identity is a no-op",
	"RemoveRoot":                 "a root that is not configured is a no-op",
	"RenameAccount":              "a missing old key returns config.not_found; an existing new key config.exists",
	"RepairConfig":               "repairs and saves; a second call reloads the repaired file",
	"RestoreFromBackup":          "restoring the same backup again writes the same config",
	"SetCheckGlobalGitignore":    "setting the same value again changes nothing",
	"SetDefaultRoot":             "the default root already being the default is a no-op",
	"SetNestedScanDepth":         "setting the same value again changes nothing",
	"SetPRBadgesEnabled":         "setting the same value again changes nothing",
	"SetPRIncludeDrafts":         "setting the same value again changes nothing",
	"SetRepoContainer":           "setting the same flag again changes nothing",
	"SetRepoWorkspace":           "setting the same workspace again changes nothing",
	"SetupMirrorRepo":            "a mirror already set up is found on the provider and kept",
	"UpdateAccount":              "writing the same fields again changes nothing; a missing account returns config.not_found",
}

// newRPCServer returns the server that answers servedMethods from a's
// backend and carries a's events to the client.
func newRPCServer(a *App, log io.Writer) *rpc.Server {
	backend := reflect.ValueOf(Backend(hostBackend{localBackend{a}}))
	requestBound := requestBoundHandlers(a)
	maps.Copy(requestBound, hostCredentialHandlers(a))
	handlers := make(map[string]rpc.Handler, len(servedMethods))
	for _, name := range servedMethods {
		method := backend.MethodByName(name)
		if !method.IsValid() {
			panic("gitbox-server: served method " + name + " is not a Backend method")
		}
		if h, ok := requestBound[name]; ok {
			handlers[name] = h
			continue
		}
		handlers[name] = rpc.Func(method.Interface())
	}
	srv := &rpc.Server{
		Handlers:     handlers,
		ToUserError:  toUserError,
		Self:         rpc.Hello{Version: version, Commit: commit, OS: runtime.GOOS, Arch: runtime.GOARCH, PID: os.Getpid()},
		Log:          log,
		SecretParams: secretParams,
		// Only a mutation is replayed on a repeated request id (#294).
		Remember: func(method string) bool {
			info, ok := classOf(method)
			return ok && info.Mutating
		},
	}
	a.events = srv
	a.hostLog = log
	return srv
}

// requestBoundHandlers serve the methods whose work a desktop starts in the
// background and returns from at once: clone, pull, move and mirror setup.
// On a server that work runs inside its request (#295), so the client's
// $/cancelRequest and the end of the connection cancel it, and the runner's
// tree kill stops its git processes. Each answers once its done event is
// queued, which is before the answer on the same connection. The window's
// backend returns at once and waits in the background (RemoteBackend.goLong).
func requestBoundHandlers(a *App) map[string]rpc.Handler {
	return map[string]rpc.Handler{
		"CloneRepo": rpc.Func(func(ctx context.Context, sourceKey, repoKey, root string) error {
			return a.inRequest(ctx, func(ctx context.Context) { a.cloneRepoRun(ctx, sourceKey, repoKey, root) })
		}),
		"PullRepo": rpc.Func(func(ctx context.Context, sourceKey, repoKey string) error {
			return a.inRequest(ctx, func(ctx context.Context) { a.pullRepoRun(ctx, sourceKey, repoKey) })
		}),
		"MoveRepo": rpc.Func(func(ctx context.Context, req MoveRequestDTO) error {
			return a.inRequest(ctx, func(ctx context.Context) { a.moveRepoRun(ctx, req) })
		}),
		"SetupMirrorRepo": rpc.Func(func(ctx context.Context, mirrorKey, repoKey string) error {
			return a.inRequest(ctx, func(ctx context.Context) { a.setupMirrorRepoRun(ctx, mirrorKey, repoKey) })
		}),
	}
}

// secretParams marks the one parameter that carries a secret: the token of
// CredentialStoreToken (#242). It travels from the window to the host once,
// in that request, and neither end logs it or lets it back into an answer.
var secretParams = rpc.SecretParams{"CredentialStoreToken": {1}}

// hostCredentialHandlers keep GCM off a host (#242). Its sign-in runs git
// credential fill with a browser or a window on the machine that runs it,
// which on a host nobody sees, and the stores it relies on are unreliable
// in an ssh session. The refusal is deterministic: the server never tries
// GCM first, because whether it works depends on the state of the host's
// keyring. Switching an account to GCM is refused the same way.
func hostCredentialHandlers(a *App) map[string]rpc.Handler {
	b := hostBackend{localBackend{a}}
	return map[string]rpc.Handler{
		"CredentialSetupGCM": rpc.Func(func(accountKey string) (CredentialSetupResult, error) {
			return CredentialSetupResult{}, gcmOnHost()
		}),
		"ChangeCredentialType": rpc.Func(func(accountKey, newType string) error {
			if strings.EqualFold(strings.TrimSpace(newType), "gcm") {
				return gcmOnHost()
			}
			return b.ChangeCredentialType(accountKey, newType)
		}),
	}
}

// gcmOnHost is the refusal of GCM on this host.
func gcmOnHost() error { return &gcmRemoteError{OS: runtime.GOOS} }

// errServerClosing answers a request that arrives while the server stops.
var errServerClosing = &remoteError{remoteNotConnected, "the server is closing"}

// inRequest runs work as tracked work of a (closing the app waits for it)
// under a ctx that ends with the request or with the app, and returns when
// work does.
func (a *App) inRequest(ctx context.Context, work func(context.Context)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(a.refreshContext(), cancel)
	defer stop()
	done := make(chan struct{})
	if !a.goTracked(func() { defer close(done); work(ctx) }) {
		return errServerClosing
	}
	<-done
	return nil
}

// The installed gitbox-server (the gitboxserver build) sets these hooks; in
// every other build, and when the binary is not installed under a server
// root, serve runs without them.
var (
	// serverLog wraps the server's log, so it also lands in the rotating
	// log file under the server root (server_logfile.go).
	serverLog func(stderr io.Writer) io.Writer
	// onServe runs when serving starts and returns what runs when it ends:
	// the in-use marker and garbage collection (server_marker.go).
	onServe func(clientID string, log io.Writer) (stop func())
)

// Exit codes of gitbox-server. A client tells a build mismatch (reinstall
// the server) from every other failure by the code.
const (
	serverExitOK       = 0
	serverExitFailed   = 1
	serverExitUsage    = 2
	serverExitMismatch = 3
)

const serverUsage = `usage:
  gitbox-server serve --stdio [--client-id <id>]   serve one client on stdin and stdout
  gitbox-server --version                          print "<version> <commit>"
  gitbox-server --doctor                           print a JSON report about this host
`

var clientIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// serverMain is gitbox-server's main, with its streams as parameters.
// Stdout carries the protocol and nothing else; everything for a human goes
// to stderr.
func serverMain(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 1 && args[0] == "--version":
		fmt.Fprintf(stdout, "%s %s\n", version, commit)
		return serverExitOK
	case len(args) == 1 && args[0] == "--doctor":
		report, err := json.MarshalIndent(serverDoctor(config.DefaultV2Path()), "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, "gitbox-server: building the report failed")
			return serverExitFailed
		}
		fmt.Fprintf(stdout, "%s\n", report)
		return serverExitOK
	case len(args) >= 2 && args[0] == "serve" && args[1] == "--stdio":
		clientID := ""
		switch rest := args[2:]; {
		case len(rest) == 0:
		case len(rest) == 2 && rest[0] == "--client-id" && clientIDRE.MatchString(rest[1]):
			clientID = rest[1]
		default:
			fmt.Fprint(stderr, serverUsage)
			return serverExitUsage
		}
		return serve(newServerApp(""), clientID, stdin, stdout, stderr)
	}
	fmt.Fprint(stderr, serverUsage)
	return serverExitUsage
}

// serve answers one client until its input ends. The end of the input is the
// only shutdown signal: it cancels everything in flight, git processes and
// what they started included, and the process exits.
func serve(a *App, clientID string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// When the ssh connection drops, a write to stdout or stderr would end
	// the process with SIGPIPE at once, maybe while a save holds the config
	// lock. Ignored, the write fails instead: the connection ends, and the
	// handlers in flight finish and release what they hold.
	signal.Ignore(syscall.SIGPIPE)

	log := stderr
	if serverLog != nil {
		log = serverLog(stderr)
	}
	if onServe != nil {
		defer onServe(clientID, log)()
	}
	srv := newRPCServer(a, log)
	backend := a.be()
	backend.Init(ctx)
	// Detect tools on this host and save through its config guard. The
	// connected window owns the timer for fleet checks.
	a.tasks.start(ctx, []*scheduledTask{a.hostHarnessTask()})
	defer backend.Close()

	if clientID != "" {
		fmt.Fprintf(log, "gitbox-server: serving client %s\n", clientID)
	}
	switch err := srv.Serve(ctx, stdin, stdout); {
	case err == nil:
		return serverExitOK
	case errors.Is(err, rpc.ErrVersionMismatch):
		return serverExitMismatch
	default:
		return serverExitFailed
	}
}

// serverDoctorReport is what gitbox-server --doctor prints: what a client
// needs to know about the host before it trusts it with work.
type serverDoctorReport struct {
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Home       string `json:"home"`
	ConfigPath string `json:"configPath"`
	// ConfigDirWritable and ConfigDirExec tell whether the folder that
	// holds gitbox.json, or its nearest existing parent, accepts new files
	// and lets them run (a home mounted noexec does not).
	ConfigDirWritable bool `json:"configDirWritable"`
	ConfigDirExec     bool `json:"configDirExec"`
	// GitVersion is what "git --version" prints, or empty when git cannot
	// run.
	GitVersion         string               `json:"gitVersion"`
	CredentialFindings []credential.Finding `json:"credentialFindings"`
	// Roots has one entry per clone root of the host's config, when the
	// config loads (#245).
	Roots []doctorRoot `json:"roots,omitempty"`
}

// doctorRoot is whether the server can read one clone root.
type doctorRoot struct {
	Path string `json:"path"`
	// State is one of the doctorRoot* values.
	State string `json:"state"`
}

const (
	doctorRootOK         = "ok"
	doctorRootMissing    = "missing"
	doctorRootTCC        = "tcc"
	doctorRootDenied     = "denied"
	doctorRootUnreadable = "unreadable"
)

// doctorRootStates is every State a client accepts.
var doctorRootStates = []string{doctorRootOK, doctorRootMissing, doctorRootTCC, doctorRootDenied, doctorRootUnreadable}

func serverDoctor(cfgPath string) serverDoctorReport {
	home, _ := os.UserHomeDir()
	report := serverDoctorReport{
		Version: version, Commit: commit, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Home: home, ConfigPath: cfgPath,
		CredentialFindings: credential.AuditCredentialDir(),
		Roots:              doctorRoots(cfgPath),
	}
	report.ConfigDirWritable, report.ConfigDirExec = probeDir(nearestDir(filepath.Dir(cfgPath)))
	if res, err := git.Run(context.Background(), git.Request{Args: []string{"--version"}, Env: git.Inherit}); err == nil {
		report.GitVersion = strings.TrimSpace(string(res.Stdout))
	}
	return report
}

// doctorRoots reads one entry of each clone root of the config at cfgPath.
// It only reads: a missing or broken config gives no roots.
func doctorRoots(cfgPath string) []doctorRoot {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil
	}
	roots := make([]doctorRoot, 0, len(cfg.Global.Roots))
	for _, r := range cfg.Global.Roots {
		p := config.ExpandTilde(r)
		roots = append(roots, doctorRoot{Path: p, State: rootState(readOneEntry(p), runtime.GOOS)})
	}
	return roots
}

// readOneEntry opens dir and reads one entry, which is what TCC refuses.
func readOneEntry(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// rootState names why a root cannot be read. On macOS, a process that
// sshd starts gets EPERM ("Operation not permitted") from the folders TCC
// protects, such as Documents, Desktop, Downloads and iCloud Drive, while
// ordinary permissions give EACCES.
func rootState(err error, goos string) string {
	switch {
	case err == nil:
		return doctorRootOK
	case errors.Is(err, fs.ErrNotExist):
		return doctorRootMissing
	case goos == "darwin" && errors.Is(err, syscall.EPERM):
		return doctorRootTCC
	case errors.Is(err, fs.ErrPermission):
		return doctorRootDenied
	}
	return doctorRootUnreadable
}

// nearestDir returns dir, or its closest parent that exists.
func nearestDir(dir string) string {
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

// probeDir reports whether dir accepts a new file and whether that file can
// run. It leaves nothing behind.
func probeDir(dir string) (writable, executable bool) {
	f, err := os.CreateTemp(dir, ".gitbox-probe-*")
	if err != nil {
		return false, false
	}
	path := f.Name()
	defer os.Remove(path)
	_, werr := f.WriteString("#!/bin/sh\nexit 0\n")
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return false, false
	}
	if runtime.GOOS == "windows" {
		return true, true // Windows has no noexec mount to detect
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return true, false
	}
	cmd := exec.Command(path)
	git.HideWindow(cmd)
	return true, cmd.Run() == nil
}
