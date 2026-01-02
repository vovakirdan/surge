package sema

import (
	"fmt"

	"fortio.org/safecast"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ensureBindingTypeMatch validates that a value type matches the declared binding type.
// This is the primary validation for let statements and variable declarations.
//
// The function handles several cases:
//   - If declared type is NoTypeID, the function returns early (type will be inferred)
//   - For struct literals without explicit type, validates field compatibility
//   - For arrays, checks element types with support for implicit element conversions
//   - For assignable types, accepts the binding directly
//   - For implicit conversions, records the conversion for later use
//   - Otherwise, reports a type mismatch error
//
// Parameters:
//   - typeExpr: The AST type expression for the declared type (for error reporting)
//   - declared: The resolved declared type (from type annotation)
//   - actual: The actual type of the value expression
//   - valueExpr: The value expression being assigned (for span extraction)
func (tc *typeChecker) ensureBindingTypeMatch(typeExpr ast.TypeID, declared, actual types.TypeID, valueExpr ast.ExprID) {
	if declared == types.NoTypeID {
		return
	}

	declaredRef := false
	declaredRefMut := false
	if tc.types != nil {
		if tt, ok := tc.types.Lookup(tc.resolveAlias(declared)); ok && tt.Kind == types.KindReference {
			declaredRef = true
			declaredRefMut = tt.Mutable
		}
	}

	if actual == types.NoTypeID {
		if tc.applyExpectedType(valueExpr, declared) {
			actual = tc.result.ExprTypes[valueExpr]
		} else {
			if declaredRef && valueExpr.IsValid() && !tc.isAddressableExpr(valueExpr) {
				tc.reportBorrowNonAddressable(valueExpr, declaredRefMut)
				return
			}
			if data, ok := tc.anonymousStructLiteral(valueExpr); ok && data != nil {
				tc.report(diag.SemaTypeMismatch, tc.exprSpan(valueExpr),
					"struct literal requires explicit type when assigning to %s", tc.typeLabel(declared))
			}
			return
		}
	}

	if applied, ok := tc.materializeNumericLiteral(valueExpr, declared); applied {
		actual = tc.result.ExprTypes[valueExpr]
		if !ok {
			return
		}
	}
	if applied, ok := tc.materializeArrayLiteral(valueExpr, declared); applied {
		if !ok {
			return
		}
		actual = tc.result.ExprTypes[valueExpr]
	}

	if declaredRef && valueExpr.IsValid() && !tc.isReferenceType(actual) && !tc.isAddressableExpr(valueExpr) {
		tc.reportBorrowNonAddressable(valueExpr, declaredRefMut)
		return
	}

	// Apply literal coercion (e.g., untyped int literal to specific int type)
	actual = tc.coerceLiteralForBinding(declared, actual, valueExpr)

	// Special handling for array types: element-wise compatibility and conversion
	if expElem, expLen, expFixed, okExp := tc.arrayInfo(declared); okExp {
		if actElem, actLen, actFixed, okAct := tc.arrayInfo(actual); okAct {
			// Check if element types are directly assignable OR can be implicitly converted
			elemAssignable := tc.typesAssignable(expElem, actElem, true)
			var elemConvertible bool
			if !elemAssignable {
				_, found, _ := tc.tryImplicitConversion(actElem, expElem)
				elemConvertible = found
			}

			if elemAssignable || elemConvertible {
				if expFixed {
					// Fixed-size array: lengths must match exactly
					if actFixed && expLen == actLen {
						if elemConvertible && !elemAssignable {
							// Element types need conversion - only allowed for array literals
							// because we can convert each element individually
							if valueExpr.IsValid() {
								if arr, okArr := tc.builder.Exprs.Array(valueExpr); okArr && arr != nil {
									tc.recordArrayElementConversions(arr, expElem)
									return
								}
							}
							// Not an array literal, can't convert elements - fall through to error
						} else {
							// Elements are assignable without conversion
							return
						}
					}
					// Try to match array literal length with fixed-size expectation
					if !actFixed && valueExpr.IsValid() {
						if arr, okArr := tc.builder.Exprs.Array(valueExpr); okArr && arr != nil {
							if l, err := safecast.Conv[uint32](len(arr.Elements)); err == nil && l == expLen {
								// Array literal length matches expected fixed size
								if elemConvertible && !elemAssignable {
									tc.recordArrayElementConversions(arr, expElem)
								}
								return
							}
						}
					}
				} else {
					// Dynamic array: only element type compatibility matters
					if elemConvertible && !elemAssignable {
						// Element types need conversion - only allowed for array literals
						if valueExpr.IsValid() {
							if arr, okArr := tc.builder.Exprs.Array(valueExpr); okArr && arr != nil {
								tc.recordArrayElementConversions(arr, expElem)
								return
							}
						}
						// Not an array literal, can't convert elements - fall through to error
					} else {
						// Elements are assignable without conversion
						return
					}
				}
			}
		}
	}

	// Standard type assignability check
	if tc.typesAssignable(declared, actual, true) {
		tc.dropImplicitBorrow(valueExpr, declared, actual, tc.exprSpan(valueExpr))
		if tc.recordTagUnionUpcast(valueExpr, actual, declared) {
			return
		}
		if tc.recordNumericWidening(valueExpr, actual, declared) {
			return
		}
		return
	}

	// Try implicit conversion before reporting error
	// This handles user-defined __to methods for type conversion
	if convType, found, ambiguous := tc.tryImplicitConversion(actual, declared); found {
		tc.recordImplicitConversion(valueExpr, actual, convType)
		return
	} else if ambiguous {
		tc.report(diag.SemaAmbiguousConversion, tc.exprSpan(valueExpr),
			"ambiguous conversion from %s to %s: multiple __to methods found",
			tc.typeLabel(actual), tc.typeLabel(declared))
		return
	}

	// Try implicit tag injection for Option<T> and Erring<T, E>
	// This enables: let x: int? = 1; (implicitly becomes Some(1))
	if convType, kind, found := tc.tryTagInjection(actual, declared); found {
		tc.recordImplicitConversionWithKind(valueExpr, actual, convType, kind)
		return
	}

	// No compatible types found - report the mismatch
	tc.reportBindingTypeMismatch(typeExpr, declared, actual, valueExpr)
}

// reportBindingTypeMismatch generates a detailed diagnostic for type mismatch errors.
// It provides two fix suggestions:
//  1. Change the variable's type annotation to match the actual value type
//  2. Add an explicit cast to convert the value to the expected type
//
// This function is called when ensureBindingTypeMatch determines that a value
// cannot be assigned to a binding due to incompatible types.
func (tc *typeChecker) reportBindingTypeMismatch(typeExpr ast.TypeID, expected, actual types.TypeID, valueExpr ast.ExprID) {
	if tc.reporter == nil {
		return
	}

	expectedLabel := tc.typeLabel(expected)
	actualLabel := tc.typeLabel(actual)

	// Determine primary diagnostic location
	primary := tc.exprSpan(valueExpr)
	if primary == (source.Span{}) {
		primary = tc.typeSpan(typeExpr)
	}

	msg := fmt.Sprintf("cannot assign %s to %s", actualLabel, expectedLabel)
	b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, primary, msg)
	if b == nil {
		return
	}

	// Fix suggestion 1: Change the declared type to match actual value
	if typeSpan := tc.typeSpan(typeExpr); typeSpan != (source.Span{}) {
		changeType := fix.ReplaceSpan(
			fmt.Sprintf("change variable type to %s", actualLabel),
			typeSpan,
			actualLabel,
			"",
			fix.WithKind(diag.FixKindRefactor),
		)
		b.WithFixSuggestion(changeType)
	}

	// Fix suggestion 2: Cast the value expression to expected type
	if insertSpan := tc.exprSpan(valueExpr); insertSpan != (source.Span{}) {
		cast := fix.InsertText(
			fmt.Sprintf("cast expression to %s", expectedLabel),
			insertSpan.ZeroideToEnd(),
			" to "+expectedLabel,
			"",
			fix.WithKind(diag.FixKindRefactorRewrite),
			fix.WithApplicability(diag.FixApplicabilityManualReview),
		)
		b.WithFixSuggestion(cast)
	}

	b.Emit()
}

