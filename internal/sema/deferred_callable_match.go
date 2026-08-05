package sema

import (
	"slices"

	"surge/internal/source"
	"surge/internal/types"
)

const deferredCallableMatchDepthLimit = 128

func matchCallableType(
	typesIn *types.Interner,
	pattern types.TypeID,
	actual types.TypeID,
	bindings map[types.TypeID]types.TypeID,
	implicitBorrow bool,
	depth int,
) bool {
	if typesIn == nil || pattern == types.NoTypeID || actual == types.NoTypeID || depth >= deferredCallableMatchDepthLimit {
		return false
	}
	if bound, ok := bindings[pattern]; ok {
		if bound == types.NoTypeID {
			if types.ContainsGenericParam(typesIn, actual) {
				return false
			}
			bindings[pattern] = actual
			return true
		}
		return callableTypesEqual(typesIn, bound, actual)
	}
	if info, ok := typesIn.TypeParamInfo(pattern); ok && info != nil {
		return false
	}
	if callableTypesEqual(typesIn, pattern, actual) {
		return true
	}
	pt, pok := typesIn.Lookup(pattern)
	at, aok := typesIn.Lookup(actual)
	if !pok || !aok {
		return false
	}
	if implicitBorrow && pt.Kind == types.KindReference {
		if at.Kind == types.KindReference {
			if pt.Mutable && !at.Mutable {
				return false
			}
			return matchCallableType(typesIn, pt.Elem, at.Elem, bindings, false, depth+1)
		}
		if at.Kind == types.KindOwn {
			actual = at.Elem
		}
		return matchCallableType(typesIn, pt.Elem, actual, bindings, false, depth+1)
	}
	if at.Kind == types.KindAlias && pt.Kind != types.KindAlias {
		if target, ok := typesIn.AliasTarget(actual); ok && target != actual {
			return matchCallableType(typesIn, pattern, target, bindings, implicitBorrow, depth+1)
		}
	}
	if pt.Kind != at.Kind || pt.Mutable != at.Mutable || pt.Width != at.Width || pt.Count != at.Count {
		return false
	}
	switch pt.Kind {
	case types.KindPointer, types.KindReference, types.KindOwn, types.KindFar, types.KindArray:
		return matchCallableType(typesIn, pt.Elem, at.Elem, bindings, false, depth+1)
	case types.KindStruct:
		pName, pDecl, pArgs, pValues, ok := callableStructShape(typesIn, pattern)
		if !ok {
			return false
		}
		aName, aDecl, aArgs, aValues, ok := callableStructShape(typesIn, actual)
		return ok && pName == aName && pDecl == aDecl &&
			callableStructValuesCompatible(typesIn, pArgs, aArgs, pValues, aValues) &&
			matchCallableTypeList(typesIn, pArgs, aArgs, bindings, depth+1)
	case types.KindUnion:
		pName, pDecl, pArgs, ok := callableUnionShape(typesIn, pattern)
		if !ok {
			return false
		}
		aName, aDecl, aArgs, ok := callableUnionShape(typesIn, actual)
		return ok && pName == aName && pDecl == aDecl && matchCallableTypeList(typesIn, pArgs, aArgs, bindings, depth+1)
	case types.KindAlias:
		pName, pDecl, pArgs, ok := callableAliasShape(typesIn, pattern)
		if !ok {
			return false
		}
		aName, aDecl, aArgs, ok := callableAliasShape(typesIn, actual)
		return ok && pName == aName && pDecl == aDecl && matchCallableTypeList(typesIn, pArgs, aArgs, bindings, depth+1)
	case types.KindEnum:
		pName, pDecl, pArgs, ok := callableEnumShape(typesIn, pattern)
		if !ok {
			return false
		}
		aName, aDecl, aArgs, ok := callableEnumShape(typesIn, actual)
		return ok && pName == aName && pDecl == aDecl && matchCallableTypeList(typesIn, pArgs, aArgs, bindings, depth+1)
	case types.KindTuple:
		pInfo, pok := typesIn.TupleInfo(pattern)
		aInfo, aok := typesIn.TupleInfo(actual)
		return pok && aok && pInfo != nil && aInfo != nil && matchCallableTypeList(typesIn, pInfo.Elems, aInfo.Elems, bindings, depth+1)
	case types.KindFn:
		pInfo, pok := typesIn.FnInfo(pattern)
		aInfo, aok := typesIn.FnInfo(actual)
		return pok && aok && pInfo != nil && aInfo != nil &&
			matchCallableTypeList(typesIn, pInfo.Params, aInfo.Params, bindings, depth+1) &&
			matchCallableType(typesIn, pInfo.Result, aInfo.Result, bindings, false, depth+1)
	default:
		return true
	}
}

