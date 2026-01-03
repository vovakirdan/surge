package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) ensureBuiltinMapType() {
	if tc == nil || tc.builder == nil || tc.types == nil {
		return
	}
	if tc.mapName == source.NoStringID {
		tc.mapName = tc.builder.StringsInterner.Intern("Map")
	}
	if !tc.mapSymbol.IsValid() {
		tc.mapSymbol = tc.lookupTypeSymbol(tc.mapName, tc.fileScope())
	}
	if !tc.mapSymbol.IsValid() {
		return
	}
	sym := tc.symbolFromID(tc.mapSymbol)
	if sym == nil {
		return
	}
	keyParam := tc.builder.StringsInterner.Intern("K")
	valueParam := tc.builder.StringsInterner.Intern("V")
	base, params := tc.types.EnsureMapNominal(tc.mapName, keyParam, valueParam, sym.Span, uint32(tc.mapSymbol))
	if base == types.NoTypeID {
		return
	}
	tc.mapType = base
	sym.Type = base
	if params[0] != types.NoTypeID {
		tc.typeParamNames[params[0]] = keyParam
	}
	if params[1] != types.NoTypeID {
		tc.typeParamNames[params[1]] = valueParam
	}
	if name := tc.lookupName(tc.mapName); name != "" {
		tc.recordTypeName(base, name)
		if tc.typeKeys != nil {
			tc.typeKeys[name] = base
		}
	}
	if len(sym.TypeParamSymbols) == 0 {
		sym.TypeParamSymbols = []symbols.TypeParamSymbol{
			{Name: keyParam, IsConst: false},
			{Name: valueParam, IsConst: false},
		}
	}
}

func (tc *typeChecker) instantiateMapType(key, value types.TypeID, span source.Span) types.TypeID {
	if key == types.NoTypeID || value == types.NoTypeID {
		return types.NoTypeID
	}
	tc.ensureBuiltinMapType()
	if !tc.mapSymbol.IsValid() {
		return types.NoTypeID
	}
	if !tc.isMapKeyType(key) {
		tc.report(diag.SemaTypeMismatch, span, "map key type must be hashable (string or integer)")
		return types.NoTypeID
	}
	return tc.instantiateType(tc.mapSymbol, []types.TypeID{key, value}, span, "type")
}

func (tc *typeChecker) mapInfo(id types.TypeID) (key, value types.TypeID, ok bool) {
	if tc == nil || tc.types == nil {
		return types.NoTypeID, types.NoTypeID, false
	}
	return tc.types.MapInfo(id)
}

func (tc *typeChecker) isMapKeyType(id types.TypeID) bool {
	if tc == nil || tc.types == nil || id == types.NoTypeID {
		return false
	}
	resolved := tc.resolveAlias(id)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindString, types.KindInt, types.KindUint:
		return true
	default:
		return false
	}
}
