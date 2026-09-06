package mir

import "surge/internal/source"

// relinquishOperand prepares an operand that is about to be given up across a
// thread boundary — staged as a far-select SEND payload, moved into a
// crossing's state, returned from a crossing or blocking body.
//
// The runtime consumes what it is handed there on every path (a winner
// commits it, a loser is destroyed in its cell, a failed submission drops it
// in the caller's storage), so the operand has to hand over a reference of its
// own, and that reference has to be the ONLY one to every counted block
// underneath: the count is not atomic, and the other thread's release would
// race any holder left on this one.
//
// Nothing is emitted for a type that cannot share a counted block, or for a
// constant, which is minted fresh at the site. Otherwise:
//
//   - a MOVE out of a bare local already hands over the local's own reference;
//     only the un-share is added, on that local, so a far-select candidate's
//     ReturnPlace keeps naming the place the losing payload comes back to;
//   - anything else — a RETAIN of a live binding, a CopyValue of a `@copy`
//     composite, a read of a global — is first assigned into a transfer temp
//     (that assignment is where the retain or the clone is materialized; an
//     OperandRetain left on the sink itself would be ignored, because the
//     emitters take a sink operand's address and never retain through it),
//     then un-shared, and the sink moves out of the temp.
//
// The temp is a TRANSFER temp on purpose (newTransferTemp, not newTemp): the
// reference it holds leaves with the value, and a temp registered for the
// region's flush would be released a second time behind the runtime's back.
// It is marked owning so that consuming it is read as the transfer it is.
//
// validateRelinquishedOperandsArePrivate is the other half: the lowering
// runs it on every function, so a site that forgot to call this refuses to
// build instead of shipping a shared block.
func (l *funcLowerer) relinquishOperand(op *Operand, span source.Span) Operand {
	if l == nil || op.Kind == OperandConst || !mayShareCountedBlockIn(l.types, op.Type) {
		return *op
	}
	if _, bare := bareLocalOf(op.Place); bare && op.Kind == OperandMove {
		l.emit(&Instr{Kind: InstrUnshare, Unshare: UnshareInstr{Place: op.Place}})
		return *op
	}
	tmp := l.markOwningTemp(l.newTransferTemp(op.Type, "private", span))
	l.emit(&Instr{Kind: InstrAssign, Assign: AssignInstr{
		Dst: Place{Local: tmp},
		Src: RValue{Kind: RValueUse, Use: *op},
	}})
	l.emit(&Instr{Kind: InstrUnshare, Unshare: UnshareInstr{Place: Place{Local: tmp}}})
	return Operand{Kind: OperandMove, Type: op.Type, Place: Place{Local: tmp}}
}
