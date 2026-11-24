package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/heal"
)

func testTransitionOwner(cfg *config.Config) TransitionOwner {
	return TransitionOwner{
		Commit: func(change func(*config.Config) error) (*config.Config, error) {
			next, err := cfg.Clone()
			if err != nil {
				return nil, err
			}
			if err := change(next); err != nil {
				return nil, err
			}
			*cfg = *next
			return cfg.Clone()
		},
		Repair: func(cfg *config.Config, source, repo string) (heal.Report, bool) {
			return heal.Repo(cfg, source, repo), true
		},
	}
}

func renameAccountInMemory(t *testing.T, cfg *config.Config, oldKey, newKey string) error {
	report, err := RenameAccount(t.Context(), cfg, oldKey, newKey, testTransitionOwner(cfg))
	if err == nil && !report.OK {
		return fmt.Errorf("rename incomplete: %+v", report)
	}
	return err
}

func failedStep(report StepReport, name CredentialStep, repo string) bool {
	for _, step := range report.Steps {
		if !step.OK && step.Name == name && step.Repo == repo {
			return true
		}
	}
	return false
}

func TestCredentialSwitchRejectsInvalidCandidateAndKeepsOldArtifacts(t *testing.T) {
	for _, scenario := range []struct {
		name string
		fail CredentialStep
	}{
		{"candidate rejected", StepVerify},
		{"save failed", StepSave},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, cfg := isolate(t)
			const canary = "fixture-secret-not-for-step-reports"
			acct := testAccount("gcm")
			if err := AddAccount(cfg, "acct", acct); err != nil {
				t.Fatal(err)
			}
			if err := credential.StoreToken("acct", "previous-pat"); err != nil {
				t.Fatal(err)
			}
			owner := testTransitionOwner(cfg)
			owner.Commit = func(func(*config.Config) error) (*config.Config, error) {
				return nil, errors.New(canary)
			}
			report, err := changeCredentialType(t.Context(), cfg, "acct", "token", canary, owner,
				func(_ context.Context, _ *config.Config, _ string, _ config.Account, presented string) error {
					if presented != canary {
						t.Error("checked a different credential")
					}
					if scenario.fail == StepVerify {
						return errors.New(canary)
					}
					return nil
				})
			if err != nil || !failedStep(report, scenario.fail, "") {
				t.Fatalf("want failure %q, got %+v (%v)", scenario.fail, report, err)
			}
			if data, _ := json.Marshal(report); strings.Contains(string(data), canary) {
				t.Fatal("step report exposed a secret")
			}
			if old, err := credential.GetToken("acct"); err != nil || old != "previous-pat" {
				t.Fatal("old token changed")
			}
			if current := cfg.Accounts["acct"]; current.DefaultCredentialType != "gcm" || current.CredentialTransition != nil {
				t.Fatalf("old account config changed: %+v", current)
			}
			if files, err := os.ReadDir(filepath.Dir(credential.CredentialFilePath("acct"))); err != nil || len(files) != 1 {
				t.Fatalf("unexpected files after rollback: %v, %v", files, err)
			}
		})
	}
}

func TestCredentialSwitchRetryAfterRepairFailure(t *testing.T) {
	root, cfg := isolate(t)
	const key, repo = "acct", "alice/repo"
	// The configured alias belongs to the fixture, so the isolated
	// successful verification path needs no OpenSSH or provider process.
	if err := AddAccount(cfg, key, testAccount("token")); err != nil {
		t.Fatal(err)
	}
	acct := cfg.Accounts[key]
	acct.SSH = &config.SSHConfig{Host: "fixture-custom"}
	cfg.Accounts[key] = acct
	if err := credential.StoreToken(key, "old-fixture"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddRepo(key, repo, config.Repo{}); err != nil {
		t.Fatal(err)
	}
	makeClone(t, cfg, key, repo, "https://alice@github.com/alice/repo.git")
	owner := testTransitionOwner(cfg)
	path := filepath.Join(root, "retry.json")
	commit := owner.Commit
	owner.Commit = func(change func(*config.Config) error) (*config.Config, error) {
		saved, err := commit(change)
		if err != nil {
			return nil, err
		}
		return saved, config.Save(saved, path)
	}
	owner.Repair = func(*config.Config, string, string) (heal.Report, bool) {
		return heal.Report{Warnings: []string{"fixture write failure"}}, true
	}
	report, err := changeCredentialType(t.Context(), cfg, key, "ssh", "", owner,
		func(context.Context, *config.Config, string, config.Account, string) error { return nil })
	if err != nil || !failedStep(report, StepReconfigure, key+"/"+repo) {
		t.Fatalf("repair = %+v (%v)", report, err)
	}
	reopened, err := config.Load(path)
	if err != nil || reopened.Accounts[key].CredentialTransition == nil {
		t.Fatalf("pending state was not saved: %v", err)
	}
	if _, err := credential.GetToken(key); err != nil {
		t.Fatal("old token retired before repair")
	}

	owner = testTransitionOwner(reopened)
	report, err = ChangeCredentialType(t.Context(), reopened, key, "ssh", "", owner)
	if err != nil || !report.OK || reopened.Accounts[key].CredentialTransition != nil {
		t.Fatalf("retry = %+v (%v)", report, err)
	}
	if _, err := credential.GetToken(key); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old token survived successful repair")
	}
}

