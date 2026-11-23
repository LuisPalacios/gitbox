package secretcheck

import (
	"reflect"
	"strings"
	"testing"
)

func TestInspectRejectsNestedSecrets(t *testing.T) {
	type secret struct {
		Value string `json:"password"`
		Token string
	}
	type nested struct {
		Items map[string][]*secret
		Next  *nested
	}
	var fields []string
	var c Checker
	c.Inspect(reflect.TypeFor[nested](), func(field string) { fields = append(fields, field) })
	if len(fields) != 2 || !strings.HasSuffix(fields[0], ".Value") || !strings.HasSuffix(fields[1], ".Token") {
		t.Fatalf("reported fields = %v; want nested JSON password and Token", fields)
	}
}

func TestInspectAllowsOnlyTheExactShape(t *testing.T) {
	type status struct {
		PAT    string `json:"pat"`
		Hidden string `json:"-"`
		token  string
	}
	allow := map[Field]Allowed{{reflect.TypeFor[status](), "PAT"}: {reflect.TypeFor[string](), "pat"}}
	var fields []string
	c := Checker{Allow: allow}
	c.Inspect(reflect.TypeFor[status](), func(field string) { fields = append(fields, field) })
	if len(fields) != 0 {
		t.Errorf("reported %v for an allowlisted field and two that never cross the wire", fields)
	}
	type renamed struct {
		PAT string `json:"patValue"`
	}
	c = Checker{Allow: map[Field]Allowed{{reflect.TypeFor[renamed](), "PAT"}: {reflect.TypeFor[string](), "pat"}}}
	fields = nil
	c.Inspect(reflect.TypeFor[renamed](), func(field string) { fields = append(fields, field) })
	if len(fields) != 1 {
		t.Errorf("a field whose JSON name changed was not reported: %v", fields)
	}
	_ = status{}.token
}
