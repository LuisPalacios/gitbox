package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

func fleet(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{Version: 5, Global: config.GlobalConfig{Roots: []string{t.TempDir(), t.TempDir()}}, Accounts: map[string]config.Account{
		"private-key":  {Provider: "forgejo", URL: "https://code.example.org/git", Username: "alice", Name: "private-signature", Email: "alice@private.invalid", DefaultCredentialType: "ssh", SSH: &config.SSHConfig{Host: "private-host"}},
		"unshared-key": {Provider: "github", URL: "https://github.com", Username: "bob", Name: "B", Email: "private@email.invalid", DefaultCredentialType: "token"},
	}, Sources: map[string]config.Source{}, Mirrors: map[string]config.Mirror{}}
	cfg.Sources["private-key"] = config.Source{Account: "private-key", Repos: map[string]config.Repo{
		"group/app": {Name: "private-signature", Email: "private@email.invalid", IdFolder: "My équipe", CloneFolder: "client/app one", Root: cfg.Global.Roots[1], Workspace: "private-workspace.code-workspace"},
		"group/lib": {CloneFolder: filepath.Join(cfg.Global.Roots[0], "private-clone")},
	}}
	cfg.Mirrors["private-mirror"] = config.Mirror{AccountSrc: "private-key", AccountDst: "unshared-key", Repos: map[string]config.MirrorRepo{
		"group/app": {Direction: "push", Origin: "src", TargetRepo: "bob/app", LastSync: "private-time", Error: "token-canary"},
	}}
	return cfg
}

func TestFleetRoundTrip(t *testing.T) {
	src := fleet(t)
	r, err := Export(src)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := r.Document()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-", "unshared-key", "token-canary", "private@email.invalid", "alice@private.invalid", src.Global.Roots[0], src.Global.Roots[1]} {
		if strings.Contains(doc, forbidden) {
			t.Fatalf("recipe disclosed %q", forbidden)
		}
	}
	r, err = Decode(doc)
	if err != nil {
		t.Fatal(err)
	}
	dest := &config.Config{Version: 5, Global: config.GlobalConfig{Roots: []string{t.TempDir(), t.TempDir()}},
		Accounts: map[string]config.Account{"local-id": {Provider: "forgejo", URL: "https://code.example.org/git/", Username: "alice", Name: "Local", Email: "local@dest.invalid"}},
		Sources:  map[string]config.Source{"local-id": {Account: "local-id", Repos: map[string]config.Repo{"keep/one": {}}}}}
	options := Options{Slots: map[string]string{"primary": dest.Global.Roots[0], "secondary-1": dest.Global.Roots[1]}, Identities: map[string]Identity{"https://github.com/bob": {"Local Bob", "bob@dest.invalid"}}}
	before := config.DomainHash(dest)
	plan, err := Plan(dest, r, options)
	if err != nil {
		t.Fatal(err)
	}
	if config.DomainHash(dest) != before || plan.Accounts != 1 || plan.Repos != 2 || plan.Collisions != 0 {
		t.Fatalf("wrong preview %+v", plan)
	}
	data, _ := json.Marshal(plan)
	for _, s := range []string{"token-canary", src.Global.Roots[0], src.Global.Roots[1], "@private.invalid", "local-id", "@dest.invalid"} {
		if strings.Contains(string(data), s) {
			t.Fatal("private value in plan", s)
		}
	}
	dest.Global.Theme = "dark"
	if err := Apply(dest, plan); err != nil {
		t.Fatal(err)
	}
	if dest.Global.Theme != "dark" || dest.Accounts["local-id"].Name != "Local" {
		t.Fatal("existing identity or preferences changed")
	}
	got := dest.Sources["local-id"].Repos["group/app"]
	if want := filepath.Join(dest.Global.Roots[1], "local-id", "My équipe", "client", "app one"); status.RepoPath(dest, "local-id", "group/app", got) != want {
		t.Fatalf("wrong destination: %+v", got)
	}
	if len(dest.Sources["local-id"].Repos) != 3 || len(dest.Mirrors) != 1 {
		t.Fatal("entries lost")
	}
	if dest.Sources["local-id"].Repos["group/lib"].CloneFolder != "" {
		t.Fatal("import carried absolute clone override")
	}
	again, err := Plan(dest, r, options)
	if err != nil || again.Accounts+again.Sources+again.Repos+again.Collisions != 0 {
		t.Fatalf("second import: %+v %v", again, err)
	}
	for _, v := range again.Rows {
		if v.State != "keep" {
			t.Fatal(v)
		}
	}
}

