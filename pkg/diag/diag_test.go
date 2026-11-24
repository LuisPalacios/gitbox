package diag

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

func TestCollect_AllowlistsConfigurationAndActivity(t *testing.T) {
	now := time.Now()
	const (
		token = "ghp_CANARY012345678901234567890123456789"
		path  = "/private/diagnostics-canary/checkout"
		email = "canary-user@some-organisation.example"
		host  = "sensitive-subdomain.example"
	)
	remoteURL := "https://canary-user:" + token + "@" + host + "/organisation/repo"
	cfg := &config.Config{
		Global: config.GlobalConfig{Roots: []string{path}},
		Accounts: map[string]config.Account{
			token: {Provider: "github", URL: remoteURL, Username: "canary-user", Email: email, DefaultCredentialType: "gcm"},
			email: {Provider: host, DefaultCredentialType: token},
		},
		Sources: map[string]config.Source{
			host: {Folder: path, Account: token, Repos: map[string]config.Repo{
				remoteURL: {CredentialType: "ssh"},
				email:     {CredentialType: path},
			}},
		},
	}
	activity := &Activity{}
	depth := 3
	raw := strings.Join([]string{token, path, email, host}, " ")
	activity.Record(Fetch, now.Add(-time.Second), now, &depth, path, &git.GitError{
		Code: git.CodeAuthFailed, Op: raw, Host: host, Detail: raw, Hint: raw, Err: errors.New(raw),
	})
	depth = 4
	activity.Record(RepoProbe, now, now, nil, path, &provider.ProviderError{Class: provider.ClassRateLimited, Detail: raw})
	activity.Record(RepoProbe, now, now, nil, path, &provider.ProviderError{Class: provider.ProviderClass(host), Detail: raw})
	activity.Record(FetchAll, now, now, nil, "", &git.GitError{Code: git.CodeCancelled})
	activity.Record(OperationCode(host), now, now, nil, path, errors.New(raw))
	state := State{
		AppVersion: "v2.7.4-62-g09d8821-dev",
		Commit:     "09d8821d2e7ca13a07c552577978864a3ac88a24",
		GitVersion: "git version 2.50.1 (Apple Git-155.1)\n",
		Config:     cfg,
		Activity:   activity,
		Scheduler:  []Task{{Name: "fetch-all", InFlight: true, LastResult: raw}, {Name: host, LastResult: "ok"}},
		StartupMS:  map[string]int64{"firstRow": 7, token: 90},
	}
	b := Collect(state, now)
	doc, err := b.Document()
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{token, path, email, host, "canary-user", "organisation/repo"} {
		if strings.Contains(doc, canary) {
			t.Errorf("bundle contains a canary %q", canary)
		}
	}
	if b.SchemaVersion != 1 || b.AppVersion != state.AppVersion || b.Commit != state.Commit || b.GitVersion != "2.50.1" {
		t.Fatalf("missing build identity: %+v", b)
	}
	if c := b.Counts; c.AccountsByProvider["github"] != 1 || c.AccountsByProvider["other"] != 1 ||
		c.Credentials["gcm"] != 1 || c.Credentials["ssh"] != 1 || c.Credentials["other"] != 2 ||
		c.Repos != 2 || c.Roots != 1 || c.Sources != 1 {
		t.Fatalf("wrong projected counts: %+v", c)
	}
	digest := sha256.Sum256([]byte(path))
	wantID := fmt.Sprintf("%x", digest[:8])
	if len(b.Operations) != 4 {
		t.Fatalf("wrong operations: %+v", b.Operations)
	}
	fetch, probe, other := b.Operations[0], b.Operations[1], b.Operations[2]
	if fetch.RepoID != wantID || fetch.ErrorCode != "git.auth_failed" || !fetch.Failed ||
		fetch.DurationMS != 1000 || fetch.QueueDepth == nil || *fetch.QueueDepth != 3 {
		t.Fatalf("wrong fetch: %+v", fetch)
	}
	if probe.RepoID != wantID || probe.ErrorCode != "provider.rate_limited" || other.ErrorCode != "provider.unknown" {
		t.Fatalf("wrong provider errors: %+v and %+v", probe, other)
	}
	bulk := b.Operations[3]
	if !bulk.Failed || bulk.ErrorCode != "git.cancelled" || bulk.RepoID != "" {
		t.Fatalf("wrong fleet error without a path: %+v", bulk)
	}
	if len(b.Scheduler) != 1 || b.Scheduler[0].LastResult != "" || !b.Scheduler[0].InFlight ||
		len(b.StartupMS) != 1 || b.StartupMS["firstRow"] != 7 {
		t.Fatalf("wrong scheduler or startup snapshot: %+v %+v", b.Scheduler, b.StartupMS)
	}
}

func TestCollect_RecognizesBuildVersions(t *testing.T) {
	for _, tc := range []struct{ app, git, wantApp, wantGit string }{
		{"v2.7.4", "git version 2.44.0.windows.1\r\n", "v2.7.4", "2.44.0.windows.1"},
		{"v2.7.4-62-g09d8821-gate", "git version 2.39.5", "v2.7.4-62-g09d8821-gate", "2.39.5"},
		{"v2.7.4-rc.1", "git version 2.48.0\n", "v2.7.4-rc.1", "2.48.0"},
		{"09d8821-dirty-dev", "git version 2.50.1 (Apple Git-155)", "09d8821-dirty-dev", "2.50.1"},
		{"internal.example", "git version 2.1.2\n/home/canary", "unknown", "unknown"},
	} {
		b := Collect(State{AppVersion: tc.app, GitVersion: tc.git}, time.Now())
		if b.AppVersion != tc.wantApp || b.GitVersion != tc.wantGit {
			t.Errorf("got %s / %s, want %s / %s", b.AppVersion, b.GitVersion, tc.wantApp, tc.wantGit)
		}
	}
}

func TestActivity_CapsAndExpiresSamples(t *testing.T) {
	now := time.Now()
	var a Activity
	a.Record(Clone, now, now, nil, "", nil)
	a.Record(Status, now.Add(-activityWindow-time.Second), now.Add(-activityWindow-time.Second), nil, "", nil)
	if records := a.recent(now); len(records) != 1 || records[0].Code != Clone {
		t.Fatalf("expired sample survived: %+v", records)
	}
	for i := range maxOperations + 3 {
		a.Record(Fetch, now.Add(-time.Millisecond*time.Duration(i)), now, &i, "", nil)
	}
	records := a.recent(now)
	if len(records) != maxOperations || *records[0].QueueDepth != 3 ||
		*records[len(records)-1].QueueDepth != maxOperations+2 {
		t.Fatalf("ring did not keep the most recent records")
	}
	*records[0].QueueDepth = 42
	if *a.recent(now)[0].QueueDepth != 3 {
		t.Fatal("snapshot changed the retained activity")
	}
}

func TestRedact_RefusesRecognizablePrivateText(t *testing.T) {
	for _, raw := range []string{
		`{"unknown":"/home/canary"}`, `{"unknown":"C:\\Users\\canary"}`, `{"unknown":"https://canary.example"}`,
		`{"unknown":"canary@example.test"}`, `{"unknown":"ghp_CANARY0123456789"}`,
	} {
		if document, err := Redact(raw); document != "" || err == nil {
			t.Errorf("accepted unsafe text: %s", document)
		}
	}
}
