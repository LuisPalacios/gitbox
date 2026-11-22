package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// execAllowed lists the only files that may start a process, and why. Git is
// never on the list outside its runner: every git command goes through
// pkg/git (git.Run or one of the package's functions), which is what applies
// the hidden console window, the environment policy, the output bound and
// cancellation in one place.
var execAllowed = map[string]string{
	"pkg/git/runner.go":          "the git runner: the one place that starts git",
	"pkg/git/browser.go":         "opens a URL with the system's handler",
	"pkg/doctor/doctor.go":       "probes external tools, including candidate git binaries, for their version",
	"pkg/credential/validate.go": "runs ssh and ssh-keygen",
	"pkg/update/relaunch.go":     "relaunches the app after an update",
	"cmd/gui/app.go":             "opens files and folders with the system and launches editors",
	"cmd/gui/profiles.go":        "launches terminals and shells",
	"cmd/gui/server.go":          "gitbox-server --doctor runs a probe file to see whether the config folder allows execution",
	"pkg/remote/ssh.go":          "runs the system ssh for remote mode, always with a hidden console window",
	"cmd/gui/remote.go":          "starts the window of a remote: this GitboxApp with --remote",
}

// TestProcessesStartOnlyWhereAllowed fails when code outside the list starts
// a process, when a listed file runs git directly, or when the list names a
// file that no longer starts one.
func TestProcessesStartOnlyWhereAllowed(t *testing.T) {
	root := filepath.Join("..", "..")
	found := make(map[string]int)
	for _, dir := range []string{"pkg", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); name == "node_modules" || name == "frontend" || name == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			for i, line := range strings.Split(string(data), "\n") {
				code, _, _ := strings.Cut(line, "//")
				if !strings.Contains(code, "exec.Command(") && !strings.Contains(code, "exec.CommandContext(") {
					continue
				}
				found[rel]++
				if _, ok := execAllowed[rel]; !ok {
					t.Errorf("%s:%d starts a process; run git through pkg/git, or add the file to execAllowed with its reason:\n\t%s", rel, i+1, strings.TrimSpace(line))
				}
				if rel != "pkg/git/runner.go" && strings.Contains(code, "GitBin(") {
					t.Errorf("%s:%d runs git outside the runner; use git.Run:\n\t%s", rel, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for file := range execAllowed {
		if found[file] == 0 {
			t.Errorf("execAllowed lists %s, which no longer starts a process: remove it", file)
		}
	}
	if len(found) == 0 {
		t.Fatal("no process starts were found: the scan is broken")
	}
}

// TestNoBareExecCommand fails when a process start in the remote-mode
// packages does not hide its console window: each exec.Command there is
// followed, in the same function, by git.HideWindow on the same command.
func TestNoBareExecCommand(t *testing.T) {
	root := filepath.Join("..", "..")
	var files []string
	for _, dir := range []string{"pkg/remote"} {
		matches, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			if !strings.HasSuffix(m, "_test.go") {
				files = append(files, m)
			}
		}
	}
	if len(files) == 0 {
		t.Fatal("no files found in pkg/remote: the scan is broken")
	}
	starts := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			code, _, _ := strings.Cut(line, "//")
			if !strings.Contains(code, "exec.Command(") && !strings.Contains(code, "exec.CommandContext(") {
				continue
			}
			starts++
			name, _, ok := strings.Cut(strings.TrimSpace(code), ":=")
			name = strings.TrimSpace(name)
			hidden := false
			for _, next := range lines[i+1 : min(i+4, len(lines))] {
				if ok && strings.Contains(next, "git.HideWindow("+name+")") {
					hidden = true
				}
			}
			if !hidden {
				t.Errorf("%s:%d starts a process without git.HideWindow right after it:\n\t%s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
	if starts == 0 {
		t.Fatal("no process starts found in pkg/remote: the scan is broken")
	}
}
