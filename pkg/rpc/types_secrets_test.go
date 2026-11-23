package rpc

import (
	"reflect"
	"testing"

	"github.com/LuisPalacios/gitbox/internal/secretcheck"
	"github.com/LuisPalacios/gitbox/pkg/usererr"
)

// TestRPCTypesCarryNoSecrets is the structural canary of #163 over the
// protocol's own types (#242): every frame, handshake and error shape, and
// the outcomes the server remembers for a replay. A field named like a
// secret fails until it is allowlisted with its exact type and JSON name.
func TestRPCTypesCarryNoSecrets(t *testing.T) {
	var c secretcheck.Checker
	for _, typ := range []reflect.Type{
		reflect.TypeFor[message](), reflect.TypeFor[messageMeta](), reflect.TypeFor[wireError](),
		reflect.TypeFor[Error](), reflect.TypeFor[usererr.UserError](),
		reflect.TypeFor[Hello](), reflect.TypeFor[clientHello](), reflect.TypeFor[verdict](),
		reflect.TypeFor[remembered](),
	} {
		c.Inspect(typ, func(field string) { t.Errorf("secret-shaped protocol field: %s", field) })
	}
}
