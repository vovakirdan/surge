package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
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
		tc.dropImplicitBorrow(expr, boolType, ty, span)
		return
	}
	if res, sig, found, reportedFailure := tc.boolMethodResult(expr, ty); found {
		if reportedFailure {
			return
		}
		if res == types.NoTypeID {
			tc.report(diag.SemaInvalidBoolContext, span, "__bool for %s has unknown return type", tc.typeLabel(ty))
			return
		}
		if tc.typesAssignable(boolType, res, true) {
			if sig == nil {
				tc.recordBoolBoundMethod(expr)
				return
			}
			if len(sig.Params) > 0 {
				if symID := tc.ensureMagicMethodSymbol("__bool", sig, span); symID.IsValid() {
					tc.recordBoolSymbol(expr, symID)
					tc.recordMethodCallInstantiation(symID, ty, nil, span)
				}
				tc.applyParamOwnership(sig.Params[0], expr, ty, span)
				tc.dropImplicitBorrowForRefParam(expr, sig.Params[0], ty, res, span)
			}
			return
		}
		tc.report(diag.SemaInvalidBoolContext, span, "__bool for %s must return bool, got %s", tc.typeLabel(ty), tc.typeLabel(res))
		return
	}
	tc.report(diag.SemaInvalidBoolContext, span, "type %s has no method __bool() -> bool", tc.typeLabel(ty))
}

// boolMethodResult tries to resolve __bool on the given receiver type and returns its result type.
// The bools indicate whether a matching receiver+arity method exists and whether
// lookup already reported a more specific failure.
func (tc *typeChecker) boolMethodResult(recvExpr ast.ExprID, recv types.TypeID) (types.TypeID, *symbols.FunctionSignature, bool, bool) {
	if recv == types.NoTypeID {
		return types.NoTypeID, nil, false, false
	}
	if res := tc.boundMethodResult(recv, "__bool", nil); res != types.NoTypeID {
		return res, nil, true, false
	}
	bestCost := -1
	var bestSig *symbols.FunctionSignature
	var bestCand typeKeyCandidate
	var borrowInfo borrowMatchInfo
	for _, cand := range tc.typeKeyCandidates(recv) {
		if cand.key == "" {
			continue
		}
		for _, sig := range tc.lookupMagicMethods(cand.key, "__bool") {
			if sig == nil || len(sig.Params) != 1 || !tc.selfParamCompatible(recv, sig.Params[0], cand.key) {
				continue
			}
			cost, ok := tc.magicParamCost(sig.Params[0], recv, recvExpr, &borrowInfo)
			if !ok {
				continue
			}
			if bestCost == -1 || cost < bestCost {
				bestCost = cost
				bestSig = sig
				bestCand = cand
			}
		}
	}
	if bestCost == -1 {
		if borrowInfo.expr.IsValid() {
			tc.reportBorrowFailure(&borrowInfo)
			return types.NoTypeID, nil, true, true
		}
		return types.NoTypeID, nil, false, false
	}
	res := tc.typeFromKey(bestSig.Result)
	return tc.adjustAliasUnaryResult(res, bestCand), bestSig, true, false
}
