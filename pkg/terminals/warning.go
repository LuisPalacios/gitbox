package terminals

import (
	"github.com/LuisPalacios/gitbox/pkg/config"
)

// MissingModernTerminal reports whether the host should surface the "install
// a modern Terminal" banner. True only on Windows AND when the persisted
// TerminalApps slice has no shell-token-aware entry. On macOS / Linux the
// catalog Terminals all host shells natively, so the banner never fires
// there.
//
// The check reads from cfg, not from a fresh detect — the GUI/TUI call this
// after Sync has populated the persisted lists, so the answer is consistent
// with what the Manager renders.
func MissingModernTerminal(cfg *config.Config, goos string) bool {
	if goos != "windows" {
		return false
	}
	if cfg == nil {
		return true
	}
	return !HasModernTerminal(cfg.Global.TerminalApps)
}
