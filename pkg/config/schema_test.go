package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type schemaObject struct {
	Properties map[string]*schemaObject `json:"properties"`
	Additional json.RawMessage          `json:"additionalProperties"`
	Items      *schemaObject            `json:"items"`
}

// TestSchemaMatchesConfig catches property drift in both directions, even at
// boundaries that permit future unknown keys.
func TestSchemaMatchesConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "json", "gitbox.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema schemaObject
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	compareSchema(t, reflect.TypeFor[Config](), &schema, "config")
}

func compareSchema(t *testing.T, model reflect.Type, schema *schemaObject, path string) {
	t.Helper()
	if schema == nil {
		t.Errorf("%s: missing schema for %v", path, model)
		return
	}
	if model.Kind() == reflect.Pointer {
		model = model.Elem()
	}
	switch model.Kind() {
	case reflect.Struct:
		known := make(map[string]bool)
		for field := range model.Fields() {
			if !field.IsExported() {
				continue
			}
			key := jsonKey(field)
			if key == "-" {
				continue
			}
			if key == "" {
				key = field.Name
			}
			known[key] = true
			if property, ok := schema.Properties[key]; ok {
				compareSchema(t, field.Type, property, path+"."+key)
			} else {
				t.Errorf("%s.%s is missing from the schema", path, key)
			}
		}
		for key := range schema.Properties {
			if !known[key] {
				t.Errorf("%s.%s is missing from %v", path, key, model)
			}
		}
		allowsExtensions := model == reflect.TypeFor[GlobalConfig]() ||
			model == reflect.TypeFor[Account]() || model == reflect.TypeFor[Source]() || model == reflect.TypeFor[Repo]()
		expected := "false"
		if allowsExtensions {
			expected = "true"
		}
		if !bytes.Equal(bytes.TrimSpace(schema.Additional), []byte(expected)) {
			t.Errorf("%s must set additionalProperties to %s", path, expected)
		}
	case reflect.Map:
		var entry schemaObject
		if err := json.Unmarshal(schema.Additional, &entry); err != nil {
			t.Errorf("%s: invalid map entry schema: %v", path, err)
			return
		}
		compareSchema(t, model.Elem(), &entry, path+".*")
	case reflect.Slice:
		compareSchema(t, model.Elem(), schema.Items, path+"[]")
	}
}

func TestMarshalUsesTheKnownJSONShape(t *testing.T) {
	// If the top-level disk shape drifts from Config, a new known field
	// could be sent to the view but lost on save.
	cfg := &Config{Schema: "schema-uri", Version: CurrentVersion, Global: GlobalConfig{Roots: []string{"~/repos"}},
		Accounts: map[string]Account{"account": {Name: "name"}},
		Sources:  map[string]Source{"source": {Account: "account", Repos: map[string]Repo{"org/repo": {Email: "x@example.org"}}}},
		Mirrors:  map[string]Mirror{"mirror": {AccountSrc: "account"}},
	}
	got, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSuffix(string(got), "\n") != string(want) {
		t.Fatalf("known JSON changed on disk:\n%s\nwant:\n%s", got, want)
	}
}
