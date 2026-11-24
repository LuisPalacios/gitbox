//go:build !gitboxserver

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const classificationHint = "add or remove its row in cmd/gui/classification.go (and the table in the remote-mode issue)"

// boundMethods parses the generated Wails bindings for the bound method names.
func boundMethods(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("frontend", "wailsjs", "go", "main", "App.d.ts"))
	if err != nil {
		t.Fatalf("read bindings: %v", err)
	}
	found := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^export function (\w+)\(`).FindAllStringSubmatch(string(data), -1) {
		found[m[1]] = true
	}
	if len(found) == 0 {
		t.Fatal("no bound methods found in App.d.ts; the parser is broken")
	}
	return found
}

func methodSet(typ reflect.Type) map[string]bool {
	set := map[string]bool{}
	for m := range typ.Methods() {
		set[m.Name] = true
	}
	return set
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestEveryAppMethodClassified fails when an exported *App method or a bound
// method has no row in methodClass, when a row is stale, and when a row
// disagrees with the Backend interface.
func TestEveryAppMethodClassified(t *testing.T) {
	exported := methodSet(reflect.TypeFor[*App]())
	bound := boundMethods(t)
	backend := methodSet(reflect.TypeFor[Backend]())
	hooks := map[string]bool{}
	for _, h := range lifecycleHooks {
		hooks[h] = true
	}

	for _, name := range sortedKeys(exported) {
		if _, ok := methodClass[name]; !ok {
			t.Errorf("exported method App.%s is not classified: %s", name, classificationHint)
		}
	}
	for _, name := range sortedKeys(bound) {
		if _, ok := methodClass[name]; !ok {
			t.Errorf("bound method %s (App.d.ts) is not classified: %s", name, classificationHint)
		}
		if !exported[name] {
			t.Errorf("bound method %s (App.d.ts) is not a method of *App; regenerate the bindings", name)
		}
	}
	for _, name := range sortedKeys(methodClass) {
		info := methodClass[name]
		switch {
		case !exported[name]:
			t.Errorf("stale classification row %s: no such method on *App; %s", name, classificationHint)
			continue
		case hooks[name]:
			if bound[name] {
				t.Errorf("lifecycle hook %s must not be a bound method", name)
			}
		case !bound[name]:
			t.Errorf("%s is classified but missing from App.d.ts; regenerate the bindings", name)
		}
		viaBackend := info.Class != classLocal && !hooks[name]
		if viaBackend != backend[name] {
			t.Errorf("%s is %s but Backend has it = %v: REMOTE and MIXED methods go through Backend, LOCAL ones never do", name, info.Class, backend[name])
		}
		if info.Mutating && !viaBackend {
			t.Errorf("%s is marked Mutating but does not go through Backend", name)
		}
		if info.Owner == "" {
			t.Errorf("%s has no owner", name)
		}
	}
	for _, name := range lifecycleHooks {
		if _, ok := methodClass[name]; !ok {
			t.Errorf("lifecycle hook %s is not classified: %s", name, classificationHint)
		}
	}
}

// eventRelayFile holds the one Emit call whose name is not a literal: the
// relay of the events a gitbox-server sent. It checks each name against
// eventClass before it emits, so it can only repeat a name that some other
// call site emits with a literal.
const eventRelayFile = "backend_remote.go"

// emittedEventNames walks the non-test Go files of this package, under every
// build tag, and returns the literal first argument of each Emit call.
func emittedEventNames(t *testing.T) map[string][]string {
	t.Helper()
	relays := 0
	defer func() {
		if relays != 2 {
			t.Errorf("%s has %d Emit calls with a name that is not a literal, want the host relay and client failure publication", eventRelayFile, relays)
		}
	}()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	names := map[string][]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			nameArg := 0
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				if fn.Name != "emit" {
					return true
				}
				nameArg = 1
			case *ast.SelectorExpr:
				if fn.Sel.Name != "Emit" || file == "events.go" {
					return true
				}
			default:
				return true
			}
			pos := fset.Position(call.Pos()).String()
			if len(call.Args) <= nameArg {
				t.Errorf("%s: event call without an event name", pos)
				return true
			}
			lit, ok := call.Args[nameArg].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				if file == eventRelayFile {
					relays++
					return true
				}
				t.Errorf("%s: the event name passed to Emit must be a string literal", pos)
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Errorf("%s: %v", pos, err)
				return true
			}
			names[name] = append(names[name], pos)
			return true
		})
	}
	return names
}

// TestEventNamesRegistered fails on an event name that is not a literal, on
// one missing from eventClass, and on a stale eventClass row.
func TestEventNamesRegistered(t *testing.T) {
	emitted := emittedEventNames(t)
	if len(emitted) == 0 {
		t.Fatal("no Emit calls found; the walker is broken")
	}
	for _, name := range sortedKeys(emitted) {
		if _, ok := eventClass[name]; !ok {
			t.Errorf("event %q (%s) is not registered: %s", name, emitted[name][0], classificationHint)
		}
	}
	for _, name := range sortedKeys(eventClass) {
		if _, ok := emitted[name]; !ok {
			t.Errorf("stale event row %q: nothing emits it; %s", name, classificationHint)
		}
		if eventClass[name].Owner == "" {
			t.Errorf("event %q has no owner", name)
		}
	}
}

// TestLocalOnlyWithoutFlag pins that only the local backend can be built
// without --experimental-remote, and that a bare App falls back to it.
func TestLocalOnlyWithoutFlag(t *testing.T) {
	a := &App{}
	if _, ok := a.be().(localBackend); !ok {
		t.Fatalf("bare App backend = %T, want localBackend", a.be())
	}
	if got, ok := selectBackend(a, false).(localBackend); !ok || got.a != a {
		t.Fatalf("selectBackend without the flag = %T, want localBackend bound to the app", selectBackend(a, false))
	}
	// Dormant flag: remote mode has no backend yet (#233).
	if _, ok := selectBackend(a, true).(localBackend); !ok {
		t.Fatalf("selectBackend with the dormant flag = %T, want localBackend", selectBackend(a, true))
	}
}
