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
		sig      *symbols.FunctionSignature
		subst    map[string]symbols.TypeKey
		usesSelf bool
	}

	var candidate *mismatchCandidate
	seen := make(map[symbols.SymbolID]struct{})
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
			var sym *symbols.Symbol
			if symID.IsValid() {
				if _, ok := seen[symID]; ok {
					continue
				}
				seen[symID] = struct{}{}
				sym = tc.symbolFromID(symID)
			} else {
				sym = tc.exportedMethodSymbolForSignature(name, sig, span)
				if sym == nil {
					sym = &symbols.Symbol{Kind: symbols.SymbolFunction, Signature: sig}
				}
			}
			if sym == nil || sym.Signature == nil {
				continue
			}
			if candidate != nil {
				if candidate.usesSelf == usesSelf && functionSignaturesMatch(candidate.sig, sig) {
					continue
				}
				return false
			}
			candidate = &mismatchCandidate{sym: sym, sig: sig, subst: subst, usesSelf: usesSelf}
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
	if tc.reportCallArgumentMismatch(candidate.sym, callArgs, nil) {
		return true
	}
	return tc.reportMethodSignatureArgumentMismatch(candidate.sig, candidate.subst, candidate.usesSelf, recv, recvExpr, args, argExprs)
}

func (tc *typeChecker) reportMethodSignatureArgumentMismatch(sig *symbols.FunctionSignature, subst map[string]symbols.TypeKey, usesSelf bool, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID) bool {
	if sig == nil {
		return false
	}
	totalArgs := len(args)
	if usesSelf {
		totalArgs++
	}
	if len(sig.Params) != totalArgs {
		return false
	}

	for i, param := range sig.Params {
		var argType types.TypeID
		argExpr := ast.NoExprID
		switch {
		case usesSelf && i == 0:
			argType = recv
			argExpr = recvExpr
		case usesSelf:
			argIndex := i - 1
			argType = args[argIndex]
			if argIndex < len(argExprs) {
				argExpr = argExprs[argIndex]
			}
		default:
			argType = args[i]
			if i < len(argExprs) {
				argExpr = argExprs[i]
			}
		}

		expected := tc.typeFromKey(substituteTypeKeyParams(param, subst))
		if expected == types.NoTypeID {
			return false
		}
		var borrowInfo borrowMatchInfo
		if _, ok := tc.matchArgument(expected, argType, tc.isLiteralExpr(argExpr), false, argExpr, &borrowInfo); ok {
			continue
		}
		if borrowInfo.expr.IsValid() {
			tc.reportBorrowFailure(&borrowInfo)
			return true
		}
		tc.reportCallArgumentTypeMismatch(expected, argType, argExpr, false)
		return true
	}
	return false
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
