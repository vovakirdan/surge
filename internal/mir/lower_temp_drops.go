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
