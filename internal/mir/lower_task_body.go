package mir

import (
	"fmt"

	"surge/internal/hir"
	"surge/internal/types"
)

// lowerTaskBody lowers the statements of an `async { }` / `blocking { }`
// body function under a context that says whose exit a `ret` is: the
// function's own. Sema types the body as a value-producing block (the same
// return-context machinery an `on` body uses), so its `ret` sites are what
// `return` sites were before the owner's 2026-08-26 ruling made `ret` the
// body's exit — and they lower to the very same terminator, keeping the
// early-versus-tail distinction the async scope join reads.
func (l *funcLowerer) lowerTaskBody(body *hir.Block) error {
	if l == nil || body == nil {
		return nil
	}
	l.returnStack = append(l.returnStack, returnCtx{taskBody: true})
	err := l.lowerBlock(body)
	l.returnStack = l.returnStack[:len(l.returnStack)-1]
	return err
}

// lowerRetStmt lowers a `ret`. Inside an async/blocking body it is the body
// function's return and takes that path whole — value, exit drops, temp
// flush and the Early flag. Inside a block expression it stores the value
// into the block's result slot and jumps to the block's exit.
func (l *funcLowerer) lowerRetStmt(st *hir.Stmt, data hir.RetData) error {
	if len(l.returnStack) == 0 {
		return fmt.Errorf("mir: ret outside of a block-return context")
	}
	ctx := l.returnStack[len(l.returnStack)-1]
	if ctx.taskBody {
		return l.lowerStmt(&hir.Stmt{
			Kind: hir.StmtReturn,
			Span: st.Span,
			Data: hir.ReturnData{
				Value:           data.Value,
				IsTail:          data.IsTail,
				DropsAfterValue: data.DropsAfterValue,
			},
		})
	}
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
		// A ret that hands ON one of the bindings this exit would drop must
		// not also free it: the result slot is the new owner.
		//
		// Guarded on there BEING exit drops. detachFromExitDrops also fires
		// on pending temps alone, and every block-expression `ret` — compare
		// arms, value blocks — reaches here with temps pending and no exit
		// drops. Those already hand their result over safely (the flush
		// below excludes the result local), so materializing a transfer temp
		// for them would be an unrequested change to a working path.
		if len(data.DropsAfterValue) > 0 {
			op = l.detachFromExitDrops(&op, data.DropsAfterValue, st.Span)
		}
		l.emit(&Instr{
			Kind: InstrAssign,
			Assign: AssignInstr{
				Dst: ctx.result,
				Src: RValue{Kind: RValueUse, Use: op},
			},
		})
	} else if data.Value != nil {
		if err := l.lowerExprForSideEffects(data.Value); err != nil {
			return err
		}
	}
	// The regions between here and the block's exit are skipped by the
	// goto below and never run their own flush, so their temps are freed
	// on this edge.
	l.flushTempDropsForRet(ctx.tempFrameDepth, ctx.result.Local)
	// Same contract the explicit-return path honours: what this exit still
	// owns is released here, after the value has been read into the result
	// slot. The goto skips the block's normal end, so these obligations
	// have nowhere else to run — a crossing body reaches its exit through
	// `ret` and nothing else.
	l.emitExitDrops(data.DropsAfterValue)
	l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: ctx.exit}})
	return nil
}
