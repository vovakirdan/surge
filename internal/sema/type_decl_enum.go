package sema

import (
	"strconv"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) populateEnumType(itemID ast.ItemID, typeItem *ast.TypeItem, typeID types.TypeID) {
	enumDecl := tc.builder.Items.TypeEnum(typeItem)
	if enumDecl == nil {
		return
	}

	_ = tc.typeSymbolForItem(itemID) // Will be used for generics support

	// Resolve base type (default to int if not specified)
	baseType := tc.types.Builtins().Int
	if enumDecl.BaseType.IsValid() {
		resolved := tc.resolveTypeExprWithScope(enumDecl.BaseType, tc.fileScope())
		if resolved != types.NoTypeID {
			baseType = resolved
		}
	}

	// Validate that base type is supported (integers or string)
	resolvedBase := tc.resolveAlias(baseType)
	if !tc.isValidEnumBaseType(resolvedBase) {
		tc.report(diag.SemaEnumInvalidBaseType, enumDecl.BaseTypeSpan,
			"enum base type must be an integer type or string, got '%s'", tc.typeLabel(baseType))
		return
	}

	tc.types.SetEnumBaseType(typeID, baseType)

	// Process variants
	if enumDecl.VariantsCount == 0 {
		tc.types.SetEnumVariants(typeID, nil)
		return
	}

	variants := make([]types.EnumVariantInfo, 0, enumDecl.VariantsCount)
	nameSet := make(map[source.StringID]source.Span)
	var nextValue int64

	// Check if this is a string enum (unwrap type aliases first)
	resolved := tc.resolveAlias(baseType)
	baseTypeDesc, _ := tc.types.Lookup(resolved)
	isStringEnum := baseTypeDesc.Kind == types.KindString

	for i := range enumDecl.VariantsCount {
		variantID := ast.EnumVariantID(uint32(enumDecl.VariantsStart) + uint32(i))
		variant := tc.builder.Items.EnumVariant(variantID)
		if variant == nil {
			continue
		}

		// Check for duplicate names
		if _, exists := nameSet[variant.Name]; exists {
			tc.report(diag.SemaEnumDuplicateVariant, variant.NameSpan,
				"duplicate variant '%s' in enum", tc.lookupName(variant.Name))
			continue
		}
		nameSet[variant.Name] = variant.NameSpan

		// Compute variant value
		var variantInfo types.EnumVariantInfo
		if variant.Value.IsValid() {
			// Explicit value
			if isStringEnum {
				stringVal, ok := tc.evalEnumStringValue(variant.Value)
				if !ok {
					continue
				}
				variantInfo = types.EnumVariantInfo{
					Name:        variant.Name,
					StringValue: stringVal,
					IsString:    true,
					Span:        variant.Span,
				}
			} else {
				intVal, ok := tc.evalEnumIntValue(variant.Value, baseType)
				if !ok {
					continue
				}
				variantInfo = types.EnumVariantInfo{
					Name:     variant.Name,
					IntValue: intVal,
					IsString: false,
					Span:     variant.Span,
				}
				nextValue = intVal + 1
			}
		} else {
			// Auto value
			if isStringEnum {
				tc.report(diag.SemaEnumValueTypeMismatch, variant.Span,
					"string enum variants must have explicit values")
				continue
			}
			variantInfo = types.EnumVariantInfo{
				Name:     variant.Name,
				IntValue: nextValue,
				IsString: false,
				Span:     variant.Span,
			}
			nextValue++
		}

		variants = append(variants, variantInfo)
	}

	tc.types.SetEnumVariants(typeID, variants)
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

// evalEnumIntValue evaluates an integer enum variant value expression
func (tc *typeChecker) evalEnumIntValue(exprID ast.ExprID, baseType types.TypeID) (int64, bool) {
	if !exprID.IsValid() {
		return 0, false
	}

	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return 0, false
	}

	// For now, only support integer and string literals
	switch expr.Kind {
	case ast.ExprLit:
		lit := tc.builder.Exprs.Literals.Get(uint32(expr.Payload))
		if lit != nil {
			return tc.evalEnumLiteral(lit, baseType, expr.Span)
		}
	default:
		tc.report(diag.SemaEnumValueTypeMismatch, expr.Span,
			"enum value must be a constant literal (complex expressions not yet supported)")
		return 0, false
	}

	return 0, false
}

