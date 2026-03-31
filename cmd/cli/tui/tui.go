package tui

import (
	"fmt"
	"os"

	"github.com/LuisPalacios/gitbox/pkg/config"
	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the TUI. Called from main when no arguments are provided
// and stdin is a terminal. cfgPath overrides the default config location;
// pass "" to use the default. testMode enables the [test] indicator.
func Run(cfgPath string, testMode bool) error {
	// Ensure truecolor (24-bit) mode. Windows Terminal and most modern
	// terminals support it, but termenv falls back to 256-color when
	// COLORTERM is unset and TERM is "xterm-256color".
	if os.Getenv("COLORTERM") == "" {
		os.Setenv("COLORTERM", "truecolor")
	}

	if cfgPath == "" {
		cfgPath = config.DefaultV2Path()
	}
	m := newModel(cfgPath)
	m.testMode = testMode
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()

	// Force-reset terminal in case background goroutines prevented clean exit.
	fmt.Fprint(os.Stdout, "\033[?1003l\033[?1006l\033[?1015l") // disable mouse modes
	fmt.Fprint(os.Stdout, "\033[?25h")                          // show cursor

	return err
}
