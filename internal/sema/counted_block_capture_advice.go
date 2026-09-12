package sema

import "surge/internal/types"

// These queries predict only the remaining capture rules after a numeric-width
// repair. Numeric width changes neither Copy nor the declaration attributes, so
// no replacement TypeID or speculative type-checker invocation is needed.
func (tc *typeChecker) countedOnCaptureAllowsWidthRepair(capType types.TypeID, capture blockingCapture) bool {
	if tc == nil || tc.types == nil || tc.result == nil || tc.isReferenceType(capType) || tc.isFarType(capType) {
		return false
	}
	nominal := tc.valueType(capType)
	elem, array := tc.types.DynamicArrayElem(nominal)
	if (array && !tc.shardMovableElement(elem)) || tc.result.DynamicArrayStaysUnchecked(capType) {
		return false
	}
	owned := tc.isOwnType(capType)
	if !owned && tc.result.IsCopyType(capType) {
		return true
	}
	// Read the live checker attributes. Result.TypeAttrFacts is populated only
	// after this capture has been judged, and would silently miss these vetoes.
	switch {
	case tc.typeHasAttr(nominal, "shard_pinned"), tc.typeHasAttr(nominal, "nosend"):
		return false
	case tc.typeHasAttr(nominal, "shard_movable"):
		return tc.shardMovableElement(nominal)
	case tc.typeHasAttr(nominal, "send"), owned && tc.typeHasAttr(nominal, "copy"):
		return false
	case owned && tc.result.IsCopyType(nominal):
		return true
	case array:
		return !tc.isArrayViewBinding(capture.symID) && !tc.holdsAnArrayView(capture.symID)
	default:
		return false
	}
}

func (tc *typeChecker) countedBlockingCaptureAllowsWidthRepair(capType types.TypeID, capture blockingCapture) bool {
	if tc == nil || tc.types == nil || tc.result == nil || tc.isReferenceType(capType) ||
		tc.isLocalTaskBinding(capture.symID) || tc.result.DynamicArrayStaysUnchecked(capType) {
		return false
	}
	return !tc.countedRepairHasNosend(tc.valueType(capType), make(map[types.TypeID]bool))
}

// Match checkSpawnSendability's nominal and nested-struct checks without
// reporting a second diagnostic. This is advice permission, not a broader
// sendability policy for tuples, unions or runtime payloads.
func (tc *typeChecker) countedRepairHasNosend(id types.TypeID, seen map[types.TypeID]bool) bool {
	if seen[id] {
		return false
	}
	seen[id] = true
	if tc.typeHasAttr(id, "nosend") {
		return true
	}
	info, ok := tc.types.StructInfo(id)
	if !ok || info == nil {
		return false
	}
	for _, field := range info.Fields {
		if tc.countedRepairHasNosend(tc.valueType(field.Type), seen) {
			return true
		}
	}
	return false
}
