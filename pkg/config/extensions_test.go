package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const extensionsFixture = `{
	"version": 5, "unknown_top": true,
	"global": {
		"roots": ["~/fleet"], "future_z": [true, null], "future_a": {"n": 9007199254740993},
		"folder": "~/retired", "extra_folders": ["~/retired-extra"], "cards_open": ["obsolete"],
		"experimental_remote": true, "THEME": "dark"
	},
	"accounts": {"sample": {
		"provider": "github", "url": "https://example.org", "username": "user",
		"name": "Fixture user", "email": "user@example.org",
		"future_account": "account extension", "_test": {"token": "fixture-token-canary"}
	}},
	"sources": {"sample": {
		"account": "sample", "future_source": false, "repos": {
			"org/repo": {"name": "Original", "future_repo": null}
		}
	}},
	"workspaces": {"old": {}}
}`

func parseExtensionsFixture(t *testing.T) *Config {
	t.Helper()
	cfg, err := Parse([]byte(extensionsFixture))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cfg
}

func assertDiskExtensions(t *testing.T, data []byte, accountKey, sourceKey string) {
	t.Helper()
	var disk struct {
		Global   map[string]json.RawMessage            `json:"global"`
		Accounts map[string]map[string]json.RawMessage `json:"accounts"`
		Sources  map[string]struct {
			Extra json.RawMessage                       `json:"future_source"`
			Repos map[string]map[string]json.RawMessage `json:"repos"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("reading saved JSON: %v", err)
	}
	source := disk.Sources[sourceKey]
	for _, value := range []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"global.future_z", disk.Global["future_z"], `[true,null]`},
		{"global.future_a", disk.Global["future_a"], `{"n":9007199254740993}`},
		{"account.future_account", disk.Accounts[accountKey]["future_account"], `"account extension"`},
		{"source.future_source", source.Extra, "false"},
		{"repo.future_repo", source.Repos["org/repo"]["future_repo"], "null"},
	} {
		var compact bytes.Buffer
		if err := json.Compact(&compact, value.raw); err != nil || compact.String() != value.want {
			t.Errorf("%s = %s, want %s (%v)", value.name, value.raw, value.want, err)
		}
	}
	for _, absent := range []string{`"unknown_top"`, `"folder"`, `"extra_folders"`, `"cards_open"`, `"experimental_remote"`, `"THEME"`, `"_test"`, "fixture-token-canary", `"workspaces"`} {
		if bytes.Contains(data, []byte(absent)) {
			t.Errorf("%s survived disk serialization", absent)
		}
	}
}

func TestUnknownMembersSurviveSaveAndStayOutOfDTOJSON(t *testing.T) {
	cfg := parseExtensionsFixture(t)
	if cfg.Global.Theme != "dark" {
		t.Fatalf("case-insensitive known key did not load: %q", cfg.Global.Theme)
	}
	path := filepath.Join(t.TempDir(), "gitbox.json")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertDiskExtensions(t, data, "sample", "sample")
	if !(bytes.Index(data, []byte(`"theme"`)) < bytes.Index(data, []byte(`"future_a"`)) &&
		bytes.Index(data, []byte(`"future_a"`)) < bytes.Index(data, []byte(`"future_z"`))) {
		t.Error("known fields must precede sorted extensions")
	}
	saved, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !EqualIgnoringUIState(cfg, saved) {
		t.Fatal("saved extensions changed config identity")
	}
	// The ordinary JSON representation is also used for frontend DTOs. It
	// must exclude unknown values at every model boundary.
	view, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"future_a", "future_z", "future_account", "future_source", "future_repo", "fixture-token-canary"} {
		if bytes.Contains(view, []byte(absent)) {
			t.Errorf("%s reached ordinary JSON", absent)
		}
	}
}

func TestUnknownMembersSurviveCloneAndKnownFieldEdits(t *testing.T) {
	cfg := parseExtensionsFixture(t)
	copied, err := cfg.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if cfg.Sources["sample"].Repos["org/repo"] != copied.Sources["sample"].Repos["org/repo"] {
		t.Fatal("cloning changed repo equality")
	}
	ApplyPrefs(copied, ClientPrefs{Theme: "light"})
	if !EqualIgnoringUIState(cfg, copied) {
		t.Fatal("theme update counted as a domain change")
	}
	copied.Global.extra = `{"future_z":false}`
	if EqualIgnoringUIState(cfg, copied) {
		t.Fatal("extension update did not count as a domain change")
	}
	copied.Global.extra = cfg.Global.extra
	acct := copied.Accounts["sample"]
	replacement := Account{Provider: acct.Provider, URL: acct.URL, Username: acct.Username, Name: "Edited", Email: acct.Email}
	if err := copied.UpdateAccount("sample", replacement); err != nil {
		t.Fatal(err)
	}
	repos := map[string]Repo{"org/repo": {Name: "Updated via source"}}
	if err := copied.UpdateSource("sample", Source{Account: "sample", Repos: repos}); err != nil {
		t.Fatal(err)
	}
	if repos["org/repo"].extra != "" {
		t.Fatal("updating a source wrote extensions into the caller's repo map")
	}
	if err := copied.UpdateRepo("sample", "org/repo", Repo{Name: "Updated directly"}); err != nil {
		t.Fatal(err)
	}
	if err := copied.RenameAccount("sample", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := copied.RenameSource("sample", "renamed-source"); err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(copied)
	if err != nil {
		t.Fatal(err)
	}
	assertDiskExtensions(t, data, "renamed", "renamed-source")
	if cfg.Sources["sample"].Repos["org/repo"].Name != "Original" {
		t.Fatal("editing the copy modified the original")
	}
}

// The compact view is gone (#342): a file that still says it was in use
// loads, keeps the full-window geometry of the local and the remote
// windows, and the next save writes none of the retired keys.
func TestRetiredCompactViewKeys(t *testing.T) {
	const fixture = `{
		"version": 5,
		"global": {
			"roots": ["~/fleet"], "view_mode": "compact",
			"window": {"x": 10, "y": 20, "width": 1100, "height": 760},
			"compact_window": {"x": 1, "y": 2, "width": 220, "height": 400},
			"remotes": [{"alias": "build-host",
				"window": {"x": 30, "y": 40, "width": 1000, "height": 700},
				"compact_window": {"x": 3, "y": 4, "width": 210, "height": 390}}]
		},
		"accounts": {}, "sources": {}
	}`
	path := filepath.Join(t.TempDir(), "gitbox.json")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if w := cfg.Global.Window; w == nil || w.X != 10 || w.Width != 1100 {
		t.Errorf("global.window = %+v, want the saved full window", w)
	}
	if len(cfg.Global.Remotes) != 1 || cfg.Global.Remotes[0].Window == nil || cfg.Global.Remotes[0].Window.X != 30 {
		t.Errorf("remotes = %+v, want the remote's window kept", cfg.Global.Remotes)
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, retired := range []string{`"view_mode"`, `"compact_window"`} {
		if bytes.Contains(data, []byte(retired)) {
			t.Errorf("%s survived the save:\n%s", retired, data)
		}
	}
	if !bytes.Contains(data, []byte(`"window"`)) {
		t.Errorf("the save lost the window geometry:\n%s", data)
	}
}
