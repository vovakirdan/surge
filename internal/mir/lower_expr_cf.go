package mir

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/hir"
	"surge/internal/types"
)

func (l *funcLowerer) exprType(e *hir.Expr) types.TypeID {
	if l == nil || e == nil {
		return types.NoTypeID
	}
	if e.Type != types.NoTypeID {
		return e.Type
	}
	if e.Kind != hir.ExprVarRef {
		return types.NoTypeID
	}
	vr, ok := e.Data.(hir.VarRefData)
	if !ok || !vr.SymbolID.IsValid() {
		return types.NoTypeID
	}
	local, ok := l.symToLocal[vr.SymbolID]
	if !ok || l.f == nil {
		return types.NoTypeID
	}
	idx := int(local)
	if idx < 0 || idx >= len(l.f.Locals) {
		return types.NoTypeID
	}
	return l.f.Locals[idx].Type
}

func (l *funcLowerer) lowerIfExpr(e *hir.Expr, data hir.IfData, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}
	cond, err := l.lowerValueExpr(data.Cond, false)
	if err != nil {
		return Operand{}, err
	}

	hasResult := e.Type != types.NoTypeID && !l.isNothingType(e.Type)
	resultLocal := NoLocalID
	if hasResult {
		resultLocal = l.newTemp(e.Type, "if", e.Span)
	}

	thenBB := l.newBlock()
	elseBB := l.newBlock()
	joinBB := l.newBlock()

	l.setTerm(&Terminator{Kind: TermIf, If: IfTerm{Cond: cond, Then: thenBB, Else: elseBB}})

	// A guarded release asks the branch that RAN whether what reaches the join
	// is this expression's to free.
	//
	// The guard stays live across the branches on purpose. A branch that is
	// itself a choice hands its value UP to this join, so whoever owns the
	// result owns what the inner built — and the inner's own minting branches
	// are the ones that know it happened, so they raise this guard. Giving the
	// inner a release of its own instead frees at the end of THIS branch,
	// before the join copies the result: a read of freed storage, measured.
	//
	// A nested choice that owns its value independently does not reach here
	// with this guard: `lowerOwnedTempExpr` sets its own before lowering it and
	// restores after.
	releaseGuard := l.pendingReleaseGuard

	l.startBlock(thenBB)
	l.pushTempDropFrame()
	if data.Then != nil {
		if hasResult {
			op, err := l.lowerExpr(data.Then, true)
			if err != nil {
				return Operand{}, err
			}
			if releaseGuard != NoLocalID && data.ThenMintsValue {
				l.emitBoolConst(releaseGuard, true)
			}
			l.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: Place{Local: resultLocal},
					Src: RValue{Kind: RValueUse, Use: op},
				},
			})
		} else {
			if err := l.lowerExprForSideEffects(data.Then); err != nil {
				return Operand{}, err
			}
		}
	} else if hasResult {
		l.setTerm(&Terminator{Kind: TermUnreachable})
	}
	l.flushTempDropFrame()
	if !l.curBlock().Terminated() {
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
	}

	l.startBlock(elseBB)
	l.pushTempDropFrame()
	if data.Else != nil {
		if hasResult {
			op, err := l.lowerExpr(data.Else, true)
			if err != nil {
				return Operand{}, err
			}
			if releaseGuard != NoLocalID && data.ElseMintsValue {
				l.emitBoolConst(releaseGuard, true)
			}
			l.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: Place{Local: resultLocal},
					Src: RValue{Kind: RValueUse, Use: op},
				},
			})
		} else {
			if err := l.lowerExprForSideEffects(data.Else); err != nil {
				return Operand{}, err
			}
		}
	} else if hasResult {
		l.setTerm(&Terminator{Kind: TermUnreachable})
	}
	l.flushTempDropFrame()
	if !l.curBlock().Terminated() {
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
	}

	l.startBlock(joinBB)
	if !hasResult {
		return l.constNothing(e.Type), nil
	}
	return l.placeOperand(Place{Local: resultLocal}, e.Type, consume), nil
}

