package main

import (
	"slices"

	"github.com/LuisPalacios/gitbox/pkg/credential"
)

// refreshCredentialAudit performs filesystem inspection outside the config lock.
// A separate lock prevents concurrent System check requests from caching stale results.
func (a *App) refreshCredentialAudit() []credential.Finding {
	a.credentialAuditMu.Lock()
	defer a.credentialAuditMu.Unlock()
	findings := credential.AuditCredentialDir()
	a.mu.Lock()
	a.credentialFindings = slices.Clone(findings)
	a.mu.Unlock()
	return findings
}
