package mir

import (
	"surge/internal/hir"
	"surge/internal/source"
)

// Statement-end temporaries: sema flags owned evaluations nothing
// consumes; HIR wraps them in ExprOwnedTemp; here each wrapped value
// materializes into a fresh temp local registered in the innermost temp
// frame. Frames open and flush strictly inside single-entry evaluation
// regions (statement, loop/if condition, short-circuit RHS, arm bodies),
// so every emitted drop is dominated by its materialization — no
// active-bit tracking, and the VM's uninitialized-slot check can never
// fire on a skipped path.
//
// INVARIANT this file leans on: expressions cannot exit their region
// early. return/break/continue are STATEMENTS, there is no try/`?`
// propagation operator, and panic is process exit without unwinding —
// so the only flush edges are normal completion and the return
// statement's explicit flushTempDropsForExit. If a future surface adds
// an expression-position early exit (a try operator), every frame open
// at that exit must flush on its edge too, or skipped flushes become
// leaks and — under unwinding — double frees.

// hasPendingTempDrops reports whether any open frame holds temps: a
// return operand projecting into one must detach before the exit flush.
func (l *funcLowerer) hasPendingTempDrops() bool {
	for _, frame := range l.tempDropFrames {
		if len(frame) > 0 {
			return true
		}
	}
	return false
}

func (l *funcLowerer) pushTempDropFrame() {
	l.tempDropFrames = append(l.tempDropFrames, nil)
}

// flushTempDropFrame pops the innermost frame, freeing its temps in
// reverse materialization order. Emission is skipped on terminated
// blocks (a return inside the region already flushed through
// flushTempDropsForExit).
func (l *funcLowerer) flushTempDropFrame() {
	if len(l.tempDropFrames) == 0 {
		return
	}
	frame := l.tempDropFrames[len(l.tempDropFrames)-1]
	l.tempDropFrames = l.tempDropFrames[:len(l.tempDropFrames)-1]
	if l.curBlock().Terminated() {
		return
	}
	for i := len(frame) - 1; i >= 0; i-- {
		l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: Place{Local: frame[i]}}})
	}
}

// flushTempDropsForExit frees every open frame's temps (innermost first)
// WITHOUT popping: a return exits all open regions at once, and the
// frames unwind naturally as lowering returns through them (their own
// flushes then see a terminated block and emit nothing).
func (l *funcLowerer) flushTempDropsForExit() {
	for f := len(l.tempDropFrames) - 1; f >= 0; f-- {
		frame := l.tempDropFrames[f]
		for i := len(frame) - 1; i >= 0; i-- {
			l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: Place{Local: frame[i]}}})
		}
	}
}

// flushTempDropsForRet frees the temps of every region a `ret` skips on its
// way to the enclosing block expression's exit — the frames opened at or above
// `depth`, which is where that block expression began.
//
// It stops at `depth` rather than walking to the bottom the way
// flushTempDropsForExit does. A frame opened BEFORE the block expression can
// hold a temp that this path never materialized (the block's own result slot
// is created that way), and releasing one reads an uninitialized word. The
// frames above the depth are entirely inside the block, so every temp in them
// is dominated by this `ret`.
//
// `keep` is the result slot: the value this exit delivers, not one it ends.
func (l *funcLowerer) flushTempDropsForRet(depth int, keep LocalID) {
	if depth < 0 {
		depth = 0
	}
	for f := len(l.tempDropFrames) - 1; f >= depth; f-- {
		frame := l.tempDropFrames[f]
		for i := len(frame) - 1; i >= 0; i-- {
			if frame[i] == keep {
				continue
			}
			l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: Place{Local: frame[i]}}})
		}
	}
}

// lowerOwnedTempExpr materializes the wrapped evaluation and registers
// it for its region's flush.
func (l *funcLowerer) lowerOwnedTempExpr(e *hir.Expr, data hir.OwnedTempData, span source.Span) (Operand, error) {
	inner, err := l.lowerExprForType(data.Inner, e.Type)
	if err != nil {
		return Operand{}, err
	}
	tmp := l.newTemp(e.Type, "owned_temp", span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUse, Use: inner},
		},
	})
	if len(l.tempDropFrames) > 0 {
		top := len(l.tempDropFrames) - 1
		l.tempDropFrames[top] = append(l.tempDropFrames[top], tmp)
	}
	return l.placeOperand(Place{Local: tmp}, e.Type, false), nil
}