// bindTuplePattern handles tuple destructuring in let statements.
// For example: `let (x, y) = (1, "hello")` binds x to 1 and y to "hello".
//
// The function:
//  1. Validates that the pattern is a tuple expression
//  2. Validates that the value type is a tuple with matching element count
//  3. Recursively binds each element (identifiers get types, nested tuples recurse)
//
// Nested destructuring is supported: `let ((a, b), c) = ((1, 2), 3)`
func (tc *typeChecker) bindTuplePattern(pattern ast.ExprID, valueType types.TypeID, scope symbols.ScopeID) {
	tuple, ok := tc.builder.Exprs.Tuple(pattern)
	if !ok || tuple == nil {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(pattern), "expected tuple pattern")
		return
	}

	// Get tuple info from the value type
	info, ok := tc.types.TupleInfo(tc.valueType(valueType))
	if !ok {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(pattern), "cannot destructure %s as tuple", tc.typeLabel(valueType))
		return
	}

	// Verify element count matches
	if len(tuple.Elements) != len(info.Elems) {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(pattern),
			"pattern has %d elements but tuple has %d", len(tuple.Elements), len(info.Elems))
		return
	}

	// Bind each pattern element to its corresponding tuple element type
	for i, elem := range tuple.Elements {
		elemType := info.Elems[i]
		node := tc.builder.Exprs.Get(elem)
		if node == nil {
			continue
		}

		switch node.Kind {
		case ast.ExprIdent:
			// Simple identifier binding: assign the element type
			ident, _ := tc.builder.Exprs.Ident(elem)
			if ident == nil {
				continue
			}
			tc.result.ExprTypes[elem] = elemType

			// Attach type to the bound symbol
			symID := tc.symbolForExpr(elem)
			if !symID.IsValid() && scope.IsValid() {
				symID = tc.symbolInScope(scope, ident.Name, symbols.SymbolLet)
			}
			if symID.IsValid() {
				tc.setBindingType(symID, elemType)
			}

		case ast.ExprTuple:
			// Nested tuple: recurse into the pattern
			tc.bindTuplePattern(elem, elemType, scope)

		default:
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(elem), "expected identifier in pattern")
		}
	}
}

