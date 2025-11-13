package types

import (
	"fmt"
	"slices"

	"fortio.org/safecast"

	"surge/internal/source"
)

// StructField describes a single field inside a nominal struct type.
type StructField struct {
	Name source.StringID
	Type TypeID
}

// StructInfo stores metadata for a struct type.
type StructInfo struct {
	Name   source.StringID
	Decl   source.Span
	Fields []StructField
}

// AliasInfo stores metadata for a nominal alias type.
type AliasInfo struct {
	Name   source.StringID
	Decl   source.Span
	Target TypeID
}

// RegisterStruct allocates a nominal struct type slot and returns its TypeID.
func (in *Interner) RegisterStruct(name source.StringID, decl source.Span) TypeID {
	slot := in.appendStructInfo(StructInfo{Name: name, Decl: decl})
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

// RegisterAlias allocates a nominal alias type slot and returns its TypeID.
func (in *Interner) RegisterAlias(name source.StringID, decl source.Span) TypeID {
	slot := in.appendAliasInfo(AliasInfo{Name: name, Decl: decl})
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

func (in *Interner) appendStructInfo(info StructInfo) uint32 {
	if in.structs == nil {
		in.structs = append(in.structs, StructInfo{})
	}
	in.structs = append(in.structs, StructInfo{
		Name:   info.Name,
		Decl:   info.Decl,
		Fields: cloneStructFields(info.Fields),
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
	in.aliases = append(in.aliases, info)
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
	return slices.Clone(fields)
}
