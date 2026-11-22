package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const configImport = "github.com/LuisPalacios/gitbox/pkg/config"

// configSaves lists every call to config.Save or config.SaveWithBackup in
// the non-test Go files under root, skipping pkg/config itself.
func configSaves(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if filepath.Base(path) == "config" && filepath.Base(filepath.Dir(path)) == "pkg" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		name := ""
		for _, imp := range file.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == configImport {
				name = "config"
				if imp.Name != nil {
					name = imp.Name.Name
				}
			}
		}
		if name == "" {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == name && (sel.Sel.Name == "Save" || sel.Sel.Name == "SaveWithBackup") {
				found = append(found, fset.Position(sel.Pos()).String())
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// TestLayeringNoSaveInPkg: only cmd/gui, the owner of the config, saves it
// (#173). Any file under pkg/ outside pkg/config that calls config.Save or
// config.SaveWithBackup breaks the rule.
func TestLayeringNoSaveInPkg(t *testing.T) {
	if found := configSaves(t, filepath.Join("..", "..", "pkg")); len(found) > 0 {
		t.Errorf("pkg/ saves the config, which only cmd/gui may do:\n%s", strings.Join(found, "\n"))
	}

	// The check itself finds a save, also under an import alias.
	dir := t.TempDir()
	bad := "package bad\n\nimport cfgpkg \"" + configImport + "\"\n\nfunc f(c *cfgpkg.Config) { _ = cfgpkg.Save(c, \"x\") }\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.go"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	if found := configSaves(t, dir); len(found) != 1 {
		t.Errorf("the layering check found %d saves in a file with one", len(found))
	}
}
