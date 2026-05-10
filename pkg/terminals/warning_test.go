package terminals

import (
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestMissingModernTerminal(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		goos string
		want bool
	}{
		{"nil-cfg-windows", nil, "windows", true},
		{"nil-cfg-darwin", nil, "darwin", false},
		{"empty-windows", emptyCfg(), "windows", true},
		{"wt-installed", cfgWithApp(fakeWTApp), "windows", false},
		{"only-bare-shell", cfgWithApp(fakeBareShellApp), "windows", true},
		{"darwin-with-bundle", cfgWithApp(fakeMacApp), "darwin", false},
		{"linux-with-modern", cfgWithApp(fakeWTApp), "linux", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MissingModernTerminal(tt.cfg, tt.goos); got != tt.want {
				t.Errorf("MissingModernTerminal=%v, want %v", got, tt.want)
			}
		})
	}
}

func emptyCfg() *config.Config {
	return &config.Config{}
}

func cfgWithApp(app config.TerminalApp) *config.Config {
	c := &config.Config{}
	c.Global.TerminalApps = []config.TerminalApp{app}
	return c
}
