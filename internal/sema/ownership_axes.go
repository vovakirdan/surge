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
	resolved := tc.resolveAlias(id)
	// A reference-counted scalar is Copy AND heap-owning at once — the whole
	// reason these are separate axes. Ask first, so the Copy answer below does
	// not swallow it.
	if tc.types.IsRefCountedScalar(resolved) {
		return true
	}
	if tc.isCopyType(id) {
		return false
	}
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
	resolved := resolveAlias(r.TypeInterner, id)
	if r.TypeInterner.IsRefCountedScalar(resolved) {
		return true
	}
	if r.IsCopyType(id) {
		return false
	}
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
	if r == nil || r.TypeInterner == nil {
		return false
	}
	// A reference-counted scalar is Copy, but its bits are a pointer to a
	// counted block, and the count is non-atomic. Letting the raw word cross
	// would put two shards on one counter. It becomes shippable again once the
	// boundary installs a deep copy. The test is RECURSIVE because a `@copy`
	// struct of floats is Copy as a whole and would otherwise carry them across
	// one level down.
	if r.ContainsRefCountedScalar(id) {
		return false
	}
	return r.IsCopyType(id)
}

// ContainsRefCountedScalar reports whether a value of this type holds, at any
// depth, an arbitrary-precision scalar — i.e. whether copying its bits would
// duplicate a reference into a counted heap block without touching the count.
//
// This is the crossing question, not the drop question: a `@copy` struct of
// floats is itself Copy, ships as plain bits today, and would hand a second
// shard a pointer into the same counted block.
//
// Unions are deliberately not walked. A union is not Copy, so it can only
// cross as an owned `@shard_movable` MOVE, which transfers the references
// rather than sharing them and needs no copy at the boundary.
func (r *Result) ContainsRefCountedScalar(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil {
		return false
	}
	return r.containsRefCountedScalar(id, make(map[types.TypeID]struct{}))
}

func (r *Result) containsRefCountedScalar(id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if id == types.NoTypeID {
		return false
	}
	in := r.TypeInterner
	id = resolveAlias(in, id)
	if in.IsRefCountedScalar(id) {
		return true
	}
	if _, ok := seen[id]; ok {
		// A recursive type reached itself; this edge contributes nothing and
		// the answer comes from its other members.
		return false
	}
	seen[id] = struct{}{}

	tt, ok := in.Lookup(id)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindOwn, types.KindArray:
		return r.containsRefCountedScalar(tt.Elem, seen)
	case types.KindReference, types.KindPointer:
		// A borrow names storage it does not carry; the pointee crosses (or
		// fails to) on its own terms, and borrows cannot cross at all.
		return false
	case types.KindStruct:
		for _, f := range in.StructFields(id) {
			if r.containsRefCountedScalar(f.Type, seen) {
				return true
			}
		}
		return false
	case types.KindTuple:
		if info, ok := in.TupleInfo(id); ok && info != nil {
			for _, el := range info.Elems {
				if r.containsRefCountedScalar(el, seen) {
					return true
				}
			}
		}
		return false
	default:
		return false
	}
}
