package launch

import (
	"fmt"
	"strings"
)

// ShellQuotePOSIX wraps s in single quotes for POSIX shells, escaping any
// embedded single quotes. Suitable for building a shell command line that
// gets passed to `sh -c` or an AppleScript `do script`.
func ShellQuotePOSIX(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// JoinPOSIX renders argv as one POSIX shell command line, quoting every
// element. Used when a terminal template offers a single string slot for the
// shell (tilda --command, guake -e) and the whole shell invocation has to
// travel as one argument the terminal word-splits itself.
func JoinPOSIX(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = ShellQuotePOSIX(a)
	}
	return strings.Join(quoted, " ")
}

// AppleScriptEscape escapes a string for embedding in an AppleScript
// double-quoted string literal. Backslashes and double quotes get prefixed
// with a backslash.
func AppleScriptEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// MacHarnessShellLine returns the shell command line that osascript will
// `do script` (Terminal.app) or `write text` (iTerm). It cd's into the
// target path and then execs the harness argv — everything POSIX-quoted so
// paths with spaces and unicode round-trip safely.
func MacHarnessShellLine(path string, harnessArgv []string) string {
	parts := make([]string, 0, 1+len(harnessArgv))
	parts = append(parts, "cd "+ShellQuotePOSIX(path))
	if len(harnessArgv) > 0 {
		parts = append(parts, JoinPOSIX(harnessArgv))
	}
	return strings.Join(parts, " && ")
}

// MacAppleScript returns the AppleScript source that launches the harness
// inside the named Terminal-family app. For Terminal.app a plain `do script`
// (which opens a new window with the command running); for iTerm, create a
// fresh window with the default profile and write the command to its
// current session. Returns "" for any other app name.
func MacAppleScript(appName, path string, harnessArgv []string) string {
	shell := MacHarnessShellLine(path, harnessArgv)
	escaped := AppleScriptEscape(shell)
	switch appName {
	case "Terminal":
		return fmt.Sprintf(`tell application "Terminal"
    activate
    do script "%s"
end tell`, escaped)
	case "iTerm":
		// iTerm's `create window with default profile` returns before the
		// session is ready to accept `write text` — with no delay the window
		// opens but the command silently drops. A small delay gives iTerm
		// time to finish initializing the session; `current session of
		// current window` then resolves to the just-created session. This
		// is the pattern that works across iTerm 3.x versions; the
		// apparently-cleaner "nested tell (create window …)" form races.
		return fmt.Sprintf(`tell application "iTerm"
    activate
    create window with default profile
    delay 0.3
    tell current session of current window
        write text "%s"
    end tell
end tell`, escaped)
	}
	return ""
}
