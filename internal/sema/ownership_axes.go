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
// The answer follows the STORAGE MODEL (docs/runtime-v2-epics/23-storage-model-
// and-typed-carrier-abi.md), one family at a time:
//
//   - a reference-counted scalar owns its counted block, and a
//     reference-counted HANDLE — `Channel<T>` — owns one reference to the
//     runtime object it names. Both are Copy AND heap-owning at once — the
//     whole reason these are separate axes — so they are asked first, before
//     any Copy answer can swallow them;
//   - a borrow (`&T`, `&mut T`, `*T`) names storage it does not own, so
//     dropping one would free a value the holder never owned;
//   - a VALUE COMPOSITE — struct, tuple, union, fixed array — lives inline and
//     owns exactly what its members own: heap iff at least one field, tuple
//     element, union member or tag payload, or array element owns heap,
//     recursively. `@copy type Pair = { a: int, b: int }` owns nothing;
//     `@copy type Pf = { a: float, b: float }` owns two counted blocks;
//     `type Tagged = { s: string, n: int }` owns a string;
//   - anything else is a HANDLE — string, dynamic array, map, task, the
//     opaque runtime resources — and owns heap iff it is not Copy.
//
// The channel used to fall into the last family and answer NO, being Copy,
// which is why a local `Channel<T>` was never reclaimed at all (RV2-DEBT-155):
// no obligation here meant no drop in MIR and nothing for the backend to emit.
//
// A value composite used to answer YES whatever its fields held, because it
// was a heap box and the box had to be reclaimed by whoever held it. The box is
// gone (`types.IsValueComposite` says "INLINE"; the VM's `cellComposite` and
// the backend's `storageFactsOf` agree), and the backend's structural leg
// (`Emitter.typeOwnsHeap`) had already switched to the walk — this leg lagged,
// and the lag was RV2-DEBT-256: SEM3197 refused handing a `@copy` pair of ints
// out of a borrowed `compare` over storage nobody could double-free.
func (tc *typeChecker) ownsHeap(id types.TypeID) bool {
	if id == types.NoTypeID || tc.types == nil {
		return false
	}
	return ownsHeapIn(tc.types, tc.isCopyType, id)
}

// OwnsHeap is the post-check leg of tc.ownsHeap, for MIR lowering and the
// build pipeline. It must keep answering identically to the in-pass form.
func (r *Result) OwnsHeap(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil || id == types.NoTypeID {
		return false
	}
	return ownsHeapIn(r.TypeInterner, r.IsCopyType, id)
}

// OwnsHeapIn is the interner-only leg of the same axis, for passes that run
// past sema's checker and hold no Result — HIR normalization asks it when it
// decides whether a compare arm's ignored payload has anything to release.
//
// The Copy leg here is the interner's own bit. Every `@copy` declaration marks
// it (`recordTypeAttrs` → `MarkCopyType`) in the same step that records the
// attribute sema's leg reads, so the three legs answer alike;
// `TestOwnsHeapLegsAgree` walks the interner and holds them to it.
func OwnsHeapIn(in *types.Interner, id types.TypeID) bool {
	if in == nil || id == types.NoTypeID {
		return false
	}
	return ownsHeapIn(in, func(t types.TypeID) bool { return in.IsCopy(resolveAlias(in, t)) }, id)
}

// ownsHeapIn is the one structural answer behind the three legs. isCopy is the
// leg's Copy authority, and it is consulted only for the handle families —
// a composite's answer comes from its members, never from its own Copy bit.
func ownsHeapIn(in *types.Interner, isCopy func(types.TypeID) bool, id types.TypeID) bool {
	return ownsHeapWalk(in, isCopy, id, make(map[types.TypeID]struct{}))
}

