package i18n

import (
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"":            "en",
		"en":          "en",
		"es":          "es",
		"ES_es.UTF-8": "es",
		"fr":          "en",
	}
	for in, want := range tests {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSupported(t *testing.T) {
	if !Supported("es-ES") {
		t.Fatal("es-ES should be supported")
	}
	if Supported("fr") {
		t.Fatal("fr should not be supported")
	}
}

func TestResolvePrecedence(t *testing.T) {
	t.Setenv("GITBOX_LANG", "es")
	cfg := &config.Config{Global: config.GlobalConfig{Language: "en"}}
	if got := Resolve("en", cfg); got != "en" {
		t.Fatalf("override Resolve = %q, want en", got)
	}
	if got := Resolve("", cfg); got != "es" {
		t.Fatalf("env Resolve = %q, want es", got)
	}
	t.Setenv("GITBOX_LANG", "")
	if got := Resolve("", cfg); got != "en" {
		t.Fatalf("config Resolve = %q, want en", got)
	}
}

func TestTranslatorFallback(t *testing.T) {
	tr := New("es")
	if got := tr.T("app.description"); got == "" || got == catalogs[English]["app.description"] {
		t.Fatalf("Spanish translation not used: %q", got)
	}
	if got := tr.T("missing.key"); got != "missing.key" {
		t.Fatalf("missing key = %q, want key", got)
	}
}
