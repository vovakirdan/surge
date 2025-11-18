package sema

import (
	"testing"

	"surge/internal/ast"
	"surge/internal/types"
)

func TestBuiltinOptionReusesInstantiation(t *testing.T) {
	tc := typeChecker{
		builder:            ast.NewBuilder(ast.Hints{}, nil),
		types:              types.NewInterner(),
		typeInstantiations: make(map[string]types.TypeID),
	}

	first := tc.makeOptionType(tc.types.Builtins().Int)
	second := tc.makeOptionType(tc.types.Builtins().Int)

	if first == types.NoTypeID || second == types.NoTypeID {
		t.Fatalf("makeOptionType returned invalid TypeID: %d, %d", first, second)
	}
	if first != second {
		t.Fatalf("expected Option<int> instantiation to be reused, got %d and %d", first, second)
	}
}

func TestBuiltinResultReusesInstantiation(t *testing.T) {
	tc := typeChecker{
		builder:            ast.NewBuilder(ast.Hints{}, nil),
		types:              types.NewInterner(),
		typeInstantiations: make(map[string]types.TypeID),
	}

	first := tc.makeResultType(tc.types.Builtins().Int, tc.types.Builtins().String)
	second := tc.makeResultType(tc.types.Builtins().Int, tc.types.Builtins().String)

	if first == types.NoTypeID || second == types.NoTypeID {
		t.Fatalf("makeResultType returned invalid TypeID: %d, %d", first, second)
	}
	if first != second {
		t.Fatalf("expected Result<int, string> instantiation to be reused, got %d and %d", first, second)
	}
}
