package credential

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

// Status represents the credential verification state.
type Status int

const (
	StatusUnknown  Status = iota // never checked
	StatusChecking               // check in progress
	StatusOK                     // working
	StatusWarning                // partially configured (e.g., SSH key exists but connection fails)
	StatusOffline                // server unreachable (network error, DNS failure, timeout)
	StatusError                  // broken or missing
	StatusNone                   // not configured (optional: user hasn't opted in)
)

// String returns the status as a lowercase label.
func (s Status) String() string {
	switch s {
	case StatusUnknown:
		return "unknown"
	case StatusChecking:
		return "checking"
	case StatusOK:
		return "ok"
	case StatusWarning:
		return "warning"
	case StatusOffline:
		return "offline"
	case StatusError:
		return "error"
	case StatusNone:
		return "none"
	default:
		return "unknown"
	}
}

// StatusResult holds the outcome of a credential check for one account.
// Primary tracks the main credential (SSH key, GCM OAuth, or Token).
// PAT tracks the companion PAT needed for API calls (optional for SSH/GCM).
type StatusResult struct {
	// Primary credential status.
	Primary       Status
	PrimaryType   string // "ssh", "gcm", "token", or ""
	PrimaryDetail string

	// Companion PAT status (for SSH and GCM accounts).
	// For Token accounts, PAT mirrors Primary (same token does everything).
	PAT       Status
	PATDetail string

	// Overall is the combined status for simple consumers (GUI badge, etc.).
	// OK = both primary and PAT are OK (or PAT not needed).
	// Warning = primary OK but PAT missing/broken.
	// Error = primary broken.
	// None = no credential type configured.
	Overall Status
}

// StatusManager tracks credential status for all accounts.
// Thread-safe. A single instance is shared by TUI and GUI.
type StatusManager struct {
	mu       sync.Mutex
	statuses map[string]StatusResult
	epochs   map[string]uint64
}

// NewStatusManager creates a manager.
func NewStatusManager() *StatusManager {
	return &StatusManager{
		statuses: make(map[string]StatusResult),
		epochs:   make(map[string]uint64),
	}
}

// Get returns the current status for an account.
// Returns StatusUnknown if never checked.
func (sm *StatusManager) Get(accountKey string) StatusResult {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	r, ok := sm.statuses[accountKey]
	if !ok {
		return StatusResult{Overall: StatusUnknown, Primary: StatusUnknown, PAT: StatusUnknown}
	}
	return r
}

// GetAll returns a snapshot of all statuses.
func (sm *StatusManager) GetAll() map[string]StatusResult {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	out := make(map[string]StatusResult, len(sm.statuses))
	for k, v := range sm.statuses {
		out[k] = v
	}
	return out
}

// StartCheck marks an account as Checking and returns an epoch token.
func (sm *StatusManager) StartCheck(accountKey string) uint64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.epochs[accountKey]++
	sm.statuses[accountKey] = StatusResult{
		Overall: StatusChecking,
		Primary: StatusChecking,
		PAT:     StatusChecking,
	}
	return sm.epochs[accountKey]
}

// CompleteCheck records a result only if the epoch still matches.
func (sm *StatusManager) CompleteCheck(accountKey string, epoch uint64, result StatusResult) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.epochs[accountKey] != epoch {
		return false
	}
	sm.statuses[accountKey] = result
	return true
}

// Invalidate resets an account to Unknown and bumps the epoch.
func (sm *StatusManager) Invalidate(accountKey string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.epochs[accountKey]++
	sm.statuses[accountKey] = StatusResult{
		Overall: StatusUnknown,
		Primary: StatusUnknown,
		PAT:     StatusUnknown,
	}
}

// Remove deletes an account from the manager.
func (sm *StatusManager) Remove(accountKey string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.statuses, accountKey)
	delete(sm.epochs, accountKey)
}

// SSHFolder returns the resolved SSH folder from config.
func SSHFolder(cfg *config.Config) string {
	sshFolder := "~/.ssh"
	if cfg.Global.CredentialSSH != nil && cfg.Global.CredentialSSH.SSHFolder != "" {
		sshFolder = cfg.Global.CredentialSSH.SSHFolder
	}
	return config.ExpandTilde(sshFolder)
}

