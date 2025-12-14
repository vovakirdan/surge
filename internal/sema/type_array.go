package sema

import (
	"strconv"
	"strings"

	"fortio.org/safecast"

	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) ensureBuiltinArrayType() {
	if tc == nil || tc.builder == nil || tc.types == nil {
		return
	}
	if tc.arrayName == source.NoStringID {
		tc.arrayName = tc.builder.StringsInterner.Intern("Array")
	}
	if !tc.arraySymbol.IsValid() {
		tc.arraySymbol = tc.lookupTypeSymbol(tc.arrayName, tc.fileScope())
	}
	if !tc.arraySymbol.IsValid() {
		return
	}
	sym := tc.symbolFromID(tc.arraySymbol)
	if sym == nil {
		return
	}
	paramName := tc.builder.StringsInterner.Intern("T")
	base, param := tc.types.EnsureArrayNominal(tc.arrayName, paramName, sym.Span, uint32(tc.arraySymbol))
	if base == types.NoTypeID {
		return
	}
	tc.arrayType = base
	sym.Type = base
	if param != types.NoTypeID {
		tc.typeParamNames[param] = paramName
	}
	if name := tc.lookupName(tc.arrayName); name != "" {
		tc.recordTypeName(base, name)
		if tc.typeKeys != nil {
			tc.typeKeys[name] = base
		}
	}
}

func (tc *typeChecker) instantiateArrayType(elem types.TypeID) types.TypeID {
	if elem == types.NoTypeID {
		return types.NoTypeID
	}
	tc.ensureBuiltinArrayType()
	if !tc.arraySymbol.IsValid() {
		return types.NoTypeID
	}
	return tc.instantiateType(tc.arraySymbol, []types.TypeID{elem}, source.Span{}, "type")
}

func (tc *typeChecker) ensureBuiltinArrayFixedType() {
	if tc == nil || tc.builder == nil || tc.types == nil {
		return
	}
	if tc.arrayFixedName == source.NoStringID {
		tc.arrayFixedName = tc.builder.StringsInterner.Intern("ArrayFixed")
	}
	if !tc.arrayFixedSymbol.IsValid() {
		tc.arrayFixedSymbol = tc.lookupTypeSymbol(tc.arrayFixedName, tc.fileScope())
	}
	if !tc.arrayFixedSymbol.IsValid() {
		return
	}
	sym := tc.symbolFromID(tc.arrayFixedSymbol)
	if sym == nil {
		return
	}
	elemParam := tc.builder.StringsInterner.Intern("T")
	lenParam := tc.builder.StringsInterner.Intern("N")
	base, params := tc.types.EnsureArrayFixedNominal(tc.arrayFixedName, elemParam, lenParam, sym.Span, uint32(tc.arrayFixedSymbol), tc.types.Builtins().Int)
	if base == types.NoTypeID {
		return
	}
	tc.arrayFixedType = base
	sym.Type = base
	if params[0] != types.NoTypeID {
		tc.typeParamNames[params[0]] = elemParam
	}
	if params[1] != types.NoTypeID {
		tc.typeParamNames[params[1]] = lenParam
	}
	if name := tc.lookupName(tc.arrayFixedName); name != "" {
		tc.recordTypeName(base, name)
		if tc.typeKeys != nil {
			tc.typeKeys[name] = base
		}
	}
	if len(sym.TypeParamSymbols) == 0 {
		sym.TypeParamSymbols = []symbols.TypeParamSymbol{
			{Name: elemParam, IsConst: false},
			{Name: lenParam, IsConst: true, ConstType: tc.types.Builtins().Int},
		}
	}
}

func (tc *typeChecker) instantiateArrayFixed(elem types.TypeID, length uint32) types.TypeID {
	if elem == types.NoTypeID {
		return types.NoTypeID
	}
	tc.ensureBuiltinArrayFixedType()
	if !tc.arrayFixedSymbol.IsValid() {
		return types.NoTypeID
	}
	lenConst := tc.types.Intern(types.MakeConstUint(length))
	return tc.instantiateArrayFixedWithArg(elem, lenConst)
}

func (tc *typeChecker) instantiateArrayFixedWithArg(elem, length types.TypeID) types.TypeID {
	if elem == types.NoTypeID || length == types.NoTypeID {
		return types.NoTypeID
	}
	tc.ensureBuiltinArrayFixedType()
	if !tc.arrayFixedSymbol.IsValid() {
		return types.NoTypeID
	}
	id := tc.instantiateType(tc.arrayFixedSymbol, []types.TypeID{elem, length}, source.Span{}, "type")
	if id != types.NoTypeID {
		if n, ok := tc.constValueFromType(length); ok {
			tc.types.SetStructValueArgs(id, []uint64{n})
		}
	}
	return id
}

