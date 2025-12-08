package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

// checkGuardedFieldAccess validates that @guarded_by field is accessed with lock held.
// isWrite determines if write lock is required (for assignments).
// Reports SemaLockGuardedByViolation if the appropriate lock is not held.
func (tc *typeChecker) checkGuardedFieldAccess(la *lockAnalyzer, exprID ast.ExprID, isWrite bool, span source.Span) {
	if !exprID.IsValid() {
		return
	}

	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil || expr.Kind != ast.ExprMember {
		return
	}

	// Get member access details
	member, ok := tc.builder.Exprs.Member(exprID)
	if !ok || member == nil {
		return
	}

	// Get the type of the base expression
	baseType, ok := tc.result.ExprTypes[member.Target]
	if !ok || baseType == 0 {
		return
	}

	// Strip references to get the underlying struct type
	baseType = tc.valueType(baseType)

	// Get struct info to find field index
	structInfo, ok := tc.types.StructInfo(baseType)
	if !ok || structInfo == nil {
		return
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
		return
	}

	// Check if field has @guarded_by
	lockFieldName := tc.getFieldGuardedBy(baseType, fieldIndex)
	if lockFieldName == 0 {
		return // Field is not guarded
	}

	// Get the base symbol to build LockKey
	baseSym := tc.getExprSymbol(member.Target)
	if !baseSym.IsValid() {
		return
	}

	// Check if appropriate lock is held
	lockHeld := false
	if isWrite {
		// Write access requires Mutex or RwWrite
		mutexKey := LockKey{Base: baseSym, FieldName: lockFieldName, Kind: LockKindMutex}
		rwWriteKey := LockKey{Base: baseSym, FieldName: lockFieldName, Kind: LockKindRwWrite}
		lockHeld = la.state.IsHeld(mutexKey) || la.state.IsHeld(rwWriteKey)
	} else {
		// Read access allows any lock kind
		mutexKey := LockKey{Base: baseSym, FieldName: lockFieldName, Kind: LockKindMutex}
		rwReadKey := LockKey{Base: baseSym, FieldName: lockFieldName, Kind: LockKindRwRead}
		rwWriteKey := LockKey{Base: baseSym, FieldName: lockFieldName, Kind: LockKindRwWrite}
		lockHeld = la.state.IsHeld(mutexKey) || la.state.IsHeld(rwReadKey) || la.state.IsHeld(rwWriteKey)
	}

	if !lockHeld {
		fieldName := tc.lookupName(member.Field)
		lockName := tc.lookupName(lockFieldName)
		if isWrite {
			tc.report(diag.SemaLockGuardedByViolation, span,
				"writing to @guarded_by field '%s' requires holding lock '%s' (mutex or write lock)",
				fieldName, lockName)
		} else {
			tc.report(diag.SemaLockGuardedByViolation, span,
				"reading @guarded_by field '%s' requires holding lock '%s'",
				fieldName, lockName)
		}
	}
}

// walkAssignmentTargetBase walks only the base of an assignment target expression,
// skipping the top-level member/index to avoid double-reporting read errors
// when checking a write access.
func (tc *typeChecker) walkAssignmentTargetBase(la *lockAnalyzer, exprID ast.ExprID) {
	if !exprID.IsValid() {
		return
	}

	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return
	}

	switch expr.Kind {
	case ast.ExprMember:
		// For member access like c.value, only walk the base 'c'
		memberData, ok := tc.builder.Exprs.Member(exprID)
		if ok && memberData != nil {
			tc.checkExprForLockOps(la, memberData.Target)
		}

	case ast.ExprIndex:
		// For index access like arr[i].field, walk both base and index
		indexData, ok := tc.builder.Exprs.Index(exprID)
		if ok && indexData != nil {
			tc.walkAssignmentTargetBase(la, indexData.Target)
			tc.checkExprForLockOps(la, indexData.Index)
		}

	default:
		// For other expressions (like plain identifiers), walk normally
		tc.checkExprForLockOps(la, exprID)
	}
}