// evalEnumLiteral evaluates a literal expression for enum value
func (tc *typeChecker) evalEnumLiteral(lit *ast.ExprLiteralData, baseType types.TypeID, span source.Span) (int64, bool) {
	_ = baseType // Will be used for overflow checking
	switch lit.Kind {
	case ast.ExprLitInt, ast.ExprLitUint:
		text := tc.lookupName(lit.Value)
		val, err := parseIntLiteral(text)
		if err != nil {
			tc.report(diag.SemaEnumValueTypeMismatch, span, "invalid integer literal: %v", err)
			return 0, false
		}
		// TODO: Check overflow based on baseType
		return val, true

	default:
		tc.report(diag.SemaEnumValueTypeMismatch, span,
			"enum value must be an integer or string literal")
		return 0, false
	}
}

// evalEnumStringValue evaluates a string enum variant value expression
func (tc *typeChecker) evalEnumStringValue(exprID ast.ExprID) (source.StringID, bool) {
	if !exprID.IsValid() {
		return source.NoStringID, false
	}

	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return source.NoStringID, false
	}

	// Only support string literals
	if expr.Kind != ast.ExprLit {
		tc.report(diag.SemaEnumValueTypeMismatch, expr.Span,
			"string enum value must be a string literal")
		return source.NoStringID, false
	}

	lit := tc.builder.Exprs.Literals.Get(uint32(expr.Payload))
	if lit == nil || lit.Kind != ast.ExprLitString {
		tc.report(diag.SemaEnumValueTypeMismatch, expr.Span,
			"string enum value must be a string literal")
		return source.NoStringID, false
	}

	return lit.Value, true
}

// parseIntLiteral parses an integer literal string
func parseIntLiteral(s string) (int64, error) {
	// Remove underscores
	s = removeUnderscores(s)

	// Handle different bases
	if len(s) > 2 {
		switch {
		case s[:2] == "0x" || s[:2] == "0X":
			val, err := strconv.ParseInt(s[2:], 16, 64)
			return val, err
		case s[:2] == "0b" || s[:2] == "0B":
			val, err := strconv.ParseInt(s[2:], 2, 64)
			return val, err
		case s[:2] == "0o" || s[:2] == "0O":
			val, err := strconv.ParseInt(s[2:], 8, 64)
			return val, err
		}
	}

	// Decimal
	val, err := strconv.ParseInt(s, 10, 64)
	return val, err
}

// removeUnderscores removes underscore separators from number literals
func removeUnderscores(s string) string {
	if !containsUnderscore(s) {
		return s
	}
	var result []byte
	for i := range len(s) {
		if s[i] != '_' {
			result = append(result, s[i])
		}
	}
	return string(result)
}

// containsUnderscore checks if string contains underscore
func containsUnderscore(s string) bool {
	for i := range len(s) {
		if s[i] == '_' {
			return true
		}
	}
	return false
}

// typeOfEnumVariant resolves Type::Variant and returns the enum's base type
func (tc *typeChecker) typeOfEnumVariant(enumType types.TypeID, variantName source.StringID, span source.Span) types.TypeID {
	info, ok := tc.types.EnumInfo(enumType)
	if !ok || info == nil {
		return types.NoTypeID
	}

	// Check if variant exists
	found := false
	for _, v := range info.Variants {
		if v.Name == variantName {
			found = true
			break
		}
	}

	if !found {
		enumName := tc.lookupName(info.Name)
		varName := tc.lookupName(variantName)
		tc.report(diag.SemaEnumVariantNotFound, span,
			"variant '%s' not found in enum '%s'", varName, enumName)
		return types.NoTypeID
	}

	// Return the base type (e.g., uint8, int, string)
	if info.BaseType != types.NoTypeID {
		return info.BaseType
	}

	// Default to int if no base type specified
	return tc.types.Builtins().Int
}

// isValidEnumBaseType checks if the resolved type is a valid enum base type
// Valid base types: int, uint, int8-64, uint8-64, string
func (tc *typeChecker) isValidEnumBaseType(resolved types.TypeID) bool {
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return false
	}
	switch tt.Kind {
	case types.KindString:
		return true
	case types.KindInt, types.KindUint:
		return true
	default:
		return false
	}
}
