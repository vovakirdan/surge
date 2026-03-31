package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) methodResultType(member *ast.ExprMemberData, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID, span source.Span, staticReceiver bool) types.TypeID {
	if member == nil || tc.magic == nil {
		return types.NoTypeID
	}
	name := tc.lookupExportedName(member.Field)
	if name == "" {
		return types.NoTypeID
	}
	if recv != types.NoTypeID {
		if res := tc.boundMethodResult(recv, name, args); res != types.NoTypeID {
			return res
		}
	}
	actualRecvKey := tc.typeKeyForType(recv)
	if actualRecvKey == "" {
		tc.report(diag.SemaUnresolvedSymbol, span, "%s has no method %s", tc.typeLabel(recv), name)
		return types.NoTypeID
	}
	sig, recvCand, subst, borrowInfo, sawReceiverMatch := tc.matchMethodSignature(name, recv, recvExpr, args, argExprs, staticReceiver)
	if sig != nil {
		resultKey := substituteTypeKeyParams(sig.Result, subst)
		res := tc.typeFromKey(resultKey)
		if res == types.NoTypeID && staticReceiver && recv != types.NoTypeID {
			recvKey := tc.typeKeyForType(recv)
			if recvKey != "" && typeKeyMatchesWithGenerics(resultKey, recvKey) {
				return tc.adjustAliasUnaryResult(recv, recvCand)
			}
		}
		return tc.adjustAliasUnaryResult(res, recvCand)
	}
	if borrowInfo.expr.IsValid() {
		tc.reportBorrowFailure(&borrowInfo)
		return types.NoTypeID
	}
	if sawReceiverMatch && tc.reportSingleMethodCandidateMismatch(name, recv, recvExpr, args, argExprs, span, staticReceiver) {
		return types.NoTypeID
	}
	if sawReceiverMatch {
		tc.report(diag.SemaNoOverload, span, "no matching overload for %s.%s", tc.typeLabel(recv), name)
		return types.NoTypeID
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "%s has no method %s", tc.typeLabel(recv), name)
	return types.NoTypeID
}

func (tc *typeChecker) matchMethodSignature(name string, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID, staticReceiver bool) (*symbols.FunctionSignature, typeKeyCandidate, map[string]symbols.TypeKey, borrowMatchInfo, bool) {
	if name == "" || tc.magic == nil {
		return nil, typeKeyCandidate{}, nil, borrowMatchInfo{}, false
	}
	var borrowInfo borrowMatchInfo
	sawReceiverMatch := false
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recvCand.key, name)
		for _, sig := range methods {
			if sig == nil {
				continue
			}
			subst := tc.methodSubst(recv, recvCand.key, sig)
			switch {
			case len(sig.Params) > 0 && tc.selfParamCompatible(recv, sig.Params[0], recvCand.key):
				if !tc.selfParamAddressable(sig.Params[0], recv, recvExpr, &borrowInfo) {
					continue
				}
				sawReceiverMatch = true
				if len(sig.Params)-1 != len(args) {
					continue
				}
				if !tc.methodParamsMatchWithSubst(sig.Params[1:], args, subst) {
					continue
				}
			case staticReceiver:
				sawReceiverMatch = true
				if len(sig.Params) != len(args) {
					continue
				}
				if name == "from_str" {
					if !tc.methodParamsMatchWithSubst(sig.Params, args, subst) {
						if !tc.methodParamsMatchWithImplicitBorrow(sig.Params, args, argExprs, subst, &borrowInfo) {
							continue
						}
					}
				} else if !tc.methodParamsMatchWithSubst(sig.Params, args, subst) {
					continue
				}
			default:
				continue
			}
			return sig, recvCand, subst, borrowInfo, sawReceiverMatch
		}
	}
	return nil, typeKeyCandidate{}, nil, borrowInfo, sawReceiverMatch
}

func (tc *typeChecker) reportSingleMethodCandidateMismatch(name string, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID, span source.Span, staticReceiver bool) bool {
	type mismatchCandidate struct {
		sym      *symbols.Symbol
		usesSelf bool
	}

	var candidate *mismatchCandidate
	seen := make(map[symbols.SymbolID]struct{})
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		for _, sig := range tc.lookupMagicMethods(recvCand.key, name) {
			if sig == nil {
				continue
			}

			usesSelf := false
			switch {
			case len(sig.Params) > 0 && tc.selfParamCompatible(recv, sig.Params[0], recvCand.key):
				if !tc.selfParamAddressable(sig.Params[0], recv, recvExpr, nil) {
					continue
				}
				if !methodSignatureAcceptsUserArgs(sig, len(args), true) {
					continue
				}
				usesSelf = true
			case staticReceiver:
				if !methodSignatureAcceptsUserArgs(sig, len(args), false) {
					continue
				}
			default:
				continue
			}

			symID := tc.magicSymbolForSignature(sig)
			if !symID.IsValid() {
				symID = tc.ensureExportedMethodSymbol(name, sig, span)
			}
			if !symID.IsValid() {
				continue
			}
			if _, ok := seen[symID]; ok {
				continue
			}
			seen[symID] = struct{}{}

			sym := tc.symbolFromID(symID)
			if sym == nil || sym.Signature == nil {
				continue
			}
			if candidate != nil {
				return false
			}
			candidate = &mismatchCandidate{sym: sym, usesSelf: usesSelf}
		}
	}
	if candidate == nil {
		return false
	}

	callArgs := make([]callArg, 0, len(args)+1)
	if candidate.usesSelf {
		callArgs = append(callArgs, callArg{
			ty:        recv,
			isLiteral: tc.isLiteralExpr(recvExpr),
			expr:      recvExpr,
		})
	}
	for i, arg := range args {
		expr := ast.NoExprID
		if i < len(argExprs) {
			expr = argExprs[i]
		}
		callArgs = append(callArgs, callArg{
			ty:        arg,
			isLiteral: tc.isLiteralExpr(expr),
			expr:      expr,
		})
	}
	return tc.reportCallArgumentMismatch(candidate.sym, callArgs, nil)
}

func methodSignatureAcceptsUserArgs(sig *symbols.FunctionSignature, argCount int, usesSelf bool) bool {
	if sig == nil {
		return false
	}

	offset := 0
	if usesSelf {
		offset = 1
	}
	if len(sig.Params) < offset {
		return false
	}

	paramCount := len(sig.Params) - offset
	variadicIndex := -1
	if len(sig.Variadic) == len(sig.Params) {
		for i := offset; i < len(sig.Variadic); i++ {
			if sig.Variadic[i] {
				variadicIndex = i - offset
				break
			}
		}
	}

	requiredParams := 0
	if len(sig.Defaults) == len(sig.Params) {
		for i := offset; i < len(sig.Defaults); i++ {
			if sig.Defaults[i] {
				continue
			}
			if variadicIndex >= 0 && i-offset == variadicIndex {
				continue
			}
			requiredParams++
		}
	} else {
		requiredParams = paramCount
	}

	if variadicIndex >= 0 {
		return argCount >= paramCount-1
	}
	return argCount >= requiredParams && argCount <= paramCount
}
