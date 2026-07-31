package mir

import (
	"surge/internal/hir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// tempDropEntry is one statement-end temporary and the plan that reclaims it.
// A nil plan is the whole release, which is every temporary nothing was taken
// out of — that is, all of them until a field could move on its own.
type tempDropEntry struct {
	local LocalID
	steps []sema.DropStep
	// guarded says the release fires only where `guard` is raised: a bool local
	// the producing paths set, assigned BEFORE the branch that may raise it, so
	// the read is dominated exactly like the value's own. See
	// hir.OwnedTempData.Guarded.
	//
	// It is a separate bool rather than a NoLocalID sentinel in `guard` because
	// NoLocalID is -1 while a struct literal's zero value is 0 — a REAL local.
	// An entry built without naming the field would otherwise read local 0 as
	// its guard, which is how this first shipped: a bool store into a string
	// slot, and an `if` on a pointer.
	guarded bool
	guard   LocalID
}

// emitTempDrop releases one temporary, narrowed to its remainder when part of
// it was taken. Whoever took a field owns that field now, so releasing it here
// would free storage this value no longer holds.
func (l *funcLowerer) emitTempDrop(entry tempDropEntry) {
	if entry.guarded {
		l.emitGuardedTempDrop(entry)
		return
	}
	if len(entry.steps) > 0 {
		l.emitResidualDropAt(Place{Local: entry.local}, entry.steps)
		return
	}
	l.emit(&Instr{Kind: InstrDrop, Drop: DropInstr{Place: Place{Local: entry.local}}})
}

// boolType is the interner's bool, for the guard locals this file mints.
func (l *funcLowerer) boolType() types.TypeID {
	if l == nil || l.types == nil {
		return types.NoTypeID
	}
	return l.types.Builtins().Bool
}

// emitBoolConst assigns a literal bool to a local.
func (l *funcLowerer) emitBoolConst(local LocalID, value bool) {
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: local},
			Src: RValue{Kind: RValueUse, Use: Operand{
				Kind:  OperandConst,
				Type:  l.boolType(),
				Const: Const{Kind: ConstBool, Type: l.boolType(), BoolValue: value},
			}},
		},
	})
}

// emitGuardedTempDrop releases a temporary only on the paths that produced it.
//
// The guard is a plain bool the producing branches raise, rather than a null
// value in the slot: the VM refuses to drop an uninitialized slot, so a
// sentinel would need an empty representative for every droppable type, while a
// bool needs nothing new. The temporary itself is assigned on every path — it
// is the expression's own result — so only WHO OWNS it varies, which is exactly
// what the guard records.
func (l *funcLowerer) emitGuardedTempDrop(entry tempDropEntry) {
	dropBB := l.newBlock()
	joinBB := l.newBlock()
	l.setTerm(&Terminator{Kind: TermIf, If: IfTerm{
		Cond: Operand{Kind: OperandCopy, Type: l.boolType(), Place: Place{Local: entry.guard}},
		Then: dropBB,
		Else: joinBB,
	}})

	l.startBlock(dropBB)
	plain := entry
	plain.guarded = false
	l.emitTempDrop(plain)
	if !l.curBlock().Terminated() {
		l.setTerm(&Terminator{Kind: TermGoto, Goto: GotoTerm{Target: joinBB}})
	}

	l.startBlock(joinBB)
}

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
		l.emitTempDrop(frame[i])
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
			l.emitTempDrop(frame[i])
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
			if frame[i].local == keep {
				continue
			}
			l.emitTempDrop(frame[i])
		}
	}
}

// lowerOwnedTempExpr materializes the wrapped evaluation and registers
// it for its region's flush.
func (l *funcLowerer) lowerOwnedTempExpr(e *hir.Expr, data hir.OwnedTempData, span source.Span) (Operand, error) {
	guard := NoLocalID
	if data.Guarded {
		// Raised inside the producing branches, so it has to exist and read
		// false before the branch runs.
		guard = l.newTemp(l.boolType(), "owns_temp", span)
		l.emitBoolConst(guard, false)
	}
	saved := l.pendingReleaseGuard
	l.pendingReleaseGuard = guard
	inner, err := l.lowerExprForType(data.Inner, e.Type)
	l.pendingReleaseGuard = saved
	if err != nil {
		return Operand{}, err
	}
	// Minted OUT of the automatic registration every refcounted-scalar temp
	// gets, because this temp gets its own entry below — one that carries the
	// residual plan and the guard. Registered twice, it was released twice: the
	// automatic entry is the whole value unconditionally, so a `float` statement
	// temporary was freed once by its plan and once again by the duplicate, and
	// a GUARDED temp would have been freed on the paths that never produced it.
	tmp := l.newTransferTemp(e.Type, "owned_temp", span)
	l.emit(&Instr{
		Kind: InstrAssign,
		Assign: AssignInstr{
			Dst: Place{Local: tmp},
			Src: RValue{Kind: RValueUse, Use: inner},
		},
	})
	if len(l.tempDropFrames) > 0 {
		top := len(l.tempDropFrames) - 1
		l.tempDropFrames[top] = append(l.tempDropFrames[top], tempDropEntry{local: tmp, steps: data.Steps, guarded: data.Guarded, guard: guard})
	}
	return l.placeOperand(Place{Local: tmp}, e.Type, false), nil
}
