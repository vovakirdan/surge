package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

func (tc *typeChecker) populateEnumType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	enumDecl := tc.builder.Items.TypeEnum(typeItem)
	if enumDecl == nil {
		return
	}

	_ = tc.typeSymbolForItem(itemID) // Will be used in iteration 4 for generics support

	// Resolve base type (default to int if not specified)
	baseType := tc.types.Builtins().Int
	if enumDecl.BaseType.IsValid() {
		resolved := tc.resolveTypeExprWithScope(enumDecl.BaseType, tc.fileScope())
		if resolved != types.NoTypeID {
			baseType = resolved
		}
	}
	tc.types.SetEnumBaseType(typeID, baseType)

	// For now, just register empty variants list
	// Full variant processing with value computation will be in iteration 4
	tc.types.SetEnumVariants(typeID, nil)
}