func matchCallableTypeList(typesIn *types.Interner, patterns, actuals []types.TypeID, bindings map[types.TypeID]types.TypeID, depth int) bool {
	if len(patterns) != len(actuals) {
		return false
	}
	for i := range patterns {
		if !matchCallableType(typesIn, patterns[i], actuals[i], bindings, false, depth+1) {
			return false
		}
	}
	return true
}

func callableTypesEqual(typesIn *types.Interner, left, right types.TypeID) bool {
	if left == right && left != types.NoTypeID {
		return true
	}
	left = resolveCallableAlias(typesIn, left)
	right = resolveCallableAlias(typesIn, right)
	return left != types.NoTypeID && left == right
}

type deferredReceiverTier struct {
	dispatch types.TypeID
	actual   types.TypeID
}

// matchDeferredReceiver mirrors normal receiver lookup without erasing the
// dispatch tier. ABI matching may borrow a value for a reference self, while
// callableReceiverDistance later requires the selected declaration owner to
// be one of the exact alias/base tiers.
func matchDeferredReceiver(
	typesIn *types.Interner,
	pattern types.TypeID,
	actual types.TypeID,
	bindings map[types.TypeID]types.TypeID,
) bool {
	for _, tier := range deferredReceiverTiers(typesIn, actual) {
		trial := make(map[types.TypeID]types.TypeID, len(bindings))
		for param, bound := range bindings {
			trial[param] = bound
		}
		if !matchCallableType(typesIn, pattern, tier.actual, trial, true, 0) {
			continue
		}
		for param := range bindings {
			bindings[param] = trial[param]
		}
		return true
	}
	return false
}

func callableReceiverDistance(typesIn *types.Interner, request, candidate types.TypeID) (int, bool) {
	for distance, tier := range deferredReceiverTiers(typesIn, request) {
		if tier.dispatch == candidate || callableDispatchOwnersEqual(typesIn, candidate, tier.dispatch) {
			return distance, true
		}
	}
	return 0, false
}

func callableDispatchOwnersEqual(typesIn *types.Interner, left, right types.TypeID) bool {
	leftType, leftOK := typesIn.Lookup(left)
	rightType, rightOK := typesIn.Lookup(right)
	if !leftOK || !rightOK || leftType.Kind != types.KindStruct || rightType.Kind != types.KindStruct {
		return false
	}
	leftName, leftDecl, leftArgs, leftValues, ok := callableStructShape(typesIn, left)
	if !ok {
		return false
	}
	rightName, rightDecl, rightArgs, rightValues, ok := callableStructShape(typesIn, right)
	return ok && leftName == rightName && leftDecl == rightDecl &&
		callableStructValuesCompatible(typesIn, leftArgs, rightArgs, leftValues, rightValues) &&
		matchCallableTypeList(typesIn, leftArgs, rightArgs, nil, 0)
}

func callableStructValuesCompatible(typesIn *types.Interner, patternArgs, actualArgs []types.TypeID, patternValues, actualValues []uint64) bool {
	if slices.Equal(patternValues, actualValues) {
		return true
	}
	for _, arg := range patternArgs {
		if types.ContainsGenericParam(typesIn, arg) {
			// The corresponding concrete const TypeID is checked by the normal
			// argument matcher; value metadata is materialized only on concrete
			// nominal instances.
			return true
		}
	}
	return callableValuesMatchConstArgs(typesIn, patternArgs, patternValues) &&
		callableValuesMatchConstArgs(typesIn, actualArgs, actualValues)
}

func callableValuesMatchConstArgs(typesIn *types.Interner, args []types.TypeID, values []uint64) bool {
	if len(values) == 0 {
		return true
	}
	encoded := make([]uint64, 0, len(values))
	for _, arg := range args {
		t, ok := typesIn.Lookup(arg)
		if !ok || t.Kind != types.KindConst {
			continue
		}
		encoded = append(encoded, uint64(t.Count))
	}
	return slices.Equal(encoded, values)
}

func deferredReceiverTiers(typesIn *types.Interner, receiver types.TypeID) []deferredReceiverTier {
	if typesIn == nil || receiver == types.NoTypeID {
		return nil
	}
	tiers := make([]deferredReceiverTier, 0, 4)
	seen := make(map[types.TypeID]struct{}, 4)
	appendTier := func(dispatch, actual types.TypeID) {
		if dispatch == types.NoTypeID || actual == types.NoTypeID {
			return
		}
		if _, ok := seen[dispatch]; ok {
			return
		}
		seen[dispatch] = struct{}{}
		tiers = append(tiers, deferredReceiverTier{dispatch: dispatch, actual: actual})
	}

	appendTier(receiver, receiver)
	if family := deferredReceiverFamily(typesIn, receiver); family != types.NoTypeID {
		appendTier(family, family)
	}
	owner, wrappers := splitDeferredReceiverWrappers(typesIn, receiver)
	if owner == types.NoTypeID {
		return tiers
	}
	valueOwner := owner
	if len(wrappers) > 0 {
		valueOwner = terminalDeferredReceiverAlias(typesIn, valueOwner)
		if valueOwner != types.NoTypeID {
			appendTier(valueOwner, wrapDeferredReceiver(typesIn, valueOwner, wrappers))
		}
	} else if target := terminalDeferredReceiverAlias(typesIn, owner); target != types.NoTypeID && target != owner {
		valueOwner = target
		appendTier(valueOwner, valueOwner)
	}
	if base, ok := typesIn.StructBase(valueOwner); ok && base != types.NoTypeID {
		appendTier(base, wrapDeferredReceiver(typesIn, base, wrappers))
	}
	return tiers
}

