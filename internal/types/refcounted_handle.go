package types

import "surge/internal/source"

// IsRefCountedHandle reports whether values of this type are one-word handles
// naming a runtime object that the RUNTIME reference-counts: copying a handle
// retains, dropping a copy releases, and the last release destroys the object
// (docs/RUNTIME_V2.md section 7, "Channel lifetime").
//
// This is the second passenger on the axis IsRefCountedScalar opened. Both are
// Copy at the surface and counted underneath, so both break the coincidence
// `IsCopy` used to rely on -- duplicable, yet not free to abandon. They stay
// two predicates rather than one because the COUNT lives in different places:
// a scalar's count is the first field of a block this compiler lays out and
// bumps inline, non-atomically, and must not cross a shard; a handle's count
// is private to the runtime object, atomic, and is what lets a copy of the
// handle live in another shard's frame. Every crossing rule that turns a
// scalar away therefore does NOT apply here, which is why widening
// IsRefCountedScalar was never an option.
//
// Only `Channel<T>` qualifies today. `Task<T>` joins when its handle count and
// entitlement are settled (Wave D4b); `Range` is single-owner and stays out.
// The family is marked from the declaration (`MarkRefCountedHandleType`),
// never inferred from a name.
func (in *Interner) IsRefCountedHandle(id TypeID) bool {
	if in == nil || id == NoTypeID || in.refCountedHandles == nil {
		return false
	}
	_, ok := in.refCountedHandles[resolveAliasAndOwn(in, id)]
	return ok
}

// IsRefCounted reports whether a value of this type is Copy at the surface and
// counted underneath -- a reference-counted scalar or a reference-counted
// handle. It is the predicate the RETAIN/RELEASE legs ask: a consuming read
// retains, a scope exit releases, a by-value parameter is a borrow the caller
// keeps its reference through. The legs that decide what may CROSS a shard
// keep asking IsRefCountedScalar, because that is the one whose count cannot
// be shared.
//
// `own` is looked through on purpose. An `own` binding or parameter of a Copy
// type is that same Copy type with a spelling: `let a = ch` on an
// `own Channel<int>` leaves `ch` usable, so the read has to retain exactly as
// it does on the bare type, and a drop of the binding releases exactly one
// reference either way.
func (in *Interner) IsRefCounted(id TypeID) bool {
	if in == nil || id == NoTypeID {
		return false
	}
	resolved := resolveAliasAndOwn(in, id)
	return in.IsRefCountedScalar(resolved) || in.IsRefCountedHandle(resolved)
}

// MarkRefCountedHandleType records a core runtime handle family as reference
// counted. The family identity is the declaration, as for
// MarkRuntimeHandleType, so every instantiation of the declared type -- the
// ones already interned and the ones RegisterStructInstance mints later --
// answers alike.
func (in *Interner) MarkRefCountedHandleType(id TypeID) {
	if in == nil || id == NoTypeID {
		return
	}
	info, ok := in.StructInfo(id)
	if !ok || info == nil {
		return
	}
	if in.refCountedHandles == nil {
		in.refCountedHandles = make(map[TypeID]struct{}, 16)
	}
	if in.refCountedFamilies == nil {
		in.refCountedFamilies = make(map[runtimeHandleFamily]struct{}, 2)
	}
	family := runtimeHandleFamily{name: info.Name, decl: info.Decl}
	in.refCountedFamilies[family] = struct{}{}
	for candidate, descriptor := range in.types {
		if descriptor.Kind != KindStruct {
			continue
		}
		candidateInfo := in.structInfo(TypeID(candidate))
		if candidateInfo != nil && candidateInfo.Name == family.name && candidateInfo.Decl == family.decl {
			in.refCountedHandles[TypeID(candidate)] = struct{}{}
		}
	}
}

// inheritRefCountedHandleFamily gives a freshly minted instantiation the
// reference-counted mark its declaration carries. Called beside
// inheritRuntimeHandleFamily from the same registration sites.
func (in *Interner) inheritRefCountedHandleFamily(id TypeID, name source.StringID, decl source.Span) {
	if in == nil || id == NoTypeID || in.refCountedFamilies == nil {
		return
	}
	if _, ok := in.refCountedFamilies[runtimeHandleFamily{name: name, decl: decl}]; !ok {
		return
	}
	if in.refCountedHandles == nil {
		in.refCountedHandles = make(map[TypeID]struct{}, 16)
	}
	in.refCountedHandles[id] = struct{}{}
}