func (tc *typeChecker) arrayElemType(id types.TypeID) (types.TypeID, bool) {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID, false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return types.NoTypeID, false
	}
	switch tt.Kind {
	case types.KindArray:
		return tt.Elem, true
	case types.KindStruct:
		info, ok := tc.types.StructInfo(resolved)
		if !ok || info == nil {
			return types.NoTypeID, false
		}
		if tc.arrayName == source.NoStringID && tc.builder != nil {
			tc.arrayName = tc.builder.StringsInterner.Intern("Array")
		}
		if info.Name == tc.arrayName && len(info.TypeArgs) == 1 {
			return info.TypeArgs[0], true
		}
		if tc.arrayFixedName == source.NoStringID && tc.builder != nil {
			tc.arrayFixedName = tc.builder.StringsInterner.Intern("ArrayFixed")
		}
		if info.Name == tc.arrayFixedName && len(info.TypeArgs) >= 1 {
			return info.TypeArgs[0], true
		}
	}
	return types.NoTypeID, false
}

func (tc *typeChecker) arrayFixedInfo(id types.TypeID) (elem types.TypeID, length uint32, ok bool) {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID, 0, false
	}
	resolved := tc.resolveAlias(id)
	info, okStruct := tc.types.StructInfo(resolved)
	if !okStruct || info == nil {
		return types.NoTypeID, 0, false
	}
	if tc.arrayFixedName == source.NoStringID && tc.builder != nil {
		tc.arrayFixedName = tc.builder.StringsInterner.Intern("ArrayFixed")
	}
	if info.Name != tc.arrayFixedName || len(info.TypeArgs) == 0 {
		return types.NoTypeID, 0, false
	}
	elem = info.TypeArgs[0]
	if vals := tc.types.StructValueArgs(resolved); len(vals) > 0 {
		if vals[0] <= uint64(^uint32(0)) {
			if l, err := safecast.Conv[uint32](vals[0]); err == nil {
				length = l
			}
		}
	} else if len(info.TypeArgs) > 1 {
		if n, ok := tc.constValueFromType(info.TypeArgs[1]); ok {
			if n <= uint64(^uint32(0)) {
				if l, err := safecast.Conv[uint32](n); err == nil {
					length = l
				}
			}
		}
	}
	return elem, length, true
}

func (tc *typeChecker) isArrayType(id types.TypeID) bool {
	_, ok := tc.arrayElemType(id)
	return ok
}

func (tc *typeChecker) arrayInfo(id types.TypeID) (elem types.TypeID, length uint32, fixed, ok bool) {
	if id == types.NoTypeID || tc.types == nil {
		return types.NoTypeID, 0, false, false
	}
	resolved := tc.resolveAlias(id)
	tt, found := tc.types.Lookup(resolved)
	if !found {
		return types.NoTypeID, 0, false, false
	}
	switch tt.Kind {
	case types.KindArray:
		elem = tt.Elem
		if tt.Count != types.ArrayDynamicLength {
			length = tt.Count
			fixed = true
		}
		return elem, length, fixed, true
	case types.KindStruct:
		if e, l, okFixed := tc.arrayFixedInfo(resolved); okFixed {
			return e, l, true, true
		}
		if e, okArr := tc.arrayElemType(resolved); okArr {
			return e, 0, false, true
		}
	}
	return types.NoTypeID, 0, false, false
}

func parseArrayKey(raw string) (elem, lengthKey string, length uint64, hasLen, ok bool) {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		content := strings.TrimSpace(s[1 : len(s)-1])
		if parts := strings.Split(content, ";"); len(parts) == 2 {
			elem = strings.TrimSpace(parts[0])
			lengthStr := strings.TrimSpace(parts[1])
			lengthKey = lengthStr
			hasLen = true
			if lengthStr != "" {
				if n, err := parseUint(lengthStr); err == nil {
					length = n
					return elem, lengthKey, length, hasLen, true
				}
			}
			return elem, lengthKey, length, hasLen, true
		}
		return content, lengthKey, 0, false, true
	}
	if strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">") {
		content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "Array<"), ">"))
		return content, "", 0, false, true
	}
	if strings.HasPrefix(s, "ArrayFixed<") && strings.HasSuffix(s, ">") {
		content := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "ArrayFixed<"), ">"))
		if parts := strings.Split(content, ","); len(parts) == 2 {
			elem = strings.TrimSpace(parts[0])
			lengthStr := strings.TrimSpace(parts[1])
			lengthKey = lengthStr
			hasLen = true
			if lengthStr != "" {
				if n, err := parseUint(lengthStr); err == nil {
					length = n
					return elem, lengthKey, length, hasLen, true
				}
			}
			return elem, lengthKey, length, hasLen, true
		}
		return content, lengthKey, 0, true, true
	}
	return "", "", 0, false, false
}

func arrayKeyInner(raw string) (string, bool) {
	elem, _, _, _, ok := parseArrayKey(raw)
	return elem, ok
}

func parseUint(raw string) (uint64, error) {
	return strconv.ParseUint(raw, 10, 64)
}

func (tc *typeChecker) constValueFromType(id types.TypeID) (uint64, bool) {
	if id == types.NoTypeID || tc.types == nil {
		return 0, false
	}
	tt, ok := tc.types.Lookup(tc.resolveAlias(id))
	if !ok || tt.Kind != types.KindConst {
		return 0, false
	}
	return uint64(tt.Count), true
}
