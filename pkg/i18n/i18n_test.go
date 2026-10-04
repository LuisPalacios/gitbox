package i18n

import "testing"

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
