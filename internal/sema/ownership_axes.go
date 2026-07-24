package sema

import "surge/internal/types"

// Ownership axes.
//
// `IsCopy` currently answers three different questions at once:
//
//  1. surface duplicability — may `let b = a` leave `a` usable?
//  2. plain-bits shippability — may the value ride a crossing as raw bits?
//  3. non-droppability — is there nothing for scope exit to reclaim?
//
// For every type the language has today those three answers coincide, so one
// predicate served all three. They stop coinciding for the arbitrary-precision
// scalars: an `int` beyond the inline fixnum range is duplicable (1) but owns a
// heap block, so it is neither shippable as raw bits (2) nor free to abandon at
// scope exit (3).
//
// These two predicates name questions 3 and 2 as their own axes. Both are
// defined here to give exactly the answers `IsCopy` gives today — this file
// introduces names, not behavior. Widening them is what makes the heap scalars
// reclaimable, and it is a deliberate later step.
//
// Other legs of the same two axes live outside sema and must widen WITH these,
// not after them:
//
//   - `Emitter.typeOwnsHeap` (`internal/backend/llvm/emit_drop_glue.go`) is the
//     backend's structural leg of OwnsHeap: it walks composites itself rather
//     than asking about Copy, and it decides which types get generated drop
//     glue.
//   - `funcLowerer.localFlags` (`internal/mir/lower.go`) records `LocalFlagCopy`,
//     and `internal/mir/validate.go` rejects `InstrDrop` on a local carrying it.
//     A droppable Copy scalar would trip that validator.
//   - `funcLowerer.placeOperand` (`internal/mir/lower_expr_helpers.go`) bit-copies
//     Copy operands instead of moving them; a refcounted scalar needs a third
//     operand kind there, since neither copy nor move expresses "retain".

// ownsHeap reports whether a value of this type carries heap storage that scope
// exit must reclaim. Answers question 3 above.
//
// Present definition: a non-Copy value that is not a borrow. A reference or raw
// pointer names storage it does not own, so dropping one would free a value the
// holder never owned.
func (tc *typeChecker) ownsHeap(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	if tc.isCopyType(id) {
		return false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer:
		return false
	}
	return true
}

// OwnsHeap is the post-check leg of tc.ownsHeap, for MIR lowering and the
// build pipeline. It must keep answering identically to the in-pass form.
func (r *Result) OwnsHeap(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil || id == types.NoTypeID {
		return false
	}
	if r.IsCopyType(id) {
		return false
	}
	resolved := resolveAlias(r.TypeInterner, id)
	tt, ok := r.TypeInterner.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer:
		return false
	}
	return true
}

// TriviallyTransportableBits reports whether a value of this type may ride a
// crossing as raw bits — copied into the state struct or the reply word with no
// per-shard fixup. Answers question 2 above.
//
// Present definition: exactly the Copy types. A type that owns heap becomes
// shippable only once the crossing installs a deep copy at the boundary, which
// is why this is its own axis rather than a synonym for OwnsHeap's negation:
// "not heap-owning" and "safe to memcpy across a shard" are different claims,
// and a `&T` satisfies the first but never the second.
func (r *Result) TriviallyTransportableBits(id types.TypeID) bool {
	return r.IsCopyType(id)
}
