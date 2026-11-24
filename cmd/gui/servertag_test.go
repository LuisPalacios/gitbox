package main

import (
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	serverTag   = "gitboxserver"
	wailsModule = "github.com/wailsapp/"
)

// serverBuildIncludes reports whether a file with these leading comments is
// part of the gitbox-server build. Every tag counts as set, so only a
// constraint that negates a tag, such as !gitboxserver, excludes a file.
func serverBuildIncludes(t *testing.T, file string, src []string) bool {
	t.Helper()
	for _, line := range src {
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		return expr.Eval(func(string) bool { return true })
	}
	return true
}

// TestWailsImportsTagged keeps the server build free of Wails: gitbox-server
// is this package built with -tags gitboxserver and CGO off, so every file
// that imports Wails must carry the !gitboxserver constraint.
func TestWailsImportsTagged(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked, tagged := 0, 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		checked++
		var header []string
		for _, group := range parsed.Comments {
			if group.Pos() >= parsed.Package {
				break
			}
			for _, c := range group.List {
				header = append(header, c.Text)
			}
		}
		inServer := serverBuildIncludes(t, file, header)
		for _, imp := range parsed.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s: %v", file, err)
			}
			if !strings.HasPrefix(path, wailsModule) {
				continue
			}
			tagged++
			if inServer {
				t.Errorf("%s imports %s but is part of the %s build: add //go:build !%s, or move the Wails call to shell.go", file, path, serverTag, serverTag)
			}
			break
		}
	}
	if checked == 0 || tagged == 0 {
		t.Fatalf("checked %d files and found %d that import Wails; the walker is broken", checked, tagged)
	}
}

// TestSentinelOnlyInTests keeps the e2e_sentinel tag, whose server writes a
// line to the home folder on every start, out of every path that builds a
// server for users: only the integration scripts, scripts/remote-it.sh and
// scripts/remote-host-it.sh (#245), may use it.
func TestSentinelOnlyInTests(t *testing.T) {
	root := filepath.Join("..", "..")
	paths := []string{
		"scripts/build-server.sh", "scripts/ship.sh", "scripts/release-macos.sh",
		"scripts/gate-lane.sh", ".github/workflows/ci.yml", ".github/workflows/pr.yml",
	}
	gen, err := filepath.Glob(filepath.Join(root, "cmd/gui/serverbin/gen/*.go"))
	if err != nil || len(gen) == 0 {
		t.Fatalf("serverbin/gen not found: %v", err)
	}
	for _, g := range gen {
		rel, _ := filepath.Rel(root, g)
		paths = append(paths, rel)
	}
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if strings.Contains(string(data), "e2e_sentinel") {
			t.Errorf("%s builds with the e2e_sentinel tag, which only the integration tests may use", p)
		}
	}
	for _, script := range []string{"scripts/remote-it.sh", "scripts/remote-host-it.sh"} {
		data, err := os.ReadFile(filepath.Join(root, script))
		if err != nil || !strings.Contains(string(data), "gitboxserver,e2e_sentinel") {
			t.Errorf("%s no longer builds the sentinel server: %v", script, err)
		}
	}
}
