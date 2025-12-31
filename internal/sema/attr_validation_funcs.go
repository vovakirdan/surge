package sema

import (
	"strconv"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

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

// validateParamAttrs validates attributes on function parameters.
func (tc *typeChecker) validateParamAttrs(fnItem *ast.FnItem) {
	if tc.builder == nil || fnItem == nil {
		return
	}
	paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
	for _, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil || param.AttrCount == 0 || !param.AttrStart.IsValid() {
			continue
		}
		tc.validateAttrs(param.AttrStart, param.AttrCount, ast.AttrTargetParam, diag.SemaError)
	}
}

// validateFunctionAttrs validates all attributes on a function declaration
func (tc *typeChecker) validateFunctionAttrs(fnItem *ast.FnItem, symID symbols.SymbolID, ownerTypeID types.TypeID) {
	// Collect attributes
	infos := tc.collectAttrs(fnItem.AttrStart, fnItem.AttrCount)
	if len(infos) == 0 {
		tc.validateParamAttrs(fnItem)
		return
	}

	// Record for later lookup (for deprecation checks)
	if symID.IsValid() {
		tc.recordSymbolAttrs(symID, infos)
	}

	// Validate target applicability
	tc.validateAttrs(fnItem.AttrStart, fnItem.AttrCount, ast.AttrTargetFn, diag.SemaError)
	tc.validateParamAttrs(fnItem)

	// Check conflicts: @nonblocking vs @waits_on
	tc.checkConflict(infos, "nonblocking", "waits_on", diag.SemaAttrNonblockingWaitsOn)
	if failfastInfo, ok := hasAttr(infos, "failfast"); ok {
		if fnItem.Flags&ast.FnModifierAsync == 0 {
			tc.report(diag.SemaFailfastNonAsync, failfastInfo.Span, "@failfast can only be applied to async blocks")
		}
	}

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

// recordSymbolAttrs stores attributes for a symbol (function, let, const) for later lookup
func (tc *typeChecker) recordSymbolAttrs(symID symbols.SymbolID, infos []AttrInfo) {
	if tc.symbolAttrs == nil {
		tc.symbolAttrs = make(map[symbols.SymbolID][]AttrInfo)
	}
	tc.symbolAttrs[symID] = infos
}

// getDeprecatedMessage extracts the optional message from @deprecated("msg") attribute
func (tc *typeChecker) getDeprecatedMessage(attrs []AttrInfo) string {
	info, ok := hasAttr(attrs, "deprecated")
	if !ok {
		return ""
	}
	if len(info.Args) == 0 {
		return ""
	}
	argExpr := tc.builder.Exprs.Get(info.Args[0])
	if argExpr.Kind != ast.ExprLit {
		return ""
	}
	lit, ok := tc.builder.Exprs.Literal(info.Args[0])
	if !ok || lit.Kind != ast.ExprLitString {
		return ""
	}
	msg := tc.lookupName(lit.Value)
	return strings.Trim(msg, "\"")
}

// checkDeprecatedSymbol emits a warning if the symbol is deprecated
func (tc *typeChecker) checkDeprecatedSymbol(symID symbols.SymbolID, kind string, usageSpan source.Span) {
	if !symID.IsValid() {
		return
	}
	attrs, ok := tc.symbolAttrs[symID]
	if !ok {
		return
	}
	if _, deprecated := hasAttr(attrs, "deprecated"); !deprecated {
		return
	}
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return
	}
	name := tc.lookupName(sym.Name)
	msg := tc.getDeprecatedMessage(attrs)
	if msg != "" {
		tc.warn(diag.SemaDeprecatedUsage, usageSpan,
			"%s '%s' deprecated. %s", kind, name, msg)
	} else {
		tc.warn(diag.SemaDeprecatedUsage, usageSpan,
			"%s '%s' deprecated.", kind, name)
	}
}

// checkDeprecatedType emits a warning if the type is deprecated
func (tc *typeChecker) checkDeprecatedType(typeID types.TypeID, usageSpan source.Span) {
	attrs, ok := tc.typeAttrs[typeID]
	if !ok {
		return
	}
	if _, deprecated := hasAttr(attrs, "deprecated"); !deprecated {
		return
	}
	typeName := tc.typeLabel(typeID)
	msg := tc.getDeprecatedMessage(attrs)
	if msg != "" {
		tc.warn(diag.SemaDeprecatedUsage, usageSpan,
			"type '%s' deprecated. %s", typeName, msg)
	} else {
		tc.warn(diag.SemaDeprecatedUsage, usageSpan,
			"type '%s' deprecated.", typeName)
	}
}

// checkDeprecatedField emits a warning if the field is deprecated
func (tc *typeChecker) checkDeprecatedField(baseType types.TypeID, fieldIndex int, fieldName source.StringID, usageSpan source.Span) {
	key := fieldKey{TypeID: baseType, FieldIndex: fieldIndex}
	attrs, ok := tc.fieldAttrs[key]
	if !ok {
		return
	}
	if _, deprecated := hasAttr(attrs, "deprecated"); !deprecated {
		return
	}
	name := tc.lookupName(fieldName)
	msg := tc.getDeprecatedMessage(attrs)
	if msg != "" {
		tc.warn(diag.SemaDeprecatedUsage, usageSpan,
			"field '%s' deprecated. %s", name, msg)
	} else {
		tc.warn(diag.SemaDeprecatedUsage, usageSpan,
			"field '%s' deprecated.", name)
	}
}

// validateLetAttrs validates all attributes on a let/const declaration
func (tc *typeChecker) validateLetAttrs(letItem *ast.LetItem, symID symbols.SymbolID) {
	// Collect attributes
	infos := tc.collectAttrs(letItem.AttrStart, letItem.AttrCount)
	if len(infos) == 0 {
		return
	}

	// Validate target applicability
	tc.validateAttrs(letItem.AttrStart, letItem.AttrCount, ast.AttrTargetLet, diag.SemaError)

	// Record for later lookup (for deprecation checks)
	tc.recordSymbolAttrs(symID, infos)
}

// validateConstAttrs validates all attributes on a const declaration
func (tc *typeChecker) validateConstAttrs(constItem *ast.ConstItem, symID symbols.SymbolID) {
	// Collect attributes
	infos := tc.collectAttrs(constItem.AttrStart, constItem.AttrCount)
	if len(infos) == 0 {
		return
	}

	// Validate target applicability
	tc.validateAttrs(constItem.AttrStart, constItem.AttrCount, ast.AttrTargetLet, diag.SemaError)

	// Record for later lookup (for deprecation checks)
	tc.recordSymbolAttrs(symID, infos)
}
