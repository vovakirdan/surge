package mir

import (
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
	cond, err := l.lowerExpr(data.Cond, false)
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

	l.startBlock(thenBB)
	if data.Then != nil {
		if hasResult {
			op, err := l.lowerExpr(data.Then, true)
			if err != nil {
				return Operand{}, err
			}
			l.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: Place{Local: resultLocal},
					Src: RValue{Kind: RValueUse, Use: op},
				},
			})
		} else {
			if _, err := l.lowerExpr(data.Then, false); err != nil {
				return Operand{}, err
			}
		}
	} else if hasResult {
		l.setTerm(&Terminator{Kind: TermUnreachable})
	}
	if !l.curBlock().Terminated() {
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
	}

	l.startBlock(elseBB)
	if data.Else != nil {
		if hasResult {
			op, err := l.lowerExpr(data.Else, true)
			if err != nil {
				return Operand{}, err
			}
			l.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: Place{Local: resultLocal},
					Src: RValue{Kind: RValueUse, Use: op},
				},
			})
		} else {
			if _, err := l.lowerExpr(data.Else, false); err != nil {
				return Operand{}, err
			}
		}
	} else if hasResult {
		l.setTerm(&Terminator{Kind: TermUnreachable})
	}
	if !l.curBlock().Terminated() {
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
	}

	l.startBlock(joinBB)
	if !hasResult {
		return l.constNothing(e.Type), nil
	}
	return l.placeOperand(Place{Local: resultLocal}, e.Type, consume), nil
}

func (l *funcLowerer) lowerBlockExpr(e *hir.Expr, data hir.BlockExprData, consume bool) (Operand, error) {
	if l == nil || e == nil {
		return Operand{}, nil
	}

	hasResult := e.Type != types.NoTypeID && !l.isNothingType(e.Type)
	resultLocal := NoLocalID
	if hasResult {
		resultLocal = l.newTemp(e.Type, "block", e.Span)
	}

	exitBB := l.newBlock()
	l.returnStack = append(l.returnStack, returnCtx{
		exit:      exitBB,
		hasResult: hasResult,
		result:    Place{Local: resultLocal},
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
	return l.placeOperand(Place{Local: resultLocal}, e.Type, consume), nil
}