func deferredReceiverFamily(typesIn *types.Interner, receiver types.TypeID) types.TypeID {
	resolved := terminalDeferredReceiverAlias(typesIn, receiver)
	t, ok := typesIn.Lookup(resolved)
	if !ok {
		return types.NoTypeID
	}
	switch t.Kind {
	case types.KindInt:
		return typesIn.Builtins().Int
	case types.KindUint:
		return typesIn.Builtins().Uint
	case types.KindFloat:
		return typesIn.Builtins().Float
	default:
		return types.NoTypeID
	}
}

func terminalDeferredReceiverAlias(typesIn *types.Interner, receiver types.TypeID) types.TypeID {
	current := receiver
	for range deferredCallableMatchDepthLimit {
		t, ok := typesIn.Lookup(current)
		if !ok || t.Kind != types.KindAlias {
			return current
		}
		next, ok := typesIn.AliasTarget(current)
		if !ok || next == types.NoTypeID || next == current {
			return current
		}
		current = next
	}
	return current
}

func splitDeferredReceiverWrappers(typesIn *types.Interner, receiver types.TypeID) (types.TypeID, []types.Type) {
	wrappers := make([]types.Type, 0, 2)
	current := receiver
	for range deferredCallableMatchDepthLimit {
		t, ok := typesIn.Lookup(current)
		if !ok {
			return types.NoTypeID, wrappers
		}
		switch t.Kind {
		case types.KindReference, types.KindOwn, types.KindPointer:
			wrappers = append(wrappers, t)
			current = t.Elem
		default:
			return current, wrappers
		}
	}
	return types.NoTypeID, wrappers
}

func wrapDeferredReceiver(typesIn *types.Interner, owner types.TypeID, wrappers []types.Type) types.TypeID {
	out := owner
	for i := len(wrappers) - 1; i >= 0; i-- {
		switch wrappers[i].Kind {
		case types.KindReference:
			out = typesIn.Intern(types.MakeReference(out, wrappers[i].Mutable))
		case types.KindOwn:
			out = typesIn.Intern(types.MakeOwn(out))
		case types.KindPointer:
			out = typesIn.Intern(types.MakePointer(out))
		}
	}
	return out
}

func resolveCallableAlias(typesIn *types.Interner, id types.TypeID) types.TypeID {
	if typesIn == nil {
		return id
	}
	for range 64 {
		t, ok := typesIn.Lookup(id)
		if !ok || t.Kind != types.KindAlias {
			return id
		}
		target, ok := typesIn.AliasTarget(id)
		if !ok || target == types.NoTypeID || target == id {
			return id
		}
		id = target
	}
	return id
}

func callableStructShape(typesIn *types.Interner, id types.TypeID) (source.StringID, source.Span, []types.TypeID, []uint64, bool) {
	info, ok := typesIn.StructInfo(id)
	if !ok || info == nil {
		return source.NoStringID, source.Span{}, nil, nil, false
	}
	return info.Name, info.Decl, info.TypeArgs, info.ValueArgs, true
}

func callableUnionShape(typesIn *types.Interner, id types.TypeID) (source.StringID, source.Span, []types.TypeID, bool) {
	info, ok := typesIn.UnionInfo(id)
	if !ok || info == nil {
		return source.NoStringID, source.Span{}, nil, false
	}
	return info.Name, info.Decl, info.TypeArgs, true
}

func callableAliasShape(typesIn *types.Interner, id types.TypeID) (source.StringID, source.Span, []types.TypeID, bool) {
	info, ok := typesIn.AliasInfo(id)
	if !ok || info == nil {
		return source.NoStringID, source.Span{}, nil, false
	}
	return info.Name, info.Decl, info.TypeArgs, true
}

func callableEnumShape(typesIn *types.Interner, id types.TypeID) (source.StringID, source.Span, []types.TypeID, bool) {
	info, ok := typesIn.EnumInfo(id)
	if !ok || info == nil {
		return source.NoStringID, source.Span{}, nil, false
	}
	return info.Name, info.Decl, info.TypeArgs, true
}
