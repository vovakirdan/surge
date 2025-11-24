package sema

import "surge/internal/ast"

func (tc *typeChecker) checkTag(id ast.ItemID, tag *ast.TagItem) {
	if tag == nil {
		return
	}
	scope := tc.scopeForItem(id)
	symID := tc.typeSymbolForItem(id)
	pushed := tc.pushTypeParams(symID, tag.Generics, nil)
	if paramIDs := tc.builder.Items.GetTypeParamIDs(tag.TypeParamsStart, tag.TypeParamsCount); len(paramIDs) > 0 {
		bounds := tc.resolveTypeParamBounds(paramIDs, scope, nil)
		tc.attachTypeParamSymbols(symID, bounds)
		tc.applyTypeParamBounds(symID)
	}
	for _, payload := range tag.Payload {
		tc.resolveTypeExprWithScope(payload, scope)
	}
	if pushed {
		tc.popTypeParams()
	}
}
