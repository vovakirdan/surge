package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

func (tc *typeChecker) isNumericType(id types.TypeID) bool {
	_, ok := tc.numericInfo(id)
	return ok
}

func (tc *typeChecker) needsNumericWidening(actual, expected types.TypeID) bool {
	if actual == types.NoTypeID || expected == types.NoTypeID {
		return false
	}
	actualInfo, okActual := tc.numericInfo(actual)
	expectedInfo, okExpected := tc.numericInfo(expected)
	if !okActual || !okExpected || actualInfo.kind != expectedInfo.kind {
		return false
	}
	if actualInfo.width == expectedInfo.width {
		return false
	}
	return widthCanWiden(actualInfo.width, expectedInfo.width)
}

func (tc *typeChecker) recordNumericWidening(expr ast.ExprID, actual, expected types.TypeID) bool {
	if !tc.needsNumericWidening(actual, expected) {
		return false
	}
	tc.recordImplicitConversion(expr, actual, expected)
	return true
}
