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

// checkGCM verifies GCM OAuth + optional companion PAT.
func checkGCM(acct config.Account, accountKey string, cfg *config.Config) StatusResult {
	r := StatusResult{PrimaryType: "gcm"}

	// Primary: GCM OAuth via git credential fill.
	token, _, err := ResolveGCMToken(acct.URL, acct.Username)
	if err != nil {
		r.Primary = StatusError
		r.PrimaryDetail = "GCM credential not found"
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
			if provider.IsNetworkError(err) {
				r.Primary = StatusOffline
				r.PrimaryDetail = fmt.Sprintf("server unreachable: %v", err)
			} else {
				r.Primary = StatusWarning
				// Forgejo/Gitea refuse account passwords at the REST API.
				// When GCM cached a password (the common case when a PAT
				// wasn't pasted into GCM's prompt), the 401 is expected and
				// the "token is invalid" phrasing from the HTTP layer is
				// misleading — the credential is fine for git push/pull,
				// it's just not a valid API credential. Rewrite the detail
				// so the user knows git still works and how to fix API.
				if isAPIAuthRejection(err) && providerNeedsPATForAPI(acct.Provider) {
					r.PrimaryDetail = fmt.Sprintf(
						"GCM credential works for git push/pull, but %s/Forgejo API refuses account passwords. Paste a PAT into GCM's prompt or store a companion PAT for API access.",
						acct.Provider,
					)
				} else {
					r.PrimaryDetail = fmt.Sprintf("GCM credential found but API check failed: %v", err)
				}
			}
		} else {
			r.Primary = StatusOK
			r.PrimaryDetail = "GCM verified"
		}
	}

	// PAT: optional companion token.
	// When the primary GCM credential already authenticates against the API
	// (GitHub OAuth token, or a Forgejo PAT pasted into GCM's password
	// prompt), a separate keyring PAT is not needed for discovery or repo
	// creation — GCM already covers both. A keyring PAT is only required
	// when the provider's API refuses what GCM cached (Forgejo + password)
	// or when a portable PAT is needed for push/pull mirrors to a server
	// that can't use machine-local GCM tokens.
	r.PAT, r.PATDetail = checkPATStatus(acct, accountKey, cfg)
	if r.PATDetail == "" {
		switch r.PAT {
		case StatusOK:
			r.PATDetail = "PAT verified"
		case StatusWarning:
			r.PATDetail = "PAT stored but API check failed"
		default: // StatusNone
			if r.Primary == StatusOK {
				r.PATDetail = "not needed — GCM covers the API (PAT only required for mirrors)"
			} else {
				r.PATDetail = "needed for discovery and repo creation — GCM credential was rejected by the API"
			}
		}
	}

	r.Overall = combineStatus(r.Primary, r.PAT)
	return r
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
// The rule is "is API access reachable via at least one credential?" — not
// "are both credentials present". For providers where the primary credential
// (e.g. GitHub GCM OAuth token) already authenticates against the API, a
// companion PAT is redundant and its absence must not degrade the overall
// state. For providers where the primary credential cannot talk to the API
// (e.g. Forgejo/Gitea GCM — the stored password is rejected by the REST API),
// the PAT is the fallback and must be OK for the overall state to be OK.
func combineStatus(primary, pat Status) Status {
	if primary == StatusError {
		return StatusError
	}
	if primary == StatusOffline {
		return StatusOffline
	}
	// API is reachable if either the primary credential succeeded against
	// the API, or the companion PAT did.
	if primary == StatusOK || pat == StatusOK {
		return StatusOK
	}
	// Neither the primary nor the PAT can reach the API; pick the most
	// actionable state for the user.
	if pat == StatusOffline {
		return StatusOffline
	}
	return StatusWarning
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
