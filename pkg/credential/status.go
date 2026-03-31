package credential

import (
	"context"
	"fmt"
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
		r.Primary = StatusWarning
		r.PrimaryDetail = "SSH key ready, connection failed"
	} else {
		r.Primary = StatusOK
		r.PrimaryDetail = "SSH connection OK"
	}

	// PAT: optional companion token for discovery/API.
	r.PAT = checkPATStatus(acct, accountKey, cfg)
	if r.PAT == StatusOK {
		r.PATDetail = "PAT verified"
	} else if r.PAT == StatusWarning {
		r.PATDetail = "PAT stored but API check failed"
	} else {
		r.PATDetail = "no PAT (discovery/API unavailable)"
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
			r.Primary = StatusWarning
			r.PrimaryDetail = fmt.Sprintf("GCM token found but API check failed: %v", err)
		} else {
			r.Primary = StatusOK
			r.PrimaryDetail = "GCM verified"
		}
	}

	// PAT: optional companion token for repo creation/mirrors.
	r.PAT = checkPATStatus(acct, accountKey, cfg)
	if r.PAT == StatusOK {
		r.PATDetail = "PAT verified"
	} else if r.PAT == StatusWarning {
		r.PATDetail = "PAT stored but API check failed"
	} else {
		r.PATDetail = "no PAT (repo creation unavailable)"
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
		r.Primary = StatusWarning
		r.PrimaryDetail = fmt.Sprintf("API check failed: %v", err)
		r.PAT = StatusWarning
		r.PATDetail = r.PrimaryDetail
		r.Overall = StatusWarning
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
func checkPATStatus(acct config.Account, accountKey string, cfg *config.Config) Status {
	token, _, err := ResolveToken(acct, accountKey)
	if err != nil {
		return StatusNone // not configured (optional)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := provider.TestAuth(ctx, acct.Provider, acct.URL, token, acct.Username); err != nil {
		return StatusWarning // stored but broken
	}
	return StatusOK
}

// combineStatus derives the overall status from primary + PAT.
// Primary errors dominate. PAT=None (optional, not configured) shows as warning.
func combineStatus(primary, pat Status) Status {
	if primary == StatusError {
		return StatusError
	}
	if primary == StatusWarning {
		return StatusWarning
	}
	// Primary is OK.
	if pat == StatusOK {
		return StatusOK
	}
	// PAT is None (not configured) or Warning (broken) — show as warning.
	return StatusWarning
}
