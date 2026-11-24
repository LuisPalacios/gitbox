package ops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"

	"golang.org/x/crypto/ssh"
)

type CredentialStep string

const (
	StepPrepare     CredentialStep = "prepare"
	StepReserve     CredentialStep = "reserve"
	StepVerify      CredentialStep = "verify"
	StepSave        CredentialStep = "save"
	StepActivate    CredentialStep = "activate"
	StepReconfigure CredentialStep = "reconfigure"
	StepRetire      CredentialStep = "retire"
	StepFinish      CredentialStep = "finish"
)

var ErrCredentialTransitionPending = errors.New("finish the pending credential change first")

// StepReport contains only fixed codes and configured repo identifiers.
// Underlying errors and provider output must never cross this boundary.
type StepReport struct {
	OK           bool   `json:"ok"`
	Steps        []Step `json:"steps"`
	SSHPublicKey string `json:"sshPublicKey,omitempty"`
	SSHAddURL    string `json:"sshAddURL,omitempty"`
	SSHVerified  bool   `json:"sshVerified,omitempty"`
}

type Step struct {
	Name CredentialStep `json:"name"`
	OK   bool           `json:"ok"`
	Err  string         `json:"err,omitempty"`
	Repo string         `json:"repo,omitempty"`
}

func (r *StepReport) record(step CredentialStep, err error) bool {
	s := Step{Name: step, OK: err == nil}
	if err != nil {
		s.Err = "failed"
	}
	r.Steps = append(r.Steps, s)
	return s.OK
}

// TransitionOwner serializes transition requests and supplies atomic config
// saves and clone repair under the owner's usual path lock. A failed save
// must leave the previous config in force.
type TransitionOwner struct {
	Commit     func(func(*config.Config) error) (*config.Config, error)
	Repair     func(*config.Config, string, string) (heal.Report, bool)
	PrepareGCM func(*config.Config, config.Account) (string, error)
}

func changeCredentialType(ctx context.Context, cfg *config.Config, key, typ, token string, owner TransitionOwner, check func(context.Context, *config.Config, string, config.Account, string) error) (StepReport, error) {
	report := StepReport{Steps: []Step{}}
	if err := ValidateAccountKey(key); err != nil {
		return report, err
	}
	acct, ok := cfg.Accounts[key]
	if !ok {
		return report, config.ErrNotFound
	}
	if !slices.Contains([]string{"token", "ssh", "gcm"}, typ) {
		return report, fmt.Errorf("unknown credential type")
	}
	if pending := acct.CredentialTransition; pending != nil {
		if pending.PreviousKey != "" || pending.RenameTo != "" || acct.DefaultCredentialType != typ {
			return report, ErrCredentialTransitionPending
		}
		return finishCredentialTransition(ctx, cfg, key, owner, report), nil
	}
	if acct.DefaultCredentialType == typ {
		report.OK = true
		return report, nil
	}

	next := acct
	applyCredentialType(&next, key, typ)
	// Explicit clone overrides continue to need the old method's settings.
	if accountUsesOverride(cfg, key, "ssh") && typ != "ssh" {
		next.SSH = acct.SSH
	}
	if acct.GCM != nil && (typ == "gcm" || accountUsesOverride(cfg, key, "gcm")) {
		next.GCM = acct.GCM
	}
	if typ == "ssh" && acct.SSH != nil {
		next.SSH = acct.SSH
	}
	transition := &config.CredentialTransition{
		PreviousType: acct.DefaultCredentialType,
		ManagedSSH:   accountOwnsSSH(cfg, key, acct),
	}
	next.CredentialTransition = transition
	token = strings.TrimSpace(token)
	beforeGCM := cfg.Global.CredentialGCM
	var err error
	switch typ {
	case "ssh":
		if acct.SSH == nil || acct.SSH.Host == "" {
			var setup SSHSetup
			setup, err = prepareSSHCredential(cfg, key, next)
			if err == nil {
				pub, _, _, _, parseErr := ssh.ParseAuthorizedKey([]byte(setup.PublicKey))
				err = parseErr
				if err == nil {
					report.SSHPublicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
					report.SSHAddURL = sshEnrollmentURL(next)
				}
			}
		}
	case "gcm":
		next.Username, err = owner.PrepareGCM(cfg, next)
	case "token":
		if token == "" {
			token, _, err = credential.ResolveToken(next, key)
		} else if envToken, _, fromEnv := credential.EnvironmentToken(key); fromEnv && envToken != token {
			// Heal and future requests honor environment overrides. Reject
			// a different candidate rather than testing one token and using another.
			err = errors.New("an environment token overrides the supplied token")
		}
	}
	if !report.record(StepPrepare, err) {
		return report, nil
	}
	if !report.record(StepVerify, check(ctx, cfg, key, next, token)) {
		return report, nil
	}
	if typ == "ssh" {
		report.SSHVerified = true
	}
	if typ == "token" {
		// Use a separate file until saving proves the switch was accepted.
		// A previous companion PAT can still serve the old credential.
		var id [16]byte
		rand.Read(id[:])
		transition.StagedID = hex.EncodeToString(id[:])
		if !report.record(StepPrepare, credential.StageToken(transition.StagedID, token, next)) {
			return report, nil
		}
	}
	saved, err := owner.Commit(func(current *config.Config) error {
		if !sameTransitionAccount(current, key, acct) {
			return ErrCredentialTransitionPending
		}
		if typ == "gcm" {
			if !sameSetting(current.Global.CredentialGCM, beforeGCM) {
				return ErrCredentialTransitionPending
			}
			current.Global.CredentialGCM = cfg.Global.CredentialGCM
		}
		return current.UpdateAccount(key, next)
	})
	if !report.record(StepSave, err) {
		if transition.StagedID != "" {
			report.record(StepRetire, credential.DiscardStagedToken(transition.StagedID))
		}
		return report, nil
	}
	return finishCredentialTransition(ctx, saved, key, owner, report), nil
}

