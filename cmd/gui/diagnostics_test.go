//go:build !gitboxserver

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/diag"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/git/testutil"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestExportDiagnostics_SavesOnlyReviewedBytes(t *testing.T) {
	git.SetRunnerForTest(t, testutil.RunnerFunc(func(_ context.Context, req git.Request) (git.Result, error) {
		if !reflect.DeepEqual(req.Args, []string{"--version"}) {
			t.Fatalf("unexpected Git operation: %v", req.Args)
		}
		return git.Result{Stdout: []byte("git version 2.50.1\n")}, nil
	}))
	dir := t.TempDir()
	a := NewApp()
	t.Cleanup(a.stopRefresh)
	a.cfgPath = filepath.Join(dir, "gitbox.json")
	const configContents = "do not replace the active configuration"
	if err := os.WriteFile(a.cfgPath, []byte(configContents), 0o600); err != nil {
		t.Fatal(err)
	}
	a.cfg = &config.Config{Global: config.GlobalConfig{Roots: []string{dir}}}
	a.timing.mark("decode")

	path := filepath.Join(dir, "report.json")
	selected := path
	var dialogs int
	original := saveDiagnosticsFile
	t.Cleanup(func() { saveDiagnosticsFile = original })
	saveDiagnosticsFile = func(_ context.Context, options wailsrt.SaveDialogOptions) (string, error) {
		dialogs++
		if options.DefaultFilename != "gitbox-diagnostics.json" {
			t.Fatalf("unexpected save dialog: %+v", options)
		}
		return selected, nil
	}
	if saved, err := a.ExportDiagnostics("{}"); saved || err == nil || dialogs != 0 {
		t.Fatalf("export without preview: saved=%v err=%v dialogs=%d", saved, err, dialogs)
	}

	doc, err := a.PreviewDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	var b diag.Bundle
	if err := json.Unmarshal([]byte(doc), &b); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.StartupMS["decode"]; !ok || b.GitVersion != "2.50.1" || b.Counts.Roots != 1 || strings.Contains(doc, dir) {
		t.Fatalf("wrong projection: %+v", b)
	}
	if saved, err := a.ExportDiagnostics(doc + " "); saved || err == nil || dialogs != 0 {
		t.Fatalf("export accepted altered JSON: saved=%v err=%v dialogs=%d", saved, err, dialogs)
	}

	a.diagnostics.Record(diag.Fetch, time.Now(), time.Now(), nil, dir, &git.GitError{Code: git.CodeOffline})
	a.cfg.Global.Roots = append(a.cfg.Global.Roots, filepath.Join(dir, "another"))
	selected = ""
	if saved, err := a.ExportDiagnostics(doc); saved || err != nil {
		t.Fatalf("cancel: saved=%v err=%v", saved, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cancel left a file: %v", err)
	}
	selected = a.cfgPath
	if saved, err := a.ExportDiagnostics(doc); saved || err == nil {
		t.Fatalf("active config overwrite: saved=%v err=%v", saved, err)
	}
	if data, err := os.ReadFile(a.cfgPath); err != nil || string(data) != configContents {
		t.Fatalf("active config changed: %v", err)
	}
	selected = path
	if saved, err := a.ExportDiagnostics(doc); !saved || err != nil {
		t.Fatalf("export: saved=%v err=%v", saved, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != doc {
		t.Fatalf("file differs from the reviewed bytes: %v", err)
	}
}

func TestDiagnostics_RemoteWindowRefusesPreviewAndExport(t *testing.T) {
	a := NewApp()
	a.backend = &RemoteBackend{}
	if doc, err := a.PreviewDiagnostics(); doc != "" || err == nil {
		t.Fatalf("remote preview: document=%q err=%v", doc, err)
	}
	if saved, err := a.ExportDiagnostics("{}"); saved || err == nil {
		t.Fatalf("remote export: saved=%v err=%v", saved, err)
	}
}
