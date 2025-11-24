package sema

import "surge/internal/ast"

func (tc *typeChecker) checkTag(id ast.ItemID, tag *ast.TagItem) {
	if tag == nil {
		return
	}
	scope := tc.scopeForItem(id)
	symID := tc.typeSymbolForItem(id)
	paramSpecs := tc.specsFromTypeParams(tc.builder.Items.GetTypeParamIDs(tag.TypeParamsStart, tag.TypeParamsCount), scope)
	if len(paramSpecs) == 0 && len(tag.Generics) > 0 {
		paramSpecs = specsFromNames(tag.Generics)
	}
	pushed := tc.pushTypeParams(symID, paramSpecs, nil)
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
