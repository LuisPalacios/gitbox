package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/update"
)

// testAppOption sets one field of an App built by newTestApp.
type testAppOption func(*App)

// newTestApp builds an App for a test with only the fields its options set;
// every other field keeps its zero value, as in a struct literal. Tests use
// it instead of an App literal so a field that moves out of App changes only
// its option here (TestNoAppLiteralsInTests enforces that).
func newTestApp(opts ...testAppOption) *App {
	a := new(App)
	a.localSession = new(localSession)
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func withCfg(cfg *config.Config) testAppOption { return func(a *App) { a.cfg = cfg } }

func withCfgPath(path string) testAppOption { return func(a *App) { a.cfgPath = path } }

func withCfgLoaded() testAppOption { return func(a *App) { a.cfgLoaded = true } }

func withCfgBase(base config.FileRevision) testAppOption { return func(a *App) { a.cfgBase = base } }

func withCfgLoadError(msg string) testAppOption { return func(a *App) { a.cfgLoadError = msg } }

func withBackend(b Backend) testAppOption { return func(a *App) { a.backend = b } }

func withPrefs(p prefsStore) testAppOption { return func(a *App) { a.prefs = p } }

func withEvents(s EventSink) testAppOption { return func(a *App) { a.events = s } }

func withCtx(ctx context.Context) testAppOption { return func(a *App) { a.ctx = ctx } }

func withPRs(prs prRefreshState) testAppOption { return func(a *App) { a.prs = prs } }

func withReadOnly() testAppOption { return func(a *App) { a.readOnly = true } }

func withRefreshClosed() testAppOption { return func(a *App) { a.refreshClosed = true } }

func withRefreshHooks(h *refreshHooks) testAppOption { return func(a *App) { a.refreshHooks = h } }

func withTestFirstRun(first bool) testAppOption { return func(a *App) { a.testFirstRun = first } }

func withTestMode() testAppOption { return func(a *App) { a.testMode = true } }

func withUpdateRecovery(r *update.Recovery) testAppOption {
	return func(a *App) { a.updateRecovery = r }
}

func withUpstreamGone(gone map[string]bool) testAppOption {
	return func(a *App) { a.upstreamGone = gone }
}

func withWindowMode(mode string) testAppOption { return func(a *App) { a.windowMode = mode } }

// TestNoAppLiteralsInTests fails on any App composite literal in this
// package's tests. Go does not promote embedded fields into struct literals,
// so a literal breaks as soon as a field moves into an embedded struct. The
// parser ignores build constraints, so the scan covers every tagged file too.
func TestNoAppLiteralsInTests(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("found no test files to scan")
	}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok {
				if id, ok := lit.Type.(*ast.Ident); ok && id.Name == "App" {
					t.Errorf("%s: App literal; build it with newTestApp(...) instead", fset.Position(lit.Pos()))
				}
			}
			return true
		})
	}
}
