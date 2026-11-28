//go:build !gitboxserver

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/recipe"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestRecipeBindings_ReviewFileAndImport(t *testing.T) {
	root := t.TempDir()
	source := windowOn(t, seedConfig(t, "source.json", func(c *config.Config) {
		c.Global.Roots = []string{t.TempDir()}
		s := c.Sources["acct"]
		s.Repos = map[string]config.Repo{"org/repo": {Name: "private-canary", Email: "private@host.invalid"}}
		c.Sources["acct"] = s
	}))
	doc, err := source.PreviewRecipeExport()
	if err != nil || strings.Contains(doc, "private") {
		t.Fatal("export preview contains private information or failed", err)
	}
	path := filepath.Join(t.TempDir(), "recipe.json")
	oldSave, oldOpen := saveRecipeFile, openRecipeFile
	t.Cleanup(func() { saveRecipeFile, openRecipeFile = oldSave, oldOpen })
	saveRecipeFile = func(context.Context, wailsrt.SaveDialogOptions) (string, error) { return path, nil }
	openRecipeFile = func(context.Context, wailsrt.OpenDialogOptions) (string, error) { return path, nil }
	if _, err := source.ExportRecipe(doc + " "); err == nil {
		t.Fatal("saved different bytes from preview")
	}
	if ok, err := source.ExportRecipe(doc); err != nil || !ok {
		t.Fatal(ok, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != doc {
		t.Fatal("saved file differs from preview", err)
	}
	localPath := seedConfig(t, "dest.json", func(c *config.Config) { c.Global.Roots = []string{root}; c.Accounts = nil; c.Sources = nil })
	dest := windowOn(t, localPath)
	in, err := dest.OpenRecipeImport()
	if err != nil || in.Document != doc || len(in.NewAccounts) != 1 || len(in.Slots) != 1 {
		t.Fatal(in, err)
	}
	opt := recipe.Options{Slots: map[string]string{"primary": root}, Identities: map[string]recipe.Identity{"https://github.com/alice": {Name: "My local identity", Email: "local@example.org"}}}
	preview, err := dest.PreviewRecipeImport(in.Document, opt)
	if err != nil || preview.Accounts != 1 || preview.Repos != 1 {
		t.Fatal(preview, err)
	}
	if len(loadConfig(t, localPath).Accounts) != 0 {
		t.Fatal("preview wrote to disk")
	}
	other := windowOn(t, localPath)
	if err := other.SetNestedScanDepth(4); err != nil {
		t.Fatal(err)
	}
	if err := dest.ApplyRecipeImport(doc, opt, preview.Digest); err == nil {
		t.Fatal("saved across external domain change")
	}
	if len(dest.cfg.Accounts) != 0 || len(loadConfig(t, localPath).Accounts) != 0 || dest.cfg.Global.NestedScanDepth != 4 {
		t.Fatal("failed save lost or installed changes")
	}
	preview, err = dest.PreviewRecipeImport(doc, opt)
	if err != nil {
		t.Fatal(err)
	}
	if err := dest.ApplyRecipeImport(doc, opt, preview.Digest); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(t, localPath)
	a := cfg.Accounts["github-alice"]
	if len(cfg.Sources["github-alice"].Repos) != 1 || a.Email != "local@example.org" || cfg.Global.NestedScanDepth != 4 {
		t.Fatal("saved configuration missing reviewed changes")
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatal("import touched the clone root")
	}
}

func TestRecipeBindings_RefuseRemote(t *testing.T) {
	a := newTestApp(withBackend(&RemoteBackend{}))
	if _, err := a.PreviewRecipeExport(); err == nil {
		t.Fatal("export preview allowed")
	}
	if _, err := a.ExportRecipe(""); err == nil {
		t.Fatal("export allowed")
	}
	if _, err := a.OpenRecipeImport(); err == nil {
		t.Fatal("file picker allowed")
	}
	if _, err := a.PreviewRecipeImport("", recipe.Options{}); err == nil {
		t.Fatal("dry run allowed")
	}
	if err := a.ApplyRecipeImport("", recipe.Options{}, ""); err == nil {
		t.Fatal("import allowed")
	}
}