func TestRenameAccountDoesNotOverwriteDestinationCredential(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "old", testAccount("token")); err != nil {
		t.Fatal(err)
	}
	if err := credential.StoreToken("old", "original-fixture"); err != nil {
		t.Fatal(err)
	}
	if err := credential.StoreToken("new", "unrelated-fixture"); err != nil {
		t.Fatal(err)
	}
	report, err := RenameAccount(t.Context(), cfg, "old", "new", testTransitionOwner(cfg))
	if err != nil || !failedStep(report, StepPrepare, "") {
		t.Fatalf("rename = %+v (%v)", report, err)
	}
	if _, exists := cfg.Accounts["new"]; exists || cfg.Accounts["old"].CredentialTransition != nil {
		t.Fatal("rename changed config despite destination conflict")
	}
	for key, want := range map[string]string{"old": "original-fixture", "new": "unrelated-fixture"} {
		if got, err := credential.GetToken(key); err != nil || got != want {
			t.Fatalf("credential %s was overwritten", key)
		}
	}
}

func TestRenameAccountResumesAfterSaveFailure(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "old", testAccount("token")); err != nil {
		t.Fatal(err)
	}
	if err := credential.StoreToken("old", "fixture"); err != nil {
		t.Fatal(err)
	}
	owner := testTransitionOwner(cfg)
	commit := owner.Commit
	owner.Commit = func(change func(*config.Config) error) (*config.Config, error) {
		if cfg.Accounts["old"].CredentialTransition != nil {
			return nil, errors.New("fixture commit failure")
		}
		return commit(change)
	}
	report, err := RenameAccount(t.Context(), cfg, "old", "new", owner)
	if err != nil || !failedStep(report, StepSave, "") {
		t.Fatalf("rename = %+v (%v)", report, err)
	}
	if _, exists := cfg.Accounts["new"]; exists {
		t.Fatal("old account stopped being active")
	}
	if err := AddAccount(cfg, "new", testAccount("token")); !errors.Is(err, ErrCredentialTransitionPending) {
		t.Fatalf("another account acquired the reserved name: %v", err)
	}
	if _, err := credential.GetToken("old"); err != nil {
		t.Fatal("old token retired on save failure")
	}
	reopened, err := cfg.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if err := renameAccountInMemory(t, reopened, "old", "new"); err != nil {
		t.Fatal(err)
	}
	if _, err := credential.GetToken("old"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old token survived the completed rename")
	}
	if got, err := credential.GetToken("new"); err != nil || got != "fixture" {
		t.Fatal("new token is missing")
	}
}

func TestRenameAccountReadOnlyCredentialDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory mode bits are not enforced on Windows")
	}
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "old", testAccount("token")); err != nil {
		t.Fatal(err)
	}
	if err := credential.StoreToken("old", "original-fixture"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(credential.CredentialFilePath("old"))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Error(err)
		}
	}()
	if probe, err := os.CreateTemp(dir, ".probe"); err == nil {
		probe.Close()
		os.Remove(probe.Name())
		t.Skip("this process can write to read-only directories")
	}
	report, err := RenameAccount(t.Context(), cfg, "old", "new", testTransitionOwner(cfg))
	if err != nil || !failedStep(report, StepPrepare, "") {
		t.Fatalf("rename = %+v (%v)", report, err)
	}
	if _, exists := cfg.Accounts["new"]; exists {
		t.Fatal("rename committed without preparing the replacement")
	}
	if got, err := credential.GetToken("old"); err != nil || got != "original-fixture" {
		t.Fatal("old token changed")
	}
	if _, err := credential.GetToken("new"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed rename left a destination token")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := renameAccountInMemory(t, cfg, "old", "new"); err != nil {
		t.Fatal(err)
	}
}

func TestRenameAccountRetriesRetirementAndRejectsReassignedOldKey(t *testing.T) {
	_, cfg := isolate(t)
	const repo = "alice/repo"
	if err := AddAccount(cfg, "old", testAccount("token")); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddRepo("old", repo, config.Repo{}); err != nil {
		t.Fatal(err)
	}
	if err := credential.StoreToken("old", "fixture"); err != nil {
		t.Fatal(err)
	}
	makeClone(t, cfg, "old", repo, "https://alice@github.com/alice/repo.git")
	owner := testTransitionOwner(cfg)
	owner.Repair = func(*config.Config, string, string) (heal.Report, bool) {
		if err := credential.StoreToken("old", "independent-replacement"); err != nil {
			t.Fatal(err)
		}
		return heal.Report{}, true
	}
	report, err := RenameAccount(t.Context(), cfg, "old", "new", owner)
	if err != nil || !failedStep(report, StepRetire, "") {
		t.Fatalf("rename = %+v (%v)", report, err)
	}
	if got, _ := credential.GetToken("old"); got != "independent-replacement" {
		t.Fatal("retirement deleted the changed credential")
	}
	cfg.Accounts["old"] = testAccount("token") // Simulate an external writer.
	if _, err := RenameAccount(t.Context(), cfg, "old", "new", testTransitionOwner(cfg)); !errors.Is(err, config.ErrExists) {
		t.Fatalf("rename retry did not reject re-created key: %v", err)
	}
	if got, _ := credential.GetToken("old"); got != "independent-replacement" {
		t.Fatal("retry deleted the re-created account's credential")
	}
	delete(cfg.Accounts, "old")
	if err := credential.StoreToken("old", "fixture"); err != nil {
		t.Fatal(err)
	}
	if err := renameAccountInMemory(t, cfg, "old", "new"); err != nil {
		t.Fatal(err)
	}
}
