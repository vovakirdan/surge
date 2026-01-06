package types //nolint:revive

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/source"
)

// EnumVariantInfo stores metadata for a single enum variant.
type EnumVariantInfo struct {
	Name        source.StringID
	IntValue    int64
	StringValue source.StringID
	IsString    bool
	Span        source.Span
}

// EnumInfo stores metadata for an enum type.
type EnumInfo struct {
	Name     source.StringID
	Decl     source.Span
	BaseType TypeID
	Variants []EnumVariantInfo
	TypeArgs []TypeID
}

// RegisterEnum allocates a nominal enum type slot and returns its TypeID.
func (in *Interner) RegisterEnum(name source.StringID, decl source.Span) TypeID {
	slot := in.appendEnumInfo(EnumInfo{Name: name, Decl: decl})
	return in.internRaw(Type{Kind: KindEnum, Payload: slot})
}

// RegisterEnumInstance allocates an enum instantiation with concrete type arguments.
func (in *Interner) RegisterEnumInstance(name source.StringID, decl source.Span, args []TypeID) TypeID {
	slot := in.appendEnumInfo(EnumInfo{Name: name, Decl: decl, TypeArgs: cloneTypeArgs(args)})
	return in.internRaw(Type{Kind: KindEnum, Payload: slot})
}

// SetEnumBaseType stores the base type for the enum.
func (in *Interner) SetEnumBaseType(typeID, baseType TypeID) {
	info := in.enumInfo(typeID)
	if info == nil {
		return
	}
	info.BaseType = baseType
}

// SetEnumVariants stores the resolved variants for the enum type.
func (in *Interner) SetEnumVariants(typeID TypeID, variants []EnumVariantInfo) {
	info := in.enumInfo(typeID)
	if info == nil {
		return
	}
	info.Variants = cloneEnumVariants(variants)
}

// EnumInfo returns metadata for the provided enum TypeID.
func (in *Interner) EnumInfo(typeID TypeID) (*EnumInfo, bool) {
	info := in.enumInfo(typeID)
	if info == nil {
		return nil, false
	}
	return info, true
}

// EnumArgs returns type arguments for the enum instantiation.
func (in *Interner) EnumArgs(typeID TypeID) []TypeID {
	info := in.enumInfo(typeID)
	if info == nil || len(info.TypeArgs) == 0 {
		return nil
	}
	return cloneTypeArgs(info.TypeArgs)
}

func (in *Interner) enumInfo(typeID TypeID) *EnumInfo {
	if typeID == NoTypeID {
		return nil
	}
	tt, ok := in.Lookup(typeID)
	if !ok || tt.Kind != KindEnum {
		return nil
	}
	if tt.Payload == 0 || int(tt.Payload) >= len(in.enums) {
		return nil
	}
	return &in.enums[tt.Payload]
}

func (in *Interner) appendEnumInfo(info EnumInfo) uint32 {
	if in.enums == nil {
		in.enums = append(in.enums, EnumInfo{})
	}
	in.enums = append(in.enums, EnumInfo{
		Name:     info.Name,
		Decl:     info.Decl,
		BaseType: info.BaseType,
		Variants: cloneEnumVariants(info.Variants),
		TypeArgs: cloneTypeArgs(info.TypeArgs),
	})
	slot, err := safecast.Conv[uint32](len(in.enums) - 1)
	if err != nil {
		panic(fmt.Errorf("enum info overflow: %w", err))
	}
	return slot
}

func cloneEnumVariants(variants []EnumVariantInfo) []EnumVariantInfo {
	if len(variants) == 0 {
		return nil
	}
	result := make([]EnumVariantInfo, len(variants))
	copy(result, variants)
	return result
}
