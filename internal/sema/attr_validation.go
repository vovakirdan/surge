package sema

import (
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// AttrInfo holds information about a parsed attribute including its spec and arguments
type AttrInfo struct {
	Spec ast.AttrSpec // Attribute specification from catalog
	Attr *ast.Attr    // The actual attribute node
	Span source.Span  // Source location
	Args []ast.ExprID // Argument expressions
}

// collectAttrs gathers all attributes from the given range and returns parsed AttrInfo
func (tc *typeChecker) collectAttrs(start ast.AttrID, count uint32) []AttrInfo {
	if count == 0 || !start.IsValid() {
		return nil
	}

	attrs := tc.builder.Items.CollectAttrs(start, count)
	result := make([]AttrInfo, 0, len(attrs))

	for _, attr := range attrs {
		spec, ok := ast.LookupAttrID(tc.builder.StringsInterner, attr.Name)
		if !ok {
			// Unknown attribute - will be reported by validateAttrs
			continue
		}

		// Collect arguments
		args := make([]ast.ExprID, 0, len(attr.Args))
		args = append(args, attr.Args...)

		result = append(result, AttrInfo{
			Spec: spec,
			Attr: &attr,
			Span: attr.Span,
			Args: args,
		})
	}

	return result
}

// hasAttr checks if the given attribute name exists in the list
// Returns the AttrInfo and true if found, zero value and false otherwise
func hasAttr(infos []AttrInfo, attrName string) (AttrInfo, bool) {
	for _, info := range infos {
		if strings.EqualFold(info.Spec.Name, attrName) {
			return info, true
		}
	}
	return AttrInfo{}, false
}

// checkConflict detects if two conflicting attributes appear together
func (tc *typeChecker) checkConflict(infos []AttrInfo, attr1, attr2 string, code diag.Code) {
	_, has1 := hasAttr(infos, attr1)
	info2, has2 := hasAttr(infos, attr2)

	if has1 && has2 {
		tc.report(code, info2.Span,
			"attribute '@%s' conflicts with '@%s'", attr2, attr1)
	}
}

// checkPackedAlignConflict is a special handler for @packed + @align conflicts
// @packed and @align can coexist if alignment is natural, but we reject them
// together to keep validation simple
func (tc *typeChecker) checkPackedAlignConflict(infos []AttrInfo) {
	packedInfo, hasPacked := hasAttr(infos, "packed")
	alignInfo, hasAlign := hasAttr(infos, "align")

	if hasPacked && hasAlign {
		tc.report(diag.SemaAttrPackedAlign, alignInfo.Span,
			"@align conflicts with @packed on the same declaration")
		// Also report on packed for clarity
		tc.report(diag.SemaAttrPackedAlign, packedInfo.Span,
			"@packed conflicts with @align on the same declaration")
	}
}

// validateAllConflicts checks for all known conflicting attribute pairs
func (tc *typeChecker) validateAllConflicts(infos []AttrInfo) {
	// @send vs @nosend
	tc.checkConflict(infos, "send", "nosend", diag.SemaAttrSendNosend)

	// @nonblocking vs @waits_on
	tc.checkConflict(infos, "nonblocking", "waits_on", diag.SemaAttrNonblockingWaitsOn)

	// @packed vs @align (special handler)
	tc.checkPackedAlignConflict(infos)
}

// validateAlignParameter validates that @align(N) has a valid power-of-2 argument
func (tc *typeChecker) validateAlignParameter(info AttrInfo) {
	if len(info.Args) == 0 {
		tc.report(diag.SemaAttrMissingParameter, info.Span,
			"@align requires a numeric argument: @align(8)")
		return
	}

	// Get the first argument expression
	argExpr := tc.builder.Exprs.Get(info.Args[0])

	// Check if it's a literal
	if argExpr.Kind != ast.ExprLit {
		tc.report(diag.SemaAttrAlignInvalidValue, argExpr.Span,
			"@align requires a numeric literal argument")
		return
	}

	// Get the literal data
	lit, ok := tc.builder.Exprs.Literal(info.Args[0])
	if !ok || lit.Kind != ast.ExprLitInt {
		tc.report(diag.SemaAttrAlignInvalidValue, argExpr.Span,
			"@align requires an integer literal argument")
		return
	}

	// Parse the integer value from the string representation
	valueStr := tc.lookupName(lit.Value)
	value, err := strconv.ParseUint(valueStr, 10, 64)
	if err != nil {
		tc.report(diag.SemaAttrAlignInvalidValue, argExpr.Span,
			"@align argument is not a valid integer")
		return
	}

	// Check if it's a power of 2
	// A number is a power of 2 if: (value & (value - 1)) == 0 && value != 0
	if value == 0 || (value&(value-1)) != 0 {
		tc.report(diag.SemaAttrAlignNotPowerOfTwo, argExpr.Span,
			"@align argument must be a positive power of 2 (1, 2, 4, 8, 16, ...); got %d", value)
		return
	}
}

// validateBackendParameter validates that @backend("target") has a known target
func (tc *typeChecker) validateBackendParameter(info AttrInfo) bool {
	if len(info.Args) == 0 {
		tc.report(diag.SemaAttrMissingParameter, info.Span,
			"@backend requires a target argument: @backend(\"cpu\")")
		return false
	}

	// Get the first argument expression
	argExpr := tc.builder.Exprs.Get(info.Args[0])

	// Check if it's a literal
	if argExpr.Kind != ast.ExprLit {
		tc.report(diag.SemaAttrBackendInvalidArg, argExpr.Span,
			"@backend requires a string literal argument")
		return false
	}

	// Get the literal data
	lit, ok := tc.builder.Exprs.Literal(info.Args[0])
	if !ok || lit.Kind != ast.ExprLitString {
		tc.report(diag.SemaAttrBackendInvalidArg, argExpr.Span,
			"@backend requires a string literal argument")
		return false
	}

	// Get the string value
	target := tc.lookupName(lit.Value)
	// Strip quotes from string literal
	target = strings.Trim(target, "\"")

	// Known backend targets
	knownTargets := map[string]bool{
		"cpu":    true,
		"gpu":    true,
		"tpu":    true,
		"wasm":   true,
		"native": true,
	}

	if !knownTargets[target] {
		// Issue a warning for unknown targets (not an error - might be valid in future)
		tc.report(diag.SemaAttrBackendUnknown, argExpr.Span,
			"unknown backend target '%s'; known targets: cpu, gpu, tpu, wasm, native", target)
	}

	return true
}

// isLockType checks if a type is Mutex or RwLock
func (tc *typeChecker) isLockType(typeID types.TypeID) bool {
	if typeID == types.NoTypeID {
		return false
	}
	typeName := tc.typeLabel(typeID)
	return typeName == "Mutex" || typeName == "RwLock"
}

// isConditionOrSemaphore checks if a type is Condition or Semaphore
func (tc *typeChecker) isConditionOrSemaphore(typeID types.TypeID) bool {
	if typeID == types.NoTypeID {
		return false
	}
	typeName := tc.typeLabel(typeID)
	return typeName == "Condition" || typeName == "Semaphore"
}

// isAtomicCompatibleType checks if a type is valid for @atomic (int, uint, bool, *T)
func (tc *typeChecker) isAtomicCompatibleType(typeID types.TypeID) bool {
	if typeID == types.NoTypeID {
		return false
	}
	// Check for pointer types first
	if t, ok := tc.types.Lookup(typeID); ok && t.Kind == types.KindPointer {
		return true
	}
	// Check primitive types
	typeName := tc.typeLabel(typeID)
	return typeName == "int" || typeName == "uint" || typeName == "bool"
}

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

// recordTypeAttrs stores attributes for a type for later lookup
func (tc *typeChecker) recordTypeAttrs(typeID types.TypeID, infos []AttrInfo) {
	if tc.typeAttrs == nil {
		tc.typeAttrs = make(map[types.TypeID][]AttrInfo)
	}
	tc.typeAttrs[typeID] = infos
	if tc.types != nil && typeID != types.NoTypeID {
		tc.types.SetTypeLayoutAttrs(typeID, tc.typeLayoutAttrsFromInfos(infos))
	}
	if _, ok := hasAttr(infos, "copy"); ok && typeID != types.NoTypeID {
		if tc.copyTypes == nil {
			tc.copyTypes = make(map[types.TypeID]struct{})
		}
		tc.copyTypes[typeID] = struct{}{}
	}
}

// typeHasAttr checks if a type has the specified attribute
func (tc *typeChecker) typeHasAttr(typeID types.TypeID, attrName string) bool {
	infos, ok := tc.typeAttrs[typeID]
	if !ok {
		return false
	}
	_, found := hasAttr(infos, attrName)
	return found
}

// validateTypeAttrs validates all attributes on a type declaration
func (tc *typeChecker) validateTypeAttrs(typeItem *ast.TypeItem, typeID types.TypeID) {
	// Collect attributes
	infos := tc.collectAttrs(typeItem.AttrStart, typeItem.AttrCount)
	if len(infos) == 0 {
		return
	}

	// Validate target applicability
	tc.validateAttrs(typeItem.AttrStart, typeItem.AttrCount, ast.AttrTargetType, diag.SemaError)

	// Check conflicts
	tc.validateAllConflicts(infos)

	// Validate parameters
	if alignInfo, ok := hasAttr(infos, "align"); ok {
		tc.validateAlignParameter(alignInfo)
	}

	// Record for later lookup
	tc.recordTypeAttrs(typeID, infos)

	// Validate @send type field composition
	tc.validateSendTypeFields(typeID, typeItem.Span)

	// Validate @copy attribute (all fields must be Copy)
	tc.validateCopyAttr(typeID, typeItem.Span)
}

// validateSendTypeFields checks that @send types only contain sendable fields
func (tc *typeChecker) validateSendTypeFields(typeID types.TypeID, span source.Span) {
	// Only validate types with @send attribute
	if !tc.typeHasAttr(typeID, "send") {
		return
	}

	structInfo, ok := tc.types.StructInfo(typeID)
	if !ok || structInfo == nil {
		return // Not a struct, nothing to validate
	}

	for i, field := range structInfo.Fields {
		fieldType := tc.valueType(field.Type)

		// Check if field is @atomic or @guarded_by (these are considered safe for @send)
		if tc.fieldHasAttr(typeID, i, "atomic") || tc.fieldHasAttr(typeID, i, "guarded_by") {
			continue
		}

		// Check if field type is sendable
		if !tc.isSendableType(fieldType) {
			fieldName := tc.lookupName(field.Name)
			fieldTypeName := tc.typeLabel(fieldType)
			tc.report(diag.SemaSendContainsNonsend, span,
				"type marked as @send but field '%s' has non-sendable type '%s'",
				fieldName, fieldTypeName)
		}
	}
}

// isSendableType checks if a type can be safely sent between tasks/threads
func (tc *typeChecker) isSendableType(typeID types.TypeID) bool {
	if typeID == types.NoTypeID {
		return false
	}

	// Primitives are always sendable
	typeName := tc.typeLabel(typeID)
	switch typeName {
	case "int", "uint", "float", "bool", "string", "nothing", "unit":
		return true
	}

	// Check if type has @nosend - not sendable
	if tc.typeHasAttr(typeID, "nosend") {
		return false
	}

	// Check if type has @send - explicitly sendable
	if tc.typeHasAttr(typeID, "send") {
		return true
	}

	// Check pointer types - pointer to @nosend is not sendable
	if t, ok := tc.types.Lookup(typeID); ok && t.Kind == types.KindPointer {
		elemType := t.Elem
		if tc.typeHasAttr(elemType, "nosend") {
			return false
		}
		// Check if element type itself is sendable
		return tc.isSendableType(elemType)
	}

	// Struct without @send/@nosend: check all fields recursively
	structInfo, ok := tc.types.StructInfo(typeID)
	if ok && structInfo != nil {
		for _, field := range structInfo.Fields {
			if !tc.isSendableType(tc.valueType(field.Type)) {
				return false
			}
		}
		return true
	}

	// Default: consider sendable (primitives, aliases to primitives, etc.)
	return true
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

// validateCopyAttr validates @copy attribute on a type declaration
// Checks that all fields are Copy types (recursively)
func (tc *typeChecker) validateCopyAttr(typeID types.TypeID, span source.Span) {
	if !tc.typeHasAttr(typeID, "copy") {
		return
	}
	// Use a map to track visited types: false = in progress, true = validated
	visited := make(map[types.TypeID]bool)
	tc.validateCopyFields(typeID, span, visited)
}

// validateCopyFields recursively checks that all fields of a @copy type are Copy
func (tc *typeChecker) validateCopyFields(typeID types.TypeID, span source.Span, visited map[types.TypeID]bool) bool {
	// Check for cycles: if we've seen this type before
	if done, inProgress := visited[typeID]; inProgress {
		if !done {
			// Cycle detected - we're still processing this type
			tc.report(diag.SemaAttrCopyCyclicDep, span,
				"@copy type '%s' has cyclic dependency", tc.typeLabel(typeID))
			return false
		}
		// Already validated successfully
		return true
	}

	// Mark as in progress
	visited[typeID] = false

	// Get struct info
	structInfo, ok := tc.types.StructInfo(typeID)
	if !ok || structInfo == nil {
		// Not a struct - check union
		unionInfo, ok := tc.types.UnionInfo(typeID)
		if ok && unionInfo != nil {
			// Validate union members
			for _, member := range unionInfo.Members {
				switch member.Kind {
				case types.UnionMemberType:
					if !tc.isExpandedCopyType(member.Type, span, visited) {
						typeName := tc.typeLabel(typeID)
						memberTypeName := tc.typeLabel(member.Type)
						tc.report(diag.SemaAttrCopyNonCopyField, span,
							"@copy union '%s' has non-Copy member of type '%s'",
							typeName, memberTypeName)
						return false
					}
				case types.UnionMemberTag:
					for _, tagArg := range member.TagArgs {
						if tc.isExpandedCopyType(tagArg, span, visited) {
							continue
						}
						typeName := tc.typeLabel(typeID)
						tagName := tc.lookupName(member.TagName)
						argTypeName := tc.typeLabel(tagArg)
						tc.report(diag.SemaAttrCopyNonCopyField, span,
							"@copy union '%s' tag '%s' contains non-Copy type '%s'",
							typeName, tagName, argTypeName)
						return false
					}
				case types.UnionMemberNothing:
					// nothing is always Copy
					continue
				}
			}
		}
		// Mark as validated
		visited[typeID] = true
		return true
	}

	// Validate struct fields
	for _, field := range structInfo.Fields {
		fieldType := tc.valueType(field.Type)
		if !tc.isExpandedCopyType(fieldType, span, visited) {
			typeName := tc.typeLabel(typeID)
			fieldName := tc.lookupName(field.Name)
			fieldTypeName := tc.typeLabel(fieldType)
			tc.report(diag.SemaAttrCopyNonCopyField, span,
				"@copy type '%s' has non-Copy field '%s' of type '%s'",
				typeName, fieldName, fieldTypeName)
			return false
		}
	}

	// Mark as validated
	visited[typeID] = true
	return true
}

// isExpandedCopyType checks if a type is Copy in the expanded sense:
// either a builtin Copy type or a user type with @copy attribute
func (tc *typeChecker) isExpandedCopyType(typeID types.TypeID, span source.Span, visited map[types.TypeID]bool) bool {
	if typeID == types.NoTypeID {
		return false
	}

	// Resolve alias
	resolved := tc.resolveAlias(typeID)

	// Check builtin Copy types first
	if tc.types != nil && tc.types.IsCopy(resolved) {
		return true
	}

	// Check for @copy attribute on user types
	if tc.typeHasAttr(resolved, "copy") {
		// Recursively validate the @copy type
		return tc.validateCopyFields(resolved, span, visited)
	}

	return false
}

// validateFunctionAttrs validates all attributes on a function declaration
func (tc *typeChecker) validateFunctionAttrs(fnItem *ast.FnItem, ownerTypeID types.TypeID) {
	// Collect attributes
	infos := tc.collectAttrs(fnItem.AttrStart, fnItem.AttrCount)
	if len(infos) == 0 {
		return
	}

	// Validate target applicability
	tc.validateAttrs(fnItem.AttrStart, fnItem.AttrCount, ast.AttrTargetFn, diag.SemaError)

	// Check conflicts: @nonblocking vs @waits_on
	tc.checkConflict(infos, "nonblocking", "waits_on", diag.SemaAttrNonblockingWaitsOn)

	// Validate @backend parameter
	if backendInfo, ok := hasAttr(infos, "backend"); ok {
		tc.validateBackendParameter(backendInfo)
	}

	// Validate @waits_on parameter (field reference) with type checking
	if waitsInfo, ok := hasAttr(infos, "waits_on"); ok {
		condFieldType := tc.validateFieldReferenceWithType(waitsInfo, ownerTypeID,
			diag.SemaAttrWaitsOnNotField,
			"@waits_on requires a field name argument: @waits_on(\"condition\")")
		// Validate that the referenced field is a Condition/Semaphore
		if condFieldType != types.NoTypeID && !tc.isConditionOrSemaphore(condFieldType) {
			argExpr := tc.builder.Exprs.Get(waitsInfo.Args[0])
			tc.report(diag.SemaAttrWaitsOnNotCondition, argExpr.Span,
				"@waits_on field must be of type Condition or Semaphore, got '%s'",
				tc.typeLabel(condFieldType))
		}
	}

	// Validate @requires_lock parameter (field reference) with type checking
	if requiresInfo, ok := hasAttr(infos, "requires_lock"); ok {
		lockFieldType := tc.validateFieldReferenceWithType(requiresInfo, ownerTypeID,
			diag.SemaAttrRequiresLockNotField,
			"@requires_lock requires a field name argument: @requires_lock(\"lock\")")
		// Validate that the referenced field is a Mutex/RwLock
		if lockFieldType != types.NoTypeID && !tc.isLockType(lockFieldType) {
			argExpr := tc.builder.Exprs.Get(requiresInfo.Args[0])
			tc.report(diag.SemaLockFieldNotLockType, argExpr.Span,
				"@requires_lock field must be of type Mutex or RwLock, got '%s'",
				tc.typeLabel(lockFieldType))
		}
	}

	// Validate @acquires_lock parameter (field reference) with type checking
	if acquiresInfo, ok := hasAttr(infos, "acquires_lock"); ok {
		lockFieldType := tc.validateFieldReferenceWithType(acquiresInfo, ownerTypeID,
			diag.SemaLockAcquiresNotField,
			"@acquires_lock requires a field name argument: @acquires_lock(\"lock\")")
		// Validate that the referenced field is a Mutex/RwLock
		if lockFieldType != types.NoTypeID && !tc.isLockType(lockFieldType) {
			argExpr := tc.builder.Exprs.Get(acquiresInfo.Args[0])
			tc.report(diag.SemaLockFieldNotLockType, argExpr.Span,
				"@acquires_lock field must be of type Mutex or RwLock, got '%s'",
				tc.typeLabel(lockFieldType))
		}
	}

	// Validate @releases_lock parameter (field reference) with type checking
	if releasesInfo, ok := hasAttr(infos, "releases_lock"); ok {
		lockFieldType := tc.validateFieldReferenceWithType(releasesInfo, ownerTypeID,
			diag.SemaLockReleasesNotField,
			"@releases_lock requires a field name argument: @releases_lock(\"lock\")")
		// Validate that the referenced field is a Mutex/RwLock
		if lockFieldType != types.NoTypeID && !tc.isLockType(lockFieldType) {
			argExpr := tc.builder.Exprs.Get(releasesInfo.Args[0])
			tc.report(diag.SemaLockFieldNotLockType, argExpr.Span,
				"@releases_lock field must be of type Mutex or RwLock, got '%s'",
				tc.typeLabel(lockFieldType))
		}
	}
}
