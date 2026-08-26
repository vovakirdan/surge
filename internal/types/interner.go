package types //nolint:revive

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

// Builtins stores TypeIDs for common primitive types.
type Builtins struct {
	Invalid TypeID
	Unit    TypeID
	Nothing TypeID
	Bool    TypeID
	String  TypeID
	Int     TypeID
	Int8    TypeID
	Int16   TypeID
	Int32   TypeID
	Int64   TypeID
	Uint    TypeID
	Uint8   TypeID
	Uint16  TypeID
	Uint32  TypeID
	Uint64  TypeID
	Float   TypeID
	Float16 TypeID
	Float32 TypeID
	Float64 TypeID
}

// Interner provides stable TypeIDs by hashing structural descriptors.
type Interner struct {
	types           []Type
	index           map[typeKey]TypeID
	builtins        Builtins
	structs         []StructInfo
	aliases         []AliasInfo
	Strings         *source.Interner
	typeLayoutAttrs map[TypeID]LayoutAttrs
	copyTypes       map[TypeID]struct{}
	placementTypes  map[TypeID]struct{}
	runtimeHandles  map[TypeID]struct{}
	runtimeFamilies map[runtimeHandleFamily]struct{}
	// refCountedHandles is the subset of runtimeHandles whose object the
	// runtime reference-counts (refcounted_handle.go); refCountedFamilies is
	// the declaration set a new instantiation inherits the mark from.
	refCountedHandles  map[TypeID]struct{}
	refCountedFamilies map[runtimeHandleFamily]struct{}
	params             []TypeParamInfo
	unions             []UnionInfo
	enums              []EnumInfo
	tuples             []TupleInfo
	tupleIndex         map[string]TypeID
	fns                []FnInfo
	arrayType          TypeID
	arrayParam         TypeID
	arrayFixedType     TypeID
	arrayFixedParams   [2]TypeID
	mapType            TypeID
	mapParams          [2]TypeID
}

type runtimeHandleFamily struct {
	name source.StringID
	decl source.Span
}

// NewInterner constructs an interner seeded with built-in primitives.
func NewInterner() *Interner {
	in := &Interner{
		index: make(map[typeKey]TypeID, 64),
	}
	in.structs = append(in.structs, StructInfo{}) // reserve 0 as invalid sentinel
	in.aliases = append(in.aliases, AliasInfo{})
	in.params = append(in.params, TypeParamInfo{})
	in.unions = append(in.unions, UnionInfo{})
	in.enums = append(in.enums, EnumInfo{})
	in.fns = append(in.fns, FnInfo{})
	in.builtins.Invalid = in.internRaw(Type{Kind: KindInvalid})
	in.builtins.Unit = in.Intern(Type{Kind: KindUnit})
	in.builtins.Nothing = in.Intern(Type{Kind: KindNothing})
	in.builtins.Bool = in.Intern(Type{Kind: KindBool})
	in.builtins.String = in.Intern(Type{Kind: KindString})
	in.builtins.Int = in.Intern(MakeInt(WidthAny))
	in.builtins.Int8 = in.Intern(MakeInt(Width8))
	in.builtins.Int16 = in.Intern(MakeInt(Width16))
	in.builtins.Int32 = in.Intern(MakeInt(Width32))
	in.builtins.Int64 = in.Intern(MakeInt(Width64))
	in.builtins.Uint = in.Intern(MakeUint(WidthAny))
	in.builtins.Uint8 = in.Intern(MakeUint(Width8))
	in.builtins.Uint16 = in.Intern(MakeUint(Width16))
	in.builtins.Uint32 = in.Intern(MakeUint(Width32))
	in.builtins.Uint64 = in.Intern(MakeUint(Width64))
	in.builtins.Float = in.Intern(MakeFloat(WidthAny))
	in.builtins.Float16 = in.Intern(MakeFloat(Width16))
	in.builtins.Float32 = in.Intern(MakeFloat(Width32))
	in.builtins.Float64 = in.Intern(MakeFloat(Width64))
	return in
}

// Builtins returns TypeIDs for primitive types.
func (in *Interner) Builtins() Builtins {
	return in.builtins
}

// Intern ensures the provided descriptor has a stable TypeID.
func (in *Interner) Intern(t Type) TypeID {
	if t.Kind == KindInvalid {
		return NoTypeID
	}
	key := typeKey(t)
	if id, ok := in.index[key]; ok {
		return id
	}
	return in.internRaw(t)
}

// internRaw adds the descriptor to the storage without consulting the map.
func (in *Interner) internRaw(t Type) TypeID {
	lenTypes, err := safecast.Conv[uint32](len(in.types))
	if err != nil {
		panic(fmt.Errorf("len(types) overflow: %w", err))
	}
	id := TypeID(lenTypes)
	in.types = append(in.types, t)
	key := typeKey(t)
	in.index[key] = id
	return id
}

// Lookup returns the descriptor for a TypeID.
func (in *Interner) Lookup(id TypeID) (Type, bool) {
	if id == NoTypeID || int(id) >= len(in.types) {
		return Type{}, false
	}
	return in.types[id], true
}

// MustLookup panics when id is invalid.
func (in *Interner) MustLookup(id TypeID) Type {
	tt, ok := in.Lookup(id)
	if !ok {
		panic("types: invalid TypeID")
	}
	return tt
}

