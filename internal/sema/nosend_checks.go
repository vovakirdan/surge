package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// checkSpawnSendability verifies that a symbol's type can be safely sent to a task.
// Types with the @nosend attribute cannot cross task boundaries as they may contain
// thread-local state, non-atomic reference counts, or other non-thread-safe data.
//
// This check is performed when a variable is captured by a task expression
// to ensure structured concurrency safety.
func (tc *typeChecker) checkSpawnSendability(symID symbols.SymbolID, span source.Span) {
	if !symID.IsValid() {
		return
	}

	valueType := tc.bindingType(symID)
	if valueType == types.NoTypeID {
		return
	}

	// Strip ownership/reference modifiers to get base type
	baseType := tc.valueType(valueType)

	// Check if the base type has @nosend
	if tc.typeHasAttr(baseType, "nosend") {
		label := tc.symbolLabel(symID)
		typeName := tc.typeLabel(baseType)
		tc.report(diag.SemaNosendInSpawn, span,
			"cannot send %s of @nosend type '%s' to task", label, typeName)
	}

	// Recursively check struct fields for nested @nosend types
	tc.checkNestedNosendWith(baseType, span, diag.SemaNosendInSpawn)
}

// checkChannelSendValue checks if a value being sent through a channel has @nosend attribute.
// Channel sends transfer ownership to another task, so @nosend types are prohibited.
//
// This check is performed when evaluating channel send operations (ch.send(value)).
func (tc *typeChecker) checkChannelSendValue(valueExpr ast.ExprID, span source.Span) {
	if !valueExpr.IsValid() {
		return
	}

	valueType := tc.result.ExprTypes[valueExpr]
	if valueType == types.NoTypeID {
		return
	}

	// Strip ownership/reference modifiers to get base type
	baseType := tc.valueType(valueType)

	// Check if the base type has @nosend
	if tc.typeHasAttr(baseType, "nosend") {
		typeName := tc.typeLabel(baseType)
		tc.report(diag.SemaChannelNosendValue, span,
			"cannot send @nosend type '%s' through channel", typeName)
		return
	}

	// Recursively check struct fields for nested @nosend types
	tc.checkNestedNosendWith(baseType, span, diag.SemaChannelNosendValue)
}

// checkNestedNosendWith recursively checks struct fields for @nosend attribute.
// This is a unified implementation used by both task and channel send checking.
//
// The function traverses struct fields recursively to find any @nosend types
// that might be embedded within composite types. A visited set prevents
// infinite recursion with recursive struct definitions.
//
// Parameters:
//   - typeID: The type to check for nested @nosend fields
//   - span: Source location for error reporting
//   - diagCode: The diagnostic code to use (SemaNosendInSpawn or SemaChannelNosendValue)
func (tc *typeChecker) checkNestedNosendWith(typeID types.TypeID, span source.Span, diagCode diag.Code) {
	tc.checkNestedNosendWithVisited(typeID, span, diagCode, make(map[types.TypeID]struct{}))
}

// checkNestedNosendWithVisited is the internal implementation with cycle detection.
func (tc *typeChecker) checkNestedNosendWithVisited(typeID types.TypeID, span source.Span, diagCode diag.Code, visited map[types.TypeID]struct{}) {
	// Prevent infinite recursion with recursive types
	if _, seen := visited[typeID]; seen {
		return
	}
	visited[typeID] = struct{}{}

	// Only structs can have nested fields to check
	structInfo, ok := tc.types.StructInfo(typeID)
	if !ok || structInfo == nil {
		return
	}

	// Check each field for @nosend attribute
	for _, field := range structInfo.Fields {
		fieldType := tc.valueType(field.Type)
		if tc.typeHasAttr(fieldType, "nosend") {
			typeName := tc.typeLabel(typeID)
			fieldTypeName := tc.typeLabel(fieldType)
			tc.report(diagCode, span,
				"type '%s' contains @nosend field of type '%s'", typeName, fieldTypeName)
		}
		// Recurse into nested structs
		tc.checkNestedNosendWithVisited(fieldType, span, diagCode, visited)
	}
}
