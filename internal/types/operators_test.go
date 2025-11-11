package types

import (
	"testing"

	"surge/internal/ast"
)

func TestBinarySpecsAdditionCoversNumericAndString(t *testing.T) {
	specs := BinarySpecs(ast.ExprBinaryAdd)
	if len(specs) == 0 {
		t.Fatalf("expected specs for addition")
	}
	var seenNumeric, seenString bool
	for _, spec := range specs {
		if spec.Left&FamilyNumeric != 0 && spec.Right&FamilyNumeric != 0 && spec.Result == BinaryResultNumeric {
			seenNumeric = true
		}
		if spec.Left&FamilyString != 0 && spec.Right&FamilyString != 0 && spec.Flags&BinaryFlagSameFamily != 0 {
			seenString = true
		}
	}
	if !seenNumeric {
		t.Fatalf("numeric addition rule missing")
	}
	if !seenString {
		t.Fatalf("string addition rule missing")
	}
}

func TestUnarySpecLookup(t *testing.T) {
	spec, ok := UnarySpecFor(ast.ExprUnaryMinus)
	if !ok {
		t.Fatalf("missing unary spec for minus")
	}
	if spec.Operand&FamilyNumeric == 0 || spec.Result != UnaryResultNumeric {
		t.Fatalf("unexpected spec %+v", spec)
	}
}

func TestAssignmentFlagPresent(t *testing.T) {
	specs := BinarySpecs(ast.ExprBinaryAddAssign)
	if len(specs) != 1 {
		t.Fatalf("expected single spec for add-assign")
	}
	if specs[0].Flags&BinaryFlagAssignment == 0 {
		t.Fatalf("assignment flag missing")
	}
}
