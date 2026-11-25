package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Keep the exception specific to the bound entry point, the function that
// performs the operation and the operation itself.
var commandPolicyExceptions = map[commandRoute]string{
	{"DeleteRepoFolder", "ops.removeCloneFolder", "remove folder"}: "Advanced uses a dedicated clone deletion confirmation",
	{"DeleteAccount", "ops.removeCloneFolder", "remove folder"}:    "account confirmation requires the Advanced folder opt-in",
	{"ConfirmSweep", "git.deletePinnedBranch", "git update-ref"}:   "the confirmed sweep deletes only previewed branch tips under ref locks",
}

type commandRoute struct {
	entry, function, operation string
}

func TestBoundCommandPolicy(t *testing.T) {
	graph := loadCommandGraph(t)
	if violations := graph.violations(); len(violations) != 0 {
		t.Fatalf("unapproved bound command routes:\n%s", strings.Join(violations, "\n"))
	}
}

func TestBoundCommandPolicyRejectsFixture(t *testing.T) {
	graph := loadCommandGraph(t)
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package main
import (
	"context"
	"github.com/LuisPalacios/gitbox/pkg/git"
)
func (a *App) FixturePush(path string) { fixtureHelper(path) }
func fixtureHelper(path string) {
	args := []string{"push", "origin"}
	git.Run(context.Background(), git.Request{Dir: path, Args: args})
}
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	graph.addFile(file)
	got := graph.violations()
	want := []string{"FixturePush reaches main.fixtureHelper: git push"}
	if !slices.Equal(got, want) {
		t.Fatalf("negative fixture: got %v, want %v", got, want)
	}
}

type commandNode struct {
	calls      []string
	operations []string
}

type commandGraph map[string]*commandNode

func loadCommandGraph(t *testing.T) commandGraph {
	t.Helper()
	graph := commandGraph{}
	for _, dir := range []string{".", "../../pkg/git", "../../pkg/ops", "../../pkg/mirror"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			graph.addFile(file)
		}
	}
	return graph
}

func (g commandGraph) violations() []string {
	var failures []string
	seenExceptions := make(map[commandRoute]bool)
	for start := range g {
		entry, ok := strings.CutPrefix(start, "main.App.")
		if !ok || !ast.IsExported(entry) {
			continue
		}
		seen := make(map[string]bool)
		queue := []string{start}
		for len(queue) != 0 {
			key := queue[0]
			queue = queue[1:]
			node := g[key]
			if node == nil || seen[key] {
				continue
			}
			seen[key] = true
			queue = append(queue, node.calls...)
			for _, op := range node.operations {
				route := commandRoute{entry, key, op}
				if commandPolicyExceptions[route] == "" {
					failures = append(failures, fmt.Sprintf("%s reaches %s: %s", entry, key, op))
				} else {
					seenExceptions[route] = true
				}
			}
		}
	}
	for route := range commandPolicyExceptions {
		if !seenExceptions[route] {
			failures = append(failures, fmt.Sprintf("stale exception: %s reaches %s: %s", route.entry, route.function, route.operation))
		}
	}
	slices.Sort(failures)
	return failures
}

func (g commandGraph) addFile(file *ast.File) {
	pkg := file.Name.Name
	imports := make(map[string]string)
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		base := filepath.Base(path)
		alias := base
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		imports[alias] = base
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		key := pkg + "."
		var receiver string
		if fn.Recv != nil {
			field := fn.Recv.List[0]
			recv := field.Type
			if ptr, ok := recv.(*ast.StarExpr); ok {
				recv = ptr.X
			}
			typ, ok := recv.(*ast.Ident)
			if !ok {
				continue
			}
			key += typ.Name + "."
			if len(field.Names) != 0 {
				receiver = field.Names[0].Name
			}
		}
		nodeKey := key + fn.Name.Name
		node := g[nodeKey]
		if node == nil {
			node = &commandNode{}
			g[nodeKey] = node
		}

		locals := commandAssignments(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.SelectorExpr:
				// Follow references as well as calls because a backend
				// often hands a function to its transaction or worker.
				switch x := e.X.(type) {
				case *ast.Ident:
					if imported := imports[x.Name]; imported != "" {
						node.calls = append(node.calls, imported+"."+e.Sel.Name)
					} else if receiver != "" && x.Name == receiver {
						node.calls = append(node.calls, key+e.Sel.Name)
					}
				case *ast.SelectorExpr:
					// Local backend delegates to its App.
					if key == "main.localBackend." && x.Sel.Name == "a" {
						node.calls = append(node.calls, "main.App."+e.Sel.Name)
					}
				case *ast.CallExpr:
					// App.be resolves to the local or remote backend.
					// The served remote command runs the local method.
					if be, ok := x.Fun.(*ast.SelectorExpr); ok && key == "main.App." && be.Sel.Name == "be" {
						node.calls = append(node.calls, "main.localBackend."+e.Sel.Name)
					}
				}
			case *ast.CallExpr:
				if name, ok := e.Fun.(*ast.Ident); ok {
					node.calls = append(node.calls, pkg+"."+name.Name)
				}
				if op := commandOperation(pkg, imports, locals, e); op != "" && !slices.Contains(node.operations, op) {
					node.operations = append(node.operations, op)
				}
			}
			return true
		})
	}
}

