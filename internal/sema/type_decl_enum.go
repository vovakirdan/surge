package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
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

// enumTypeForExpr checks if the expression is a reference to an enum type name
func (tc *typeChecker) enumTypeForExpr(id ast.ExprID) types.TypeID {
	if !id.IsValid() {
		return types.NoTypeID
	}

	// Check if this is an identifier expression
	ident, ok := tc.builder.Exprs.Ident(id)
	if !ok || ident == nil {
		return types.NoTypeID
	}

	// Look up the symbol
	symID := tc.lookupSymbolAny(ident.Name, tc.currentScope())
	if !symID.IsValid() {
		return types.NoTypeID
	}

	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolType {
		return types.NoTypeID
	}

	// Check if this type is an enum
	if sym.Type == types.NoTypeID {
		return types.NoTypeID
	}

	tt, ok := tc.types.Lookup(sym.Type)
	if !ok || tt.Kind != types.KindEnum {
		return types.NoTypeID
	}

	return sym.Type
}

// typeOfEnumVariant resolves Type::Variant and returns the enum's base type
func (tc *typeChecker) typeOfEnumVariant(enumType types.TypeID, variantName source.StringID, span source.Span) types.TypeID {
	_ = variantName // Will be used in iteration 4 to verify variant exists
	_ = span        // Will be used in iteration 4 for error reporting
	// For now, just return the base type of the enum
	// In iteration 4, we'll actually verify the variant exists and register constants
	info, ok := tc.types.EnumInfo(enumType)
	if !ok || info == nil {
		return types.NoTypeID
	}

	// Return the base type (e.g., uint8, int, string)
	if info.BaseType != types.NoTypeID {
		return info.BaseType
	}

	// Default to int if no base type specified
	return tc.types.Builtins().Int
}
