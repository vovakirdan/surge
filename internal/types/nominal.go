package types

import (
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/source"
)

// StructField describes a single field inside a nominal struct type.
type StructField struct {
	Name  source.StringID
	Type  TypeID
	Attrs []source.StringID
}

// StructInfo stores metadata for a struct type.
type StructInfo struct {
	Name       source.StringID
	Decl       source.Span
	Fields     []StructField
	TypeParams []TypeID
	TypeArgs   []TypeID
	ValueArgs  []uint64
}

// AliasInfo stores metadata for a nominal alias type.
type AliasInfo struct {
	Name     source.StringID
	Decl     source.Span
	Target   TypeID
	TypeArgs []TypeID
}

// RegisterStruct allocates a nominal struct type slot and returns its TypeID.
func (in *Interner) RegisterStruct(name source.StringID, decl source.Span) TypeID {
	slot := in.appendStructInfo(&StructInfo{Name: name, Decl: decl})
	return in.internRaw(Type{Kind: KindStruct, Payload: slot})
}

// RegisterStructInstance allocates a nominal struct instantiation with type arguments.
func (in *Interner) RegisterStructInstance(name source.StringID, decl source.Span, args []TypeID) TypeID {
	slot := in.appendStructInfo(&StructInfo{Name: name, Decl: decl, TypeArgs: cloneTypeArgs(args)})
	return in.internRaw(Type{Kind: KindStruct, Payload: slot})
}

// RegisterStructInstanceWithValues allocates a nominal struct instantiation with type and value arguments.
func (in *Interner) RegisterStructInstanceWithValues(name source.StringID, decl source.Span, args []TypeID, values []uint64) TypeID {
	slot := in.appendStructInfo(&StructInfo{
		Name:      name,
		Decl:      decl,
		TypeArgs:  cloneTypeArgs(args),
		ValueArgs: cloneValueArgs(values),
	})
	return in.internRaw(Type{Kind: KindStruct, Payload: slot})
}

// SetStructFields stores the resolved field descriptors for the struct type.
func (in *Interner) SetStructFields(typeID TypeID, fields []StructField) {
	info := in.structInfo(typeID)
	if info == nil {
		return
	}
	info.Fields = cloneStructFields(fields)
}

// SetStructTypeParams records the generic parameters used by the struct definition.
func (in *Interner) SetStructTypeParams(typeID TypeID, params []TypeID) {
	info := in.structInfo(typeID)
	if info == nil {
		return
	}
	info.TypeParams = cloneTypeArgs(params)
}

// StructInfo returns metadata for the provided struct TypeID.
func (in *Interner) StructInfo(typeID TypeID) (*StructInfo, bool) {
	info := in.structInfo(typeID)
	if info == nil {
		return nil, false
	}
	return info, true
}

// StructFields returns a copy of struct fields for the TypeID.
func (in *Interner) StructFields(typeID TypeID) []StructField {
	info := in.structInfo(typeID)
	if info == nil || len(info.Fields) == 0 {
		return nil
	}
	return cloneStructFields(info.Fields)
}

// StructArgs returns type arguments for the struct instantiation.
func (in *Interner) StructArgs(typeID TypeID) []TypeID {
	info := in.structInfo(typeID)
	if info == nil || len(info.TypeArgs) == 0 {
		return nil
	}
	return cloneTypeArgs(info.TypeArgs)
}

// SetStructValueArgs records the value arguments used by the struct instantiation.
func (in *Interner) SetStructValueArgs(typeID TypeID, args []uint64) {
	info := in.structInfo(typeID)
	if info == nil {
		return
	}
	info.ValueArgs = cloneValueArgs(args)
}

// StructValueArgs returns value arguments for the struct instantiation.
func (in *Interner) StructValueArgs(typeID TypeID) []uint64 {
	info := in.structInfo(typeID)
	if info == nil || len(info.ValueArgs) == 0 {
		return nil
	}
	return cloneValueArgs(info.ValueArgs)
}

// RegisterAlias allocates a nominal alias type slot and returns its TypeID.
func (in *Interner) RegisterAlias(name source.StringID, decl source.Span) TypeID {
	slot := in.appendAliasInfo(AliasInfo{Name: name, Decl: decl})
	return in.internRaw(Type{Kind: KindAlias, Payload: slot})
}