// ChangeCredentialType verifies and saves the desired credential before
// repairing clones and retiring artifacts no longer in use.
func ChangeCredentialType(ctx context.Context, cfg *config.Config, key, typ, token string, owner TransitionOwner) (StepReport, error) {
	return changeCredentialType(ctx, cfg, key, typ, token, owner, verifyCredentialTransition)
}

// Enrollment links are derived from the provider origin. User info,
// queries and fragments in an account URL cannot cross into the report.
func sshEnrollmentURL(acct config.Account) string {
	u, err := url.Parse(acct.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return credential.SSHPublicKeyURL(acct.Provider, u.String())
}

func verifyCredentialTransition(ctx context.Context, cfg *config.Config, key string, acct config.Account, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch acct.DefaultCredentialType {
	case "token":
		_, err := provider.CheckAuth(ctx, acct.Provider, acct.URL, token, acct.Username)
		return err
	case "gcm":
		// Check the GCM credential itself. A companion PAT cannot attest
		// that the new helper authenticated.
		var err error
		token, _, err = credential.ResolveGCMTokenContext(ctx, acct.URL, acct.Username)
		if err != nil {
			return err
		}
		_, err = provider.CheckAuth(ctx, acct.Provider, acct.URL, token, acct.Username)
		return err
	case "ssh":
		keyFile, err := credential.FindSSHKey(credential.SSHFolder(cfg), acct.SSH.Host, acct.SSH.KeyType)
		if err != nil {
			return err
		}
		_, err = credential.TestSSHIdentity(ctx, credential.SSHFolder(cfg), acct.SSH.Host, keyFile)
		return err
	}
	return errors.New("unsupported credential type")
}

func sameTransitionAccount(cfg *config.Config, key string, expected config.Account) bool {
	current, ok := cfg.Accounts[key]
	return ok && current.Provider == expected.Provider && current.URL == expected.URL &&
		current.Username == expected.Username && current.DefaultCredentialType == expected.DefaultCredentialType &&
		current.Name == expected.Name && current.Email == expected.Email &&
		sameSetting(current.SSH, expected.SSH) && sameSetting(current.GCM, expected.GCM) &&
		sameSetting(current.CredentialTransition, expected.CredentialTransition)
}

func sameSetting[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func accountUsesOverride(cfg *config.Config, key, typ string) bool {
	for _, src := range cfg.Sources {
		if src.Account == key {
			for _, repo := range src.Repos {
				if repo.CredentialType == typ {
					return true
				}
			}
		}
	}
	return false
}

func finishCredentialTransition(ctx context.Context, cfg *config.Config, key string, owner TransitionOwner, report StepReport) StepReport {
	acct := cfg.Accounts[key]
	pending := acct.CredentialTransition
	if pending == nil {
		return report
	}
	if !validTransition(cfg, key, acct, pending) {
		report.record(StepPrepare, ErrCredentialTransitionPending)
		return report
	}
	if pending.StagedID != "" {
		ok, err := credential.ActivateStagedToken(pending.StagedID, key)
		if err == nil && !ok {
			// The stage disappears atomically when it becomes the active file.
			// A retry after that boundary verifies the actual installed PAT.
			var token string
			token, err = credential.GetToken(key)
			if err == nil {
				err = verifyCredentialTransition(ctx, cfg, key, acct, token)
			}
		}
		if !report.record(StepActivate, err) {
			return report
		}
	}

	repaired := true
	for _, sourceKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[sourceKey]
		if src.Account != key {
			continue
		}
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			if status.RepoRootOffline(cfg, sourceKey, repo) {
				report.Steps = append(report.Steps, Step{Name: StepReconfigure, Err: "offline", Repo: sourceKey + "/" + repoKey})
				repaired = false
				continue
			}
			repair, current := owner.Repair(cfg, sourceKey, repoKey)
			if !current || len(repair.Warnings) != 0 {
				report.Steps = append(report.Steps, Step{Name: StepReconfigure, Err: "failed", Repo: sourceKey + "/" + repoKey})
				repaired = false
			}
		}
	}
	if !repaired {
		return report
	}
	report.record(StepReconfigure, nil)

	if pending.PreviousKey != "" {
		if !report.record(StepRetire, retireRenamedCredentials(cfg, key, pending)) {
			return report
		}
	} else if !report.record(StepRetire, retirePreviousType(cfg, key, acct, pending)) {
		return report
	}
	_, err := owner.Commit(func(current *config.Config) error {
		if !sameTransitionAccount(current, key, acct) {
			return ErrCredentialTransitionPending
		}
		acct.CredentialTransition = nil
		return current.UpdateAccount(key, acct)
	})
	report.OK = report.record(StepFinish, err)
	return report
}