func ownsHeapWalk(in *types.Interner, isCopy func(types.TypeID) bool, id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if id == types.NoTypeID {
		return false
	}
	resolved := resolveAlias(in, id)
	if in.IsRefCounted(resolved) {
		return true
	}
	tt, ok := in.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindReference, types.KindPointer:
		return false
	case types.KindOwn:
		// `own T` is T with a transfer obligation; what it owns is T's to say.
		return ownsHeapWalk(in, isCopy, tt.Elem, seen)
	}
	if !in.IsValueComposite(resolved) {
		return !isCopy(resolved)
	}
	if _, ok := seen[resolved]; ok {
		// A recursive type reached itself: this edge contributes nothing and
		// the answer comes from its other members.
		return false
	}
	seen[resolved] = struct{}{}
	if elem, _, isFixed := in.ArrayFixedInfo(resolved); isFixed {
		return ownsHeapWalk(in, isCopy, elem, seen)
	}
	switch tt.Kind {
	case types.KindArray:
		return ownsHeapWalk(in, isCopy, tt.Elem, seen)
	case types.KindStruct:
		for _, f := range in.StructFields(resolved) {
			if ownsHeapWalk(in, isCopy, f.Type, seen) {
				return true
			}
		}
		return false
	case types.KindTuple:
		info, ok := in.TupleInfo(resolved)
		if !ok || info == nil {
			return true
		}
		for _, el := range info.Elems {
			if ownsHeapWalk(in, isCopy, el, seen) {
				return true
			}
		}
		return false
	case types.KindUnion:
		// The FULL membership: a bare type member owns whatever its type owns,
		// and a tag member owns whatever its payloads own. A union whose
		// membership cannot be read fails CLOSED — a missing release is the
		// leak nobody notices, a spare one is a validator's to refuse.
		info, ok := in.UnionInfo(resolved)
		if !ok || info == nil {
			return true
		}
		for i := range info.Members {
			m := &info.Members[i]
			if m.Kind == types.UnionMemberType && ownsHeapWalk(in, isCopy, m.Type, seen) {
				return true
			}
			for _, arg := range m.TagArgs {
				if ownsHeapWalk(in, isCopy, arg, seen) {
					return true
				}
			}
		}
		return false
	}
	// A composite the walk cannot see into keeps whatever release it had.
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
	// A value composite rides again. It lives inline, so its bits ARE the
	// value; what each crossing has to settle is who owns, on each side, what
	// those bits own in turn.
	//
	// The three crossing shapes reach that differently, which is why no test
	// here can say "clone it": a CAPTURE is duplicated at its operand, so the
	// destination's state holds a value of its own; a RESULT is produced by the
	// body and handed to the caller, which is a transfer with one owner at a
	// time and needs no copy at all; a channel ELEMENT is duplicated at the
	// send. This axis only answers whether the bits may travel, and once each
	// route has an owner on the far side, they may.
	//
	// What still may NOT travel is a composite carrying a reference-counted
	// scalar — `ContainsRefCountedScalar` above turns that away, because the
	// count is non-atomic and the copy these routes perform RETAINS such a
	// field rather than deep-copying it. Right on one shard, wrong across two.
	return r.IsCopyType(id)
}

// IsCopyValueComposite reports the one combination a crossing copies MEMBER BY
// MEMBER: a struct, tuple, union or fixed array that is also duplicable.
//
// Both halves are load-bearing, which is why this is its own question rather
// than either predicate alone. A move-only composite is equally an inline
// aggregate, but it crosses by transfer, so exactly one shard ends up owning
// its members. A Copy SCALAR is equally duplicable, but its word is the value
// and there are no members to copy. Only their intersection is duplicated
// into a second aggregate whose members (a counted scalar among them) each
// need an owner on the far side.
//
// It is stated here, next to the axes, because several crossing routes need the
// same answer and each had been deriving it differently.
func (r *Result) IsCopyValueComposite(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil || id == types.NoTypeID {
		return false
	}
	if !r.IsCopyType(id) {
		return false
	}
	return r.TypeInterner.IsValueComposite(resolveAlias(r.TypeInterner, id))
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
