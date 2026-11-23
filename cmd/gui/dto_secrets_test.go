//go:build !gitboxserver

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/internal/secretcheck"
	"github.com/LuisPalacios/gitbox/pkg/config"
)

// I allow only these exact metadata fields and types, never a whole DTO.
var secretMetadataTypes = map[secretcheck.Field]secretcheck.Allowed{
	// Empty credential settings carry no token; I still inspect nested fields.
	{Owner: reflect.TypeFor[config.GlobalConfig](), Name: "CredentialToken"}: {Type: reflect.TypeFor[*config.TokenGlobal](), JSONName: "credential_token"},
	// These flags describe credential presence or a required setup action.
	{Owner: reflect.TypeFor[MirrorCredentialCheck](), Name: "HasMirrorToken"}: {Type: reflect.TypeFor[bool](), JSONName: "hasMirrorToken"},
	{Owner: reflect.TypeFor[MirrorCredentialCheck](), Name: "NeedsPAT"}:       {Type: reflect.TypeFor[bool](), JSONName: "needsPat"},
	{Owner: reflect.TypeFor[CredentialSetupResult](), Name: "NeedsPAT"}:       {Type: reflect.TypeFor[bool](), JSONName: "needsPAT"},
	// PAT is a status label (ok/warning/error/none), not the credential value.
	{Owner: reflect.TypeFor[CredentialStatus](), Name: "PAT"}: {Type: reflect.TypeFor[string](), JSONName: "pat"},
}

func TestDTOsCarryNoSecrets(t *testing.T) {
	check := secretcheck.Checker{Allow: secretMetadataTypes}
	report := func(field string) { t.Errorf("secret-shaped output field: %s", field) }
	// Reflection discovers every bound method's output, including snapshots,
	// config accounts, status results and DTOs nested in containers.
	app := reflect.TypeFor[*App]()
	for method := range app.Methods() {
		for output := range method.Type.Outs() {
			check.Inspect(output, report)
		}
	}
	// I register event-only DTOs and input DTOs explicitly, so the source
	// inventory below can guard every exported type whose name ends in DTO.
	for _, typ := range []reflect.Type{
		reflect.TypeFor[MoveProgressEventDTO](), reflect.TypeFor[MoveResultDTO](),
		reflect.TypeFor[PRAccountUpdateDTO](), reflect.TypeFor[AdoptRequestDTO](),
		reflect.TypeFor[MoveRequestDTO](), reflect.TypeFor[MirrorStatusResult](),
		reflect.TypeFor[WindowErrorDTO](),
	} {
		check.Inspect(typ, report)
	}
	covered := make(map[string]bool)
	for typ := range check.Seen {
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
