package vm

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/source"
	"surge/internal/types"
)

func typeLabel(typesIn *types.Interner, id types.TypeID) string {
	return typeLabelDepth(typesIn, id, 0)
}

func typeLabelDepth(typesIn *types.Interner, id types.TypeID, depth int) string {
	if id == types.NoTypeID {
		return "?"
	}
	if depth > 6 {
		return "..."
	}
	if typesIn == nil {
		return "?"
	}
	tt, ok := typesIn.Lookup(id)
	if !ok {
		return "?"
	}
	switch tt.Kind {
	case types.KindUnit:
		return "()"
	case types.KindNothing:
		return "nothing"
	case types.KindBool:
		return "bool"
	case types.KindString:
		return "string"
	case types.KindInt:
		return formatIntType(tt.Width, true)
	case types.KindUint:
		return formatIntType(tt.Width, false)
	case types.KindFloat:
		return formatFloatType(tt.Width)
	case types.KindConst:
		return fmt.Sprintf("const %d", tt.Count)
	case types.KindPointer:
		return "*" + typeLabelDepth(typesIn, tt.Elem, depth+1)
	case types.KindReference:
		if tt.Mutable {
			return "&mut " + typeLabelDepth(typesIn, tt.Elem, depth+1)
		}
		return "&" + typeLabelDepth(typesIn, tt.Elem, depth+1)
	case types.KindOwn:
		return "own " + typeLabelDepth(typesIn, tt.Elem, depth+1)
	case types.KindArray:
		elem := typeLabelDepth(typesIn, tt.Elem, depth+1)
		if tt.Count == types.ArrayDynamicLength {
			return "[" + elem + "]"
		}
		return fmt.Sprintf("[%s; %d]", elem, tt.Count)
	case types.KindStruct:
		return formatStructType(typesIn, id, depth)
	case types.KindAlias:
		return formatAliasType(typesIn, id, depth)
	case types.KindUnion:
		return formatUnionType(typesIn, id, depth)
	case types.KindEnum:
		return formatEnumType(typesIn, id, depth)
	case types.KindTuple:
		info, ok := typesIn.TupleInfo(id)
		if !ok || info == nil {
			return "(?)"
		}
		parts := make([]string, len(info.Elems))
		for i, elem := range info.Elems {
			parts[i] = typeLabelDepth(typesIn, elem, depth+1)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case types.KindFn:
		info, ok := typesIn.FnInfo(id)
		if !ok || info == nil {
			return "fn(?)"
		}
		params := make([]string, len(info.Params))
		for i, param := range info.Params {
			params[i] = typeLabelDepth(typesIn, param, depth+1)
		}
		ret := typeLabelDepth(typesIn, info.Result, depth+1)
		return "fn(" + strings.Join(params, ", ") + ") -> " + ret
	case types.KindGenericParam:
		if info, ok := typesIn.TypeParamInfo(id); ok && info != nil {
			if name, ok := lookupName(typesIn.Strings, info.Name); ok {
				return name
			}
		}
		return "T"
	default:
		return "?"
	}
}

func formatStructType(typesIn *types.Interner, id types.TypeID, depth int) string {
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil {
		return "?"
	}
	name := lookupNameFallback(typesIn.Strings, info.Name)
	args := make([]string, 0, len(info.TypeArgs)+len(info.ValueArgs))
	for _, arg := range typesIn.StructArgs(id) {
		args = append(args, typeLabelDepth(typesIn, arg, depth+1))
	}
	for _, v := range typesIn.StructValueArgs(id) {
		args = append(args, strconv.FormatUint(v, 10))
	}
	if len(args) == 0 {
		return name
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

func formatAliasType(typesIn *types.Interner, id types.TypeID, depth int) string {
	info, ok := typesIn.AliasInfo(id)
	if !ok || info == nil {
		return "?"
	}
	name := lookupNameFallback(typesIn.Strings, info.Name)
	args := make([]string, 0, len(info.TypeArgs))
	for _, arg := range typesIn.AliasArgs(id) {
		args = append(args, typeLabelDepth(typesIn, arg, depth+1))
	}
	if len(args) == 0 {
		return name
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

func formatUnionType(typesIn *types.Interner, id types.TypeID, depth int) string {
	info, ok := typesIn.UnionInfo(id)
	if !ok || info == nil {
		return "?"
	}
	name := lookupNameFallback(typesIn.Strings, info.Name)
	args := make([]string, 0, len(info.TypeArgs))
	for _, arg := range typesIn.UnionArgs(id) {
		args = append(args, typeLabelDepth(typesIn, arg, depth+1))
	}
	if len(args) == 0 {
		return name
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

func formatEnumType(typesIn *types.Interner, id types.TypeID, depth int) string {
	info, ok := typesIn.EnumInfo(id)
	if !ok || info == nil {
		return "?"
	}
	name := lookupNameFallback(typesIn.Strings, info.Name)
	args := make([]string, 0, len(info.TypeArgs))
	for _, arg := range typesIn.EnumArgs(id) {
		args = append(args, typeLabelDepth(typesIn, arg, depth+1))
	}
	if len(args) == 0 {
		return name
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

func lookupName(strings *source.Interner, id source.StringID) (string, bool) {
	if strings == nil {
		return "", false
	}
	name, ok := strings.Lookup(id)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

func lookupNameFallback(strings *source.Interner, id source.StringID) string {
	if name, ok := lookupName(strings, id); ok {
		return name
	}
	return "?"
}

func formatIntType(width types.Width, signed bool) string {
	prefix := "int"
	if !signed {
		prefix = "uint"
	}
	if width == types.WidthAny {
		return prefix
	}
	return fmt.Sprintf("%s%d", prefix, width)
}

func formatFloatType(width types.Width) string {
	if width == types.WidthAny {
		return "float"
	}
	return fmt.Sprintf("float%d", width)
}
