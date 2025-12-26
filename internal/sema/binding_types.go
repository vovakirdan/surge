package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) setBindingType(symID symbols.SymbolID, ty types.TypeID) {
	if !symID.IsValid() || ty == types.NoTypeID {
		return
	}
	if tc.bindingTypes == nil {
		tc.bindingTypes = make(map[symbols.SymbolID]types.TypeID)
	}
	tc.bindingTypes[symID] = ty
	tc.assignSymbolType(symID, ty)
}

func (tc *typeChecker) bindingType(symID symbols.SymbolID) types.TypeID {
	if !symID.IsValid() {
		return types.NoTypeID
	}
	if tc.bindingTypes != nil {
		if ty := tc.bindingTypes[symID]; ty != types.NoTypeID {
			return ty
		}
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return types.NoTypeID
	}
	return sym.Type
}

func (tc *typeChecker) registerFnParamTypes(fnID ast.ItemID, fnItem *ast.FnItem, allowRawPointer bool) {
	if tc.builder == nil || fnItem == nil {
		return
	}
	scope := tc.scopeForItem(fnID)
	paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
	for _, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil || param.Name == source.NoStringID || tc.isWildcardName(param.Name) {
			continue
		}
		paramType := tc.resolveTypeExprWithScopeAllowPointer(param.Type, scope, allowRawPointer)
		if param.Variadic {
			paramType = tc.instantiateArrayType(paramType)
		}
		symID := tc.symbolInScope(scope, param.Name, symbols.SymbolParam)
		if paramType != types.NoTypeID {
			tc.setBindingType(symID, paramType)
		}
	}
}

func (tc *typeChecker) isWildcardName(name source.StringID) bool {
	return name != source.NoStringID && tc.lookupName(name) == "_"
}

func (tc *typeChecker) symbolInScope(scope symbols.ScopeID, name source.StringID, kind symbols.SymbolKind) symbols.SymbolID {
	if name == source.NoStringID || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil || tc.symbols.Table.Symbols == nil {
		return symbols.NoSymbolID
	}
	scopeData := tc.symbols.Table.Scopes.Get(scope)
	if scopeData == nil {
		return symbols.NoSymbolID
	}
	ids := scopeData.NameIndex[name]
	for i := len(ids) - 1; i >= 0; i-- {
		symID := ids[i]
		sym := tc.symbols.Table.Symbols.Get(symID)
		if sym == nil {
			continue
		}
		if sym.Kind == kind {
			return symID
		}
	}
	return symbols.NoSymbolID
}
