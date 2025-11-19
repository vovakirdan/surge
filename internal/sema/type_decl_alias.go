package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) populateAliasType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	aliasDecl := tc.builder.Items.TypeAlias(typeItem)
	if aliasDecl == nil {
		return
	}
	symID := tc.typeSymbolForItem(itemID)
	pushed := tc.pushTypeParams(symID, typeItem.Generics, nil)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	target := tc.resolveTypeExprWithScope(aliasDecl.Target, tc.fileScope())
	if target == types.NoTypeID {
		span := typeItem.Span
		name := tc.lookupName(typeItem.Name)
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaUnresolvedSymbol, span, "unable to resolve alias target for %s", name)
		return
	}
	tc.types.SetAliasTarget(typeID, target)
}

func (tc *typeChecker) instantiateAlias(typeItem *ast.TypeItem, symID symbols.SymbolID, args []types.TypeID) types.TypeID {
	aliasDecl := tc.builder.Items.TypeAlias(typeItem)
	if aliasDecl == nil {
		return types.NoTypeID
	}
	pushed := tc.pushTypeParams(symID, typeItem.Generics, args)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	target := tc.resolveTypeExprWithScope(aliasDecl.Target, tc.fileScope())
	if target == types.NoTypeID {
		span := typeItem.Span
		name := tc.lookupName(typeItem.Name)
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaUnresolvedSymbol, span, "unable to resolve alias target for %s", name)
		return types.NoTypeID
	}
	typeID := tc.types.RegisterAliasInstance(typeItem.Name, typeItem.Span, args)
	tc.types.SetAliasTarget(typeID, target)
	return typeID
}
