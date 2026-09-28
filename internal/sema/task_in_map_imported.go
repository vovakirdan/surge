package sema

import (
	"slices"

	"surge/internal/types"
)

// An imported generic struct is instantiated with its field types passed through
// substituteTypeParams, which does not rebuild a nominal: `Map<int, T>` in a field of
// `Box<T>` stays `Map<int, T>` in `Box<Task<int>>`. The task-in-map rule therefore reads
// a nominal's parts as substituteImportedType would, binding each generic parameter by its
// index to the nominal's own type arguments. A part already substituted holds no parameter
// of the nominal and reads as it is; a parameter of the generic code being checked is in
// scope, is not the nominal's, and reads as no task. When the imported instantiation
// substitutes its field types, this reading keeps giving the same answer.

// nominalHoldsTaskMap reports a Map holding a task somewhere in the nominal id.
func (tc *typeChecker) nominalHoldsTaskMap(id types.TypeID) bool {
	if tc.holdsTaskMap(id, make(map[types.TypeID]bool)) {
		return true
	}
	args := tc.nominalTypeArgs(id)
	return len(args) != 0 && tc.holdsTaskUnder(id, args, true, make(map[types.TypeID]bool))
}

func (tc *typeChecker) nominalTypeArgs(id types.TypeID) []types.TypeID {
	if tc.types == nil {
		return nil
	}
	if info, ok := tc.types.StructInfo(id); ok && info != nil {
		return info.TypeArgs
	}
	if info, ok := tc.types.UnionInfo(id); ok && info != nil {
		return info.TypeArgs
	}
	return nil
}

// holdsTaskUnder is holdsTask (wantMap false) or holdsTaskMap (wantMap true) with each
// generic parameter read as args[its index].
func (tc *typeChecker) holdsTaskUnder(id types.TypeID, args []types.TypeID, wantMap bool, seen map[types.TypeID]bool) bool {
	if tc.types == nil {
		return false
	}
	if info, ok := tc.types.TypeParamInfo(id); ok && info != nil {
		if idx := int(info.Index); idx < len(args) && args[idx] != id && !slices.Contains(tc.typeParamStack, id) {
			if wantMap {
				return tc.holdsTaskMap(args[idx], make(map[types.TypeID]bool))
			}
			return tc.holdsTask(args[idx], make(map[types.TypeID]bool))
		}
		return false
	}
	id = tc.taskWalkType(id)
	if id == types.NoTypeID || seen[id] {
		return false
	}
	seen[id] = true
	if !wantMap && tc.isCoreTask(id) {
		return true
	}
	if key, value, ok := tc.types.MapInfo(id); wantMap && ok &&
		(tc.holdsTaskUnder(key, args, false, make(map[types.TypeID]bool)) ||
			tc.holdsTaskUnder(value, args, false, make(map[types.TypeID]bool))) {
		return true
	}
	return tc.anyTaskWalkPart(id, func(part types.TypeID) bool { return tc.holdsTaskUnder(part, args, wantMap, seen) })
}
