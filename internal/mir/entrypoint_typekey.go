package mir

import (
	"fmt"
	"strings"

	"surge/internal/symbols"
	"surge/internal/types"
)

// typeKeyForType returns the TypeKey string for a given TypeID.
func (b *surgeStartBuilder) typeKeyForType(id types.TypeID) symbols.TypeKey {
	if b.typesIn == nil || id == types.NoTypeID {
		return ""
	}
	id = b.resolveAlias(id)
	if elem, length, ok := b.typesIn.ArrayFixedInfo(id); ok {
		inner := b.typeKeyForType(elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		return symbols.TypeKey("[" + string(inner) + "; " + fmt.Sprintf("%d", length) + "]")
	}
	if elem, ok := b.typesIn.ArrayInfo(id); ok {
		inner := b.typeKeyForType(elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		return symbols.TypeKey("[" + string(inner) + "]")
	}
	tt, ok := b.typesIn.Lookup(id)
	if !ok {
		return ""
	}
	switch tt.Kind {
	case types.KindInt:
		switch tt.Width {
		case types.Width8:
			return "int8"
		case types.Width16:
			return "int16"
		case types.Width32:
			return "int32"
		case types.Width64:
			return "int64"
		default:
			return "int"
		}
	case types.KindUint:
		switch tt.Width {
		case types.Width8:
			return "uint8"
		case types.Width16:
			return "uint16"
		case types.Width32:
			return "uint32"
		case types.Width64:
			return "uint64"
		default:
			return "uint"
		}
	case types.KindFloat:
		switch tt.Width {
		case types.Width16:
			return "float16"
		case types.Width32:
			return "float32"
		case types.Width64:
			return "float64"
		default:
			return "float"
		}
	case types.KindBool:
		return "bool"
	case types.KindString:
		return "string"
	case types.KindStruct:
		info, ok := b.typesIn.StructInfo(id)
		if ok && info != nil && b.mm != nil && b.mm.Source != nil && b.mm.Source.Symbols != nil && b.mm.Source.Symbols.Table != nil {
			if name, ok := b.mm.Source.Symbols.Table.Strings.Lookup(info.Name); ok && name != "" {
				return b.typeKeyWithArgs(name, info.TypeArgs)
			}
		}
	case types.KindAlias:
		if info, ok := b.typesIn.AliasInfo(id); ok && info != nil && b.mm != nil && b.mm.Source != nil && b.mm.Source.Symbols != nil && b.mm.Source.Symbols.Table != nil {
			if name, ok := b.mm.Source.Symbols.Table.Strings.Lookup(info.Name); ok && name != "" {
				return b.typeKeyWithArgs(name, info.TypeArgs)
			}
		}
	case types.KindUnion:
		if info, ok := b.typesIn.UnionInfo(id); ok && info != nil && b.mm != nil && b.mm.Source != nil && b.mm.Source.Symbols != nil && b.mm.Source.Symbols.Table != nil {
			if name, ok := b.mm.Source.Symbols.Table.Strings.Lookup(info.Name); ok && name != "" {
				return b.typeKeyWithArgs(name, info.TypeArgs)
			}
		}
	}
	return ""
}

func (b *surgeStartBuilder) typeKeyWithArgs(name string, args []types.TypeID) symbols.TypeKey {
	if len(args) == 0 {
		return symbols.TypeKey(name)
	}
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		if key := b.typeKeyForType(arg); key != "" {
			parts = append(parts, string(key))
		}
	}
	if len(parts) == 0 {
		return symbols.TypeKey(name)
	}
	return symbols.TypeKey(name + "<" + strings.Join(parts, ",") + ">")
}
