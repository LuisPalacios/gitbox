package tui

import (
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/i18n"
)

func TestSettingsSpanishText(t *testing.T) {
	env := setupTestEnv(t)
	cfg := newTestConfig(t, env.GitFolder)
	cfg.Global.Language = "es"
	m := newSettingsModel(cfg, env.CfgPath, styles.NewTheme(true), i18n.New("es"), 80, 24)

	view := m.View()
	if !strings.Contains(view, "Ajustes") {
		t.Fatalf("settings view missing Spanish title:\n%s", view)
	}
	if !strings.Contains(view, "Idioma") {
		t.Fatalf("settings view missing language field:\n%s", view)
	}
	if !strings.Contains(view, "Carpeta raíz") {
		t.Fatalf("settings view missing accented root folder field:\n%s", view)
	}
	if !strings.Contains(view, "Sincronización periódica") {
		t.Fatalf("settings view missing accented periodic sync field:\n%s", view)
	}
}
