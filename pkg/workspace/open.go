package workspace

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// OpenCommand is a fully-prepared exec.Command plus a human-readable
// description of how the workspace will be launched. The caller applies any
// platform-specific process attributes (e.g. pkg/git.HideWindow on Windows)
// before calling .Run() or .Start().
type OpenCommand struct {
	Cmd         *exec.Cmd
	Description string
}

// BuildOpenCommand returns the command that opens the workspace with its
// configured launcher. It does NOT execute the command. File must already
// exist on disk — callers are responsible for calling Generate+write first
// (or passing a workspace whose File was produced by a previous generate).
func BuildOpenCommand(cfg *config.Config, key string) (OpenCommand, error) {
	w, ok := cfg.Workspaces[key]
	if !ok {
		return OpenCommand{}, fmt.Errorf("workspace %q not found", key)
	}
	if w.File == "" {
		return OpenCommand{}, fmt.Errorf("workspace %q: no file recorded; run 'gitbox workspace generate %s' first", key, key)
	}

	switch w.Type {
	case config.WorkspaceTypeCode:
		return buildOpenCodeWorkspace(cfg, w)
	case config.WorkspaceTypeTmuxinator:
		return buildOpenTmuxinator(cfg, key, w)
	default:
		return OpenCommand{}, fmt.Errorf("workspace %q: unsupported type %q", key, w.Type)
	}
}

func buildOpenCodeWorkspace(cfg *config.Config, w config.Workspace) (OpenCommand, error) {
	editor, err := pickEditor(cfg)
	if err != nil {
		return OpenCommand{}, err
	}
	cmd := exec.Command(editor.Command, w.File)
	return OpenCommand{
		Cmd:         cmd,
		Description: fmt.Sprintf("%s %s", editor.Name, w.File),
	}, nil
}

func buildOpenTmuxinator(cfg *config.Config, key string, w config.Workspace) (OpenCommand, error) {
	if !tmuxinatorSupported() {
		return OpenCommand{}, ErrTmuxinatorUnsupported
	}
	term, err := pickTerminal(cfg)
	if err != nil {
		return OpenCommand{}, err
	}
	// tmuxinator is invoked by profile name (bare key), not by file path.
	args := expandTerminalArgs(term.Args, []string{"tmuxinator", "start", key})
	cmd := exec.Command(term.Command, args...)
	return OpenCommand{
		Cmd:         cmd,
		Description: fmt.Sprintf("%s → tmuxinator start %s", term.Name, key),
	}, nil
}

// pickEditor selects an editor from the global config. v1 uses the first
// configured entry; a future iteration can surface a picker.
func pickEditor(cfg *config.Config) (config.EditorEntry, error) {
	if len(cfg.Global.Editors) == 0 {
		return config.EditorEntry{}, fmt.Errorf("no editors configured; add one to global.editors in gitbox.json")
	}
	return cfg.Global.Editors[0], nil
}

func pickTerminal(cfg *config.Config) (config.TerminalEntry, error) {
	if len(cfg.Global.Terminals) == 0 {
		return config.TerminalEntry{}, fmt.Errorf("no terminals configured; add one to global.terminals in gitbox.json")
	}
	return cfg.Global.Terminals[0], nil
}

// expandTerminalArgs substitutes the "{command}" token in a terminal's
// arg template with the child argv (splicing in order). If the token is
// absent, the child argv is appended at the end. "{path}" is intentionally
// NOT substituted here — workspaces don't have a single path.
func expandTerminalArgs(template []string, child []string) []string {
	out := make([]string, 0, len(template)+len(child))
	replaced := false
	for _, a := range template {
		if a == "{command}" {
			out = append(out, child...)
			replaced = true
			continue
		}
		// Skip {path} — not meaningful for workspace launches. Keep other
		// args untouched.
		if a == "{path}" {
			continue
		}
		out = append(out, a)
	}
	if !replaced {
		out = append(out, child...)
	}
	// Drop empty args that might slip through string templates.
	cleaned := out[:0]
	for _, a := range out {
		if strings.TrimSpace(a) == "" {
			continue
		}
		cleaned = append(cleaned, a)
	}
	return cleaned
}
