package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func startupTestApp(root string) *App {
	parent := filepath.Join(root, "account-a", "org", "container")
	return &App{cfg: &config.Config{
		Version:  config.CurrentVersion,
		Global:   config.GlobalConfig{Folder: root, ViewMode: "compact", UIMode: "simple", Theme: "light", PeriodicSync: "15m", Collapsed: &config.CollapsedState{Repos: []string{"account-a/org/container"}, Sidebar: true, View: "account:account-b"}},
		Accounts: map[string]config.Account{"account-a": {Provider: "github"}, "account-b": {Provider: "github"}},
		Sources: map[string]config.Source{
			"account-a": {Account: "account-a", Repos: map[string]config.Repo{"org/container": {Container: true}}, RepoOrder: []string{"org/container"}},
			"account-b": {Account: "account-b", Repos: map[string]config.Repo{"org/child": {CloneFolder: filepath.Join(parent, "child")}}, RepoOrder: []string{"org/child"}},
		},
		SourceOrder: []string{"account-a", "account-b"},
	}, cfgPath: filepath.Join(root, "config.json")}
}

func TestStartupSnapshot_NestingBeforeClonesExist(t *testing.T) {
	a := startupTestApp(t.TempDir())
	snapshot, err := a.GetStartupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Nesting["account-b/org/child"] != "account-a/org/container" {
		t.Fatalf("nesting=%v", snapshot.Nesting)
	}
	g := snapshot.Config.Global
	if g.ViewMode != "compact" || g.UIMode != "simple" || g.Theme != "light" || g.PeriodicSync != "15m" || len(g.Collapsed.Repos) != 1 || !g.Collapsed.Sidebar || g.Collapsed.View != "account:account-b" {
		t.Fatalf("preferences missing: %+v", g)
	}
	if snapshot.Config.Sources["account-b"].Account != "account-b" {
		t.Fatal("child ownership changed")
	}
	if snapshot.Config.Sources["account-a"].RepoOrder[0] != "org/container" {
		t.Fatal("repo order lost")
	}
}

func TestStartupSnapshot_DetachesMutableConfiguration(t *testing.T) {
	a := startupTestApp(t.TempDir())
	snapshot, err := a.GetStartupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	a.cfg.Global.Collapsed.Repos[0] = "changed"
	src := a.cfg.Sources["account-b"]
	delete(src.Repos, "org/child")
	src.RepoOrder[0] = "changed"
	delete(a.cfg.Accounts, "account-a")
	if snapshot.Config.Global.Collapsed.Repos[0] != "account-a/org/container" || len(snapshot.Config.Sources["account-b"].Repos) != 1 || len(snapshot.Config.Accounts) != 2 || snapshot.Config.Sources["account-b"].RepoOrder[0] != "org/child" {
		t.Fatal("snapshot shares mutable state")
	}
}

func BenchmarkStartupSnapshot(b *testing.B) {
	a := startupTestApp(b.TempDir())
	src := a.cfg.Sources["account-b"]
	for i := range 30 {
		key := fmt.Sprintf("org/clone-%d", i)
		src.Repos[key] = config.Repo{}
		src.RepoOrder = append(src.RepoOrder, key)
	}
	a.cfg.Sources["account-b"] = src
	b.ResetTimer()
	for b.Loop() {
		if _, err := a.GetStartupSnapshot(); err != nil {
			b.Fatal(err)
		}
	}
}
