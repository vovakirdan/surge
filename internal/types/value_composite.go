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
	if t.Kind == KindArray && t.Count == ArrayDynamicLength {
		return []TypeID{t.Elem}, true
	}
	if elem, ok := in.ArrayInfo(resolved); ok {
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
