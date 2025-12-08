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
	scope := tc.fileScope()
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount), scope)
	if len(paramSpecs) == 0 && len(typeItem.Generics) > 0 {
		paramSpecs = specsFromNames(typeItem.Generics)
	}
	pushed := tc.pushTypeParams(symID, paramSpecs, nil)
	defer func() {
		if pushed {
			tc.popTypeParams()
		}
	}()
	if paramIDs := tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount); len(paramIDs) > 0 {
		bounds := tc.resolveTypeParamBounds(paramIDs, tc.fileScope(), nil)
		tc.attachTypeParamSymbols(symID, bounds)
		tc.applyTypeParamBounds(symID)
	} else if len(paramSpecs) > 0 && len(typeItem.Generics) > 0 {
		// Attach type param symbols for generics syntax (<T>)
		typeParamSyms := make([]symbols.TypeParamSymbol, 0, len(paramSpecs))
		for _, spec := range paramSpecs {
			typeParamSyms = append(typeParamSyms, symbols.TypeParamSymbol{
				Name:      spec.name,
				IsConst:   spec.kind == paramKindConst,
				ConstType: spec.constType,
			})
		}
		tc.attachTypeParamSymbols(symID, typeParamSyms)
	}
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
	scope := tc.fileScope()
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetTypeParamIDs(typeItem.TypeParamsStart, typeItem.TypeParamsCount), scope)
	if len(paramSpecs) == 0 && len(typeItem.Generics) > 0 {
		paramSpecs = specsFromNames(typeItem.Generics)
	}
	pushed := tc.pushTypeParams(symID, paramSpecs, args)
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
