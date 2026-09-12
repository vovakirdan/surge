package mir

import (
	"surge/internal/hir"
	"surge/internal/types"
)

type loopCtx struct {
	breakTarget    BlockID
	continueTarget BlockID
	tempFrameDepth int
}

func (l *funcLowerer) lowerBlock(b *hir.Block) error {
	if l == nil || b == nil {
		return nil
	}
	l.pushTempDropFrame()
	defer l.flushTempDropFrame()
	for i := range b.Stmts {
		if l.curBlock().Terminated() {
			return nil
		}
		l.pushTempDropFrame()
		err := l.lowerStmt(&b.Stmts[i])
		l.flushTempDropFrame()
		if err != nil {
			return err
		}
	}
	return nil
}

func (l *funcLowerer) lowerReturnStmt(st *hir.Stmt, data hir.ReturnData) error {
	early := !data.IsTail
	if len(l.returnStack) > 0 && data.IsImplicit {
		ctx := l.returnStack[len(l.returnStack)-1]
		if ctx.hasResult && data.Value != nil {
			expected := types.NoTypeID
			if l.f != nil && ctx.result.Local != NoLocalID {
				idx := int(ctx.result.Local)
				if idx >= 0 && idx < len(l.f.Locals) {
					expected = l.f.Locals[idx].Type
				}
			}
			op, err := l.lowerExprForType(data.Value, expected)
			if err != nil {
				return err
			}
			op = l.detachFromExitDrops(&op, data.DropsAfterValue, st.Span)
			l.emit(&Instr{
				Kind: InstrAssign,
				Assign: AssignInstr{
					Dst: ctx.result,
					Src: RValue{Kind: RValueUse, Use: op},
				},
			})
		} else if data.Value != nil {
			// Still lower for side effects.
			if err := l.lowerExprForSideEffects(data.Value); err != nil {
				return err
			}
		}

		l.flushTempDropsForRet(ctx.tempFrameDepth, ctx.result.Local)
		// Same contract the explicit-return path honours: these free AFTER
		// the value evaluated (it may read them) and before the terminator.
		// This path carried them unemitted, so a binding a compare arm
		// introduced was never released.
		l.emitExitDrops(data.DropsAfterValue)
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: ctx.exit}})
		return nil
	}

	if l.f != nil && l.isNothingType(l.f.Result) {
		if data.Value != nil {
			if err := l.lowerExprForSideEffects(data.Value); err != nil {
				return err
			}
		}
		l.flushTempDropsForExit()
		l.emitExitDrops(data.DropsAfterValue)
		l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{Early: early}})
		return nil
	}

	if data.Value == nil {
		l.flushTempDropsForExit()
		l.emitExitDrops(data.DropsAfterValue)
		l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{Early: early}})
		return nil
	}
	expected := types.NoTypeID
	if l.f != nil {
		expected = l.f.Result
	}
	op, err := l.lowerExprForType(data.Value, expected)
	if err != nil {
		return err
	}
	op = l.detachFromExitDrops(&op, data.DropsAfterValue, st.Span)
	l.flushTempDropsForExit()
	l.emitExitDrops(data.DropsAfterValue)
	l.setTerm(&Terminator{Kind: TermReturn, Return: ReturnTerm{HasValue: true, Value: op, Early: early}})
	return nil
}

func (l *funcLowerer) lowerWhileStmt(data hir.WhileData) error {
	headerBB := l.newBlock()
	bodyBB := l.newBlock()
	exitBB := l.newBlock()
	latchBB := headerBB
	if data.Post != nil {
		latchBB = l.newBlock()
	}

	l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: headerBB}})

	l.startBlock(headerBB)
	l.pushTempDropFrame()
	condOp, err := l.lowerValueExpr(data.Cond, false)
	if err != nil {
		return err
	}
	l.flushTempDropFrame()
	l.setTerm(&Terminator{
		Kind: TermIf,
		If: IfTerm{
			Cond: condOp,
			Then: bodyBB,
			Else: exitBB,
		},
	})

	l.startBlock(bodyBB)
	l.loopStack = append(l.loopStack, loopCtx{
		breakTarget: exitBB, continueTarget: latchBB,
		tempFrameDepth: len(l.tempDropFrames),
	})
	if err := l.lowerBlock(data.Body); err != nil {
		return err
	}
	l.loopStack = l.loopStack[:len(l.loopStack)-1]
	if !l.curBlock().Terminated() {
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: latchBB}})
	}
	if data.Post != nil {
		l.startBlock(latchBB)
		l.pushTempDropFrame()
		err := l.lowerExprForSideEffects(l.numericLoopPost(data.Post))
		l.flushTempDropFrame()
		if err != nil {
			return err
		}
		if !l.curBlock().Terminated() {
			l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: headerBB}})
		}
	}

	l.startBlock(exitBB)
	return nil
}
