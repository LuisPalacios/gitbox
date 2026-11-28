package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"
)

// A field of App with the name of a localSession field would shadow the
// promoted one, and code written for the session would silently read App's.
func TestAppAndLocalSessionShareNoFieldNames(t *testing.T) {
	session := reflect.TypeFor[localSession]()
	app := reflect.TypeFor[App]()
	embedded, ok := app.FieldByName("localSession")
	if !ok || !embedded.Anonymous || embedded.Type != reflect.PointerTo(session) {
		t.Fatalf("App must embed *localSession, got %+v", embedded)
	}
	sessionFields := make(map[string]bool, session.NumField())
	for field := range session.Fields() {
		sessionFields[field.Name] = true
	}
	for field := range app.Fields() {
		if field.Anonymous && field.Type == reflect.PointerTo(session) {
			continue
		}
		if sessionFields[field.Name] {
			t.Errorf("App.%s shadows localSession.%s", field.Name, field.Name)
		}
	}
	// The config core and mu move together (#366).
	for _, name := range []string{"mu", "cfg", "cfgPath", "cfgLoaded", "cfgLoadError", "cfgBase", "configRevision"} {
		if !sessionFields[name] {
			t.Errorf("localSession has no field %s", name)
		}
	}
}

func TestAppConstructorsAllocateLocalSession(t *testing.T) {
	if NewApp().localSession == nil {
		t.Error("NewApp left localSession nil")
	}
	if newTestApp().localSession == nil {
		t.Error("newTestApp left localSession nil")
	}
}

// sessionFiles are the files whose session state moved into localSession
// (#353). Each later move adds its files here.
var sessionFiles = []string{"pr.go", "reconcile.go", "roots.go", "containers.go"}

// sessionFieldNames is the set of localSession field names.
func sessionFieldNames() map[string]bool {
	fields := make(map[string]bool)
	for field := range reflect.TypeFor[localSession]().Fields() {
		fields[field.Name] = true
	}
	return fields
}

// appSessionReads lists each place in src where a method on *App reads a
// localSession field, promoted (a.cfg) or spelled out (a.localSession.cfg).
// Calling a session method through App is fine: the method owns the state.
func appSessionReads(t *testing.T, name string, src []byte, fields map[string]bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 || fn.Body == nil {
			continue
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if id, isIdent := star.X.(*ast.Ident); !ok || !isIdent || id.Name != "App" {
			continue
		}
		recv := fn.Recv.List[0].Names[0].Name
		isRecv := func(e ast.Expr) bool {
			id, ok := e.(*ast.Ident)
			return ok && id.Name == recv
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || !fields[sel.Sel.Name] {
				return true
			}
			inner, spelled := sel.X.(*ast.SelectorExpr)
			if isRecv(sel.X) || (spelled && inner.Sel.Name == "localSession" && isRecv(inner.X)) {
				found = append(found, fset.Position(sel.Pos()).String()+" "+fn.Name.Name+" reads "+sel.Sel.Name)
			}
			return true
		})
	}
	return found
}

// TestSessionFilesReadNoSessionStateThroughApp: the methods left on *App in
// a moved file only drive services still on App (refresh flights, events,
// the network guard); every read of session state goes through a method on
// *localSession (#367).
func TestSessionFilesReadNoSessionStateThroughApp(t *testing.T) {
	fields := sessionFieldNames()
	for _, name := range sessionFiles {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if found := appSessionReads(t, name, src, fields); len(found) > 0 {
			t.Errorf("%s reads session state through *App:\n%s", name, strings.Join(found, "\n"))
		}
	}

	// The check itself finds both spellings and ignores method calls and
	// other receivers.
	bad := []byte(`package main

func (a *App) promoted() bool { return a.cfg == nil }
func (x *App) spelled() { x.localSession.prs.workers++ }
func (a *App) call() { a.localSession.startPRPass(false, nil); a.refreshMu.Lock() }
func (s *localSession) owner() bool { return s.cfg == nil }
`)
	found := appSessionReads(t, "fixture.go", bad, fields)
	if len(found) != 2 || !strings.Contains(found[0], "promoted reads cfg") || !strings.Contains(found[1], "spelled reads prs") {
		t.Errorf("fixture: want promoted cfg and spelled prs, got %q", found)
	}
}
