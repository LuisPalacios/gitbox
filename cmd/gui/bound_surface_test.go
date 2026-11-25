//go:build !gitboxserver

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestBoundSurface(t *testing.T) {
	const inventory = "testdata/bound_methods.txt"
	data, err := os.ReadFile(inventory)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, purpose, ok := strings.Cut(line, " - ")
		if !ok || strings.TrimSpace(purpose) == "" {
			t.Errorf("%s:%d: expected a method name and its purpose", inventory, i+1)
			continue
		}
		if want[name] {
			t.Errorf("%s:%d: duplicate method %s", inventory, i+1, name)
		}
		want[name] = true
	}
	if len(want) == 0 {
		t.Fatalf("%s has no methods", inventory)
	}

	check := func(source string, actual map[string]bool) {
		t.Helper()
		for _, name := range sortedKeys(actual) {
			if !want[name] {
				t.Errorf("%s exposes %s, missing from %s", source, name, inventory)
			}
		}
		for _, name := range sortedKeys(want) {
			if !actual[name] {
				t.Errorf("%s is missing %s from %s", source, name, inventory)
			}
		}
	}
	check("*App", methodSet(reflect.TypeFor[*App]()))

	export := regexp.MustCompile(`(?m)^export function (\w+)\(`)
	for _, file := range []string{"App.js", "App.d.ts"} {
		data, err := os.ReadFile(filepath.Join("frontend", "wailsjs", "go", "main", file))
		if err != nil {
			t.Fatal(err)
		}
		generated := map[string]bool{}
		for _, match := range export.FindAllStringSubmatch(string(data), -1) {
			generated[match[1]] = true
		}
		check(file, generated)
	}
}
