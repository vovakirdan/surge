package mir

import (
	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/source"
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
	if data.DropOverwritten {
		// The approved reassignment order: RHS fully evaluated, THEN the
		// overwritten value frees, then the store lands.
		//
		// "Fully evaluated" is the load-bearing half, and an operand alone does
		// not satisfy it: an operand naming a PLACE is a lazy read, performed
		// by the store below rather than by the call above. When the source
		// place is the destination — `x = x`, `p.inner = p.inner` — the drop
		// would then free the very storage the store is about to read. Pin the
		// value into a temp first, so the drop frees what the temp no longer
		// needs to look at.
		rhs = l.materializeBeforeOverwrite(&rhs, data.Right)
		l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: dst}})
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
	if data.DropOverwritten {
		// HERE and nowhere else. The compound form READS its target before it
		// writes it, and for a string the backend hands the callee the ADDRESS
		// OF THE SLOT rather than a loaded handle - so a drop placed before the
		// operation above is not an ordering nit, it is a use-after-free the
		// runtime performs itself. After the operation the old value is dead:
		// the result is in `tmp`, which no longer names `dst`.
		//
		// `materializeBeforeOverwrite` is what the plain `=` needs and this does
		// not: there the store's source can BE the destination (`x = x`), while
		// here it is always a fresh temp, so there is nothing left to pin.
		l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: dst}})
	}
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

// materializeBeforeOverwrite pins a place-reading operand into a temp so the
// overwrite's drop cannot free storage the pending store still has to read.
//
// It fires only when the operand actually reads a place and the value carries a
// drop obligation — a constant or a plain scalar has nothing to lose by the
// drop, and forcing every reassignment through a temp would add a copy to code
// that never needed one.
func (l *funcLowerer) materializeBeforeOverwrite(rhs *Operand, src *hir.Expr) Operand {
	if l == nil {
		return *rhs
	}
	switch rhs.Kind {
	case OperandCopy, OperandCopyValue, OperandMove, OperandRetain:
	default:
		return *rhs
	}
	if rhs.Type == types.NoTypeID || !l.ownsHeap(rhs.Type) {
		return *rhs
	}
	span := source.Span{}
	if src != nil {
		span = src.Span
	}
	// A TRANSFER temp: its value leaves in the store below, so it must not
	// also be registered for the region flush. A refcounted scalar would
	// otherwise be released twice — moved out and swept up — which reads back
	// as a use-after-free rather than a leak.
	tmp := l.newTransferTemp(rhs.Type, "reassign", span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUse, Use: *rhs},
		},
	})
	// The temp now holds the value outright, so handing it on transfers rather
	// than duplicating: it is exactly the spent-temp case placeOperand names.
	return Operand{Kind: OperandMove, Type: rhs.Type, Place: Place{Local: tmp}}
}
