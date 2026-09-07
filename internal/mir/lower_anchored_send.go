package mir

import (
	"surge/internal/ast"
	"surge/internal/hir"
)

// anchoredSendGivenAway lowers the payload of an anchored body's `ch.send`
// when the element may share a counted block — a `float`, a composite with
// one inside. The payload is the body's own reference to the block, and it
// LEAVES with the send: the ring takes the bits, and the count is not atomic.
//
// Nothing is done to the value here, and nothing may be. The anchored body
// has no async split: a send that parks re-enters the body from its first
// instruction, and the runtime never reads `src` again once a value is staged
// (rt_anchored_channel_send). By the time the body runs again the block is
// the ring's, or a receiver's, or already released — so a retain, a clone, a
// fresh literal would be made again on every wake while the first was
// consumed, and even an un-share, which READS the count and decrements it on
// the clone branch, would be a walk over storage this frame no longer owns.
// Only an empty prefix replays safely.
//
// What has nothing to replay is the capture's own reference: the caller made
// it private when the capture entered the state (relinquishCapture, once, on
// the caller's thread), and the unpack at the body's entry moves it into this
// local. The send moves it out of the local, and the validator's act for this
// sink is not an un-share but that provenance: the local's only touch before
// the sink is the unpack from the state (validate_relinquish.go).
//
// Sema holds the shape to `own <captured binding>` (SemaAnchoredSendGiveAway)
// and marks the binding moved, so no later read or write of it compiles; the
// drop the poll function synthesizes for a Copy capture is withheld for this
// one (givenAwayCaptures). Any other payload — an `int`, a `float64`, a value
// that owns no counted block — is not this function's and lowers as before.
//
// A DYNAMIC ARRAY payload answers here for the same reason and by the second
// question, because the first one does not see it: an `int` has no count, so
// `mayShareCountedBlockIn` says no about `[int]` while the body owes that
// capture's header and buffer a drop all the same
// (registerCrossingBodyOwnership). Withholding it here is what makes the ring
// the only owner; without it the ring's reclaim frees the buffer the body's
// scope exit already freed. Sema's second arm accepts exactly the payloads
// this one recognizes — `own <captured binding>`, nothing else — so the two
// sides give the same value away.
func (l *funcLowerer) anchoredSendGivenAway(data hir.CallData) (Operand, bool) {
	if l == nil || l.f == nil || len(data.Args) != 1 || data.Args[0] == nil {
		return Operand{}, false
	}
	value := data.Args[0]
	if !mayShareCountedBlockIn(l.types, value.Type) && !dynamicArrayIn(l.types, value.Type) {
		return Operand{}, false
	}
	unary, ok := value.Data.(hir.UnaryOpData)
	if value.Kind != hir.ExprUnaryOp || !ok || unary.Op != ast.ExprUnaryOwn ||
		unary.Operand == nil || unary.Operand.Kind != hir.ExprVarRef {
		return Operand{}, false
	}
	ref, ok := unary.Operand.Data.(hir.VarRefData)
	if !ok || !ref.SymbolID.IsValid() {
		return Operand{}, false
	}
	local, ok := l.symToLocal[ref.SymbolID]
	if !ok || local == NoLocalID || int(local) < 0 || int(local) >= len(l.f.Locals) {
		return Operand{}, false
	}
	if l.givenAwayCaptures == nil {
		l.givenAwayCaptures = make(map[LocalID]struct{})
	}
	l.givenAwayCaptures[local] = struct{}{}
	return Operand{Kind: OperandMove, Type: l.f.Locals[local].Type, Place: Place{Local: local}}, true
}
