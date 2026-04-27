package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// inferForInElementType extracts the element type from an iterable expression.
// This is used to type the loop variable in for-in statements.
//
// The function checks several sources for the element type:
//  1. Direct Range<T> type - extracts T as the element type
//  2. Array types - returns the element type of the array
//  3. Types with __range() method - calls the method and extracts element from result
//
// If none of these succeed, an error is reported indicating the type
// doesn't implement the iterator protocol.
//
// Examples:
//   - for x in [1, 2, 3] {} // x: int (from array element type)
//   - for x in 0..10 {} // x: int (from Range<int>)
//   - for x in myCollection {} // x: T (from __range() -> Range<T>)
func (tc *typeChecker) inferForInElementType(iterableExpr ast.ExprID, iterableType types.TypeID, span source.Span) types.TypeID {
	if iterableType == types.NoTypeID {
		return types.NoTypeID
	}

	// Check if the iterable is directly a Range<T> type (or a reference to one)
	if elem, ok := tc.rangePayload(iterableType); ok {
		return elem
	}
	base := tc.valueType(iterableType)
	if base == types.NoTypeID {
		base = iterableType
	}
	if base != iterableType {
		if elem, ok := tc.rangePayload(base); ok {
			return elem
		}
	}

	// Check if the iterable is an array type (or a reference to one)
	if elem, ok := tc.arrayElemType(base); ok {
		return elem
	}

	// Look for __range() method that returns a Range<T>
	rangeType, sig, reportedFailure := tc.lookupRangeMethodResult(iterableExpr, iterableType)
	if reportedFailure {
		return types.NoTypeID
	}
	if rangeType != types.NoTypeID {
		if elem, ok := tc.rangePayload(rangeType); ok {
			if sig != nil && len(sig.Params) > 0 {
				if symID := tc.ensureMagicMethodSymbol("__range", sig, span); symID.IsValid() {
					tc.recordRangeSymbol(iterableExpr, symID, rangeType)
					tc.recordMethodCallInstantiation(symID, iterableType, nil, span)
				}
				tc.applyParamOwnership(sig.Params[0], iterableExpr, iterableType, span)
				tc.dropImplicitBorrowForRefParam(iterableExpr, sig.Params[0], iterableType, rangeType, span)
			}
			return elem
		}
	}

	// No valid iteration source found
	tc.report(diag.SemaIteratorNotImplemented, span,
		"type %s does not implement iterator (missing __range method)",
		tc.typeLabel(iterableType))
	return types.NoTypeID
}

// lookupRangeMethodResult looks up the __range magic method for a container type
// and returns the result type of that method.
//
// The __range method is part of Surge's iterator protocol. Container types
// implement __range() to return a Range<T> value that can be iterated.
//
// The function searches through all type key candidates (including generic
// instantiations) to find a matching __range method signature.
//
// Returns types.NoTypeID if no __range method is found. The bool reports whether
// lookup already emitted a more specific diagnostic.
func (tc *typeChecker) lookupRangeMethodResult(containerExpr ast.ExprID, containerType types.TypeID) (types.TypeID, *symbols.FunctionSignature, bool) {
	if containerType == types.NoTypeID {
		return types.NoTypeID, nil, false
	}

	bestCost := -1
	var bestSig *symbols.FunctionSignature
	var bestCand typeKeyCandidate
	var bestResult types.TypeID
	var borrowInfo borrowMatchInfo

	// Search through all possible type key representations
	for _, cand := range tc.typeKeyCandidates(containerType) {
		if cand.key == "" {
			continue
		}

		// Look up magic methods with name "__range"
		methods := tc.lookupMagicMethods(cand.key, "__range")
		for _, sig := range methods {
			if sig == nil || sig.Result == "" || len(sig.Params) != 1 || !tc.selfParamCompatible(containerType, sig.Params[0], cand.key) {
				continue
			}
			cost, ok := tc.magicParamCost(sig.Params[0], containerType, containerExpr, &borrowInfo)
			if !ok {
				continue
			}
			res := tc.typeFromKey(sig.Result)
			if res == types.NoTypeID {
				continue
			}
			if bestCost == -1 || cost < bestCost {
				bestCost = cost
				bestSig = sig
				bestCand = cand
				bestResult = res
			}
		}
	}

	if bestCost == -1 {
		if borrowInfo.expr.IsValid() {
			tc.reportBorrowFailure(&borrowInfo)
			return types.NoTypeID, nil, true
		}
		return types.NoTypeID, nil, false
	}
	return tc.adjustAliasUnaryResult(bestResult, bestCand), bestSig, false
}
