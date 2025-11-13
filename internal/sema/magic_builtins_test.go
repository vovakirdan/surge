package sema

import (
	"testing"

	"surge/internal/symbols"
	"surge/internal/types"
)

func TestEnsureBuiltinMagicStringMul(t *testing.T) {
	tc := &typeChecker{
		types: types.NewInterner(),
	}
	tc.ensureBuiltinMagic()
	if tc.magic == nil {
		t.Fatalf("magic map not initialized")
	}
	methods := tc.lookupMagicMethods(symbols.TypeKey("string"), "__mul")
	if len(methods) == 0 {
		t.Fatalf("expected string __mul signature")
	}
	found := false
	for _, sig := range methods {
		if len(sig.Params) == 2 && sig.Params[0] == "string" && sig.Params[1] == "int" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("string __mul<int> signature missing: %+v", methods)
	}
}