// Check runs the credential verification for one account synchronously.
// Returns a structured result with separate primary and PAT statuses.
func Check(acct config.Account, accountKey string, cfg *config.Config) StatusResult {
	if acct.DefaultCredentialType == "" {
		return StatusResult{
			Overall:     StatusNone,
			Primary:     StatusNone,
			PrimaryType: "",
			PAT:         StatusNone,
		}
	}

	switch acct.DefaultCredentialType {
	case "ssh":
		return checkSSH(acct, accountKey, cfg)
	case "gcm":
		return checkGCM(acct, accountKey, cfg)
	default: // "token"
		return checkToken(acct, accountKey, cfg)
	}
}

// checkSSH verifies SSH key + optional companion PAT.
func checkSSH(acct config.Account, accountKey string, cfg *config.Config) StatusResult {
	r := StatusResult{PrimaryType: "ssh"}

	// Primary: SSH key + connection.
	if acct.SSH == nil || acct.SSH.Host == "" {
		r.Primary = StatusError
		r.PrimaryDetail = "SSH not configured (missing host alias)"
		r.PAT = StatusNone
		r.Overall = StatusError
		return r
	}

	sshFolder := SSHFolder(cfg)
	keyType := "ed25519"
	if acct.SSH.KeyType != "" {
		keyType = acct.SSH.KeyType
	}
	if _, err := FindSSHKey(sshFolder, acct.SSH.Host, keyType); err != nil {
		r.Primary = StatusError
		r.PrimaryDetail = "SSH key missing: " + SSHKeyPath(sshFolder, accountKey)
		r.PAT = StatusNone
		r.Overall = StatusError
		return r
	}

	if _, err := TestSSHConnection(sshFolder, acct.SSH.Host); err != nil {
		if isSSHNetworkError(err) {
			r.Primary = StatusOffline
			r.PrimaryDetail = "server unreachable"
		} else {
			r.Primary = StatusWarning
			r.PrimaryDetail = "SSH key ready, connection failed"
		}
	} else {
		r.Primary = StatusOK
		r.PrimaryDetail = "SSH connection OK"
	}

	// PAT: optional companion token for discovery/API.
	r.PAT, r.PATDetail = checkPATStatus(acct, accountKey, cfg)
	if r.PATDetail == "" {
		if r.PAT == StatusOK {
			r.PATDetail = "PAT verified"
		} else if r.PAT == StatusWarning {
			r.PATDetail = "PAT stored but API check failed"
		} else {
			r.PATDetail = "no PAT (discovery/API unavailable)"
		}
	}

	r.Overall = combineStatus(r.Primary, r.PAT)
	return r
}

// checkGCM verifies a GCM-backed account by splitting two concerns:
//
//  1. Primary = "does GCM have a credential cached for this host?"
//     That is the thing GCM is responsible for (git push/pull auth).
//     We don't treat API-layer failures as a Primary problem, because the
//     REST API is a separate capability that can be covered by a
//     companion PAT — or by GCM's own token, depending on the provider.
//
//  2. PAT row = "can gitbox reach the API for discovery / repo creation?"
//     We try GCM's cached token first (works for GitHub / GitLab OAuth
//     tokens, works when the user pasted a PAT into GCM's prompt). If
//     that fails, we fall back to a PAT stored in the gitbox keyring.
//     Either success path marks the PAT row as OK with an explanatory
//     detail. Only when neither covers the API do we downgrade.
//
// Before this split, a working "GCM for git + PAT for API" setup rendered
// as "Warning" on the Primary row because the GCM-cached password (which
// Forgejo/Gitea refuse at their REST API) was being counted against GCM.
// That's wrong: GCM did its job.
func checkGCM(acct config.Account, accountKey string, cfg *config.Config) StatusResult {
	r := StatusResult{PrimaryType: "gcm"}

	token, _, err := ResolveGCMToken(acct.URL, acct.Username)
	if err != nil {
		// No credential means git operations don't work either. This IS
		// a Primary failure.
		r.Primary = StatusError
		r.PrimaryDetail = "GCM has no cached credential — run credential setup"
		r.PAT = StatusNone
		r.PATDetail = "not available until GCM has a credential"
		r.Overall = StatusError
		return r
	}

	r.Primary = StatusOK
	r.PrimaryDetail = "GCM credential present — git push/pull works"

	// API reachability: try GCM's cached token, then the keyring PAT.
	r.PAT, r.PATDetail = checkAPIReachabilityForGCM(acct, accountKey, token)
	r.Overall = combineStatus(r.Primary, r.PAT)
	return r
}

