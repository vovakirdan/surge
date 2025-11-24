package sema

import (
	"testing"

	"surge/internal/ast"
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

func TestMagicResultForArrayAdditionUsesOperandType(t *testing.T) {
	in := types.NewInterner()
	tc := &typeChecker{types: in}
	tc.ensureBuiltinMagic()

	intType := in.Builtins().Int
	dynamic := in.Intern(types.MakeArray(intType, types.ArrayDynamicLength))
	if res := tc.magicResultForBinary(dynamic, dynamic, ast.ExprBinaryAdd); res != dynamic {
		t.Fatalf("expected dynamic array result, got %v", res)
	}

	fixedTwo := in.Intern(types.MakeArray(intType, 2))
	if res := tc.magicResultForBinary(fixedTwo, fixedTwo, ast.ExprBinaryAdd); res != fixedTwo {
		t.Fatalf("expected fixed array result, got %v", res)
	}

	fixedThree := in.Intern(types.MakeArray(intType, 3))
	if res := tc.magicResultForBinary(fixedTwo, fixedThree, ast.ExprBinaryAdd); res != types.NoTypeID {
		t.Fatalf("expected mismatch for different fixed lengths, got %v", res)
	}
}
