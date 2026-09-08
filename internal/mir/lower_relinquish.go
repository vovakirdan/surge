package mir

import (
	"surge/internal/sema"
	"surge/internal/source"
)

// relinquishCapture is relinquishOperand for a crossing capture: the value
// becomes a field of the state the crossing ships, and the state's field and
// the capture record are kept as ONE operand, because the ownership verifier
// reads both positions and the emitter reads the field.
//
// The block's own anchor is the exception. It is a LEASE, not a value: the
// caller keeps the handle, the body reaches the channel through the owner-side
// pin, and the capture read borrowed it for that reason. Un-sharing it would
// walk a handle whose storage is not this frame's to give up.
func (l *funcLowerer) relinquishCapture(c *CrossingCapture, span source.Span) Operand {
	if c == nil {
		return Operand{}
	}
	if c.Mode == sema.CrossingCaptureAnchorLease {
		return c.Value
	}
	c.Value = l.relinquishOperand(&c.Value, span)
	return c.Value
}

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
// A dynamic array is here for a second reason, and it is not about privacy: a
// view's elements live in the base's buffer, which the origin shard keeps
// reading, and nothing in the type says whether the array in hand is a view or
// a base some live view still reads. Only the runtime's view registry knows,
// so every crossing of a value that CARRIES an array is handed to the walk,
// whatever its element type -- an `int[]` shares no count and would otherwise
// reach the other thread unexamined, which is exactly how a slice came to be
// written through from a worker thread.
//
// Nothing is emitted for a type that can neither share a counted block nor
// reach an array, or for a constant, which is minted fresh at the site.
// Otherwise:
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
	if l == nil || op.Kind == OperandConst || !needsRelinquishWalkIn(l.types, op.Type) {
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
