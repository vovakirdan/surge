package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/trace"
	"surge/internal/types"
)

type callArg struct {
	ty        types.TypeID
	isLiteral bool
	expr      ast.ExprID
}

func (tc *typeChecker) callResultType(call *ast.ExprCallData, span source.Span) types.TypeID {
	// Трассировка вызова функции
	var traceSpan *trace.Span
	if tc.tracer != nil && tc.tracer.Level() >= trace.LevelDebug {
		traceSpan = trace.Begin(tc.tracer, trace.ScopeNode, "call_result_type", 0)
		traceSpan.WithExtra("args", fmt.Sprintf("%d", len(call.Args)))
	}
	defer func() {
		if traceSpan != nil {
			traceSpan.End("")
		}
	}()

	if call == nil {
		return types.NoTypeID
	}
	tc.typeExpr(call.Target)
	args := make([]callArg, 0, len(call.Args))
	for _, arg := range call.Args {
		argTy := tc.typeExpr(arg)
		args = append(args, callArg{
			ty:        argTy,
			isLiteral: tc.isLiteralExpr(arg),
			expr:      arg,
		})
		tc.observeMove(arg, tc.exprSpan(arg))
	}
	if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
		if module := tc.moduleSymbolForExpr(member.Target); module != nil {
			typeArgs := tc.resolveCallTypeArgs(call.TypeArgs)
			return tc.moduleFunctionResult(module, member.Field, args, typeArgs, span)
		}
	}
	ident, ok := tc.builder.Exprs.Ident(call.Target)
	if !ok || ident == nil {
		return types.NoTypeID
	}
	name := tc.lookupName(ident.Name)
	if name == "default" {
		return tc.handleDefaultLikeCall(name, call, span)
	}
	candidates := tc.functionCandidates(ident.Name)
	if traceSpan != nil {
		traceSpan.WithExtra("candidates", fmt.Sprintf("%d", len(candidates)))
	}
	if len(candidates) == 0 {
		if symID := tc.symbolForExpr(call.Target); symID.IsValid() {
			if sym := tc.symbolFromID(symID); sym != nil && sym.Kind == symbols.SymbolFunction {
				candidates = append(candidates, symID)
			}
		}
	}
	if len(candidates) == 0 {
		if name == "" {
			name = "_"
		}
		tc.report(diag.SemaNoOverload, span, "no matching overload for %s", name)
		return types.NoTypeID
	}
	typeArgs := tc.resolveCallTypeArgs(call.TypeArgs)

	displayName := name
	if displayName == "" {
		displayName = "_"
	}

	bestSym, bestType, bestArgs, ambiguous, ok := tc.selectBestCandidate(candidates, args, typeArgs, false)
	if ambiguous {
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", displayName)
		return types.NoTypeID
	}
	if ok {
		if sym := tc.symbolFromID(bestSym); sym != nil {
			tc.validateFunctionCall(sym, call, tc.collectArgTypes(args))
		}
		tc.rememberFunctionInstantiation(bestSym, bestArgs)
		return bestType
	}

	bestSym, bestType, bestArgs, ambiguous, ok = tc.selectBestCandidate(candidates, args, typeArgs, true)
	if ambiguous {
		tc.report(diag.SemaAmbiguousOverload, span, "ambiguous overload for %s", displayName)
		return types.NoTypeID
	}
	if ok {
		if sym := tc.symbolFromID(bestSym); sym != nil {
			tc.validateFunctionCall(sym, call, tc.collectArgTypes(args))
		}
		tc.rememberFunctionInstantiation(bestSym, bestArgs)
		return bestType
	}

	if len(call.TypeArgs) == 0 {
		if missing := tc.missingTypeParams(candidates, args); len(missing) > 0 {
			tc.reportCannotInferTypeParams(displayName, missing, span, call)
			return types.NoTypeID
		}
	} else {
		if expected := tc.expectedTypeArgCount(candidates); expected > 0 && expected != len(typeArgs) {
			tc.report(diag.SemaNoOverload, span, "%s expects %d type argument(s)", displayName, expected)
			return types.NoTypeID
		}
	}

	tc.report(diag.SemaNoOverload, span, "no matching overload for %s", displayName)
	return types.NoTypeID
}

