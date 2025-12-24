package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

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
	visited := make(map[types.TypeID]struct{})
	return tc.isSendableTypeWithVisited(typeID, visited)
}

// isSendableTypeWithVisited checks sendability with cycle detection
func (tc *typeChecker) isSendableTypeWithVisited(typeID types.TypeID, visited map[types.TypeID]struct{}) bool {
	if typeID == types.NoTypeID {
		return false
	}

	// Check for cycles - if already visited, assume sendable to break cycle
	if _, seen := visited[typeID]; seen {
		return true
	}
	visited[typeID] = struct{}{}

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
		return tc.isSendableTypeWithVisited(elemType, visited)
	}

	// Struct without @send/@nosend: check all fields recursively
	structInfo, ok := tc.types.StructInfo(typeID)
	if ok && structInfo != nil {
		for _, field := range structInfo.Fields {
			if !tc.isSendableTypeWithVisited(tc.valueType(field.Type), visited) {
				return false
			}
		}
		return true
	}

	// Default: consider sendable (primitives, aliases to primitives, etc.)
	return true
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
	if v, ok := visited[typeID]; ok {
		if !v {
			// v is false means in-progress, cycle detected
			tc.report(diag.SemaAttrCopyCyclicDep, span,
				"@copy type '%s' has cyclic dependency", tc.typeLabel(typeID))
			return false
		}
		// v is true means already validated successfully
		return true
	}

	// Mark as in progress (false = not yet validated)
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
