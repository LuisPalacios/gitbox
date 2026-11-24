package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// globalKeyClass classifies every global key as client or domain (#236).
// TestStripClientPrefs fails when GlobalConfig gains a key with no row here.
var globalKeyClass = map[string]string{
	"roots":                  "domain",
	"nested_scan_depth":      "domain",
	"ignored_clones":         "domain",
	"language":               "client",
	"periodic_sync":          "client",
	"window":                 "client",
	"compact_window":         "client",
	"view_mode":              "client",
	"ui_mode":                "client",
	"show_pull_plan":         "client",
	"theme":                  "client",
	"collapsed":              "split", // sidebar is client, the rest is domain
	"credential_ssh":         "domain",
	"credential_gcm":         "domain",
	"credential_token":       "domain",
	"editors":                "client",
	"ai_harnesses":           "domain",
	"terminals":              "client",
	"terminal_apps":          "client",
	"shells":                 "client",
	"terminal_profiles":      "client",
	"pr_badges_enabled":      "domain",
	"pr_include_drafts":      "domain",
	"check_global_gitignore": "domain",
	"experimental_remote":    "client",
	"remotes":                "client",
}

func fullPrefs() ClientPrefs {
	return ClientPrefs{
		Theme:            "dark",
		Language:         "es",
		UIMode:           "simple",
		ShowPullPlan:     new(false),
		ViewMode:         "compact",
		Window:           &WindowState{X: 1, Y: 2, Width: 900, Height: 700},
		CompactWindow:    &WindowState{X: 3, Y: 4, Width: 220, Height: 400},
		SidebarCollapsed: true,
		Editors:          []EditorEntry{{Name: "VS Code", Command: "/usr/bin/code"}},
		Terminals:        []TerminalEntry{{Name: "Legacy", Command: "xterm", Args: []string{"-e"}}},
		TerminalApps:     []TerminalApp{{ID: "wez", Name: "WezTerm", Command: "wezterm", ArgsTemplate: []string{"start", "--cwd", "{path}"}}},
		Shells:           []ShellEntry{{ID: "zsh", Name: "zsh", Command: "/bin/zsh", Args: []string{"-l"}}},
		TerminalProfiles: []TerminalProfile{{ID: "wez-zsh", Name: "WezTerm + zsh", TerminalID: "wez", ShellID: "zsh", Args: []string{"{path}"}, Default: true}},
		PeriodicSync:     "15m",

		ExperimentalRemote: true,
		Remotes: []RemoteEntry{{
			Alias: "my-server", ServerDir: "~/srv", Editor: "code",
			Window:        &WindowState{X: 5, Y: 6, Width: 800, Height: 600},
			CompactWindow: &WindowState{X: 7, Y: 8, Width: 210, Height: 390},
		}},
	}
}

func fullGlobal() GlobalConfig {
	yes := true
	c := &Config{Global: GlobalConfig{
		Roots:                []string{"~/git"},
		NestedScanDepth:      2,
		IgnoredClones:        []string{"/tmp/ignored"},
		Collapsed:            &CollapsedState{Sources: []string{"src"}, Repos: []string{"src/org/repo"}, View: "all"},
		CredentialSSH:        &SSHGlobal{},
		CredentialGCM:        &GCMGlobal{},
		CredentialToken:      &TokenGlobal{},
		AIHarnesses:          []AIHarnessEntry{{Name: "Harness", Command: "harness"}},
		PRBadgesEnabled:      &yes,
		PRIncludeDrafts:      &yes,
		CheckGlobalGitignore: &yes,
	}}
	ApplyPrefs(c, fullPrefs())
	return c.Global
}

func jsonKey(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	return name
}

func TestPrefsRoundTrip(t *testing.T) {
	full := fullPrefs()
	fv := reflect.ValueOf(full)
	for i := 0; i < fv.NumField(); i++ {
		name := fv.Type().Field(i).Name
		if fv.Field(i).IsZero() {
			t.Fatalf("fullPrefs leaves %s zero: the round trip would not cover it", name)
		}
		t.Run(name, func(t *testing.T) {
			var p ClientPrefs
			reflect.ValueOf(&p).Elem().Field(i).Set(fv.Field(i))
			c := &Config{}
			ApplyPrefs(c, p)
			if got := ExtractPrefs(c); !reflect.DeepEqual(got, p) {
				t.Errorf("round trip of %s:\n got %+v\nwant %+v", name, got, p)
			}
		})
	}

	c := &Config{}
	ApplyPrefs(c, full)
	got := ExtractPrefs(c)
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("full round trip:\n got %+v\nwant %+v", got, full)
	}
	// The copy is detached in both directions.
	got.Editors[0].Name = "changed"
	got.Remotes[0].Window.X = 99
	got.TerminalProfiles[0].Args[0] = "changed"
	if again := ExtractPrefs(c); !reflect.DeepEqual(again, full) {
		t.Errorf("mutating an extracted copy changed the config: %+v", again)
	}
}

