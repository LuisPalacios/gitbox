package git

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestTrustFindings_Scopes checks that only the settings a clone controls
// stop it: the user's global file and the system file are trusted (#344).
func TestTrustFindings_Scopes(t *testing.T) {
	const origin = "https://example.org/team/project.git"
	const redirect = "[url \"https://other.example/\"]\n\tinsteadOf = https://example.org/\n"
	type setup struct {
		global, system string
		local          [][]string // git config arguments run in the clone
	}
	for _, tc := range []struct {
		name  string
		setup func(outside, root string) setup
		want  []TrustFinding
	}{
		{"global hooksPath", func(outside, _ string) setup {
			return setup{global: "[core]\n\thooksPath = " + outside + "\n"}
		}, nil},
		{"system hooksPath", func(outside, _ string) setup {
			return setup{system: "[core]\n\thooksPath = " + outside + "\n"}
		}, nil},
		{"global include", func(outside, root string) setup {
			file := filepath.Join(root, "global-hooks.inc")
			writeTestFile(t, file, "[core]\n\thooksPath = "+outside+"\n")
			return setup{global: "[include]\n\tpath = " + file + "\n"}
		}, nil},
		{"local hooksPath", func(outside, _ string) setup {
			return setup{local: [][]string{{"core.hooksPath", outside}}}
		}, []TrustFinding{{TrustHooksPath, ScopeLocal}}},
		{"local inside overrides global", func(outside, _ string) setup {
			return setup{global: "[core]\n\thooksPath = " + outside + "\n", local: [][]string{{"core.hooksPath", ".git/own-hooks"}}}
		}, nil},
		{"local outside overrides global", func(outside, _ string) setup {
			return setup{global: "[core]\n\thooksPath = " + outside + "\n", local: [][]string{{"core.hooksPath", outside}}}
		}, []TrustFinding{{TrustHooksPath, ScopeLocal}}},
		{"local include", func(outside, root string) setup {
			file := filepath.Join(root, "local-hooks.inc")
			writeTestFile(t, file, "[core]\n\thooksPath = "+outside+"\n")
			return setup{local: [][]string{{"include.path", file}}}
		}, []TrustFinding{{TrustHooksPath, ScopeInclude}}},
		{"worktree hooksPath", func(outside, _ string) setup {
			return setup{local: [][]string{{"extensions.worktreeConfig", "true"}, {"--worktree", "core.hooksPath", outside}}}
		}, []TrustFinding{{TrustHooksPath, ScopeWorktree}}},
		{"global fsmonitor", func(string, string) setup {
			return setup{global: "[core]\n\tfsmonitor = some-monitor\n"}
		}, nil},
		{"local fsmonitor", func(string, string) setup {
			return setup{local: [][]string{{"core.fsmonitor", "some-monitor"}}}
		}, []TrustFinding{{TrustFSMonitor, ScopeLocal}}},
		{"global insteadOf", func(string, string) setup {
			return setup{global: redirect}
		}, nil},
		{"local insteadOf", func(string, string) setup {
			return setup{local: [][]string{{"url.https://other.example/.insteadOf", "https://example.org/"}}}
		}, []TrustFinding{{TrustInsteadOf, ScopeLocal}}},
		{"local insteadOf behind longer global", func(string, string) setup {
			return setup{
				global: "[url \"https://example.org/team/\"]\n\tinsteadOf = https://example.org/team/\n",
				local:  [][]string{{"url.https://other.example/.insteadOf", "https://example.org/"}},
			}
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "hooks")
			s := tc.setup(outside, root)
			global, system := filepath.Join(root, "global"), filepath.Join(root, "system")
			writeTestFile(t, global, s.global)
			writeTestFile(t, system, s.system)
			t.Setenv("GIT_CONFIG_GLOBAL", global)
			t.Setenv("GIT_CONFIG_SYSTEM", system)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
			clone := filepath.Join(root, "clone")
			snapshotGit(t, root, "init", "-q", clone)
			snapshotGit(t, clone, "remote", "add", "origin", origin)
			for _, args := range s.local {
				snapshotGit(t, clone, append([]string{"config"}, args...)...)
			}
			snapshot, err := ReadConfigSnapshotContext(t.Context(), clone)
			if err != nil {
				t.Fatal(err)
			}
			if got := snapshot.TrustFindings(t.Context(), clone, origin); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("findings = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTrustFindingErr(t *testing.T) {
	err := TrustFinding{TrustHooksPath, ScopeLocal}.Err()
	if err.Code != CodeUnsafeConfig || err.Error() != "git config: unsafe_config (core.hooksPath)" {
		t.Fatalf("err = %v", err)
	}
	f, ok := errors.AsType[TrustFinding](error(err))
	if !ok || f != (TrustFinding{TrustHooksPath, ScopeLocal}) {
		t.Fatalf("finding beneath the error = %v, %v", f, ok)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
