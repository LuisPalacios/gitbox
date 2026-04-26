package tui

import (
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/i18n"
)

func TestDashboardSpanishStartupText(t *testing.T) {
	env := setupTestEnv(t)
	cfg := newDummyConfig(t, env.GitFolder)
	cfg.Global.Language = "es"
	env = setupTestEnvWithConfig(t, cfg)

	m := newModel(env.CfgPath, i18n.New("es"))
	m = sendWindowSize(m, 100, 30)
	m = initModel(t, m)

	view := m.View()
	for _, want := range []string{"[Cuentas]", "Mirrors", "Workspaces"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dashboard missing Spanish startup text %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "[Accounts]") {
		t.Fatalf("dashboard still shows English Accounts tab after Spanish language resolution:\n%s", view)
	}
}
