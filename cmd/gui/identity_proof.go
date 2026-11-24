package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// IdentityProofDTO is the host's display-safe evidence for one tracked clone.
type IdentityProofDTO heal.IdentityProof

type proofVerification struct {
	identity string
	started  time.Time
	value    heal.Verification
}

func credentialCheckIdentity(cfg *config.Config, key string, revision uint64) string {
	fingerprint, _ := json.Marshal(struct {
		Account  config.Account
		SSH      *config.SSHGlobal
		GCM      *config.GCMGlobal
		Revision uint64
	}{cfg.Accounts[key], cfg.Global.CredentialSSH, cfg.Global.CredentialGCM, revision})
	return string(fingerprint)
}

// GetIdentityProof reads the owning host's Git config without repairing it.
func (a *App) GetIdentityProof(source, repo string) (IdentityProofDTO, error) {
	return a.be().GetIdentityProof(source, repo)
}

func (a *App) getIdentityProofLocal(source, repo string) IdentityProofDTO {
	a.mu.Lock()
	cfg := a.refreshConfigLocked()
	account := cfg.Sources[source].Account
	revision := a.credentialRevision[account]
	a.mu.Unlock()

	a.refreshMu.Lock()
	cached := a.proofChecks[account]
	a.refreshMu.Unlock()
	var check heal.Verification
	acct := cfg.Accounts[account]
	r := cfg.Sources[source].Repos[repo]
	if r.EffectiveCredentialType(&acct) == acct.DefaultCredentialType && cached.identity == credentialCheckIdentity(cfg, account, revision) {
		check = cached.value
	}
	ctx, cancel := context.WithTimeout(a.refreshContext(), healReadTimeout)
	defer cancel()
	return IdentityProofDTO(heal.Proof(ctx, cfg, source, repo, check, time.Now()))
}

// RepairRepoIdentity applies the existing clone maintenance without a network fetch.
func (a *App) RepairRepoIdentity(source, repo string) error {
	return a.be().RepairRepoIdentity(source, repo)
}

func (a *App) repairRepoIdentityLocal(source, repo string) error {
	if a.readOnly {
		return errors.New("repository repair is unavailable")
	}
	cfg := a.snapshotRefreshConfig()
	if _, ok := cfg.Sources[source].Repos[repo]; !ok {
		return errors.New("tracked clone not found")
	}
	report, current := a.repairClone(cfg, source, repo)
	if !current || report.Skipped != "" {
		return errors.New("tracked clone is unavailable")
	}
	if len(report.Warnings) > 0 {
		return errors.New("some Git settings still require attention")
	}
	return nil
}

// OpenRepoConfigInEditor opens the shared config of a tracked local clone.
func (a *App) OpenRepoConfigInEditor(source, repo string) error {
	if err := a.refuseInRemote(); err != nil {
		return err
	}
	cfg := a.snapshotRefreshConfig()
	entry, ok := cfg.Sources[source].Repos[repo]
	if !ok {
		return errors.New("tracked clone not found")
	}
	path := status.RepoPath(cfg, source, repo, entry)
	if !git.IsRepo(path) && !git.IsLinkedWorktree(path) {
		return errors.New("tracked clone is unavailable")
	}
	ctx, cancel := context.WithTimeout(a.refreshContext(), healReadTimeout)
	defer cancel()
	configPath, err := git.CommonConfigPath(ctx, path)
	if err != nil || !filepath.IsAbs(configPath) {
		return errors.New("git config is unavailable")
	}
	info, err := os.Lstat(configPath)
	if err != nil || !info.Mode().IsRegular() || !a.sameRefreshRepo(cfg, source, repo) {
		return errors.New("git config is unavailable")
	}

	return a.openRepoGitConfig(configPath)
}
