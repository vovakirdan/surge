package types //nolint:revive

import (
	"testing"

	"surge/internal/ast"
)

func TestBinarySpecsLogicalAnd(t *testing.T) {
	specs := BinarySpecs(ast.ExprBinaryLogicalAnd)
	if len(specs) != 1 {
		t.Fatalf("expected single spec for logical and")
	}
	spec := specs[0]
	if spec.Left&FamilyBool == 0 || spec.Right&FamilyBool == 0 {
		t.Fatalf("logical and expects bool operands, got %+v", spec)
	}
	if spec.Result != BinaryResultBool {
		t.Fatalf("expected bool result, got %+v", spec)
	}
}

func TestBinarySpecsNullCoalescing(t *testing.T) {
	specs := BinarySpecs(ast.ExprBinaryNullCoalescing)
	if len(specs) == 0 {
		t.Fatalf("expected specs for null coalescing")
	}
}
