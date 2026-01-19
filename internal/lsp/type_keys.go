package lsp

import (
	"fmt"
	"strings"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func typeKeyCandidates(interner *types.Interner, id types.TypeID) []symbols.TypeKey {
	if interner == nil || id == types.NoTypeID {
		return nil
	}
	candidates := make([]symbols.TypeKey, 0, 6)
	add := func(key symbols.TypeKey) {
		if key == "" {
			return
		}
		for _, existing := range candidates {
			if typeKeyEqual(existing, key) {
				return
			}
		}
		candidates = append(candidates, key)
	}
	add(typeKeyForType(interner, id))
	if base := valueType(interner, id); base != types.NoTypeID && base != id {
		add(typeKeyForType(interner, base))
	}
	if aliasBase := aliasBaseType(interner, id); aliasBase != types.NoTypeID && aliasBase != id {
		add(typeKeyForType(interner, aliasBase))
	}
	if base := valueType(interner, id); base != types.NoTypeID {
		if structBase, ok := interner.StructBase(base); ok && structBase != types.NoTypeID {
			add(typeKeyForType(interner, structBase))
		}
	}
	if elem, _, ok := interner.ArrayFixedInfo(id); ok {
		if inner := typeKeyForType(interner, elem); inner != "" {
			add(symbols.TypeKey("[" + string(inner) + "]"))
		}
	}
	add(familyKeyForType(interner, id))
	return candidates
}

func typeKeyForType(interner *types.Interner, id types.TypeID) symbols.TypeKey {
	if interner == nil || id == types.NoTypeID {
		return ""
	}
	if elem, length, ok := interner.ArrayFixedInfo(id); ok {
		inner := typeKeyForType(interner, elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		if length > 0 {
			return symbols.TypeKey("[" + string(inner) + "; " + fmt.Sprintf("%d", length) + "]")
		}
		return symbols.TypeKey("[" + string(inner) + "]")
	}
	if elem, ok := interner.ArrayInfo(id); ok {
		inner := typeKeyForType(interner, elem)
		if inner == "" {
			return symbols.TypeKey("[]")
		}
		return symbols.TypeKey("[" + string(inner) + "]")
	}

	tt, ok := interner.Lookup(id)
	if !ok {
		return ""
	}
	switch tt.Kind {
	case types.KindBool:
		return symbols.TypeKey("bool")
	case types.KindInt:
		switch tt.Width {
		case types.Width8:
			return symbols.TypeKey("int8")
		case types.Width16:
			return symbols.TypeKey("int16")
		case types.Width32:
			return symbols.TypeKey("int32")
		case types.Width64:
			return symbols.TypeKey("int64")
		default:
			return symbols.TypeKey("int")
		}
	case types.KindUint:
		switch tt.Width {
		case types.Width8:
			return symbols.TypeKey("uint8")
		case types.Width16:
			return symbols.TypeKey("uint16")
		case types.Width32:
			return symbols.TypeKey("uint32")
		case types.Width64:
			return symbols.TypeKey("uint64")
		default:
			return symbols.TypeKey("uint")
		}
	case types.KindFloat:
		switch tt.Width {
		case types.Width16:
			return symbols.TypeKey("float16")
		case types.Width32:
			return symbols.TypeKey("float32")
		case types.Width64:
			return symbols.TypeKey("float64")
		default:
			return symbols.TypeKey("float")
		}
	case types.KindString:
		return symbols.TypeKey("string")
	case types.KindConst:
		return symbols.TypeKey(fmt.Sprintf("%d", tt.Count))
	case types.KindGenericParam:
		if info, ok := interner.TypeParamInfo(id); ok && info != nil {
			if name := lookupTypeName(interner.Strings, info.Name); name != "" {
				return symbols.TypeKey(name)
			}
		}
	case types.KindReference:
		if inner := typeKeyForType(interner, tt.Elem); inner != "" {
			if tt.Mutable {
				return symbols.TypeKey("&mut " + string(inner))
			}
			return symbols.TypeKey("&" + string(inner))
		}
	case types.KindOwn:
		if inner := typeKeyForType(interner, tt.Elem); inner != "" {
			return symbols.TypeKey("own " + string(inner))
		}
	case types.KindPointer:
		if inner := typeKeyForType(interner, tt.Elem); inner != "" {
			return symbols.TypeKey("*" + string(inner))
		}
	case types.KindArray:
		if inner := typeKeyForType(interner, tt.Elem); inner != "" {
			return symbols.TypeKey("[" + string(inner) + "]")
		}
		return symbols.TypeKey("[]")
	case types.KindNothing:
		return symbols.TypeKey("nothing")
	case types.KindUnit:
		return symbols.TypeKey("unit")
	case types.KindStruct:
		if info, ok := interner.StructInfo(id); ok && info != nil {
			name := lookupTypeName(interner.Strings, info.Name)
			if name == "" {
				return ""
			}
			args := typeArgsKeys(interner, info.TypeArgs)
			if len(args) == 0 {
				return symbols.TypeKey(name)
			}
			return symbols.TypeKey(name + "<" + strings.Join(args, ",") + ">")
		}
	case types.KindAlias:
		if info, ok := interner.AliasInfo(id); ok && info != nil {
			name := lookupTypeName(interner.Strings, info.Name)
			if name == "" {
				return ""
			}
			args := typeArgsKeys(interner, info.TypeArgs)
			if len(args) == 0 {
				return symbols.TypeKey(name)
			}
			return symbols.TypeKey(name + "<" + strings.Join(args, ",") + ">")
		}
	case types.KindUnion:
		if info, ok := interner.UnionInfo(id); ok && info != nil {
			name := lookupTypeName(interner.Strings, info.Name)
			if name == "" {
				return ""
			}
			args := typeArgsKeys(interner, info.TypeArgs)
			if len(args) == 0 {
				return symbols.TypeKey(name)
			}
			return symbols.TypeKey(name + "<" + strings.Join(args, ",") + ">")
		}
	case types.KindEnum:
		if info, ok := interner.EnumInfo(id); ok && info != nil {
			name := lookupTypeName(interner.Strings, info.Name)
			if name == "" {
				return ""
			}
			args := typeArgsKeys(interner, info.TypeArgs)
			if len(args) == 0 {
				return symbols.TypeKey(name)
			}
			return symbols.TypeKey(name + "<" + strings.Join(args, ",") + ">")
		}
	case types.KindTuple:
		if info, ok := interner.TupleInfo(id); ok && info != nil {
			elems := make([]string, 0, len(info.Elems))
			for _, elem := range info.Elems {
				if key := typeKeyForType(interner, elem); key != "" {
					elems = append(elems, string(key))
				}
			}
			return symbols.TypeKey("(" + strings.Join(elems, ",") + ")")
		}
		return symbols.TypeKey("()")
	case types.KindFn:
		if info, ok := interner.FnInfo(id); ok && info != nil {
			params := make([]string, 0, len(info.Params))
			for _, param := range info.Params {
				if key := typeKeyForType(interner, param); key != "" {
					params = append(params, string(key))
				}
			}
			resultKey := typeKeyForType(interner, info.Result)
			if resultKey == "" {
				resultKey = symbols.TypeKey("nothing")
			}
			return symbols.TypeKey("fn(" + strings.Join(params, ",") + ")->" + string(resultKey))
		}
		return symbols.TypeKey("fn()")
	}
	return ""
}

func familyKeyForType(interner *types.Interner, id types.TypeID) symbols.TypeKey {
	if interner == nil || id == types.NoTypeID {
		return ""
	}
	tt, ok := interner.Lookup(valueType(interner, id))
	if !ok {
		return ""
	}
	switch tt.Kind {
	case types.KindInt:
		return symbols.TypeKey("int")
	case types.KindUint:
		return symbols.TypeKey("uint")
	case types.KindFloat:
		return symbols.TypeKey("float")
	default:
		return ""
	}
}

func aliasBaseType(interner *types.Interner, id types.TypeID) types.TypeID {
	if interner == nil || id == types.NoTypeID {
		return types.NoTypeID
	}
	seen := make(map[types.TypeID]struct{}, 8)
	current := id
	for current != types.NoTypeID {
		if _, ok := seen[current]; ok {
			return types.NoTypeID
		}
		seen[current] = struct{}{}
		tt, ok := interner.Lookup(current)
		if !ok || tt.Kind != types.KindAlias {
			if current != id {
				return current
			}
			return types.NoTypeID
		}
		target, ok := interner.AliasTarget(current)
		if !ok || target == types.NoTypeID || target == current {
			if current != id {
				return current
			}
			return types.NoTypeID
		}
		current = target
	}
	return types.NoTypeID
}

func lookupTypeName(interner *source.Interner, id source.StringID) string {
	if interner == nil || id == source.NoStringID {
		return ""
	}
	name, ok := interner.Lookup(id)
	if !ok {
		return ""
	}
	return name
}

func typeArgsKeys(interner *types.Interner, args []types.TypeID) []string {
	if interner == nil || len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if key := typeKeyForType(interner, arg); key != "" {
			out = append(out, string(key))
		}
	}
	return out
}

func typeKeyMatchesWithGenerics(a, b symbols.TypeKey) bool {
	if typeKeyEqual(a, b) {
		return true
	}
	return genericTypeKeyCompatible(a, b) || genericTypeKeyCompatible(b, a)
}

func typeKeyEqual(a, b symbols.TypeKey) bool {
	return canonicalTypeKey(a) == canonicalTypeKey(b)
}

func canonicalTypeKey(key symbols.TypeKey) symbols.TypeKey {
	if key == "" {
		return ""
	}
	s := strings.TrimSpace(string(key))
	prefix := ""
	switch {
	case strings.HasPrefix(s, "&mut "):
		prefix = "&mut "
		s = strings.TrimSpace(strings.TrimPrefix(s, "&mut "))
	case strings.HasPrefix(s, "&"):
		prefix = "&"
		s = strings.TrimSpace(strings.TrimPrefix(s, "&"))
	case strings.HasPrefix(s, "own "):
		prefix = "own "
		s = strings.TrimSpace(strings.TrimPrefix(s, "own "))
	case strings.HasPrefix(s, "*"):
		prefix = "*"
		s = strings.TrimSpace(strings.TrimPrefix(s, "*"))
	}
	if _, _, hasLen, ok := parseArrayKey(s); ok {
		if hasLen {
			return symbols.TypeKey(prefix + "[;]")
		}
		return symbols.TypeKey(prefix + "[]")
	}
	return symbols.TypeKey(prefix + s)
}

func genericTypeKeyCompatible(genericKey, concreteKey symbols.TypeKey) bool {
	genericStr := stripTypeKeyWrappers(genericKey)
	concreteStr := stripTypeKeyWrappers(concreteKey)
	baseGen, genArgs, okGen := parseGenericTypeKey(genericStr)
	if !okGen {
		return false
	}
	baseCon, conArgs, okCon := parseGenericTypeKey(concreteStr)
	if !okCon {
		return false
	}
	if baseGen != baseCon || len(genArgs) != len(conArgs) {
		return false
	}
	return true
}

func stripTypeKeyWrappers(key symbols.TypeKey) string {
	s := strings.TrimSpace(string(key))
	for {
		switch {
		case strings.HasPrefix(s, "&mut "):
			s = strings.TrimSpace(strings.TrimPrefix(s, "&mut "))
		case strings.HasPrefix(s, "&"):
			s = strings.TrimSpace(strings.TrimPrefix(s, "&"))
		case strings.HasPrefix(s, "own "):
			s = strings.TrimSpace(strings.TrimPrefix(s, "own "))
		case strings.HasPrefix(s, "*"):
			s = strings.TrimSpace(strings.TrimPrefix(s, "*"))
		default:
			return s
		}
	}
}

func parseGenericTypeKey(raw string) (base string, args []string, ok bool) {
	s := strings.TrimSpace(raw)
	start := strings.Index(s, "<")
	end := strings.LastIndex(s, ">")
	if start < 0 || end <= start {
		return "", nil, false
	}
	base = strings.TrimSpace(s[:start])
	if base == "" {
		return "", nil, false
	}
	args = splitTypeArgs(s[start+1 : end])
	if len(args) == 0 {
		return "", nil, false
	}
	return base, args, true
}

func splitTypeArgs(s string) []string {
	var out []string
	var buf strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<', '(', '[', '{':
			depth++
		case '>', ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(buf.String())
				if part != "" {
					out = append(out, part)
				}
				buf.Reset()
				continue
			}
		}
		buf.WriteRune(r)
	}
	if tail := strings.TrimSpace(buf.String()); tail != "" {
		out = append(out, tail)
	}
	return out
}

func parseArrayKey(raw string) (elem, lengthKey string, hasLen, ok bool) {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		content := strings.TrimSpace(s[1 : len(s)-1])
		if parts := strings.Split(content, ";"); len(parts) == 2 {
			elem = strings.TrimSpace(parts[0])
			lengthKey = strings.TrimSpace(parts[1])
			hasLen = true
			return elem, lengthKey, hasLen, true
		}
		return content, "", false, true
	}
	if strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">") {
		content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "Array<"), ">"))
		return content, "", false, true
	}
	return "", "", false, false
}
