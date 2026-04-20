package workspace

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// codeWorkspace is the on-disk schema of a .code-workspace file.
type codeWorkspace struct {
	Folders  []codeFolder   `json:"folders"`
	Settings map[string]any `json:"settings,omitempty"`
}

type codeFolder struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

// defaultCodeSettings are the minimal settings gitbox injects into a fresh
// .code-workspace. They make VS Code detect nested repos under a shared root,
// which is the whole point of a multi-repo workspace. Anything else is left
// to the user's User/Folder/Remote settings.
func defaultCodeSettings() map[string]any {
	return map[string]any{
		"git.autoRepositoryDetection":        true,
		"git.repositoryScanMaxDepth":         2,
		"git.openRepositoryInParentFolders":  "always",
	}
}

func defaultCodeWorkspaceFile(key string, memberPaths []string) string {
	root := commonAncestor(memberPaths)
	if root == "" {
		// No shared ancestor (unlikely given gitbox's folder model, but fall
		// back to the first member's parent so we never return "").
		root = filepath.Dir(memberPaths[0])
	}
	return filepath.Join(root, key+".code-workspace")
}

func generateCodeWorkspace(key string, w config.Workspace, memberPaths []string) (GenerateResult, error) {
	file := w.File
	if file == "" {
		file = defaultCodeWorkspaceFile(key, memberPaths)
	}

	root := filepath.Dir(file)

	folders := make([]codeFolder, 0, len(memberPaths))
	for i, p := range memberPaths {
		rel, err := filepath.Rel(root, p)
		path := filepath.ToSlash(p)
		if err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			path = filepath.ToSlash(rel)
		}
		folders = append(folders, codeFolder{
			Path: path,
			Name: labelForMember(w.Members[i]),
		})
	}

	ws := codeWorkspace{
		Folders:  folders,
		Settings: defaultCodeSettings(),
	}

	buf, err := json.MarshalIndent(ws, "", "    ")
	if err != nil {
		return GenerateResult{}, fmt.Errorf("marshal .code-workspace: %w", err)
	}
	buf = append(buf, '\n')

	return GenerateResult{
		File:          file,
		Content:       buf,
		ResolvedPaths: memberPaths,
	}, nil
}

// labelForMember builds a short, human-friendly label for a member.
// Uses the tail of "org/repo" if the repo key is slash-separated, otherwise
// the repo key as-is.
func labelForMember(m config.WorkspaceMember) string {
	if i := strings.IndexByte(m.Repo, '/'); i >= 0 && i < len(m.Repo)-1 {
		return m.Repo[i+1:]
	}
	return m.Repo
}
