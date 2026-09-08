package types

// IsValueComposite reports whether values of this type live INLINE — a struct,
// tuple, tagged union or fixed array whose members are laid out in place, as
// docs/ABI_LAYOUT.md describes them.
//
// The complement is the handle-backed set: `string`, dynamic `Array<T>`, `Map`,
// `Range`, `Task`, `Channel` and the opaque runtime resources. Those name their
// storage through a handle, are duplicated by duplicating that handle, and each
// governs its own lifetime. A reference or raw pointer names storage it does
// not carry and is never a value composite.
//
// This answers the CATEGORY question — how is a value STORED — and it is
// deliberately not `IsCopy`, which answers whether a value may be DUPLICATED.
// `type File = { fd: int }` and `@copy type Point = { x: int }` are both
// physically values; only the second is Copy. Reading storage off duplicability
// is what let a composite value be a shared pointer, so the two questions get
// two predicates and this is the storage one.
//
// A bare enum is deliberately excluded: it carries a discriminant and no
// members, so there is nothing to lay out inline and nothing to reclaim. So is
// a far handle, which names storage in another shard.
func (in *Interner) IsValueComposite(id TypeID) bool {
	if in == nil || id == NoTypeID {
		return false
	}
	resolved := resolveAliasAndOwn(in, id)
	tt, ok := in.Lookup(resolved)
	if !ok {
		return false
	}

	// Checked BEFORE anything structural: a `&T` resolves down to its pointee
	// under some walks, and a borrow that answered its pointee's question here
	// would have callers copying and dropping through a reference.
	switch tt.Kind {
	case KindReference, KindPointer, KindFar:
		return false
	}

	// Runtime-owned handles are nominal structs (or dynamic arrays), so a bare
	// kind switch would sweep them into inline aggregate storage.
	if _, handleBacked := in.RuntimeHandlePayloads(resolved); handleBacked {
		return false
	}

	// A FIXED array is a value composite; a dynamic one was excluded above.
	if _, _, isFixed := in.ArrayFixedInfo(resolved); isFixed {
		return true
	}

	switch tt.Kind {
	case KindStruct, KindTuple, KindUnion:
		return true
	case KindArray:
		return tt.Count != ArrayDynamicLength
	default:
		return false
	}
}

// RuntimeHandlePayloads reports whether id is a runtime-owned handle value and
// returns the concrete type arguments whose values the handle may own. The
// handle itself is pointer-sized; payloads remain separate layout roots for
// generated element/result operations. Raw pointers, references, far handles,
// and function pointers are intentionally not runtime owners and return false.
func (in *Interner) RuntimeHandlePayloads(id TypeID) ([]TypeID, bool) {
	if in == nil || id == NoTypeID {
		return nil, false
	}
	resolved := resolveAliasAndOwn(in, id)
	t, ok := in.Lookup(resolved)
	if !ok {
		return nil, false
	}
	if t.Kind == KindString {
		return nil, true
	}
	if elem, ok := in.DynamicArrayElem(resolved); ok {
		return []TypeID{elem}, true
	}
	if key, value, ok := in.MapInfo(resolved); ok {
		return []TypeID{key, value}, true
	}
	if in.IsRuntimeHandleType(resolved) {
		info, _ := in.StructInfo(resolved)
		return cloneTypeArgs(info.TypeArgs), true
	}
	if in.IsRuntimePlacementType(resolved) {
		return nil, true
	}
	return nil, false
}

// ContainsDynamicArray reports whether a value of this type can reach a DYNAMIC
// array anywhere its bytes go: as the whole value, a struct field, a tuple
// element, a fixed-array element, a union arm's payload, and through any
// nesting of those. An array answers immediately, so an array of arrays is
// covered without descending into the element.
//
// It is the STRUCTURAL half of the question a value asks before it is given up
// across a thread boundary. The other half -- may this value share a counted
// block -- is about privacy and needs sema's ownership axes; this one is about
// reachability alone and needs nothing but the type graph, which is why it
// lives here: sema, MIR lowering and the LLVM backend each ask it, and one
// function they all import cannot drift the way three near-copies would.
//
// The walk deliberately STOPS at every other runtime handle -- a string, a
// map's table, a channel's ring, a task's slot. Those name storage no
// per-element walk can step, so an array behind one is not made reachable by
// answering true here; it would only ask the runtime to walk something it
// cannot address. A `Map<string, int[]>` therefore answers false -- and it is
// REFUSED at the crossing gate instead, by the companion predicate
// ContainsDynamicArrayBehindHandle, so the stop is a refusal rather than a
// silence. Borrows, pointers, far handles and function values name storage
// they do not carry and answer for nothing.
//
// A union whose membership cannot be read answers false rather than failing
// closed, because the counted-block half already fails closed for exactly that
// shape; a second closed answer here would only demand a walk of a type nobody
// can inspect.
func (in *Interner) ContainsDynamicArray(id TypeID) bool {
	return in.containsDynamicArray(id, make(map[TypeID]struct{}))
}

