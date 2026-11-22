package main

import "time"

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
)

// I bound fetches across single-clone and bulk requests. Tune the per-host
// starting point with the fetch-all benchmark that blackholes one host.
const (
	fetchAllConcurrency     = 8
	fetchPerHostConcurrency = 2
)

// A clone has no bound: its size is not known in advance. It is cancelled
// when the app closes.

// A call to a gitbox-server may take as long as the work it asks for, plus
// remoteCallMargin for the round trip. remoteWorkBounds lists the methods
// whose work has its own bound; the rest get remoteDefaultBound.
const (
	remoteCallMargin   = 5 * time.Second
	remoteDefaultBound = fetchTimeout
)

var remoteWorkBounds = map[string]time.Duration{
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
