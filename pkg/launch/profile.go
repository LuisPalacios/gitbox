// Package launch builds the argv that opens a TerminalProfile in a folder.
//
// The expansion logic is shared between the GUI bridge (cmd/gui) and the TUI
// launcher (cmd/cli/tui) so both frontends interpret a Profile's tokens the
// same way byte-for-byte. Splitting it out also lets us unit-test the rules
// without dragging in either the Wails runtime or the Bubble Tea event loop.
//
// The only public entry point is ResolveArgs. Everything else is an
// implementation detail.
package launch

import "strings"

// Tokens recognised inside a TerminalProfile.Args / TerminalApp.ArgsTemplate.
// They are exposed as constants so callers and tests can reference them
// without re-typing the literal string.
const (
	// TokenPath is substring-replaced (not whole-arg) inside every arg —
	// that lets templates like "--working-directory={path}" splice the
	// repo path into a flag value.
	TokenPath = "{path}"
	// TokenShellCommand is whole-arg-only. The arg "{shell_command}" is
	// replaced by the shell binary path; when no shell is set the arg is
	// dropped from argv (zero-item splice).
	TokenShellCommand = "{shell_command}"
	// TokenShellArgs is whole-arg-only. The arg "{shell_args}" is spliced
	// in place by the shell's flag list; when no shell is set the arg is
	// dropped (zero-item splice).
	TokenShellArgs = "{shell_args}"
	// TokenCommand is whole-arg-only. The arg "{command}" is spliced in
	// place by the AI-harness argv; when no harness is set the arg is
	// dropped (zero-item splice). Kept for AI-harness integration.
	TokenCommand = "{command}"
)

// ProfileArgs bundles the inputs to ResolveArgs. The struct shape keeps the
// call sites readable when several tokens are in play and lets future tokens
// land without a signature churn.
type ProfileArgs struct {
	// Template is the per-app argv template (TerminalApp.ArgsTemplate) or
	// the per-profile override (TerminalProfile.Args, used by WT-profile
	// rows and migrated legacy entries). May be nil/empty.
	Template []string
	// Path is the repo working directory spliced into {path} tokens.
	Path string
	// ShellCommand is the resolved shell binary (e.g. "C:\\Program
	// Files\\PowerShell\\7\\pwsh.exe"). Empty means "use the terminal's
	// default shell" — both shell tokens collapse to zero items.
	ShellCommand string
	// ShellArgs are the shell's flags (e.g. ["-l"] for login shells,
	// ["-d", "Ubuntu-24.04"] for WSL). Empty splices to zero items.
	ShellArgs []string
	// HarnessArgv is the AI-harness command + args spliced into {command}.
	// Nil means no harness; the {command} token then collapses to zero
	// items. Distinct from ShellArgs because harness launches keep the
	// shell wrapping intact (terminal → shell → harness).
	HarnessArgv []string
}

// ResolveArgs expands every token in Template and returns the final argv
// ready for exec.Command(terminal, …).
//
// Rules:
//   - Whole-arg "{shell_command}": replaced by ShellCommand when non-empty,
//     dropped otherwise.
//   - Whole-arg "{shell_args}": spliced in place by ShellArgs (0..N items).
//   - Whole-arg "{command}": spliced in place by HarnessArgv (0..N items).
//   - Substring "{path}" inside any other arg: replaced by Path.
//   - Legacy fallback: when none of the four tokens appeared in Template
//     AND HarnessArgv is nil AND Template is non-empty, Path is appended as
//     the final argv. Preserves the historical `open -a Terminal <path>`
//     and migrated AppleScript launches.
//   - Empty Template returns nil (caller falls back to cmd.Dir = path).
func ResolveArgs(in ProfileArgs) []string {
	if len(in.Template) == 0 {
		return nil
	}

	tokenSeen := false
	out := make([]string, 0, len(in.Template)+len(in.ShellArgs)+len(in.HarnessArgv))

	for _, a := range in.Template {
		switch a {
		case TokenShellCommand:
			tokenSeen = true
			if in.ShellCommand != "" {
				out = append(out, in.ShellCommand)
			}
			continue
		case TokenShellArgs:
			tokenSeen = true
			out = append(out, in.ShellArgs...)
			continue
		case TokenCommand:
			tokenSeen = true
			out = append(out, in.HarnessArgv...)
			continue
		}
		if strings.Contains(a, TokenPath) {
			tokenSeen = true
			out = append(out, strings.ReplaceAll(a, TokenPath, in.Path))
			continue
		}
		out = append(out, a)
	}

	if !tokenSeen && in.HarnessArgv == nil {
		out = append(out, in.Path)
	}
	return out
}
