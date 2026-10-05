package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// A first launch on a fresh config must not write the legacy v2.0
// global.terminals list: the next load would migrate it into extra
// "migrated" profiles next to the detected ones.
func TestStartupSyncNeverWritesLegacyTerminals(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	cfgPath := filepath.Join(t.TempDir(), "gitbox.json")

	cfg := &config.Config{
		Version:  config.CurrentVersion,
		Global:   config.GlobalConfig{Folder: "~/x"},
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}

	a := &App{cfg: cfg, cfgPath: cfgPath, mu: sync.Mutex{}}
	a.syncDetected()

	if len(cfg.Global.Terminals) != 0 {
		t.Errorf("startup sync filled legacy terminals: %+v", cfg.Global.Terminals)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"terminals"`) {
		t.Errorf("saved config contains the legacy terminals key:\n%s", data)
	}
}
