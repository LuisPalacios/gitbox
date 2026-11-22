//go:build !gitboxserver

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

var secretFieldName = regexp.MustCompile(`(?i)token|secret|password|pat$`)

type secretMetadataField struct {
	owner reflect.Type
	name  string
}

// I allow only these exact metadata fields and types, never a whole DTO.
type secretMetadataType struct {
	typ      reflect.Type
	jsonName string
}

var secretMetadataTypes = map[secretMetadataField]secretMetadataType{
	// Empty credential settings carry no token; I still inspect nested fields.
	{reflect.TypeFor[config.GlobalConfig](), "CredentialToken"}: {reflect.TypeFor[*config.TokenGlobal](), "credential_token"},
	// These flags describe credential presence or a required setup action.
	{reflect.TypeFor[MirrorCredentialCheck](), "HasMirrorToken"}: {reflect.TypeFor[bool](), "hasMirrorToken"},
	{reflect.TypeFor[MirrorCredentialCheck](), "NeedsPAT"}:       {reflect.TypeFor[bool](), "needsPat"},
	{reflect.TypeFor[CredentialSetupResult](), "NeedsPAT"}:       {reflect.TypeFor[bool](), "needsPAT"},
	// PAT is a status label (ok/warning/error/none), not the credential value.
	{reflect.TypeFor[CredentialStatus](), "PAT"}: {reflect.TypeFor[string](), "pat"},
}

func inspectSecretFields(typ reflect.Type, seen map[reflect.Type]bool, report func(string)) {
	if seen[typ] {
		return
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		inspectSecretFields(typ.Elem(), seen, report)
	case reflect.Map:
		inspectSecretFields(typ.Key(), seen, report)
		inspectSecretFields(typ.Elem(), seen, report)
	case reflect.Struct:
		for field := range typ.Fields() {
			if !field.IsExported() || field.Tag.Get("json") == "-" {
				continue
			}
			jsonName, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if secretFieldName.MatchString(field.Name) || secretFieldName.MatchString(jsonName) {
				allowed, ok := secretMetadataTypes[secretMetadataField{typ, field.Name}]
				if !ok || allowed.typ != field.Type || allowed.jsonName != jsonName {
					report(typ.String() + "." + field.Name)
				}
			}
			inspectSecretFields(field.Type, seen, report)
		}
	}
}

func TestDTOsCarryNoSecrets(t *testing.T) {
	seen := make(map[reflect.Type]bool)
	report := func(field string) { t.Errorf("secret-shaped output field: %s", field) }
	// Reflection discovers every bound method's output, including snapshots,
	// config accounts, status results and DTOs nested in containers.
	app := reflect.TypeFor[*App]()
	for method := range app.Methods() {
		for output := range method.Type.Outs() {
			inspectSecretFields(output, seen, report)
		}
	}
	// I register event-only DTOs and input DTOs explicitly, so the source
	// inventory below can guard every exported type whose name ends in DTO.
	for _, typ := range []reflect.Type{
		reflect.TypeFor[MoveProgressEventDTO](), reflect.TypeFor[MoveResultDTO](),
		reflect.TypeFor[PRAccountUpdateDTO](), reflect.TypeFor[AdoptRequestDTO](),
		reflect.TypeFor[MoveRequestDTO](), reflect.TypeFor[MirrorStatusResult](),
	} {
		inspectSecretFields(typ, seen, report)
	}
	covered := make(map[string]bool)
	for typ := range seen {
		if typ.PkgPath() == reflect.TypeFor[App]().PkgPath() {
			covered[typ.Name()] = true
		}
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.TypeSpec)
			if ok && decl.Name.IsExported() && strings.HasSuffix(decl.Name.Name, "DTO") && !covered[decl.Name.Name] {
				t.Errorf("register DTO %s in the secret canary", decl.Name.Name)
			}
			return true
		})
	}
}

func TestDTOSecretCanary_RejectsNestedSecrets(t *testing.T) {
	type secret struct {
		Value string `json:"password"`
		Token string
	}
	type nested struct {
		Items map[string][]*secret
		Next  *nested
	}
	var fields []string
	inspectSecretFields(reflect.TypeFor[nested](), make(map[reflect.Type]bool), func(field string) {
		fields = append(fields, field)
	})
	if len(fields) != 2 || !strings.HasSuffix(fields[0], ".Value") || !strings.HasSuffix(fields[1], ".Token") {
		t.Fatalf("reported fields = %v; want nested JSON password and Token", fields)
	}
}
