package types

import (
	"fmt"
	"strconv"
	"strings"

	"surge/internal/source"
)

// Label returns a user-friendly label for a TypeID.
func Label(typesIn *Interner, id TypeID) string {
	return labelDepth(typesIn, id, 0)
}

func labelDepth(typesIn *Interner, id TypeID, depth int) string {
	if id == NoTypeID {
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
	case KindUnit:
		return "()"
	case KindNothing:
		return "nothing"
	case KindBool:
		return "bool"
	case KindString:
		return "string"
	case KindInt:
		return formatIntType(tt.Width, true)
	case KindUint:
		return formatIntType(tt.Width, false)
	case KindFloat:
		return formatFloatType(tt.Width)
	case KindConst:
		return fmt.Sprintf("const %d", tt.Count)
	case KindPointer:
		return "*" + labelDepth(typesIn, tt.Elem, depth+1)
	case KindReference:
		if tt.Mutable {
			return "&mut " + labelDepth(typesIn, tt.Elem, depth+1)
		}
		return "&" + labelDepth(typesIn, tt.Elem, depth+1)
	case KindOwn:
		return "own " + labelDepth(typesIn, tt.Elem, depth+1)
	case KindArray:
		elem := labelDepth(typesIn, tt.Elem, depth+1)
		if tt.Count == ArrayDynamicLength {
			return "[" + elem + "]"
		}
		return fmt.Sprintf("[%s; %d]", elem, tt.Count)
	case KindStruct:
		return formatStructType(typesIn, id, depth)
	case KindAlias:
		return formatAliasType(typesIn, id, depth)
	case KindUnion:
		return formatUnionType(typesIn, id, depth)
	case KindEnum:
		return formatEnumType(typesIn, id, depth)
	case KindTuple:
		info, ok := typesIn.TupleInfo(id)
		if !ok || info == nil {
			return "(?)"
		}
		parts := make([]string, len(info.Elems))
		for i, elem := range info.Elems {
			parts[i] = labelDepth(typesIn, elem, depth+1)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case KindFn:
		info, ok := typesIn.FnInfo(id)
		if !ok || info == nil {
			return "fn(?)"
		}
		params := make([]string, len(info.Params))
		for i, param := range info.Params {
			params[i] = labelDepth(typesIn, param, depth+1)
		}
		ret := labelDepth(typesIn, info.Result, depth+1)
		return "fn(" + strings.Join(params, ", ") + ") -> " + ret
	case KindGenericParam:
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

func formatStructType(typesIn *Interner, id TypeID, depth int) string {
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil {
		return "?"
	}
	name := lookupNameFallback(typesIn.Strings, info.Name)
	args := make([]string, 0, len(info.TypeArgs)+len(info.ValueArgs))
	for _, arg := range typesIn.StructArgs(id) {
		args = append(args, labelDepth(typesIn, arg, depth+1))
	}
	for _, v := range typesIn.StructValueArgs(id) {
		args = append(args, strconv.FormatUint(v, 10))
	}
	if len(args) == 0 {
		return name
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

func formatAliasType(typesIn *Interner, id TypeID, depth int) string {
	info, ok := typesIn.AliasInfo(id)
	if !ok || info == nil {
		return "?"
	}
	name := lookupNameFallback(typesIn.Strings, info.Name)
	args := make([]string, 0, len(info.TypeArgs))
	for _, arg := range typesIn.AliasArgs(id) {
		args = append(args, labelDepth(typesIn, arg, depth+1))
	}
	if len(args) == 0 {
		return name
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

func formatUnionType(typesIn *Interner, id TypeID, depth int) string {
	info, ok := typesIn.UnionInfo(id)
	if !ok || info == nil {
		return "?"
	}
	name := lookupNameFallback(typesIn.Strings, info.Name)
	args := make([]string, 0, len(info.TypeArgs))
	for _, arg := range typesIn.UnionArgs(id) {
		args = append(args, labelDepth(typesIn, arg, depth+1))
	}
	if len(args) == 0 {
		return name
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

func formatEnumType(typesIn *Interner, id TypeID, depth int) string {
	info, ok := typesIn.EnumInfo(id)
	if !ok || info == nil {
		return "?"
	}
	name := lookupNameFallback(typesIn.Strings, info.Name)
	args := make([]string, 0, len(info.TypeArgs))
	for _, arg := range typesIn.EnumArgs(id) {
		args = append(args, labelDepth(typesIn, arg, depth+1))
	}
	if len(args) == 0 {
		return name
	}
	return name + "<" + strings.Join(args, ", ") + ">"
}

func lookupName(stringsIn *source.Interner, id source.StringID) (string, bool) {
	if stringsIn == nil {
		return "", false
	}
	name, ok := stringsIn.Lookup(id)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

func lookupNameFallback(stringsIn *source.Interner, id source.StringID) string {
	if name, ok := lookupName(stringsIn, id); ok {
		return name
	}
	return "?"
}

func formatIntType(width Width, signed bool) string {
	prefix := "int"
	if !signed {
		prefix = "uint"
	}
	if width == WidthAny {
		return prefix
	}
	return fmt.Sprintf("%s%d", prefix, width)
}

func formatFloatType(width Width) string {
	if width == WidthAny {
		return "float"
	}
	return fmt.Sprintf("float%d", width)
}
