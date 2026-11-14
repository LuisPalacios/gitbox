package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

func TestCredentialAudit_StartupSnapshotAndRecheck(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	dir := filepath.Join(root, "gitbox", "credentials")
	unexpected := filepath.Join(dir, "unexpected-directory")
	if err := os.MkdirAll(unexpected, 0o700); err != nil {
		t.Fatal(err)
	}
	a := NewApp()
	a.cfgPath = filepath.Join(root, "missing-config.json")
	a.Startup(context.Background())
	snapshot, err := a.GetStartupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i, finding := range snapshot.CredentialFindings {
		if finding.Path == unexpected && finding.Problem == "unexpected_type" {
			found = true
			snapshot.CredentialFindings[i].Problem = "changed"
		}
	}
	if !found {
		t.Fatal("startup omitted the credential audit")
	}
	again, err := a.GetStartupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range again.CredentialFindings {
		if finding.Problem == "changed" {
			t.Fatal("snapshot aliases cached findings")
		}
	}
	if _, err := os.Stat(unexpected); err != nil {
		t.Fatal("audit modified the entry")
	}
	if err := os.Remove(unexpected); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if findings := a.refreshCredentialAudit(); len(findings) != 0 {
		t.Fatalf("stale findings: %+v", findings)
	}
	refreshed, err := a.GetStartupSnapshot()
	if err != nil || len(refreshed.CredentialFindings) != 0 {
		t.Fatalf("cached findings after recheck: %+v, %v", refreshed.CredentialFindings, err)
	}
}

func TestDoctorReport_CredentialFindingsPreventSuccess(t *testing.T) {
	for _, exposed := range []bool{false, true} {
		finding := credential.Finding{Path: "test-path", Problem: "unverifiable", Exposed: exposed}
		report := makeDoctorReport(nil, nil, []credential.Finding{finding})
		if report.AllOK || len(report.CredentialFindings) != 1 || report.MissingReq != 0 {
			t.Fatalf("credential findings reported as success: %+v", report)
		}
	}
	if !makeDoctorReport(nil, nil, nil).AllOK {
		t.Fatal("clean report should pass")
	}
}

func TestCredentialStoreToken_UsesAccountFormatWithoutClones(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("GIT_TOKEN", "unrelated-environment-token")
	acct := config.Account{Provider: "gitlab", URL: "https://example.org", Username: "test-user", DefaultCredentialType: "token"}
	a := &App{cfg: &config.Config{Accounts: map[string]config.Account{"test": acct}, Sources: map[string]config.Source{}}}
	if result := a.CredentialStoreToken("test", "  supplied-token\n"); !result.OK {
		t.Fatal(result.Message)
	}
	data, err := os.ReadFile(credential.CredentialFilePath("test"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "https://test-user:supplied-token@example.org\n" {
		t.Fatalf("stored value = %q", data)
	}
	for _, call := range []struct{ key, token string }{{"missing", "token"}, {"test", " \n"}} {
		if a.CredentialStoreToken(call.key, call.token).OK {
			t.Fatal("invalid request accepted")
		}
	}
	unchanged, err := os.ReadFile(credential.CredentialFilePath("test"))
	if err != nil || string(unchanged) != string(data) {
		t.Fatal("invalid request altered existing credential")
	}
}
