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
	"reflect"
	"regexp"
	"runtime"
	"strings"

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
// runs where the harnesses run; P5 (#294) adds the config mutations, each
// with its row in mutationIdempotence. Each method decodes its parameters
// into their Go types and checks what it is given (hostPath, #241). The
// repository mutations (#295) and the credential methods (#242, #244) are
// not served yet, so a remote window refuses them itself.
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
	"CheckGlobalGCMConfig",
	"CheckGlobalGitignore",
	"CheckGlobalIdentity",
	"CheckMirrorCredentials",
	"CheckMoveReadiness",
	"CredentialVerify",
	"DeleteAccount",
	"DeleteMirrorGroup",
	"DeleteMirrorRepo",
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
	"OpenAccountInBrowser",
	"OpenRepoInBrowser",
	"PreflightMove",
	"PreviewSweep",
	"RecommendedGlobalGitignoreBody",
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
	"ScanFolderForClones",
	"SetCheckGlobalGitignore",
	"SetDefaultRoot",
	"SetNestedScanDepth",
	"SetPRBadgesEnabled",
	"SetPRIncludeDrafts",
	"SetRepoContainer",
	"SetRepoWorkspace",
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
// of #241). The served config mutations save (#294); the repair of each
// clone's .git/config waits for the repository mutations (#295). cfgPath
// empty means the host's default gitbox.json.
func newServerApp(cfgPath string) *App {
	a := NewApp()
	a.cfgPath = cfgPath
	a.skipCloneRepair = true
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
	"DeleteAccount":              "a missing account returns config.not_found",
	"DeleteMirrorGroup":          "a missing group returns config.not_found",
	"DeleteMirrorRepo":           "a missing repo returns config.not_found",
	"DiscardAccount":             "a missing account returns config.not_found",
	"FixGlobalGCMConfig":         "writes only what is missing; a second call finds nothing to fix",
	"IgnoreClone":                "setting the same ignore state again changes nothing",
	"InitRoot":                   "an existing root is kept and becomes the default; the mode is kept",
	"InstallGlobalGitignore":     "the managed block is written once; a second call finds it in place",
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
	"UpdateAccount":              "writing the same fields again changes nothing; a missing account returns config.not_found",
}

// newRPCServer returns the server that answers servedMethods from a's
// backend and carries a's events to the client.
func newRPCServer(a *App, log io.Writer) *rpc.Server {
	backend := reflect.ValueOf(Backend(hostBackend{localBackend{a}}))
	handlers := make(map[string]rpc.Handler, len(servedMethods))
	for _, name := range servedMethods {
		method := backend.MethodByName(name)
		if !method.IsValid() {
			panic("gitbox-server: served method " + name + " is not a Backend method")
		}
		handlers[name] = rpc.Func(method.Interface())
	}
	srv := &rpc.Server{
		Handlers:    handlers,
		ToUserError: toUserError,
		Self:        rpc.Hello{Version: version, Commit: commit, OS: runtime.GOOS, Arch: runtime.GOARCH, PID: os.Getpid()},
		Log:         log,
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
	// The background workers (harness watcher, upstream probes) run as on a
	// desktop. The server is read-only, so the watcher keeps what it detects
	// in memory and reports it, and nothing reaches the host's config.
	backend.Start()
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
}

func serverDoctor(cfgPath string) serverDoctorReport {
	home, _ := os.UserHomeDir()
	report := serverDoctorReport{
		Version: version, Commit: commit, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Home: home, ConfigPath: cfgPath,
		CredentialFindings: credential.AuditCredentialDir(),
	}
	report.ConfigDirWritable, report.ConfigDirExec = probeDir(nearestDir(filepath.Dir(cfgPath)))
	if res, err := git.Run(context.Background(), git.Request{Args: []string{"--version"}, Env: git.Inherit}); err == nil {
		report.GitVersion = strings.TrimSpace(string(res.Stdout))
	}
	return report
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
