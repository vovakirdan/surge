package mir

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestLowerLogicalShortCircuitExprUsesOperandTypeWithoutInterner(t *testing.T) {
	boolTy := types.NewInterner().Builtins().Bool
	leftSym := symbols.SymbolID(1)
	rightSym := symbols.SymbolID(2)
	l := newLogicalShortCircuitTestLowerer(nil)
	l.f.Locals = append(l.f.Locals,
		Local{Name: "left", Type: boolTy, Flags: LocalFlagCopy},
		Local{Name: "right", Type: boolTy, Flags: LocalFlagCopy},
	)
	l.symToLocal[leftSym] = 0
	l.symToLocal[rightSym] = 1

	op, err := l.lowerLogicalShortCircuitExpr(
		&hir.Expr{
			Kind: hir.ExprBinaryOp,
			Type: types.NoTypeID,
			Data: hir.BinaryOpData{
				Op: ast.ExprBinaryLogicalAnd,
				Left: &hir.Expr{
					Kind: hir.ExprVarRef,
					Type: types.NoTypeID,
					Data: hir.VarRefData{Name: "left", SymbolID: leftSym},
				},
				Right: &hir.Expr{
					Kind: hir.ExprVarRef,
					Type: types.NoTypeID,
					Data: hir.VarRefData{Name: "right", SymbolID: rightSym},
				},
			},
		},
		hir.BinaryOpData{
			Op: ast.ExprBinaryLogicalAnd,
			Left: &hir.Expr{
				Kind: hir.ExprVarRef,
				Type: types.NoTypeID,
				Data: hir.VarRefData{Name: "left", SymbolID: leftSym},
			},
			Right: &hir.Expr{
				Kind: hir.ExprVarRef,
				Type: types.NoTypeID,
				Data: hir.VarRefData{Name: "right", SymbolID: rightSym},
			},
		},
		false,
	)
	if err != nil {
		t.Fatalf("lowerLogicalShortCircuitExpr failed: %v", err)
	}
	if op.Type != boolTy {
		t.Fatalf("result type = %v, want %v", op.Type, boolTy)
	}
	if got := l.f.Locals[2].Type; got != boolTy {
		t.Fatalf("short-circuit temp type = %v, want %v", got, boolTy)
	}
}

func TestLowerLogicalShortCircuitExprRejectsMissingResultType(t *testing.T) {
	l := newLogicalShortCircuitTestLowerer(nil)
	expr := &hir.Expr{
		Kind: hir.ExprBinaryOp,
		Type: types.NoTypeID,
		Data: hir.BinaryOpData{
			Op:    ast.ExprBinaryLogicalOr,
			Left:  boolLiteral(types.NoTypeID, false),
			Right: boolLiteral(types.NoTypeID, true),
		},
	}

	_, err := l.lowerLogicalShortCircuitExpr(expr, expr.Data.(hir.BinaryOpData), false)
	if err == nil {
		t.Fatal("expected missing result type error")
	}
	if !strings.Contains(err.Error(), "unable to resolve result type") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(l.f.Locals) != 0 {
		t.Fatalf("created %d locals before resolving result type", len(l.f.Locals))
	}
}

func newLogicalShortCircuitTestLowerer(typesIn *types.Interner) *funcLowerer {
	l := &funcLowerer{
		types:      typesIn,
		symToLocal: make(map[symbols.SymbolID]LocalID),
		nextTemp:   1,
	}
	l.f = &Func{Name: "test"}
	entry := l.newBlock()
	l.f.Entry = entry
	l.cur = entry
	return l
}

func boolLiteral(ty types.TypeID, value bool) *hir.Expr {
	return &hir.Expr{
		Kind: hir.ExprLiteral,
		Type: ty,
		Data: hir.LiteralData{Kind: hir.LiteralBool, BoolValue: value},
	}
}
