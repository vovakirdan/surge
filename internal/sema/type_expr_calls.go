package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) callResultType(call *ast.ExprCallData) types.TypeID {
	if call == nil {
		return types.NoTypeID
	}
	tc.typeExpr(call.Target)
	argTypes := make([]types.TypeID, 0, len(call.Args))
	for _, arg := range call.Args {
		argTy := tc.typeExpr(arg)
		argTypes = append(argTypes, argTy)
		tc.observeMove(arg, tc.exprSpan(arg))
	}
	if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
		name := tc.lookupName(ident.Name)
		if name == "default" && len(call.TypeArgs) == 1 {
			scope := tc.scopeOrFile(tc.currentScope())
			targetType := tc.resolveTypeExprWithScope(call.TypeArgs[0], scope)
			if targetType == types.NoTypeID {
				return types.NoTypeID
			}
			if !tc.defaultable(targetType) {
				tc.report(diag.SemaTypeMismatch, tc.exprSpan(call.Target), "default is not defined for %s", tc.typeLabel(targetType))
				return types.NoTypeID
			}
			return targetType
		}
		if symID := tc.symbolForExpr(call.Target); symID.IsValid() {
			if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolFunction {
				tc.validateFunctionCall(symID, sym, call, argTypes)
			}
		}
	}
	return types.NoTypeID
}

func (tc *typeChecker) methodResultType(member *ast.ExprMemberData, recv types.TypeID, args []types.TypeID, span source.Span) types.TypeID {
	if member == nil || tc.magic == nil {
		return types.NoTypeID
	}
	name := tc.lookupExportedName(member.Field)
	if name == "" {
		return types.NoTypeID
	}
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recvCand.key, name)
		for _, sig := range methods {
			if sig == nil || len(sig.Params) == 0 || !typeKeyEqual(sig.Params[0], recvCand.key) {
				continue
			}
			if len(sig.Params)-1 != len(args) {
				continue
			}
			if !tc.methodParamsMatch(sig.Params[1:], args) {
				continue
			}
			res := tc.typeFromKey(sig.Result)
			return tc.adjustAliasUnaryResult(res, recvCand)
		}
	}
	tc.report(diag.SemaUnresolvedSymbol, span, "%s has no method %s", tc.typeLabel(recv), name)
	return types.NoTypeID
}

func (tc *typeChecker) methodParamsMatch(expected []symbols.TypeKey, args []types.TypeID) bool {
	if len(expected) != len(args) {
		return false
	}
	for i, arg := range args {
		if !tc.methodParamMatches(expected[i], arg) {
			return false
		}
	}
	return true
}

func (tc *typeChecker) methodParamMatches(expected symbols.TypeKey, arg types.TypeID) bool {
	if expected == "" {
		return false
	}
	for _, cand := range tc.typeKeyCandidates(arg) {
		if typeKeyEqual(cand.key, expected) {
			return true
		}
	}
	return false
}