func commandOperation(pkg string, imports map[string]string, locals map[string][]ast.Expr, call *ast.CallExpr) string {
	switch callee := call.Fun.(type) {
	case *ast.Ident:
		if pkg == "git" && len(call.Args) > 1 && slices.Contains([]string{"run", "output", "inspectOutput"}, callee.Name) {
			return dangerousGitArgs(commandWords(locals, call.Args[1:]...))
		}
		if pkg == "git" && len(call.Args) > 2 && slices.Contains([]string{"runContext", "outputContext", "localObjectOutput"}, callee.Name) {
			return dangerousGitArgs(commandWords(locals, call.Args[2:]...))
		}
	case *ast.SelectorExpr:
		sel := callee.Sel.Name
		base, _ := callee.X.(*ast.Ident)
		if base != nil && imports[base.Name] == "exec" {
			at := 0
			if sel == "CommandContext" {
				at = 1
			}
			if (sel == "Command" || sel == "CommandContext") && len(call.Args) > at {
				if slices.Contains(commandWords(locals, call.Args[at]), "git") || usesGitBin(call.Args[at]) {
					return dangerousGitArgs(commandWords(locals, call.Args[at+1:]...))
				}
			}
		}
		if sel == "Run" && len(call.Args) == 2 {
			return dangerousGitArgs(commandRequestWords(locals, call.Args[1], map[string]bool{}))
		}
		if sel == "RemoveAll" || sel == "Remove" {
			if pkg == "ops" && sel == "RemoveAll" {
				return "remove folder"
			}
			if base != nil && imports[base.Name] == "os" && refersToClonePath(locals, call.Args...) {
				return "remove folder"
			}
		}
	}
	return ""
}

func usesGitBin(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "GitBin"
}

func refersToClonePath(locals map[string][]ast.Expr, expressions ...ast.Expr) bool {
	seen := make(map[string]bool)
	var find func(ast.Expr) bool
	find = func(expr ast.Expr) bool {
		found := false
		ast.Inspect(expr, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.SelectorExpr:
				found = found || v.Sel.Name == "RepoPath" || v.Sel.Name == "SourceRepoPath"
			case *ast.Ident:
				if !seen[v.Name] {
					seen[v.Name] = true
					found = found || slices.ContainsFunc(locals[v.Name], find)
				}
			}
			return !found
		})
		return found
	}
	return slices.ContainsFunc(expressions, find)
}

func commandAssignments(body *ast.BlockStmt) map[string][]ast.Expr {
	locals := make(map[string][]ast.Expr)
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			if len(v.Lhs) == len(v.Rhs) {
				for i, lhs := range v.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						locals[id.Name] = append(locals[id.Name], v.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			for i, value := range v.Values {
				if i < len(v.Names) {
					id := v.Names[i].Name
					locals[id] = append(locals[id], value)
				}
			}
		}
		return true
	})
	return locals
}

func commandRequestWords(locals map[string][]ast.Expr, expr ast.Expr, seen map[string]bool) []string {
	switch v := expr.(type) {
	case *ast.CompositeLit:
		for _, field := range v.Elts {
			if kv, ok := field.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Args" {
					return commandWords(locals, kv.Value)
				}
			}
		}
	case *ast.Ident:
		if !seen[v.Name] {
			seen[v.Name] = true
			var words []string
			for _, rhs := range locals[v.Name] {
				words = append(words, commandRequestWords(locals, rhs, seen)...)
			}
			return words
		}
	}
	return nil
}

func commandWords(locals map[string][]ast.Expr, args ...ast.Expr) []string {
	var words []string
	seen := make(map[string]bool)
	var collect func(ast.Expr)
	collect = func(expr ast.Expr) {
		switch v := expr.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				word, _ := strconv.Unquote(v.Value)
				words = append(words, word)
			}
		case *ast.CompositeLit:
			for _, e := range v.Elts {
				collect(e)
			}
		case *ast.Ident:
			if !seen[v.Name] {
				seen[v.Name] = true
				if len(locals[v.Name]) == 0 {
					words = append(words, "")
				}
				for _, rhs := range locals[v.Name] {
					collect(rhs)
				}
			}
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "append" {
				for _, arg := range v.Args {
					collect(arg)
				}
			}
		}
	}
	for _, arg := range args {
		collect(arg)
	}
	return words
}

func dangerousGitArgs(words []string) string {
	for i := 0; i < len(words); i++ {
		word := words[i]
		switch word {
		case "":
			continue
		case "-c", "-C", "--git-dir", "--work-tree":
			i++
			continue
		case "push":
			if slices.Contains(words[i+1:], "--mirror") {
				return "git push --mirror"
			}
			return "git push"
		case "commit", "merge", "rebase", "clean", "checkout", "update-ref":
			return "git " + word
		case "reset":
			if slices.Contains(words[i+1:], "--hard") {
				return "git reset --hard"
			}
		case "branch":
			if slices.Contains(words[i+1:], "-D") {
				return "git branch -D"
			}
		default:
			if strings.HasPrefix(word, "-") {
				continue
			}
		}
		return ""
	}
	return ""
}
