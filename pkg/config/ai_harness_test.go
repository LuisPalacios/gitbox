package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAIHarnessEntryRoundTripsSourceAndMissing(t *testing.T) {
	in := AIHarnessEntry{
		Name:    "Codex CLI",
		Command: "/opt/homebrew/bin/codex",
		Args:    []string{"--model", "x"},
		Source:  "detected",
		Missing: true,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"source":"detected"`, `"missing":true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("serialized entry missing %s: %s", want, raw)
		}
	}
	var out AIHarnessEntry
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip mismatch:\n in=%+v\nout=%+v", in, out)
	}
}

func TestAIHarnessEntryOmitsZeroSourceAndMissing(t *testing.T) {
	raw, err := json.Marshal(AIHarnessEntry{Name: "Claude Code", Command: "claude"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, unwanted := range []string{`"source"`, `"missing"`} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("zero-value %s must be omitted so old configs stay byte-identical: %s", unwanted, raw)
		}
	}
}

func TestVisibleAIHarnessesFiltersMissingAndKeepsOrder(t *testing.T) {
	g := GlobalConfig{AIHarnesses: []AIHarnessEntry{
		{Name: "A", Command: "a", Source: "detected"},
		{Name: "B", Command: "b", Source: "detected", Missing: true},
		{Name: "C", Command: "c", Source: "user", Args: []string{"-x"}},
		{Name: "D", Command: "d", Source: "user", Missing: true},
	}}
	got := g.VisibleAIHarnesses()
	if len(got) != 2 {
		t.Fatalf("expected 2 visible entries, got %d: %+v", len(got), got)
	}
	if got[0].Name != "A" || got[1].Name != "C" {
		t.Errorf("order/filter wrong: %+v", got)
	}
	if !reflect.DeepEqual(got[1].Args, []string{"-x"}) {
		t.Errorf("args not carried through: %+v", got[1])
	}
}

func TestVisibleAIHarnessesEmpty(t *testing.T) {
	if got := (GlobalConfig{}).VisibleAIHarnesses(); got != nil {
		t.Errorf("expected nil for no harnesses, got %+v", got)
	}
}