func TestApplyPrefsKeepsDomainKeys(t *testing.T) {
	c := &Config{Global: fullGlobal()}
	shared := c.Global.Collapsed
	before := *shared

	ApplyPrefs(c, ClientPrefs{})

	if !reflect.DeepEqual(*shared, before) {
		t.Errorf("ApplyPrefs wrote through a shared collapsed state: %+v", *shared)
	}
	want := fullGlobal().Collapsed
	want.Sidebar = false
	if !reflect.DeepEqual(c.Global.Collapsed, want) {
		t.Errorf("collapsed = %+v, want %+v", c.Global.Collapsed, want)
	}
}

func TestStripClientPrefs(t *testing.T) {
	full := fullGlobal()
	c := &Config{Global: full}
	StripClientPrefs(c)

	fullV, gotV := reflect.ValueOf(full), reflect.ValueOf(c.Global)
	seen := make(map[string]bool)
	for i := 0; i < fullV.NumField(); i++ {
		field := fullV.Type().Field(i)
		if !field.IsExported() {
			continue // Persistence extensions have no client preference classification.
		}
		key := jsonKey(field)
		seen[key] = true
		if fullV.Field(i).IsZero() {
			t.Fatalf("fullGlobal leaves %q zero: the strip would not cover it", key)
		}
		switch globalKeyClass[key] {
		case "client":
			if !gotV.Field(i).IsZero() {
				t.Errorf("client key %q survived the strip: %+v", key, gotV.Field(i).Interface())
			}
		case "domain":
			if !reflect.DeepEqual(gotV.Field(i).Interface(), fullV.Field(i).Interface()) {
				t.Errorf("domain key %q changed: %+v", key, gotV.Field(i).Interface())
			}
		case "split":
			want := *full.Collapsed
			want.Sidebar = false
			if c.Global.Collapsed == nil || !reflect.DeepEqual(*c.Global.Collapsed, want) {
				t.Errorf("collapsed = %+v, want %+v", c.Global.Collapsed, want)
			}
		default:
			t.Errorf("global key %q has no class: add it to globalKeyClass and, if it is a client key, to ClientPrefs", key)
		}
	}
	for key := range globalKeyClass {
		if !seen[key] {
			t.Errorf("globalKeyClass lists %q, which GlobalConfig no longer has", key)
		}
	}

	// A collapsed state that only held the sidebar disappears.
	only := &Config{Global: GlobalConfig{Collapsed: &CollapsedState{Sidebar: true}}}
	StripClientPrefs(only)
	if only.Global.Collapsed != nil {
		t.Errorf("sidebar-only collapsed survived: %+v", only.Global.Collapsed)
	}
}

func TestPreserveClientPrefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitbox.json")
	host := &Config{Version: CurrentVersion, Global: fullGlobal(), Accounts: map[string]Account{}, Sources: map[string]Source{}}
	host.Global.Collapsed = &CollapsedState{Sidebar: true}
	host.Global.Terminals = nil // the legacy key never survives a load
	if err := Save(host, path); err != nil {
		t.Fatal(err)
	}
	onDisk, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := ExtractPrefs(onDisk)
	if len(want.Remotes) != 1 || want.Theme != "dark" {
		t.Fatalf("fixture lost its client keys on load: %+v", want)
	}

	// The server works on a config without client keys, changes the domain
	// and saves it over the host's file.
	server, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	StripClientPrefs(server)
	server.Global.IgnoredClones = append(server.Global.IgnoredClones, "/tmp/second")
	PreserveClientPrefs(server, onDisk)
	if err := Save(server, path); err != nil {
		t.Fatal(err)
	}

	after, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := ExtractPrefs(after); !reflect.DeepEqual(got, want) {
		t.Errorf("client keys after the server save:\n got %+v\nwant %+v", got, want)
	}
	if got := after.Global.IgnoredClones; len(got) != 2 || got[1] != "/tmp/second" {
		t.Errorf("domain change lost: ignored_clones = %v", got)
	}
}

func configJSON(global string) []byte {
	return []byte(`{"version":5,"global":{"roots":["~/git"]` + global + `},"accounts":{},"sources":{}}`)
}

