package mir

import (
	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

// lowerAssignExpr lowers an assignment expression.
func (l *funcLowerer) lowerAssignExpr(e *hir.Expr, data hir.BinaryOpData, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}
	dst, err := l.lowerPlace(data.Left)
	if err != nil {
		return Operand{}, err
	}
	expected := l.exprType(data.Left)
	if data.Left != nil && data.Left.Kind == hir.ExprIndex {
		expected = l.unwrapReferenceType(expected)
	}
	rhs, err := l.lowerExprForType(data.Right, expected)
	if err != nil {
		return Operand{}, err
	}
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: dst,
			Src: RValue{Kind: RValueUse, Use: rhs},
		},
	})

	resultTy := e.Type
	if resultTy == types.NoTypeID {
		if leftTy := l.exprType(data.Left); leftTy != types.NoTypeID {
			resultTy = leftTy
		} else {
			resultTy = rhs.Type
		}
	}
	return l.placeOperand(dst, resultTy, consume), nil
}

// lowerCompoundAssignExpr lowers a compound assignment expression (+=, -=, etc.).
func (l *funcLowerer) lowerCompoundAssignExpr(e *hir.Expr, data hir.BinaryOpData, base ast.ExprBinaryOp, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}
	dst, err := l.lowerPlace(data.Left)
	if err != nil {
		return Operand{}, err
	}

	resultTy := e.Type
	if resultTy == types.NoTypeID {
		resultTy = l.exprType(data.Left)
	}
	if data.Left != nil && data.Left.Kind == hir.ExprIndex {
		resultTy = l.unwrapReferenceType(resultTy)
	}

	left := l.placeOperand(dst, resultTy, false)
	right, err := l.lowerExprForType(data.Right, resultTy)
	if err != nil {
		return Operand{}, err
	}

	tmp := l.newTemp(resultTy, "cassign", e.Span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueBinaryOp, Binary: BinaryOp{Op: base, Left: left, Right: right}},
		},
	})
	tmpOp := l.placeOperand(Place{Local: tmp}, resultTy, true)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: dst,
			Src: RValue{Kind: RValueUse, Use: tmpOp},
		},
	})

	return l.placeOperand(dst, resultTy, consume), nil
}

// assignmentBaseOp returns the base binary operator for a compound assignment.
func assignmentBaseOp(op ast.ExprBinaryOp) (ast.ExprBinaryOp, bool) {
	switch op {
	case ast.ExprBinaryAddAssign:
		return ast.ExprBinaryAdd, true
	case ast.ExprBinarySubAssign:
		return ast.ExprBinarySub, true
	case ast.ExprBinaryMulAssign:
		return ast.ExprBinaryMul, true
	case ast.ExprBinaryDivAssign:
		return ast.ExprBinaryDiv, true
	case ast.ExprBinaryModAssign:
		return ast.ExprBinaryMod, true
	case ast.ExprBinaryBitAndAssign:
		return ast.ExprBinaryBitAnd, true
	case ast.ExprBinaryBitOrAssign:
		return ast.ExprBinaryBitOr, true
	case ast.ExprBinaryBitXorAssign:
		return ast.ExprBinaryBitXor, true
	case ast.ExprBinaryShlAssign:
		return ast.ExprBinaryShiftLeft, true
	case ast.ExprBinaryShrAssign:
		return ast.ExprBinaryShiftRight, true
	default:
		return 0, false
	}
}