func validTransition(cfg *config.Config, key string, acct config.Account, pending *config.CredentialTransition) bool {
	if ValidateAccountKey(key) != nil || pending.RenameTo != "" {
		return false
	}
	if pending.PreviousKey != "" {
		if ValidateAccountKey(pending.PreviousKey) != nil || pending.PreviousKey == key ||
			pending.PreviousType != "" || pending.StagedID != "" {
			return false
		}
		for other := range cfg.Accounts {
			if other != key && strings.EqualFold(other, pending.PreviousKey) {
				return false
			}
		}
		return true
	}
	return slices.Contains([]string{"", "token", "ssh", "gcm"}, pending.PreviousType) &&
		pending.PreviousType != acct.DefaultCredentialType &&
		(pending.StagedID == "" || acct.DefaultCredentialType == "token")
}

func accountOwnsSSH(cfg *config.Config, key string, acct config.Account) bool {
	if acct.SSH == nil || acct.SSH.Host != credential.SSHHostAlias(key) {
		return false
	}
	for other, otherAcct := range cfg.Accounts {
		if other != key && otherAcct.SSH != nil && strings.EqualFold(otherAcct.SSH.Host, acct.SSH.Host) {
			return false
		}
	}
	folder := credential.SSHFolder(cfg)
	file, err := credential.FindSSHKey(folder, acct.SSH.Host, acct.SSH.KeyType)
	return err == nil && pathkey.Compare(file) == pathkey.Compare(credential.SSHKeyPath(folder, key))
}

func retirePreviousType(cfg *config.Config, key string, acct config.Account, pending *config.CredentialTransition) error {
	typ := pending.PreviousType
	if typ == acct.DefaultCredentialType || accountUsesOverride(cfg, key, typ) {
		return nil
	}
	switch typ {
	case "token":
		return credential.DeleteToken(key)
	case "ssh":
		if pending.ManagedSSH {
			_, err := removeSSHKey(cfg, key)
			return err
		}
	case "gcm":
		host := HostnameFromURL(acct.URL)
		for other, candidate := range cfg.Accounts {
			if other != key && strings.EqualFold(candidate.Username, acct.Username) &&
				strings.EqualFold(HostnameFromURL(candidate.URL), host) &&
				(candidate.DefaultCredentialType == "gcm" || accountUsesOverride(cfg, other, "gcm")) {
				return nil
			}
		}
		return rejectGCMCredential(host, acct.Username)
	}
	return nil
}