// RegisterAliasInstance allocates a nominal alias instantiation with type arguments.
func (in *Interner) RegisterAliasInstance(name source.StringID, decl source.Span, args []TypeID) TypeID {
	slot := in.appendAliasInfo(AliasInfo{Name: name, Decl: decl, TypeArgs: cloneTypeArgs(args)})
	return in.internRaw(Type{Kind: KindAlias, Payload: slot})
}

// SetAliasTarget sets the aliased target type for the provided alias TypeID.
func (in *Interner) SetAliasTarget(typeID, target TypeID) {
	info := in.aliasInfo(typeID)
	if info == nil {
		return
	}
	info.Target = target
}

// AliasTarget retrieves the aliased target type.
func (in *Interner) AliasTarget(typeID TypeID) (TypeID, bool) {
	info := in.aliasInfo(typeID)
	if info == nil || info.Target == NoTypeID {
		return NoTypeID, false
	}
	return info.Target, true
}

// AliasArgs returns type arguments for the alias instantiation.
func (in *Interner) AliasArgs(typeID TypeID) []TypeID {
	info := in.aliasInfo(typeID)
	if info == nil || len(info.TypeArgs) == 0 {
		return nil
	}
	return cloneTypeArgs(info.TypeArgs)
}

// AliasInfo returns metadata for the provided alias TypeID.
func (in *Interner) AliasInfo(typeID TypeID) (*AliasInfo, bool) {
	info := in.aliasInfo(typeID)
	if info == nil {
		return nil, false
	}
	return info, true
}

func (in *Interner) structInfo(typeID TypeID) *StructInfo {
	if typeID == NoTypeID {
		return nil
	}
	tt, ok := in.Lookup(typeID)
	if !ok || tt.Kind != KindStruct {
		return nil
	}
	if tt.Payload == 0 || int(tt.Payload) >= len(in.structs) {
		return nil
	}
	return &in.structs[tt.Payload]
}

func (in *Interner) aliasInfo(typeID TypeID) *AliasInfo {
	if typeID == NoTypeID {
		return nil
	}
	tt, ok := in.Lookup(typeID)
	if !ok || tt.Kind != KindAlias {
		return nil
	}
	if tt.Payload == 0 || int(tt.Payload) >= len(in.aliases) {
		return nil
	}
	return &in.aliases[tt.Payload]
}

func (in *Interner) appendStructInfo(info *StructInfo) uint32 {
	if in.structs == nil {
		in.structs = append(in.structs, StructInfo{})
	}
	if info == nil {
		info = &StructInfo{}
	}
	in.structs = append(in.structs, StructInfo{
		Name:       info.Name,
		Decl:       info.Decl,
		Fields:     cloneStructFields(info.Fields),
		TypeParams: cloneTypeArgs(info.TypeParams),
		TypeArgs:   cloneTypeArgs(info.TypeArgs),
		ValueArgs:  cloneValueArgs(info.ValueArgs),
	})
	slot, err := safecast.Conv[uint32](len(in.structs) - 1)
	if err != nil {
		panic(fmt.Errorf("struct info overflow: %w", err))
	}
	return slot
}

func (in *Interner) appendAliasInfo(info AliasInfo) uint32 {
	if in.aliases == nil {
		in.aliases = append(in.aliases, AliasInfo{})
	}
	in.aliases = append(in.aliases, AliasInfo{
		Name:     info.Name,
		Decl:     info.Decl,
		Target:   info.Target,
		TypeArgs: cloneTypeArgs(info.TypeArgs),
	})
	slot, err := safecast.Conv[uint32](len(in.aliases) - 1)
	if err != nil {
		panic(fmt.Errorf("alias info overflow: %w", err))
	}
	return slot
}

func cloneStructFields(fields []StructField) []StructField {
	if len(fields) == 0 {
		return nil
	}
	clone := make([]StructField, len(fields))
	for i, f := range fields {
		clone[i] = f
		if len(f.Attrs) > 0 {
			clone[i].Attrs = slices.Clone(f.Attrs)
		}
	}
	return clone
}

func cloneTypeArgs(args []TypeID) []TypeID {
	if len(args) == 0 {
		return nil
	}
	return slices.Clone(args)
}

func cloneValueArgs(args []uint64) []uint64 {
	if len(args) == 0 {
		return nil
	}
	return slices.Clone(args)
}
