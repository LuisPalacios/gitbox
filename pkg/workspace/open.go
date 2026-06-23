package workspace

import (
	"fmt"
	"os/exec"

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

// BuildOpenCommand returns the command that opens a discovered .code-workspace
// file in the user's configured editor. It does NOT execute the command. The
// workspace File must exist on disk (it was discovered there).
func BuildOpenCommand(cfg *config.Config, key string) (OpenCommand, error) {
	w, ok := cfg.Workspaces[key]
	if !ok {
		return OpenCommand{}, fmt.Errorf("workspace %q not found", key)
	}
	if w.File == "" {
		return OpenCommand{}, fmt.Errorf("workspace %q: no file recorded", key)
	}
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

// pickEditor selects an editor from the global config (first configured entry).
func pickEditor(cfg *config.Config) (config.EditorEntry, error) {
	if len(cfg.Global.Editors) == 0 {
		return config.EditorEntry{}, fmt.Errorf("no editors configured; add one to global.editors in gitbox.json")
	}
	return cfg.Global.Editors[0], nil
}
