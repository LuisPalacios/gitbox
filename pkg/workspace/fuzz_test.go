package workspace

import (
	"bytes"
	"encoding/json"
	"testing"
)

func FuzzStripJSONC(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		out := stripJSONC(data)
		if len(out) > len(data) {
			t.Fatalf("stripping JSONC grew input from %d to %d bytes", len(data), len(out))
		}
		if json.Valid(data) && !bytes.Equal(out, data) {
			t.Fatalf("stripping changed valid JSON:\nbefore: %q\nafter: %q", data, out)
		}
	})
}
