package terminals

import (
	"slices"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// The Windows terminals that run a remote terminal of a remote window
// (#286) as their own program. A Windows client otherwise starts ssh.exe in
// a console of its own (remote.StartConsole), because Windows Terminal and
// the cmd.exe wrapper re-parse the command line. These terminals are GUI
// apps that gitbox starts directly, and their catalog template hands the
// program over as argv, so the only parsers are the terminal's own argv
// parser and then ssh.exe's.
var argvSafeWindows = map[string]string{
	"wezterm":   "start -- <program> <args>: argv parsed with the CommandLineToArgvW rules and re-composed for CreateProcess; stub round trip (TestWindowsTerminalArgvRoundTrip) and the real WezTerm where it can open a window (TestWindowsTerminalRealWezTerm)",
	"alacritty": "-e <program> <args>: argv parsed with the CommandLineToArgvW rules and re-composed for CreateProcess; stub round trip only (TestWindowsTerminalArgvRoundTrip), no Windows host with Alacritty",
}

// ProgramArgvSafe reports whether a remote terminal can run in the terminal
// of profileID on a goos client: only on Windows, only for a terminal of
// argvSafeWindows, and only with that terminal's catalog template, so a
// customised template that wraps the program in a shell falls back to the
// console.
func ProgramArgvSafe(g config.GlobalConfig, profileID, goos string) bool {
	if goos != "windows" {
		return false
	}
	profile, app, _, err := LookupProfile(g, profileID)
	if err != nil || profile.TerminalID == "" {
		return false
	}
	if _, ok := argvSafeWindows[profile.TerminalID]; !ok {
		return false
	}
	template := profile.Args
	if len(template) == 0 {
		template = app.ArgsTemplate
	}
	for _, c := range windowsTerminals {
		if c.ID == profile.TerminalID {
			return slices.Equal(template, c.ArgsTemplate)
		}
	}
	return false
}
