package sema

import (
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// getFieldTypeByIndex returns the type of a field at the given index in a struct.
// Returns NoTypeID if the struct or field cannot be found.
func (tc *typeChecker) getFieldTypeByIndex(ownerTypeID types.TypeID, fieldIndex int) types.TypeID {
	if ownerTypeID == types.NoTypeID {
		return types.NoTypeID
	}
	structInfo, ok := tc.types.StructInfo(ownerTypeID)
	if !ok || structInfo == nil {
		return types.NoTypeID
	}
	if fieldIndex < 0 || fieldIndex >= len(structInfo.Fields) {
		return types.NoTypeID
	}
	return structInfo.Fields[fieldIndex].Type
}

// validateFieldReferenceWithType validates that an attribute parameter references an existing field
// and returns the field type. Returns NoTypeID if validation fails.
// Used by @guarded_by("lock"), @requires_lock("lock"), @waits_on("cond")
func (tc *typeChecker) validateFieldReferenceWithType(info AttrInfo, ownerTypeID types.TypeID, errorCode diag.Code, message string) types.TypeID {
	if len(info.Args) == 0 {
		tc.report(diag.SemaAttrMissingParameter, info.Span, "%s", message)
		return types.NoTypeID
	}

	// Get the first argument expression
	argExpr := tc.builder.Exprs.Get(info.Args[0])

	// Check if it's a literal
	if argExpr.Kind != ast.ExprLit {
		tc.report(diag.SemaAttrInvalidParameter, argExpr.Span,
			"attribute parameter must be a string literal")
		return types.NoTypeID
	}

	// Get the literal data
	lit, ok := tc.builder.Exprs.Literal(info.Args[0])
	if !ok || lit.Kind != ast.ExprLitString {
		tc.report(diag.SemaAttrInvalidParameter, argExpr.Span,
			"attribute parameter must be a string literal")
		return types.NoTypeID
	}

	// Get the field name - strip quotes from string literal
	fieldNameRaw := tc.lookupName(lit.Value)
	fieldNameStr := strings.Trim(fieldNameRaw, "\"")

	// Validate that the field exists in ownerTypeID
	if ownerTypeID == types.NoTypeID {
		// Can't validate without owner type - skip for now
		return types.NoTypeID
	}

	// Check if ownerTypeID is a struct and get its info
	structInfo, ok := tc.types.StructInfo(ownerTypeID)
	if !ok || structInfo == nil {
		// Not a struct - can't have fields
		return types.NoTypeID
	}

	// Look up the field and get its type
	for _, field := range structInfo.Fields {
		if tc.lookupName(field.Name) == fieldNameStr {
			return field.Type
		}
	}

	// Field not found
	tc.report(errorCode, argExpr.Span,
		"field '%s' not found in type", fieldNameStr)
	return types.NoTypeID
}

// recordFieldAttrs stores attributes for a field for later lookup
func (tc *typeChecker) recordFieldAttrs(typeID types.TypeID, fieldIndex int, infos []AttrInfo) {
	if tc.fieldAttrs == nil {
		tc.fieldAttrs = make(map[fieldKey][]AttrInfo)
	}
	key := fieldKey{TypeID: typeID, FieldIndex: fieldIndex}
	tc.fieldAttrs[key] = infos
}

// fieldHasAttr checks if a field has the specified attribute
func (tc *typeChecker) fieldHasAttr(typeID types.TypeID, fieldIndex int, attrName string) bool {
	key := fieldKey{TypeID: typeID, FieldIndex: fieldIndex}
	infos, ok := tc.fieldAttrs[key]
	if !ok {
		return false
	}
	_, found := hasAttr(infos, attrName)
	return found
}

// getFieldGuardedBy returns the lock field name if the field has @guarded_by attribute.
// Returns 0 if no @guarded_by attribute exists.
func (tc *typeChecker) getFieldGuardedBy(typeID types.TypeID, fieldIndex int) source.StringID {
	key := fieldKey{TypeID: typeID, FieldIndex: fieldIndex}
	infos, ok := tc.fieldAttrs[key]
	if !ok {
		return 0
	}
	guardedInfo, found := hasAttr(infos, "guarded_by")
	if !found || len(guardedInfo.Args) == 0 {
		return 0
	}
	// Extract field name from string literal argument
	argExpr := tc.builder.Exprs.Get(guardedInfo.Args[0])
	if argExpr == nil || argExpr.Kind != ast.ExprLit {
		return 0
	}
	lit, ok := tc.builder.Exprs.Literal(guardedInfo.Args[0])
	if !ok || lit.Kind != ast.ExprLitString {
		return 0
	}
	// Get the field name - strip quotes from string literal
	fieldNameRaw := tc.lookupName(lit.Value)
	if len(fieldNameRaw) < 2 {
		return 0
	}
	fieldNameStr := fieldNameRaw[1 : len(fieldNameRaw)-1] // Remove quotes
	return tc.builder.StringsInterner.Intern(fieldNameStr)
}

// validateFieldAttrs validates all attributes on a struct field
func (tc *typeChecker) validateFieldAttrs(field *ast.TypeStructField, ownerTypeID types.TypeID, fieldIndex int) {
	// Collect attributes
	infos := tc.collectAttrs(field.AttrStart, field.AttrCount)
	if len(infos) == 0 {
		return
	}

	// Validate target applicability
	tc.validateAttrs(field.AttrStart, field.AttrCount, ast.AttrTargetField, diag.SemaError)

	// Check conflicts (fields can also have @align/@packed)
	tc.checkPackedAlignConflict(infos)

	// Validate parameters for @guarded_by
	if guardedInfo, ok := hasAttr(infos, "guarded_by"); ok {
		// Validate field exists and get its type
		lockFieldType := tc.validateFieldReferenceWithType(guardedInfo, ownerTypeID,
			diag.SemaAttrGuardedByNotField,
			"@guarded_by requires a field name argument: @guarded_by(\"lock\")")
		// Validate that the referenced field is a Mutex/RwLock
		if lockFieldType != types.NoTypeID && !tc.isLockType(lockFieldType) {
			argExpr := tc.builder.Exprs.Get(guardedInfo.Args[0])
			tc.report(diag.SemaAttrGuardedByNotLock, argExpr.Span,
				"@guarded_by field must be of type Mutex or RwLock, got '%s'",
				tc.typeLabel(lockFieldType))
		}
	}

	// Validate parameters for @align
	if alignInfo, ok := hasAttr(infos, "align"); ok {
		tc.validateAlignParameter(alignInfo)
	}

	// Validate @atomic field type
	if atomicInfo, ok := hasAttr(infos, "atomic"); ok {
		// Get the field type from the struct info
		fieldType := tc.getFieldTypeByIndex(ownerTypeID, fieldIndex)
		if fieldType != types.NoTypeID && !tc.isAtomicCompatibleType(fieldType) {
			tc.report(diag.SemaAttrAtomicInvalidType, atomicInfo.Span,
				"@atomic field must be of type int, uint, bool, or *T; got '%s'",
				tc.typeLabel(fieldType))
		}
	}

	// Record for later lookup
	tc.recordFieldAttrs(ownerTypeID, fieldIndex, infos)
}

// checkAtomicFieldDirectAccess checks if an @atomic field is being accessed directly
// (without using atomic intrinsics). Returns true if a violation was detected.
// The isAddressOf parameter indicates if the parent expression is taking the address
// of the field (which is allowed, as atomic intrinsics take pointers).
func (tc *typeChecker) checkAtomicFieldDirectAccess(targetExpr ast.ExprID, isAddressOf bool, span source.Span) bool {
	expr := tc.builder.Exprs.Get(targetExpr)
	if expr == nil || expr.Kind != ast.ExprMember {
		return false // Not a member access
	}

	// Get member access details
	member, ok := tc.builder.Exprs.Member(targetExpr)
	if !ok || member == nil {
		return false
	}

	// Get the type of the base expression
	baseType, ok := tc.result.ExprTypes[member.Target]
	if !ok || baseType == types.NoTypeID {
		return false
	}

	// Strip references to get the underlying struct type
	baseType = tc.valueType(baseType)

	// Get struct info to find field index
	structInfo, ok := tc.types.StructInfo(baseType)
	if !ok || structInfo == nil {
		return false
	}

	// Find the field index by name
	fieldIndex := -1
	for i, field := range structInfo.Fields {
		if field.Name == member.Field {
			fieldIndex = i
			break
		}
	}

	if fieldIndex < 0 {
		return false // Field not found
	}

	// Check if field has @atomic attribute
	if !tc.fieldHasAttr(baseType, fieldIndex, "atomic") {
		return false // Not atomic, normal access is fine
	}

	// Address-of on @atomic field is allowed (for use with atomic intrinsics)
	if isAddressOf {
		return false
	}

	// Direct access to @atomic field is forbidden
	fieldName := tc.lookupName(member.Field)
	tc.report(diag.SemaAtomicDirectAccess, span,
		"@atomic field '%s' must be accessed via atomic operations (atomic_load, atomic_store, etc.)",
		fieldName)
	return true
}

// checkReadonlyFieldWrite checks if an expression is trying to write to a @readonly field
// Returns true if a @readonly violation was detected and reported
func (tc *typeChecker) checkReadonlyFieldWrite(targetExpr ast.ExprID, span source.Span) bool {
	expr := tc.builder.Exprs.Get(targetExpr)
	if expr == nil {
		return false
	}
	if expr.Kind != ast.ExprMember {
		return false // Not a member access
	}

	// Get member access details
	member, ok := tc.builder.Exprs.Member(targetExpr)
	if !ok {
		return false
	}

	// Get the type of the base expression
	baseType, ok := tc.result.ExprTypes[member.Target]
	if !ok || baseType == types.NoTypeID {
		return false
	}

	// Strip references to get the underlying struct type
	baseType = tc.valueType(baseType)

	// Get struct info to find field index
	structInfo, ok := tc.types.StructInfo(baseType)
	if !ok || structInfo == nil {
		return false
	}

	// Find the field index by name
	fieldIndex := -1
	for i, field := range structInfo.Fields {
		if field.Name == member.Field {
			fieldIndex = i
			break
		}
	}

	if fieldIndex < 0 {
		return false // Field not found
	}

	// Check if field has @readonly attribute
	if tc.fieldHasAttr(baseType, fieldIndex, "readonly") {
		fieldName := tc.lookupName(member.Field)
		tc.report(diag.SemaAttrReadonlyWrite, span,
			"cannot write to @readonly field '%s'", fieldName)
		return true
	}

	return false
}
