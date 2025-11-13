package types

import (
	"fmt"

	"fortio.org/safecast"
)

// Builtins stores TypeIDs for common primitive types.
type Builtins struct {
	Invalid TypeID
	Unit    TypeID
	Nothing TypeID
	Bool    TypeID
	String  TypeID
	Int     TypeID
	Uint    TypeID
	Float   TypeID
}

// Interner provides stable TypeIDs by hashing structural descriptors.
type Interner struct {
	types    []Type
	index    map[typeKey]TypeID
	builtins Builtins
	structs  []StructInfo
	aliases  []AliasInfo
}

// NewInterner constructs an interner seeded with built-in primitives.
func NewInterner() *Interner {
	in := &Interner{
		index: make(map[typeKey]TypeID, 64),
	}
	in.structs = append(in.structs, StructInfo{}) // reserve 0 as invalid sentinel
	in.aliases = append(in.aliases, AliasInfo{})
	in.builtins.Invalid = in.internRaw(Type{Kind: KindInvalid})
	in.builtins.Unit = in.Intern(Type{Kind: KindUnit})
	in.builtins.Nothing = in.Intern(Type{Kind: KindNothing})
	in.builtins.Bool = in.Intern(Type{Kind: KindBool})
	in.builtins.String = in.Intern(Type{Kind: KindString})
	in.builtins.Int = in.Intern(MakeInt(WidthAny))
	in.builtins.Uint = in.Intern(MakeUint(WidthAny))
	in.builtins.Float = in.Intern(MakeFloat(WidthAny))
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