func (l *funcLowerer) lowerLogicalShortCircuitExpr(e *hir.Expr, data hir.BinaryOpData, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}
	if !isLogicalShortCircuitOp(data.Op) {
		return Operand{}, fmt.Errorf("mir: logical short-circuit: unsupported op %s", data.Op)
	}
	resultTy, typeErr := l.logicalShortCircuitResultType(e, data, types.NoTypeID)
	left := Operand{}
	leftReady := false
	if typeErr != nil {
		var err error
		left, err = l.lowerValueExpr(data.Left, false)
		if err != nil {
			return Operand{}, err
		}
		leftReady = true
		resultTy, typeErr = l.logicalShortCircuitResultType(e, data, left.Type)
		if typeErr != nil {
			return Operand{}, typeErr
		}
	}
	resultLocal := l.newTemp(resultTy, "logic", e.Span)

	if !leftReady {
		var err error
		left, err = l.lowerValueExpr(data.Left, false)
		if err != nil {
			return Operand{}, err
		}
	}

	rhsBB := l.newBlock()
	shortBB := l.newBlock()
	joinBB := l.newBlock()

	thenBB := rhsBB
	elseBB := shortBB
	shortValue := false
	if data.Op == ast.ExprBinaryLogicalOr {
		thenBB = shortBB
		elseBB = rhsBB
		shortValue = true
	}
	l.setTerm(&Terminator{Kind: TermIf, If: IfTerm{Cond: left, Then: thenBB, Else: elseBB}})

	l.startBlock(shortBB)
	l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: resultLocal},
		Src: RValue{Kind: RValueUse, Use: Operand{Kind: OperandConst, Type: resultTy, Const: Const{
			Kind:      ConstBool,
			Type:      resultTy,
			BoolValue: shortValue,
		}}},
	}})
	l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})

	l.startBlock(rhsBB)
	// The RHS runs conditionally: its temporaries flush inside this
	// block so the skipped path never sees them.
	l.pushTempDropFrame()
	right, err := l.lowerValueExpr(data.Right, false)
	if err != nil {
		return Operand{}, err
	}
	l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: resultLocal},
		Src: RValue{Kind: RValueUse, Use: right},
	}})
	l.flushTempDropFrame()
	if !l.curBlock().Terminated() {
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
	}

	l.startBlock(joinBB)
	return l.placeOperand(Place{Local: resultLocal}, resultTy, consume), nil
}

func isLogicalShortCircuitOp(op ast.ExprBinaryOp) bool {
	return op == ast.ExprBinaryLogicalAnd || op == ast.ExprBinaryLogicalOr
}

func (l *funcLowerer) logicalShortCircuitResultType(e *hir.Expr, data hir.BinaryOpData, leftTy types.TypeID) (types.TypeID, error) {
	if e != nil && e.Type != types.NoTypeID {
		return e.Type, nil
	}
	if l != nil && l.types != nil {
		return l.types.Builtins().Bool, nil
	}
	if leftTy != types.NoTypeID {
		return leftTy, nil
	}
	if data.Left != nil && data.Left.Type != types.NoTypeID {
		return data.Left.Type, nil
	}
	if data.Right != nil && data.Right.Type != types.NoTypeID {
		return data.Right.Type, nil
	}
	return types.NoTypeID, fmt.Errorf("mir: logical short-circuit: unable to resolve result type")
}

func (l *funcLowerer) lowerBlockExpr(e *hir.Expr, data hir.BlockExprData, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}

	hasResult := e.Type != types.NoTypeID && !l.isNothingType(e.Type)
	resultLocal := NoLocalID
	if hasResult {
		// The result slot is a TRANSFER: `ret` stores a reference into it and
		// the consumer takes that reference, so it must not also be released
		// here — see the read at the end of this function.
		resultLocal = l.markOwningTemp(l.newTransferTemp(e.Type, "block", e.Span))
	}

	exitBB := l.newBlock()
	l.returnStack = append(l.returnStack, returnCtx{
		exit:           exitBB,
		hasResult:      hasResult,
		result:         Place{Local: resultLocal},
		tempFrameDepth: len(l.tempDropFrames),
	})
	if err := l.lowerBlock(data.Block); err != nil {
		return Operand{}, err
	}
	l.returnStack = l.returnStack[:len(l.returnStack)-1]

	// If we fall off the end of a non-nothing block expression, treat it as unreachable.
	if !l.curBlock().Terminated() {
		if hasResult {
			l.setTerm(&Terminator{Kind: TermUnreachable})
		} else {
			l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: exitBB}})
		}
	}

	l.startBlock(exitBB)
	if !hasResult {
		return l.constNothing(e.Type), nil
	}
	// Reading the transfer slot must not retain again: `ret` already gave it
	// the reference the consumer receives. A reference-counted value is Copy
	// at the language level, but this synthesized slot is spent here just like
	// every other transfer temp, so its MIR read is an explicit move.
	if l.isRefCounted(e.Type) {
		return Operand{Kind: OperandMove, Type: e.Type, Place: Place{Local: resultLocal}}, nil
	}
	return l.placeOperand(Place{Local: resultLocal}, e.Type, consume), nil
}