func TestRemotesValidation(t *testing.T) {
	tests := []struct {
		name    string
		remotes string
		wantErr string
	}{
		{"valid", `[{"alias":"my-server"},{"alias":"box.lan_2","server_dir":"~/srv"},{"alias":"abs","server_dir":"/opt/gitbox"},{"alias":"win","server_dir":"C:\\gitbox"}]`, ""},
		{"empty alias", `[{"alias":""}]`, "alias"},
		{"alias with space", `[{"alias":"my server"}]`, "alias"},
		{"alias with user and host", `[{"alias":"me@host"}]`, "alias"},
		{"alias read as an option", `[{"alias":"-oProxyCommand=x"}]`, "alias"},
		{"alias too long", `[{"alias":"` + strings.Repeat("a", 65) + `"}]`, "alias"},
		{"duplicate alias", `[{"alias":"one"},{"alias":"one"}]`, "duplicate alias"},
		{"relative server_dir", `[{"alias":"one","server_dir":"srv/gitbox"}]`, "server_dir"},
		{"bare tilde server_dir", `[{"alias":"one","server_dir":"~other/srv"}]`, "server_dir"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse(configJSON(`,"remotes":` + tt.remotes))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				if len(cfg.Global.Remotes) != 4 {
					t.Errorf("remotes = %+v", cfg.Global.Remotes)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse error = %v, want one naming %q", err, tt.wantErr)
			}
		})
	}
}

// TestAdditiveKeysKeepVersion5 pins the decision to keep version 5 when
// adding optional keys. V5 loaders accept unknown members.
func TestAdditiveKeysKeepVersion5(t *testing.T) {
	if CurrentVersion != 5 {
		t.Fatalf("CurrentVersion = %d: revisit the unknown-key policy with the bump", CurrentVersion)
	}
	data := []byte(`{"version":5,"future_top_level":{"a":1},"global":{"roots":["~/git"],"future_key":"x","remotes":[{"alias":"one","future_field":true}]},"accounts":{},"sources":{}}`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("a v5 config with unknown keys must load: %v", err)
	}
	if len(cfg.Global.Roots) != 1 || len(cfg.Global.Remotes) != 1 {
		t.Errorf("known keys lost next to unknown ones: %+v", cfg.Global)
	}
}

func TestWindowFor(t *testing.T) {
	p := fullPrefs()
	ws := &WindowState{X: 42}

	if !p.SetWindow("", false, ws) || p.Window != ws || p.Remotes[0].Window == ws {
		t.Errorf("local full window not stored in global.window")
	}
	if !p.SetWindow("my-server", true, ws) || p.Remotes[0].CompactWindow != ws || p.CompactWindow == ws {
		t.Errorf("remote compact window not stored in its own entry")
	}
	if full, compact := p.WindowFor("my-server"); full != p.Remotes[0].Window || compact != ws {
		t.Errorf("WindowFor(my-server) = %v, %v", full, compact)
	}
	if p.SetWindow("unknown", false, ws) {
		t.Errorf("SetWindow accepted an alias that names no remote")
	}
	if full, compact := p.WindowFor("unknown"); full != nil || compact != nil {
		t.Errorf("WindowFor(unknown) = %v, %v", full, compact)
	}
}

// schemaProps returns the "properties" object found by following path from
// the root of the JSON schema.
func schemaProps(t *testing.T, path ...string) (props map[string]json.RawMessage, required []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "json", "gitbox.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	node := json.RawMessage(data)
	for _, step := range path {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(node, &obj); err != nil {
			t.Fatalf("schema at %q: %v", step, err)
		}
		next, ok := obj[step]
		if !ok {
			t.Fatalf("schema has no %q on the way to %v", step, path)
		}
		node = next
	}
	var obj struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(node, &obj); err != nil {
		t.Fatal(err)
	}
	return obj.Properties, obj.Required
}

// TestSchemaKeepsRemoteKeysOptional protects the config contract introduced
// by #236: a file without these keys stays valid.
func TestSchemaKeepsRemoteKeysOptional(t *testing.T) {
	check := func(optional []string, path ...string) {
		t.Helper()
		_, required := schemaProps(t, path...)
		for _, key := range optional {
			if slices.Contains(required, key) {
				t.Errorf("schema %v requires %q; it must stay optional", path, key)
			}
		}
	}
	check([]string{"remotes", "experimental_remote"}, "properties", "global")
	check([]string{"server_dir", "editor", "window", "compact_window"},
		"properties", "global", "properties", "remotes", "items")

	_, required := schemaProps(t, "properties", "global", "properties", "remotes", "items")
	if len(required) != 1 || required[0] != "alias" {
		t.Errorf("a remote entry must require exactly \"alias\", got %v", required)
	}
}