func TestCollisionAndStalePreview(t *testing.T) {
	cfg := fleet(t)
	r, err := Export(cfg)
	if err != nil {
		t.Fatal(err)
	}
	src := &r.Sources[0]
	src.Repos = append(src.Repos, Repo{Key: "other/path", IDFolder: "my équipe", CloneFolder: "client/app one", RootSlot: "secondary-1"})
	options := Options{Slots: map[string]string{"primary": cfg.Global.Roots[0], "secondary-1": cfg.Global.Roots[1]}}
	p, err := Plan(cfg, r, options)
	if err != nil || p.Collisions != 1 {
		t.Fatalf("case-folded destination collision: %+v %v", p, err)
	}
	if Apply(cfg, p) == nil {
		t.Fatal("collision applied")
	}
	src.Repos[len(src.Repos)-1] = Repo{Key: "other/path"}
	dest := filepath.Join(cfg.Global.Roots[0], "private-key", "other", "path")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err = Plan(cfg, r, options)
	if err != nil || p.Collisions != 1 {
		t.Fatalf("occupied destination: %+v %v", p, err)
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	p, err = Plan(cfg, r, options)
	if err != nil || p.Collisions != 0 {
		t.Fatal(p, err)
	}
	cfg.Global.NestedScanDepth++
	if Apply(cfg, p) == nil {
		t.Fatal("stale plan applied")
	}
}

func TestPortableInputs(t *testing.T) {
	for _, path := range []string{"/home/clones", `C:\private\repo`, "../outside", "a/CON.txt", "a/.git", "~person", "org/..", "dot./repo", "host@example.org", "nul\x00byte"} {
		if relativePath(path) {
			t.Errorf("accepted %q", path)
		}
	}
	for _, s := range []string{"https://user:private-token@code.example.org", "https://code.example.org?token=private-token", "https://code.example.org/#private-token", "ssh://alias", "file:///tmp/root"} {
		cfg := fleet(t)
		a := cfg.Accounts["private-key"]
		a.URL = s
		cfg.Accounts["private-key"] = a
		if _, err := Export(cfg); err == nil || strings.Contains(err.Error(), "private-token") {
			t.Fatal("unportable or secret-bearing server reached export")
		}
	}
	r, err := Export(fleet(t))
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := r.Document()
	for _, invalid := range []string{doc + "{}", strings.Replace(doc, `"version": 1`, `"version": 2`, 1), strings.Replace(doc, `"version": 1`, `"token": "secret", "version": 1`, 1)} {
		if _, err := Decode(invalid); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
}

// TestSchemaUsesOnlyRecipeFields checks every nested object in both
// directions, including the boundary on extra properties.
func TestSchemaUsesOnlyRecipeFields(t *testing.T) {
	type node struct {
		Props map[string]json.RawMessage `json:"properties"`
		Extra *bool                      `json:"additionalProperties"`
		Items json.RawMessage            `json:"items"`
		Ref   string                     `json:"$ref"`
		Defs  map[string]json.RawMessage `json:"$defs"`
	}
	data, err := os.ReadFile("../../json/gitbox-recipe.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema node
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	var check func(reflect.Type, json.RawMessage)
	check = func(typ reflect.Type, raw json.RawMessage) {
		var n node
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatal(err)
		}
		if n.Ref != "" {
			check(typ, schema.Defs[strings.TrimPrefix(n.Ref, "#/$defs/")])
			return
		}
		switch typ.Kind() {
		case reflect.Slice:
			check(typ.Elem(), n.Items)
		case reflect.Struct:
			if n.Extra == nil || *n.Extra || len(n.Props) != typ.NumField() {
				t.Fatalf("%s is not a closed matching object", typ)
			}
			for field := range typ.Fields() {
				key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
				check(field.Type, n.Props[key])
			}
		}
	}
	check(reflect.TypeFor[Recipe](), data)
}
