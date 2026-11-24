package main

import "time"

// Background check and focus refresh intervals.
const (
	updateCheckInterval = 6 * time.Hour
	harnessInitialDelay = 2 * time.Second
	harnessSyncInterval = 10 * time.Minute
	harnessMinInterval  = time.Minute
	probeMinInterval    = time.Minute
)

// How long each kind of work may take. Everything is also cancelled when
// the app closes, and network work when its server stops answering; these
// bounds catch the command that neither finishes nor fails.
const (
	// statusTimeout bounds the local status check of one clone.
	statusTimeout = 20 * time.Second
	// fetchTimeout and pullTimeout bound one fetch and one pull.
	fetchTimeout = 120 * time.Second
	pullTimeout  = 120 * time.Second
	// healReadTimeout bounds the reads of a clone's configuration before a
	// fetch or a pull.
	healReadTimeout = 15 * time.Second
	// shutdownWait is how long closing the app waits for work in flight to
	// stop. That work was already cancelled, and its processes with it.
	shutdownWait = 5 * time.Second
	// moveTimeout bounds a whole move, and mirrorSetupTimeout one mirror
	// setup, which is provider calls only.
	moveTimeout        = 10 * time.Minute
	mirrorSetupTimeout = 60 * time.Second
)

// I bound fetches across single-clone and bulk requests. Tune the per-host
// starting point with the fetch-all benchmark that blackholes one host.
const (
	fetchAllConcurrency     = 8
	fetchPerHostConcurrency = 2
)

// A clone has no bound: its size is not known in advance. It is cancelled
// when the app closes, and a remote clone when its connection ends.
//
// createRepoTimeout bounds the provider call that creates a repository,
// discoverMirrorsTimeout the discovery behind ApplyDiscoveredMirrors, and
// reconcileBound a reconcile pass over every root.
const (
	createRepoTimeout      = 30 * time.Second
	discoverMirrorsTimeout = 120 * time.Second
	reconcileBound         = 2 * time.Minute
)

// remoteUnbounded are the calls to a gitbox-server with no deadline: the
// work has none, and the connection's pings tell a dead host.
var remoteUnbounded = map[string]bool{
	"CloneRepo": true,
	// Individual git and provider calls are bounded. A complete pass may
	// take several of those bounds and stops with its window or connection.
	"RunFleetCheck": true,
	"PullAll":       true,
}

// A call to a gitbox-server may take as long as the work it asks for, plus
// remoteCallMargin for the round trip. remoteWorkBounds lists the methods
// whose work has its own bound; the rest get remoteDefaultBound.
const (
	remoteCallMargin   = 5 * time.Second
	remoteDefaultBound = fetchTimeout
)

var remoteWorkBounds = map[string]time.Duration{
	// The repository mutations (#295), as long as their work may take.
	"PullRepo":               healReadTimeout + pullTimeout + fetchTimeout,
	"MoveRepo":               moveTimeout,
	"SetupMirrorRepo":        mirrorSetupTimeout,
	"CreateNewRepo":          createRepoTimeout,
	"ApplyDiscoveredMirrors": discoverMirrorsTimeout,
	"Reconcile":              reconcileBound,
	"AdoptClones":            reconcileBound,
	"DeleteRepo":             statusTimeout,
	"ConfirmSweep":           statusTimeout,
	// The token credentials (#242): a store or a type change reconfigures
	// every clone of the account.
	"CredentialStoreToken": reconcileBound,
	"ChangeCredentialType": reconcileBound,
	"CredentialDelete":     statusTimeout,
	"CredentialSetupGCM":   healReadTimeout,
	// The SSH keys (#244): ssh-keygen, a connection test of up to 15 s and
	// every clone of the account reconfigured; the agent report runs
	// ssh-add for at most 5 s.
	"CredentialSetupSSH":             reconcileBound,
	"CredentialRegenerateSSH":        reconcileBound,
	"CredentialSSHAgentReport":       healReadTimeout,
	"GetConfig":                      healReadTimeout,
	"GetStartupSnapshot":             healReadTimeout,
	"ReloadConfig":                   healReadTimeout,
	"GetCollapsed":                   healReadTimeout,
	"GetOS":                          healReadTimeout,
	"GetConfigPath":                  healReadTimeout,
	"IsFirstRun":                     healReadTimeout,
	"GetConfigLoadError":             healReadTimeout,
	"ListConfigBackups":              healReadTimeout,
	"ListRoots":                      healReadTimeout,
	"SuggestAccountKey":              healReadTimeout,
	"AccountDeletionImpact":          healReadTimeout,
	"GetNestedScanDepth":             healReadTimeout,
	"RepoNesting":                    healReadTimeout,
	"GetPRSettings":                  healReadTimeout,
	"GetCheckGlobalGitignore":        healReadTimeout,
	"RecommendedGlobalGitignoreBody": healReadTimeout,
	"GetTokenGuide":                  healReadTimeout,
	"CheckMirrorCredentials":         healReadTimeout,
	"ListMoveDestinationOwners":      healReadTimeout,
	"OpenAccountInBrowser":           healReadTimeout,
	"OpenRepoInBrowser":              healReadTimeout,
	"GetRepoDetail":                  statusTimeout,
	"PreviewSweep":                   statusTimeout,
	"RepoWorkspaceFiles":             statusTimeout,
	"CheckGlobalIdentity":            statusTimeout,
	"IsGlobalGCMConfigNeeded":        statusTimeout,
	"CheckGlobalGCMConfig":           statusTimeout,
	"CheckGlobalGitignore":           statusTimeout,
}