func (in *Interner) containsDynamicArray(id TypeID, seen map[TypeID]struct{}) bool {
	if in == nil || id == NoTypeID {
		return false
	}
	resolved := resolveAliasAndOwn(in, id)
	// Asked BEFORE the handle arm below, which answers for an array as it does
	// for any handle and would stop the walk at the very thing it looks for.
	if _, ok := in.DynamicArrayElem(resolved); ok {
		return true
	}
	if _, ok := seen[resolved]; ok {
		// A recursive shape contributes nothing through this edge; the answer
		// comes from its other members.
		return false
	}
	seen[resolved] = struct{}{}
	if in.namesStorageItDoesNotCarry(resolved) {
		return false
	}
	if _, handleBacked := in.RuntimeHandlePayloads(resolved); handleBacked {
		return false
	}
	return in.anyInlineMember(resolved, func(member TypeID) bool {
		return in.containsDynamicArray(member, seen)
	})
}

// ContainsDynamicArrayBehindHandle reports whether a value of this type reaches
// a dynamic array ONLY by stepping through storage the relinquishing walk can
// never step: a map's table, a channel's ring, a task's result slot.
//
// It is the companion of ContainsDynamicArray at exactly the stop that
// predicate makes, and neither is the other's negation -- they answer about
// DIFFERENT arrays. `{ xs: int[], m: Map<int, int[]> }` carries one array the
// walk reaches and one it does not, so both answer true; a bare `int[]` is the
// first alone; a `Map<int, int[]>` is the second alone.
//
// Why a value that answers true is REFUSED at the crossing gate rather than
// walked: the runtime's view check reads an array's header out of a slot it is
// handed, and a map's table keeps its entries at offsets no per-element
// callback ever sees. Arming the walk there would emit a call that walks
// nothing -- silence with a call site in front of it. The counted-block half
// already draws that line at this same stop and refuses
// (CountedBlockCanBeMadePrivate), and this is the array half of the same
// sentence.
//
// A dynamic array is NOT such a stop: the runtime iterates its buffer with the
// element's own body, so the walk steps through it and only a handle its
// element carries can answer here.
func (in *Interner) ContainsDynamicArrayBehindHandle(id TypeID) bool {
	return in.containsDynamicArrayBehindHandle(id, make(map[TypeID]struct{}))
}

func (in *Interner) containsDynamicArrayBehindHandle(id TypeID, seen map[TypeID]struct{}) bool {
	if in == nil || id == NoTypeID {
		return false
	}
	resolved := resolveAliasAndOwn(in, id)
	if _, ok := seen[resolved]; ok {
		return false
	}
	seen[resolved] = struct{}{}
	if in.namesStorageItDoesNotCarry(resolved) {
		return false
	}
	if elem, ok := in.DynamicArrayElem(resolved); ok {
		return in.containsDynamicArrayBehindHandle(elem, seen)
	}
	if payloads, handleBacked := in.RuntimeHandlePayloads(resolved); handleBacked {
		// Past this stop every array is out of the walk's reach, however deep
		// it sits, so the payload is asked the walk-nothing question: does it
		// carry an array ANYWHERE, handles included.
		for _, payload := range payloads {
			if in.containsDynamicArrayThroughHandles(payload, make(map[TypeID]struct{})) {
				return true
			}
		}
		return false
	}
	return in.anyInlineMember(resolved, func(member TypeID) bool {
		return in.containsDynamicArrayBehindHandle(member, seen)
	})
}

