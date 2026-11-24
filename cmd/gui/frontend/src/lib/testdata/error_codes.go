// I export the declared Git and provider codes for the frontend catalog test.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
)

func main() {
	var codes []string
	for _, source := range []struct{ file, kind, prefix string }{
		{"../../../pkg/git/errors.go", "GitCode", "git."},
		{"../../../pkg/provider/errors.go", "ProviderClass", "provider."},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), source.file, nil, 0)
		if err != nil {
			panic(err)
		}
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			var kind ast.Expr
			var values []ast.Expr
			for _, spec := range group.Specs {
				value := spec.(*ast.ValueSpec)
				if len(value.Values) > 0 {
					kind, values = value.Type, value.Values
				}
				if name, ok := kind.(*ast.Ident); !ok || name.Name != source.kind {
					continue
				}
				for _, expr := range values {
					literal, ok := expr.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						panic(fmt.Sprintf("%s must have a string literal value", source.kind))
					}
					code, err := strconv.Unquote(literal.Value)
					if err != nil {
						panic(err)
					}
					codes = append(codes, source.prefix+code)
				}
			}
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(codes); err != nil {
		panic(err)
	}
}
