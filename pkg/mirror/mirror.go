// Package mirror handles repository mirror setup, status checking, and manual setup guides.
package mirror


// SetupResult describes what happened when setting up a mirror.
type SetupResult struct {
	RepoKey      string // "org/repo"
	Created      bool   // target repo was created
	Mirrored     bool   // mirror was configured via API
	Method       string // "api" or "manual"
	Instructions string // non-empty if method is "manual" — step-by-step guide
	Error        string // non-empty on failure
}

// StatusResult describes the live status of a mirror repo.
type StatusResult struct {
	RepoKey    string // "org/repo"
	Active     bool   // mirror is functioning
	Direction  string // "push" or "pull"
	OriginAcct string // account key of the source of truth
	BackupAcct string // account key of the backup
	SyncStatus string // "synced", "behind", or "" if unknown
	HeadCommit string // short SHA when synced
	OriginHead string // short SHA of origin when behind
	BackupHead string // short SHA of backup when behind
	Warning    string // non-empty for warnings (e.g., repo is public)
	NeedsSetup bool   // target 404 on a row that has never been set up yet — render as neutral "needs setup", not a red error
	Error      string // non-empty on failure
}