// recordArrayElementConversions records implicit conversions for array elements
// when the expected element type differs from the actual element types.
// This is used when assigning an array literal to a variable with a different
// but convertible element type.
//
// For example:
//
//	let arr: MyInt[] = [1, 2, 3]  // where MyInt has __from(int)
//
// Each integer literal needs an implicit conversion to MyInt, which this function records.
func (tc *typeChecker) recordArrayElementConversions(arr *ast.ExprArrayData, expectedElemType types.TypeID) {
	if arr == nil || expectedElemType == types.NoTypeID {
		return
	}

	for _, elem := range arr.Elements {
		if !elem.IsValid() {
			continue
		}

		// Get the actual type of this element (should already be typed)
		actualElemType := tc.result.ExprTypes[elem]
		if actualElemType == types.NoTypeID {
			continue
		}

		// Check if implicit conversion is needed
		if tc.recordTagUnionUpcast(elem, actualElemType, expectedElemType) {
			continue
		}
		if tc.recordNumericWidening(elem, actualElemType, expectedElemType) {
			continue
		}
		if !tc.typesAssignable(expectedElemType, actualElemType, true) {
			if convType, found, _ := tc.tryImplicitConversion(actualElemType, expectedElemType); found {
				tc.recordImplicitConversion(elem, actualElemType, convType)
			}
		}
	}
}

func (tc *typeChecker) applyExpectedType(expr ast.ExprID, expected types.TypeID) bool {
	if expected == types.NoTypeID || !expr.IsValid() || tc.builder == nil {
		return false
	}
	if tc.result.ExprTypes[expr] != types.NoTypeID {
		return false
	}
	node := tc.builder.Exprs.Get(expr)
	if node == nil {
		return false
	}
	switch node.Kind {
	case ast.ExprGroup:
		group, ok := tc.builder.Exprs.Group(expr)
		if !ok || group == nil {
			return false
		}
		if tc.applyExpectedType(group.Inner, expected) {
			tc.result.ExprTypes[expr] = tc.result.ExprTypes[group.Inner]
			return true
		}
	case ast.ExprStruct:
		data, ok := tc.builder.Exprs.Struct(expr)
		if !ok || data == nil || data.Type.IsValid() {
			return false
		}
		if info, _ := tc.structInfoForType(expected); info == nil {
			return false
		}
		tc.validateStructLiteralFields(expected, data, tc.exprSpan(expr))
		tc.result.ExprTypes[expr] = expected
		return true
	case ast.ExprArray:
		arr, ok := tc.builder.Exprs.Array(expr)
		if !ok || arr == nil {
			return false
		}
		expElem, expLen, expFixed, ok := tc.arrayInfo(expected)
		if !ok {
			return false
		}
		if expFixed {
			if length, err := safecast.Conv[uint32](len(arr.Elements)); err == nil && expLen != length {
				tc.report(diag.SemaTypeMismatch, tc.exprSpan(expr),
					"array literal length %d does not match expected length %d", length, expLen)
				return false
			}
		}
		for _, elem := range arr.Elements {
			if !elem.IsValid() {
				continue
			}
			if tc.result.ExprTypes[elem] == types.NoTypeID {
				tc.applyExpectedType(elem, expElem)
			}
		}
		tc.result.ExprTypes[expr] = expected
		return true
	}
	return false
}

func (tc *typeChecker) anonymousStructLiteral(expr ast.ExprID) (*ast.ExprStructData, bool) {
	if !expr.IsValid() || tc.builder == nil {
		return nil, false
	}
	cur := expr
	for cur.IsValid() {
		node := tc.builder.Exprs.Get(cur)
		if node == nil {
			return nil, false
		}
		switch node.Kind {
		case ast.ExprGroup:
			group, ok := tc.builder.Exprs.Group(cur)
			if !ok || group == nil {
				return nil, false
			}
			cur = group.Inner
			continue
		case ast.ExprStruct:
			data, ok := tc.builder.Exprs.Struct(cur)
			if !ok || data == nil || data.Type.IsValid() {
				return nil, false
			}
			return data, true
		default:
			return nil, false
		}
	}
	return nil, false
}
