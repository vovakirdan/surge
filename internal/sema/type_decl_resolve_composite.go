package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) resolveArrayTypeExpr(id ast.TypeID, span source.Span, scope symbols.ScopeID) types.TypeID {
	arr, ok := tc.builder.Types.Array(id)
	if !ok || arr == nil {
		return types.NoTypeID
	}
	elem := tc.resolveTypeExprWithScope(arr.Elem, scope)
	if elem == types.NoTypeID {
		return types.NoTypeID
	}
	if tc.rejectRefInAggregate(elem, tc.typeSpan(arr.Elem), "array element") {
		return types.NoTypeID
	}
	if tc.isFarType(elem) {
		tc.report(diag.FutFarLocalArrayPostponed, span, "local arrays of `far` handles are not supported yet")
		return types.NoTypeID
	}
	if arr.Kind == ast.ArraySized {
		lengthArg := tc.resolveArrayLengthArg(arr, span)
		if lengthArg == types.NoTypeID {
			tc.report(diag.SemaTypeMismatch, span, "array length must be a constant")
			return types.NoTypeID
		}
		return tc.instantiateArrayFixedWithArg(elem, lengthArg)
	}
	return tc.instantiateArrayType(elem)
}

func (tc *typeChecker) resolveTupleTypeExpr(id ast.TypeID, scope symbols.ScopeID) types.TypeID {
	tup, ok := tc.builder.Types.Tuple(id)
	if !ok || tup == nil {
		return types.NoTypeID
	}
	// Empty tuple is unit type
	if len(tup.Elems) == 0 {
		return tc.types.Builtins().Unit
	}
	elems := make([]types.TypeID, 0, len(tup.Elems))
	for _, elem := range tup.Elems {
		resolved := tc.resolveTypeExprWithScope(elem, scope)
		if resolved == types.NoTypeID {
			return types.NoTypeID
		}
		if tc.rejectRefInAggregate(resolved, tc.typeSpan(elem), "tuple element") {
			return types.NoTypeID
		}
		elems = append(elems, resolved)
	}
	return tc.types.RegisterTuple(elems)
}
