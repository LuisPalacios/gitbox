// Package secretcheck is the structural half of the secret canary (#163,
// #249, #242): it walks a type and reports every exported field whose Go or
// JSON name looks like a secret (token, secret, password, a trailing "pat"),
// unless an allowlist entry names that exact field, its type and its JSON
// name. Only tests import it.
package secretcheck

import (
	"reflect"
	"regexp"
	"strings"
)

// Name matches a field name that looks like it holds a secret.
var Name = regexp.MustCompile(`(?i)token|secret|password|pat$`)

// Field names one field of one struct type.
type Field struct {
	Owner reflect.Type
	Name  string
}

// Allowed is the exact shape an allowlisted field must keep: a field that
// changes its type or its JSON name is reported again.
type Allowed struct {
	Type     reflect.Type
	JSONName string
}

// Checker inspects types against its allowlist. The zero value allows
// nothing. Seen records every type inspected so far, so a caller can check
// what the walk covered.
type Checker struct {
	Allow map[Field]Allowed
	Seen  map[reflect.Type]bool
}

// Inspect walks typ through pointers, slices, arrays, maps and struct
// fields, and calls report with "Type.Field" for each secret-shaped field
// that the allowlist does not cover. Fields tagged json:"-" and unexported
// fields are skipped: they never cross the wire.
func (c *Checker) Inspect(typ reflect.Type, report func(string)) {
	if c.Seen == nil {
		c.Seen = make(map[reflect.Type]bool)
	}
	if c.Seen[typ] {
		return
	}
	c.Seen[typ] = true
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		c.Inspect(typ.Elem(), report)
	case reflect.Map:
		c.Inspect(typ.Key(), report)
		c.Inspect(typ.Elem(), report)
	case reflect.Struct:
		for field := range typ.Fields() {
			if !field.IsExported() || field.Tag.Get("json") == "-" {
				continue
			}
			jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if Name.MatchString(field.Name) || Name.MatchString(jsonName) {
				allowed, ok := c.Allow[Field{typ, field.Name}]
				if !ok || allowed.Type != field.Type || allowed.JSONName != jsonName {
					report(typ.String() + "." + field.Name)
				}
			}
			c.Inspect(field.Type, report)
		}
	}
}
