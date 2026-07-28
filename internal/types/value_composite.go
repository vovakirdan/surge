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

	// The handle-backed builtins are nominal STRUCTS, so `KindStruct` alone
	// would sweep them in. They are excluded by identity where the interner
	// tracks one, and by name where it does not — the same way the backends
	// have always recognised them.
	if _, isArray := in.ArrayInfo(resolved); isArray {
		return false
	}
	if _, _, isMap := in.MapInfo(resolved); isMap {
		return false
	}
	if in.isHandleBackedNominal(resolved) {
		return false
	}
	// `Placement` is DECLARED as a struct and is not stored like one: it is an
	// intrinsic whose runtime value is a tagged word — low bits the kind, upper
	// bits the payload — so there is no box and nothing to lay out. Answering
	// "yes" here would make it a composite for copy, drop and transport
	// purposes and would, among other things, stop placements from crossing a
	// shard boundary, which is the one thing they exist to do.
	if in.IsRuntimePlacementType(resolved) {
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

// handleBackedNominals are the built-in generic structs whose value is a handle
// to runtime-owned storage. The interner has no dedicated TypeID for these the
// way it does for Array and Map, so they are matched by name.
var handleBackedNominals = [...]string{"Range", "Task", "Channel"}

func (in *Interner) isHandleBackedNominal(id TypeID) bool {
	if in == nil || in.Strings == nil {
		return false
	}
	info, ok := in.StructInfo(id)
	if !ok || info == nil {
		return false
	}
	name, ok := in.Strings.Lookup(info.Name)
	if !ok {
		return false
	}
	for _, h := range handleBackedNominals {
		if name == h {
			return true
		}
	}
	return false
}
