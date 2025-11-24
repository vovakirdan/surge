package sema

import (
	"strings"

	"surge/internal/source"
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
	return tc.instantiateType(tc.arraySymbol, []types.TypeID{elem})
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
	}
	return types.NoTypeID, false
}

func (tc *typeChecker) isArrayType(id types.TypeID) bool {
	_, ok := tc.arrayElemType(id)
	return ok
}

func arrayKeyInner(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return strings.TrimSpace(s[1 : len(s)-1]), true
	}
	if strings.HasPrefix(s, "Array<") && strings.HasSuffix(s, ">") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "Array<"), ">")), true
	}
	return "", false
}