// containsDynamicArrayThroughHandles is ContainsDynamicArray with every stop
// removed: it steps a map's table and a channel's ring as readily as a struct
// field. It answers for what sits BEHIND a handle, where the distinction
// between storage the walk reaches and storage it does not has already been
// settled by the handle itself.
func (in *Interner) containsDynamicArrayThroughHandles(id TypeID, seen map[TypeID]struct{}) bool {
	if in == nil || id == NoTypeID {
		return false
	}
	resolved := resolveAliasAndOwn(in, id)
	if _, ok := in.DynamicArrayElem(resolved); ok {
		return true
	}
	if _, ok := seen[resolved]; ok {
		return false
	}
	seen[resolved] = struct{}{}
	if in.namesStorageItDoesNotCarry(resolved) {
		return false
	}
	step := func(member TypeID) bool {
		return in.containsDynamicArrayThroughHandles(member, seen)
	}
	if payloads, handleBacked := in.RuntimeHandlePayloads(resolved); handleBacked {
		for _, payload := range payloads {
			if step(payload) {
				return true
			}
		}
		return false
	}
	return in.anyInlineMember(resolved, step)
}

// namesStorageItDoesNotCarry reports the shapes that answer for nothing in the
// three walks above: a borrow, a raw pointer, a far handle naming another
// shard's storage, a function value -- and a type this interner cannot look up
// at all, which carries nothing it can be asked about.
func (in *Interner) namesStorageItDoesNotCarry(resolved TypeID) bool {
	tt, ok := in.Lookup(resolved)
	if !ok {
		return true
	}
	switch tt.Kind {
	case KindReference, KindPointer, KindFar, KindFn:
		return true
	}
	return false
}

// anyInlineMember calls step for every type a value of `id` reaches through
// storage laid out IN PLACE -- a struct field, a tuple element, a fixed
// array's element, a union arm's payload -- and stops at the first true.
//
// A dynamic array and every other runtime handle are the CALLER's to answer
// for, because that is the one place the three array walks above differ: one
// stops at a handle, one steps through it, one asks a different question on
// the far side. Sharing the inline enumeration keeps the part they agree on
// from drifting into three near-copies -- the union arm that ContainsDynamicArray
// gained on 2026-09-08 would otherwise have to be added three times.
//
// Both spellings of a FIXED array are enumerated: the nominal
// `ArrayFixed<T, N>`, whose element only ArrayFixedInfo knows because the
// struct declares no fields, and the structural `[T; N]`, which carries its
// element on the type. A DYNAMIC array never reaches here.
func (in *Interner) anyInlineMember(id TypeID, step func(TypeID) bool) bool {
	if elem, _, isFixed := in.ArrayFixedInfo(id); isFixed {
		return step(elem)
	}
	tt, ok := in.Lookup(id)
	if !ok {
		return false
	}
	switch tt.Kind {
	case KindArray:
		return step(tt.Elem)
	case KindStruct:
		for _, f := range in.StructFields(id) {
			if step(f.Type) {
				return true
			}
		}
	case KindTuple:
		if info, ok := in.TupleInfo(id); ok && info != nil {
			for _, el := range info.Elems {
				if step(el) {
					return true
				}
			}
		}
	case KindUnion:
		info, ok := in.UnionInfo(id)
		if !ok || info == nil {
			return false
		}
		for i := range info.Members {
			m := &info.Members[i]
			if m.Kind == UnionMemberType && step(m.Type) {
				return true
			}
			for _, arg := range m.TagArgs {
				if step(arg) {
					return true
				}
			}
		}
	}
	return false
}

// DynamicArrayElem reports whether id is a DYNAMIC array -- a handle to a
// buffer the runtime owns -- and returns the element type that buffer holds.
// Both spellings answer, the structural `[T]` and the nominal `Array<T>`, and
// an alias or `own` wrapper is looked through, so every asker sees one array
// however the source spelled it.
//
// It is the one question the relinquishing walk, sema's crossing predicate
// and the handle roster ask of an array, and it lives here so they cannot
// drift on which element a buffer holds: the walk hands that element's own
// body to the runtime, which calls it once per slot. A FIXED array is not a
// dynamic one -- its elements live inline and ArrayFixedInfo answers for them
// -- and a map, a string or a channel is a handle whose storage no
// per-element walk reaches.
func (in *Interner) DynamicArrayElem(id TypeID) (TypeID, bool) {
	if in == nil || id == NoTypeID {
		return NoTypeID, false
	}
	resolved := resolveAliasAndOwn(in, id)
	if t, ok := in.Lookup(resolved); ok && t.Kind == KindArray && t.Count == ArrayDynamicLength {
		return t.Elem, true
	}
	if elem, ok := in.ArrayInfo(resolved); ok {
		return elem, true
	}
	return NoTypeID, false
}