func (tc *typeChecker) functionCandidates(name source.StringID) []symbols.SymbolID {
	if name == source.NoStringID || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Scopes == nil {
		return nil
	}
	seen := make(map[string]struct{})
	scope := tc.currentScope()
	if !scope.IsValid() {
		scope = tc.fileScope()
	}
	for scope.IsValid() {
		scopeData := tc.symbols.Table.Scopes.Get(scope)
		if scopeData == nil {
			break
		}
		if ids := scopeData.NameIndex[name]; len(ids) > 0 {
			out := make([]symbols.SymbolID, 0, len(ids))
			for _, id := range ids {
				sym := tc.symbolFromID(id)
				if sym != nil && (sym.Kind == symbols.SymbolFunction || sym.Kind == symbols.SymbolTag) {
					if key := tc.candidateKey(sym); key != "" {
						if _, dup := seen[key]; dup {
							continue
						}
						seen[key] = struct{}{}
					}
					out = append(out, id)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		scope = scopeData.Parent
	}
	return nil
}

func (tc *typeChecker) handleDefaultLikeCall(name string, call *ast.ExprCallData, span source.Span) types.TypeID {
	if call == nil {
		return types.NoTypeID
	}
	if len(call.TypeArgs) == 0 {
		tc.reportCannotInferTypeParams(name, []string{"T"}, span, call)
		return types.NoTypeID
	}
	if len(call.TypeArgs) != 1 {
		tc.report(diag.SemaNoOverload, span, "%s expects 1 type argument", name)
		return types.NoTypeID
	}
	if len(call.Args) != 0 {
		tc.report(diag.SemaNoOverload, span, "%s does not take arguments", name)
		return types.NoTypeID
	}
	scope := tc.scopeOrFile(tc.currentScope())
	targetType := tc.resolveTypeExprWithScope(call.TypeArgs[0], scope)
	if targetType == types.NoTypeID {
		return types.NoTypeID
	}
	if name == "default" && !tc.defaultable(targetType) {
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(call.Target), "default is not defined for %s", tc.typeLabel(targetType))
		return types.NoTypeID
	}
	return targetType
}

func (tc *typeChecker) reportCannotInferTypeParams(name string, missing []string, span source.Span, call *ast.ExprCallData) {
	if tc.reporter == nil || len(missing) == 0 {
		return
	}
	displayName := name
	if displayName == "" {
		displayName = "_"
	}
	missingLabel := strings.Join(missing, ", ")
	msg := fmt.Sprintf("cannot infer type parameter %s for %s; use %s::<%s>(...)", missingLabel, displayName, displayName, missingLabel)
	b := diag.ReportError(tc.reporter, diag.SemaNoOverload, span, msg)
	if b == nil {
		return
	}
	if call != nil {
		if targetSpan := tc.exprSpan(call.Target); targetSpan != (source.Span{}) {
			insert := targetSpan.ZeroideToEnd()
			title := fmt.Sprintf("insert %s::<%s>", displayName, missingLabel)
			b.WithFixSuggestion(fix.InsertText(title, insert, "::<"+missingLabel+">", "", fix.Preferred()))
		}
	}
	b.Emit()
}

func (tc *typeChecker) methodResultType(member *ast.ExprMemberData, recv types.TypeID, args []types.TypeID, span source.Span, staticReceiver bool) types.TypeID {
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
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(recvCand.key, name)
		for _, sig := range methods {
			if sig == nil {
				continue
			}
			if len(sig.Params) == 0 {
				// static/associated method: allowed only when invoked on a type
				if !staticReceiver || len(args) != 0 {
					continue
				}
			} else {
				if !typeKeyEqual(sig.Params[0], recvCand.key) {
					continue
				}
				if len(sig.Params)-1 != len(args) {
					continue
				}
				if !tc.methodParamsMatch(sig.Params[1:], args) {
					continue
				}
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
