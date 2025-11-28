package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// ensureBoolContext validates that the given expression can participate in boolean contexts
// like if/while conditions. Accepts plain bool/alias-to-bool or a __bool() -> bool method.
func (tc *typeChecker) ensureBoolContext(expr ast.ExprID, span source.Span) {
	if !expr.IsValid() || tc.types == nil {
		return
	}
	ty := tc.typeExpr(expr)
	if ty == types.NoTypeID {
		return
	}
	boolType := tc.types.Builtins().Bool
	if tc.typesAssignable(boolType, ty, true) {
		return
	}
	if res, found := tc.boolMethodResult(ty); found {
		if res == types.NoTypeID {
			tc.report(diag.SemaInvalidBoolContext, span, "__bool for %s has unknown return type", tc.typeLabel(ty))
			return
		}
		if tc.typesAssignable(boolType, res, true) {
			return
		}
		tc.report(diag.SemaInvalidBoolContext, span, "__bool for %s must return bool, got %s", tc.typeLabel(ty), tc.typeLabel(res))
		return
	}
	tc.report(diag.SemaInvalidBoolContext, span, "type %s has no method __bool() -> bool", tc.typeLabel(ty))
}

// boolMethodResult tries to resolve __bool on the given receiver type and returns its result type.
// The second bool indicates whether a matching receiver+arity method exists.
func (tc *typeChecker) boolMethodResult(recv types.TypeID) (types.TypeID, bool) {
	if recv == types.NoTypeID {
		return types.NoTypeID, false
	}
	if res := tc.boundMethodResult(recv, "__bool", nil); res != types.NoTypeID {
		return res, true
	}
	for _, cand := range tc.typeKeyCandidates(recv) {
		if cand.key == "" {
			continue
		}
		for _, sig := range tc.lookupMagicMethods(cand.key, "__bool") {
			if sig == nil || len(sig.Params) != 1 || !typeKeyEqual(sig.Params[0], cand.key) {
				continue
			}
			res := tc.typeFromKey(sig.Result)
			return tc.adjustAliasUnaryResult(res, cand), true
		}
	}
	return types.NoTypeID, false
}
