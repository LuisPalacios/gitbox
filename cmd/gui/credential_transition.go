package main

import (
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/ops"
)

func (a *App) credentialTransitionConfig() (*config.Config, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.saveRefused(); err != nil {
		return nil, err
	}
	return a.cfg.Clone()
}

func (a *App) renameAccountLocal(oldKey, newKey string) (ops.StepReport, error) {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	a.localSession.invalidateCredential(oldKey)
	a.localSession.invalidateCredential(newKey)
	defer a.localSession.invalidateCredential(oldKey)
	defer a.localSession.invalidateCredential(newKey)

	cfg, err := a.credentialTransitionConfig()
	if err != nil {
		return ops.StepReport{}, err
	}
	return ops.RenameAccount(a.localSession.refreshContext(), cfg, oldKey, newKey, ops.TransitionOwner{
		Commit: func(change func(*config.Config) error) (*config.Config, error) {
			saved, err := a.commitConfig(change)
			if err == nil {
				if _, oldExists := saved.Accounts[oldKey]; oldExists {
					return saved, nil
				}
				a.mu.Lock()
				if entry, ok := a.prs.cache[oldKey]; oldKey != newKey && ok {
					a.prs.cache[newKey] = entry
					delete(a.prs.cache, oldKey)
				}
				a.mu.Unlock()
			}
			return saved, err
		},
		Repair: a.repairUnderMaintenance,
	})
}

func (a *App) changeCredentialTypeLocal(key, typ, token string) (ops.StepReport, error) {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	a.localSession.invalidateCredential(key)
	defer a.localSession.invalidateCredential(key)

	cfg, err := a.credentialTransitionConfig()
	if err != nil {
		return ops.StepReport{}, err
	}
	return ops.ChangeCredentialType(a.localSession.refreshContext(), cfg, key, typ, token, ops.TransitionOwner{
		Commit: a.commitConfig,
		Repair: a.repairUnderMaintenance,
		PrepareGCM: func(cfg *config.Config, acct config.Account) (string, error) {
			if _, err := credential.FixGlobalGCMConfig(cfg); err != nil {
				return "", err
			}
			return signInGCM(acct, &cfg.Global)
		},
	})
}
