package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/heal"
)

func TestOnboardingPreviewsNeverWrite(t *testing.T) {
	root := t.TempDir()
	clone := filepath.Join(root, "current", "org", "project")
	initClone(t, clone, "https://github.com/org/project.git")
	path := seedConfig(t, "gitbox.json", func(c *config.Config) {
		c.Global.Roots = []string{root}
		c.Sources["acct"] = config.Source{Account: "acct", Folder: "custom", Repos: map[string]config.Repo{"org/old": {CloneFolder: clone}}}
	})
	a := windowOn(t, path)
	before := stamp(t, path)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := a.PreviewRoot(root)
	if err != nil || !preview.Exists || !preview.Writable || preview.ExistingClones != 1 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	missing := filepath.Join(root, "missing", "part")
	preview, err = a.PreviewRoot(missing)
	if err != nil || preview.Exists || !preview.Writable {
		t.Fatalf("missing preview = %+v, %v", preview, err)
	}
	for repo, want := range map[string]string{"org/project": filepath.Join(root, "custom", "org", "project"), "org/old": clone} {
		if got, err := a.PreviewClonePath("acct", repo); got != want || err != nil {
			t.Errorf("preview %s = %q, %v, want %q", repo, got, err, want)
		}
	}
	if _, err := a.PreviewClonePath("acct", "../elsewhere"); err == nil {
		t.Error("accepted a relative escape")
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	current, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entries, after) || !info.ModTime().Equal(current.ModTime()) || stamp(t, path) != before {
		t.Fatal("preview modified the root or config")
	}
}

func TestOnboardingAdoptsOnlyTheReviewedClone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root := t.TempDir()
	cfgPath := seedConfig(t, "gitbox.json", func(c *config.Config) {
		c.Global.Roots = []string{root}
		acct := c.Accounts["acct"]
		acct.DefaultCredentialType = "gcm"
		c.Accounts["acct"] = acct
	})
	a := windowOn(t, cfgPath)
	chosen := filepath.Join(root, "checkouts", "chosen")
	other := filepath.Join(root, "other")
	for path, repo := range map[string]string{chosen: "alice/chosen", other: "alice/other"} {
		initClone(t, path, "https://github.com/"+repo+".git")
	}
	for _, req := range []struct{ repo, path string }{{"alice/renamed", chosen}, {"alice/chosen", filepath.Join(root, "gone")}, {"alice/chosen", t.TempDir()}} {
		if _, err := a.AdoptFirstProject("acct", req.repo, req.path); err == nil {
			t.Fatal("accepted stale or outside selection", req)
		}
	}
	if ready, err := a.AdoptFirstProject("acct", "alice/chosen", chosen); err != nil || !ready {
		t.Fatalf("adopt = %v, %v", ready, err)
	}
	saved := loadConfig(t, cfgPath)
	if len(saved.Sources["acct"].Repos) != 1 {
		t.Fatalf("adopted extra clones: %+v", saved.Sources)
	}
	physical, _ := filepath.EvalSymlinks(chosen)
	if path, err := a.PreviewClonePath("acct", "alice/chosen"); err != nil || filepath.Clean(path) != physical {
		t.Errorf("project path %q, %v", path, err)
	}
	if gitConfigGet(t, chosen, "user.email") != "a@e" || gitConfigGet(t, other, "user.email") != "" {
		t.Error("identity changed on the wrong clone")
	}
	before := stamp(t, cfgPath)
	if ready, err := a.AdoptFirstProject("acct", "alice/chosen", chosen); err != nil || !ready || stamp(t, cfgPath) != before {
		t.Fatalf("repeat = %v, %v", ready, err)
	}
	a.refreshHooks = &refreshHooks{heal: func(*config.Config, string, string) heal.Report {
		return heal.Report{Path: physical, Warnings: []string{"identity unavailable"}}
	}}
	if ready, err := a.AdoptFirstProject("acct", "alice/chosen", chosen); err != nil || ready {
		t.Fatalf("unverified identity reported ready: %v, %v", ready, err)
	}
}

func TestOnboardingTestFixtureStartsInIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	for _, first := range []bool{false, true} {
		dir := t.TempDir()
		marker := ""
		if first {
			marker = `"_test":{"firstRun":true},`
		}
		data := "{" + marker + `"version":5,"global":{"roots":["~/fixture"],"language":"es"},"accounts":{},"sources":{},"mirrors":{}}`
		if err := os.WriteFile(filepath.Join(dir, "test-gitbox.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		if !first {
			path, cleanup, err := config.SetupTestMode()
			if err != nil {
				t.Fatal(err)
			}
			if len(loadConfig(t, path).Global.Roots) != 1 {
				t.Fatal("the compatibility helper lost its temporary root")
			}
			cleanup()
		}
		path, firstRun, cleanup, err := config.PrepareTestFixture()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(cleanup)
		a := &App{cfgPath: path, testMode: true, testFirstRun: firstRun}
		a.initLocal(context.Background())
		if a.IsFirstRun() != first || a.GetLanguage() != "es" {
			t.Fatalf("first run = %v, language %s", a.IsFirstRun(), a.GetLanguage())
		}
		p, err := a.PreviewRoot("")
		want := filepath.Join(filepath.Dir(filepath.Dir(path)), "git")
		if err != nil || p.Abs != want || strings.HasPrefix(p.Abs, home+string(os.PathSeparator)) {
			t.Fatalf("suggested root = %+v, %v", p, err)
		}
		if first {
			chosen := filepath.Join(dir, "my-projects")
			if err := a.InitRoot(chosen); err != nil {
				t.Fatal(err)
			}
			if got := loadConfig(t, path).Global.Roots; !reflect.DeepEqual(got, []string{chosen}) {
				t.Fatalf("fresh fixture saved roots = %v", got)
			}
		}
	}
}