type typeKey struct {
	Kind    Kind
	Elem    TypeID
	Count   uint32
	Width   Width
	Mutable bool
	Payload uint32
}

// IsCopy reports whether values of type id can be implicitly copied.
// Copy types include: bool, int/uint (all widths), float (all widths),
// unit, nothing, raw pointers (*T), shared references (&T), function types, and enums.
// Mutable references (&mut T), strings, structs, unions, arrays, and tuples are NOT Copy.
func (in *Interner) IsCopy(id TypeID) bool {
	if id == NoTypeID {
		return false
	}
	if in != nil && in.copyTypes != nil {
		if _, ok := in.copyTypes[id]; ok {
			return true
		}
	}
	tt, ok := in.Lookup(id)
	if !ok {
		return false
	}
	switch tt.Kind {
	case KindBool, KindInt, KindUint, KindFloat, KindConst, KindUnit, KindNothing:
		return true
	case KindPointer:
		return true // Raw pointers are Copy
	case KindReference:
		return !tt.Mutable // &T is Copy, &mut T is NOT Copy
	case KindFn:
		return true // Function pointers are Copy
	case KindEnum:
		return true // Enums are just integers
	case KindOwn:
		return in.IsCopy(tt.Elem) // own T is Copy if T is Copy
	default:
		// KindString, KindStruct, KindUnion, KindArray, KindTuple, KindAlias, KindGenericParam
		return false
	}
}

// MarkCopyType records a nominal type as Copy-capable.
func (in *Interner) MarkCopyType(id TypeID) {
	if in == nil || id == NoTypeID {
		return
	}
	if in.copyTypes == nil {
		in.copyTypes = make(map[TypeID]struct{}, 64)
	}
	in.copyTypes[id] = struct{}{}
}

// MarkRuntimePlacementType records the core runtime Placement ABI type.
func (in *Interner) MarkRuntimePlacementType(id TypeID) {
	if in == nil || id == NoTypeID {
		return
	}
	if in.placementTypes == nil {
		in.placementTypes = make(map[TypeID]struct{}, 4)
	}
	in.placementTypes[id] = struct{}{}
}

// IsRuntimePlacementType reports whether id is the core runtime Placement ABI type.
func (in *Interner) IsRuntimePlacementType(id TypeID) bool {
	if in == nil || id == NoTypeID {
		return false
	}
	const maxDepth = 32
	for range maxDepth {
		if in.placementTypes != nil {
			if _, ok := in.placementTypes[id]; ok {
				return true
			}
		}
		tt, ok := in.Lookup(id)
		if !ok || tt.Kind != KindAlias {
			return false
		}
		target, ok := in.AliasTarget(id)
		if !ok || target == NoTypeID || target == id {
			return false
		}
		id = target
	}
	return false
}

// MarkRuntimeHandleType records a core runtime-owned nominal handle family.
// The family identity includes the declaration span, so an unrelated user type
// with the same spelling is never classified as a runtime handle.
func (in *Interner) MarkRuntimeHandleType(id TypeID) {
	if in == nil || id == NoTypeID {
		return
	}
	info, ok := in.StructInfo(id)
	if !ok || info == nil {
		return
	}
	if in.runtimeHandles == nil {
		in.runtimeHandles = make(map[TypeID]struct{}, 16)
	}
	if in.runtimeFamilies == nil {
		in.runtimeFamilies = make(map[runtimeHandleFamily]struct{}, 4)
	}
	family := runtimeHandleFamily{name: info.Name, decl: info.Decl}
	in.runtimeFamilies[family] = struct{}{}
	for candidate, descriptor := range in.types {
		if descriptor.Kind != KindStruct {
			continue
		}
		candidateInfo := in.structInfo(TypeID(candidate))
		if candidateInfo != nil && candidateInfo.Name == family.name && candidateInfo.Decl == family.decl {
			in.runtimeHandles[TypeID(candidate)] = struct{}{}
		}
	}
}

// IsRuntimeHandleType reports whether id belongs to an explicitly marked core
// runtime-owned nominal handle family. It never infers ownership from a name.
func (in *Interner) IsRuntimeHandleType(id TypeID) bool {
	if in == nil || id == NoTypeID {
		return false
	}
	const maxDepth = 32
	for range maxDepth {
		if in.runtimeHandles != nil {
			if _, ok := in.runtimeHandles[id]; ok {
				return true
			}
		}
		t, ok := in.Lookup(id)
		if !ok {
			return false
		}
		switch t.Kind {
		case KindAlias:
			target, targetOK := in.AliasTarget(id)
			if !targetOK || target == NoTypeID || target == id {
				return false
			}
			id = target
		case KindOwn:
			if t.Elem == NoTypeID || t.Elem == id {
				return false
			}
			id = t.Elem
		default:
			return false
		}
	}
	return false
}

func (in *Interner) inheritRuntimeHandleFamily(id TypeID, name source.StringID, decl source.Span) {
	in.inheritRefCountedHandleFamily(id, name, decl)
	if in == nil || id == NoTypeID || in.runtimeFamilies == nil {
		return
	}
	if _, ok := in.runtimeFamilies[runtimeHandleFamily{name: name, decl: decl}]; !ok {
		return
	}
	if in.runtimeHandles == nil {
		in.runtimeHandles = make(map[TypeID]struct{}, 16)
	}
	in.runtimeHandles[id] = struct{}{}
}