// checkAPIReachabilityForGCM produces the PAT-row status for a GCM-backed
// account by trying two paths in order: the GCM-cached token, then any
// companion PAT in env vars / keyring. The first success wins; details
// reflect which path worked (or what failed if neither did).
func checkAPIReachabilityForGCM(acct config.Account, accountKey string, gcmToken string) (Status, string) {
	// 1. GCM-token path.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	gcmErr := provider.TestAuth(ctx, acct.Provider, acct.URL, gcmToken, acct.Username)
	if gcmErr == nil {
		return StatusOK, "API via GCM (no separate PAT needed; only required for mirrors)"
	}
	if provider.IsNetworkError(gcmErr) {
		return StatusOffline, fmt.Sprintf("server unreachable: %v", gcmErr)
	}

	// 2. Keyring / env-var PAT fallback.
	patToken, _, patErr := ResolveToken(acct, accountKey)
	if patErr != nil {
		// No PAT configured. The 401 we got could be either a wrong
		// credential or a provider-API policy restriction — gitbox can't
		// tell them apart from a single response. Spell out both paths so
		// the user isn't misled into blaming the server for a typo.
		if isAPIAuthRejection(gcmErr) {
			if providerNeedsPATForAPI(acct.Provider) {
				return StatusNone, fmt.Sprintf(
					"WARNING: API rejected the GCM credential (HTTP 401). Either the user or the password are wrong (most probably), or your %s installation is configured to require a Personal Access Token at its REST API (passwords are not always accepted). Delete and re-setup the credential with the right value, or click 'Setup API token' to store a companion PAT.",
					providerDisplayName(acct.Provider),
				)
			}
			return StatusNone, "WARNING: API rejected the GCM credential (HTTP 401). The user or password are most likely wrong or expired — delete and re-setup the credential with the correct value, or click 'Setup API token' to store a companion PAT."
		}
		return StatusNone, fmt.Sprintf("WARNING: API via GCM failed (%v) and no companion PAT is configured", gcmErr)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()
	if err := provider.TestAuth(ctx2, acct.Provider, acct.URL, patToken, acct.Username); err != nil {
		if provider.IsNetworkError(err) {
			return StatusOffline, fmt.Sprintf("WARNING: server unreachable: %v", err)
		}
		return StatusWarning, fmt.Sprintf("WARNING: companion PAT stored but API check failed: %v", err)
	}
	return StatusOK, "API via companion PAT (GCM handles git operations)"
}

// checkToken verifies the PAT used for everything.
func checkToken(acct config.Account, accountKey string, cfg *config.Config) StatusResult {
	r := StatusResult{PrimaryType: "token"}

	token, _, err := ResolveAPIToken(acct, accountKey)
	if err != nil {
		r.Primary = StatusError
		r.PrimaryDetail = "API token not found"
		r.PAT = StatusError
		r.PATDetail = r.PrimaryDetail
		r.Overall = StatusError
		return r
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
		if provider.IsNetworkError(err) {
			r.Primary = StatusOffline
			r.PrimaryDetail = fmt.Sprintf("server unreachable: %v", err)
			r.PAT = StatusOffline
			r.PATDetail = r.PrimaryDetail
			r.Overall = StatusOffline
		} else {
			r.Primary = StatusWarning
			r.PrimaryDetail = fmt.Sprintf("API check failed: %v", err)
			r.PAT = StatusWarning
			r.PATDetail = r.PrimaryDetail
			r.Overall = StatusWarning
		}
		return r
	}

	r.Primary = StatusOK
	r.PrimaryDetail = "OK"
	r.PAT = StatusOK
	r.PATDetail = "OK"
	r.Overall = StatusOK
	return r
}

// checkPATStatus checks if a companion PAT file exists and works.
// Returns (status, detail). Detail is empty for StatusOK/StatusNone so the
// caller can fill in context-appropriate messages.
func checkPATStatus(acct config.Account, accountKey string, cfg *config.Config) (Status, string) {
	token, _, err := ResolveToken(acct, accountKey)
	if err != nil {
		return StatusNone, "" // not configured (optional)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
		if provider.IsNetworkError(err) {
			return StatusOffline, fmt.Sprintf("server unreachable: %v", err)
		}
		return StatusWarning, "" // stored but broken
	}
	return StatusOK, ""
}

// combineStatus derives the overall status from primary + PAT.
//
// After the GCM refactor, Primary carries credential-presence information
// (is the credential artefact there at all?) and PAT carries API-reach
// information (is the REST API actually reachable with something?). That
// makes the merge straightforward: primary-level failures dominate; when
// primary is OK or Warning, the PAT row decides.
//
//	primary=Error     → Error      (no credential → nothing works)
//	primary=Offline   → Offline    (only token/ssh can surface this)
//	pat=OK            → OK         (API reachable, primary at worst Warning)
//	pat=Offline       → Offline    (we can't reach the server)
//	pat=Error         → Error      (rare; reserved for future use)
//	pat=None/Warning  → Warning    (API unavailable; user can still git)
func combineStatus(primary, pat Status) Status {
	if primary == StatusError {
		return StatusError
	}
	if primary == StatusOffline {
		return StatusOffline
	}
	switch pat {
	case StatusOK:
		return StatusOK
	case StatusOffline:
		return StatusOffline
	case StatusError:
		return StatusError
	default:
		// pat is None (no PAT configured) or Warning (stored but broken).
		return StatusWarning
	}
}

// isAPIAuthRejection reports whether an error from provider.TestAuth looks
// like the server rejecting the credential (HTTP 401 or text that names an
// authentication failure), as opposed to a network error, a TLS error, or
// some other HTTP failure mode. It's intentionally conservative — the only
// place that wraps 401 as a human string is pkg/provider/http.go, so we
// match on those signatures rather than on a numeric status.
func isAPIAuthRejection(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 401") ||
		strings.Contains(msg, "authentication failed") ||
		strings.Contains(msg, "token is invalid")
}

// providerNeedsPATForAPI reports whether the provider's REST API refuses
// account passwords at the auth layer (so a GCM-cached password won't work
// for discovery / repo creation even though it works for `git push`/`pull`).
// Forgejo and Gitea share that constraint; Bitbucket Cloud also requires
// app passwords / PATs. GitHub and GitLab GCM entries are OAuth tokens and
// do not fall into this bucket.
func providerNeedsPATForAPI(p string) bool {
	switch p {
	case "forgejo", "gitea", "bitbucket":
		return true
	default:
		return false
	}
}

// providerDisplayName returns a human-facing capitalization of the
// provider identifier for use in user-visible status messages. Provider
// IDs are lowercase in config; messages look wrong when rendered raw.
func providerDisplayName(p string) string {
	switch p {
	case "github":
		return "GitHub"
	case "gitlab":
		return "GitLab"
	case "gitea":
		return "Gitea"
	case "forgejo":
		return "Forgejo"
	case "bitbucket":
		return "Bitbucket"
	default:
		return p
	}
}

// isSSHNetworkError checks if an SSH connection error indicates a network
// problem (server unreachable) rather than an authentication issue.
func isSSHNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Connection refused") ||
		strings.Contains(msg, "Connection timed out") ||
		strings.Contains(msg, "Could not resolve hostname") ||
		strings.Contains(msg, "No route to host") ||
		strings.Contains(msg, "Network is unreachable")
}
