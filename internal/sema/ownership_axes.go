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
// which is why a local `Channel<T>` was never reclaimed at all:
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
// Present definition: the Copy types whose counted blocks, if any, the
// relinquishing walk can make private (a `float`, a `@copy` composite of
// them) — not a Copy handle whose payload lives in storage this shard keeps.
// A type that owns heap becomes shippable only once the crossing installs a
// deep copy at the boundary, which is why this is its own axis rather than a
// synonym for OwnsHeap's negation: "not heap-owning" and "safe to memcpy
// across a shard" are different claims, and a `&T` satisfies the first but
// never the second.
func (r *Result) TriviallyTransportableBits(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil {
		return false
	}
	// A reference-counted scalar is Copy, but its bits are a pointer to a
	// counted block, and the count is non-atomic. The word may cross only
	// once the block behind it is PRIVATE to the value that travels, and the
	// producer of a crossing result makes it so: the `ret` of a `spawn on`,
	// `on` or `blocking` body un-shares the result in its relinquishing
	// operand before the reply names it (rewriteSpawnOnPollReturns,
	// rewriteBlockingReturns), and the asker moves it exactly once. What no
	// walk can make private — a map's table, a channel's ring — stays refused,
	// in the same words the capture gate uses (CountedBlockStaysShared, held in
	// lock step with the emitter's walk). A dynamic array's buffer is walked
	// element by element by the runtime where an owned move relinquishes it,
	// so this predicate admits `float[]`; the plain-copy rule below still
	// refuses it on a crossing reply, because an array is not Copy.
	if r.CountedBlockStaysShared(id) {
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
// This is the Copy-bits question, not the drop question, and no crossing
// gate asks it any more: a `@copy` struct of floats is itself Copy and
// would hand a second shard a pointer into the same counted block
// if it shipped as plain bits — which is why every crossing now makes the
// value private in its relinquishing operand and asks CountedBlockStaysShared
// instead. What still asks this question is the traceable axis
// (capability_axes.go) and the heap-owning leg of a Copy composite.
//
// Unions are deliberately not walked here. A union is not Copy, so it never
// ships as plain bits; the Traceable axis reaches its payloads through its
// own fixpoint. The owned-MOVE question is MayShareCountedBlock's, below.
func (r *Result) ContainsRefCountedScalar(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil {
		return false
	}
	return r.containsRefCountedScalar(id, make(map[types.TypeID]struct{}), false)
}

// MayShareCountedBlock reports whether a value of this type can hold, at any
// depth — union payloads included — a reference into a counted heap block
// that another holder on this shard may still hold too.
//
// This is the question an owned MOVE across a shard boundary has to ask, and
// it is not answered by exclusivity. `own P{ v: a }` retains `a`'s block into
// the field, so the moved value and the live `a` name one block: moving the
// value transfers ONE of the references, and the non-atomic count is then
// raced from two shards. A type for which this answers true is un-shared in
// the relinquishing operand before it crosses -- when the walk can reach its
// leaves (CountedBlockCanBeMadePrivate) -- and refused otherwise.
func (r *Result) MayShareCountedBlock(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil {
		return false
	}
	return r.containsRefCountedScalar(id, make(map[types.TypeID]struct{}), true)
}

// CountedBlockCanBeMadePrivate reports whether the relinquishing walk can make
// every counted leaf of a value of this type private before the value is
// given up across a shard or thread boundary: a scalar, a struct, a tuple, a
// fixed array, a union of those -- and a dynamic array, whose buffer the
// runtime walks slot by slot with the element's own walk
// (rt_array_unshare_walk), through nesting. It is the other half of
// MayShareCountedBlock: a shape that MAY share and CAN be made private is
// un-shared in the relinquishing operand and crosses; a shape that may share
// and cannot is refused, and this is the question the refusal asks.
//
// What answers false is a RUNTIME HANDLE whose payload may share and whose
// storage no per-element walk reaches: a map's table (`Map<K, V>` keyed or
// valued by a counted scalar), a channel's ring (`Channel<float>`, which stays
// on the creator's shard), a task's slot. An array of such handles answers as
// its element does. Placement carries nothing.
//
// It is the same walk the backend runs on its side (canUnshareValue in
// internal/backend/llvm); the two are held in lock step by a labelled table
// there, because a shape sema admits and the emitter cannot serve is a build
// failure instead of a diagnostic.
func (r *Result) CountedBlockCanBeMadePrivate(id types.TypeID) bool {
	if r == nil || r.TypeInterner == nil {
		return true
	}
	return r.countedBlockCanBeMadePrivate(id, make(map[types.TypeID]struct{}))
}

// CountedBlockStaysShared is the crossing refusal: the type may share a
// counted block and the relinquishing walk cannot make it private.
func (r *Result) CountedBlockStaysShared(id types.TypeID) bool {
	return r.MayShareCountedBlock(id) && !r.CountedBlockCanBeMadePrivate(id)
}

func (r *Result) countedBlockCanBeMadePrivate(id types.TypeID, seen map[types.TypeID]struct{}) bool {
	if id == types.NoTypeID {
		return true
	}
	in := r.TypeInterner
	id = resolveAlias(in, id)
	if in.IsRefCountedScalar(id) {
		return true
	}
	if _, ok := seen[id]; ok {
		return true
	}
	seen[id] = struct{}{}
	// A dynamic array answers for its element: the runtime walks the buffer
	// slot by slot with the element's own walk, so the array can be made
	// private exactly when its element can. Asked BEFORE the handle arm below,
	// which would otherwise answer for the array as for any handle whose
	// payload may share.
	if elem, ok := in.DynamicArrayElem(id); ok {
		return r.countedBlockCanBeMadePrivate(elem, seen)
	}
	if payloads, ok := in.RuntimeHandlePayloads(id); ok && !in.IsRuntimePlacementType(id) {
		for _, payload := range payloads {
			if r.MayShareCountedBlock(payload) {
				return false
			}
		}
		return true
	}
	if elem, _, ok := in.ArrayFixedInfo(id); ok {
		return r.countedBlockCanBeMadePrivate(elem, seen)
	}
	tt, ok := in.Lookup(id)
	if !ok {
		return true
	}
	switch tt.Kind {
	case types.KindOwn:
		return r.countedBlockCanBeMadePrivate(tt.Elem, seen)
	case types.KindStruct:
		for _, f := range in.StructFields(id) {
			if !r.countedBlockCanBeMadePrivate(f.Type, seen) {
				return false
			}
		}
		return true
	case types.KindTuple:
		if info, ok := in.TupleInfo(id); ok && info != nil {
			for _, el := range info.Elems {
				if !r.countedBlockCanBeMadePrivate(el, seen) {
					return false
				}
			}
		}
		return true
	case types.KindUnion:
		info, ok := in.UnionInfo(id)
		if !ok || info == nil {
			return false
		}
		for i := range info.Members {
			m := &info.Members[i]
			if m.Kind == types.UnionMemberType && !r.countedBlockCanBeMadePrivate(m.Type, seen) {
				return false
			}
			for _, arg := range m.TagArgs {
				if !r.countedBlockCanBeMadePrivate(arg, seen) {
					return false
				}
			}
		}
		return true
	default:
		return true
	}
}

func (r *Result) containsRefCountedScalar(id types.TypeID, seen map[types.TypeID]struct{}, throughUnions bool) bool {
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
	// A fixed array is a NOMINAL struct that declares no fields -- its
	// elements live inline and the element type is only in ArrayFixedInfo, so
	// the struct walk below would answer "nothing inside" for `float[4]` and
	// let four counted handles ship as plain bits. Both questions ask it: a
	// Copy `float[4]` copied across a boundary duplicates four references as
	// surely as a bare float does. Found by the G1 reviewers of 2026-09-06.
	if elem, _, ok := in.ArrayFixedInfo(id); ok {
		return r.containsRefCountedScalar(elem, seen, throughUnions)
	}
	if throughUnions {
		// The owned-MOVE question reaches into every runtime handle's payload,
		// containers and resources alike. A `float[]` is a handle whose
		// elements are counted blocks, each retained from whatever was pushed,
		// so moving the array moves one reference per element while the
		// pushers keep theirs -- which is why the runtime walks its buffer
		// element by element in the relinquishing operand (the other half,
		// countedBlockCanBeMadePrivate, says so). A `Channel<float>` is worse:
		// its own count is atomic precisely so a copy of the HANDLE may live on
		// another shard, and a `send` from that shard retains a block into a
		// ring the creator's shard owns -- one non-atomic count under two
		// threads with no float captured at all, and no walk reaches that ring
		// or a map's table. So a handle counts as sharing whenever its payload
		// does; only Placement carries nothing. The Copy-bits question never
		// gets here.
		if payloads, ok := in.RuntimeHandlePayloads(id); ok && !in.IsRuntimePlacementType(id) {
			for _, payload := range payloads {
				if r.containsRefCountedScalar(payload, seen, throughUnions) {
					return true
				}
			}
			return false
		}
	}
	switch tt.Kind {
	case types.KindOwn, types.KindArray:
		return r.containsRefCountedScalar(tt.Elem, seen, throughUnions)
	case types.KindReference, types.KindPointer:
		// A borrow names storage it does not carry; the pointee crosses (or
		// fails to) on its own terms, and borrows cannot cross at all.
		return false
	case types.KindStruct:
		for _, f := range in.StructFields(id) {
			if r.containsRefCountedScalar(f.Type, seen, throughUnions) {
				return true
			}
		}
		return false
	case types.KindTuple:
		if info, ok := in.TupleInfo(id); ok && info != nil {
			for _, el := range info.Elems {
				if r.containsRefCountedScalar(el, seen, throughUnions) {
					return true
				}
			}
		}
		return false
	case types.KindUnion:
		if !throughUnions {
			return false
		}
		// The full membership, as ownsHeapWalk reads it: a bare type member
		// holds whatever its type holds, a tag member whatever its payloads
		// hold. A union whose membership cannot be read fails CLOSED — the
		// refusal is the safe answer, and a spare one is a validator's to lift.
		info, ok := in.UnionInfo(id)
		if !ok || info == nil {
			return true
		}
		for i := range info.Members {
			m := &info.Members[i]
			if m.Kind == types.UnionMemberType && r.containsRefCountedScalar(m.Type, seen, throughUnions) {
				return true
			}
			for _, arg := range m.TagArgs {
				if r.containsRefCountedScalar(arg, seen, throughUnions) {
					return true
				}
			}
		}
		return false
	default:
		return false
	}
}
