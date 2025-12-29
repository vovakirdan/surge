package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

func (tc *typeChecker) pushFnParams(params []symbols.SymbolID) func() {
	tc.fnParamsStack = append(tc.fnParamsStack, params)
	return func() {
		if len(tc.fnParamsStack) > 0 {
			tc.fnParamsStack = tc.fnParamsStack[:len(tc.fnParamsStack)-1]
		}
	}
}

func (tc *typeChecker) currentFnParams() []symbols.SymbolID {
	if tc == nil || len(tc.fnParamsStack) == 0 {
		return nil
	}
	return tc.fnParamsStack[len(tc.fnParamsStack)-1]
}

func (tc *typeChecker) fnParamSymbols(fnItem *ast.FnItem, scope symbols.ScopeID) []symbols.SymbolID {
	if tc == nil || tc.builder == nil || fnItem == nil {
		return nil
	}
	paramIDs := tc.builder.Items.GetFnParamIDs(fnItem)
	params := make([]symbols.SymbolID, len(paramIDs))
	scope = tc.scopeOrFile(scope)
	for i, pid := range paramIDs {
		param := tc.builder.Items.FnParam(pid)
		if param == nil || param.Name == source.NoStringID || tc.isWildcardName(param.Name) {
			params[i] = symbols.NoSymbolID
			continue
		}
		params[i] = tc.symbolInScope(scope, param.Name, symbols.SymbolParam)
	}
	return params
}

func (tc *typeChecker) checkTrivialReturnRecursion(exprID ast.ExprID) {
	if tc == nil || tc.reporter == nil || tc.builder == nil || !exprID.IsValid() {
		return
	}
	fnSym := tc.currentFnSym()
	if !fnSym.IsValid() {
		return
	}
	params := tc.currentFnParams()
	if params == nil {
		return
	}
	exprID = tc.stripTrivialWrappers(exprID)
	if !exprID.IsValid() {
		return
	}
	if !tc.isSelfCycleExpr(exprID, fnSym, params) {
		return
	}
	span := tc.exprSpan(exprID)
	msg := "obvious infinite recursion: this expression resolves to a call to the same function with the same arguments; rewrite the call to target a different overload or change the arguments"
	if b := diag.ReportError(tc.reporter, diag.SemaTrivialRecursion, span, msg); b != nil {
		b.WithNote(span, "hint: if you meant to call an intrinsic implementation, call it explicitly (if supported) or cast to route to a different overload")
		b.Emit()
	}
}

func (tc *typeChecker) isSelfCycleExpr(exprID ast.ExprID, fnSym symbols.SymbolID, params []symbols.SymbolID) bool {
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil {
		return false
	}
	switch expr.Kind {
	case ast.ExprCall:
		if tc.symbolForExpr(exprID) != fnSym {
			return false
		}
		call, ok := tc.builder.Exprs.Call(exprID)
		if !ok || call == nil {
			return false
		}
		return tc.callArgsMatchParams(call, params)
	case ast.ExprBinary:
		if tc.magicBinarySymbol(exprID) != fnSym {
			return false
		}
		bin, ok := tc.builder.Exprs.Binary(exprID)
		if !ok || bin == nil || len(params) != 2 {
			return false
		}
		return tc.isParamRef(bin.Left, params[0]) && tc.isParamRef(bin.Right, params[1])
	case ast.ExprUnary:
		if tc.magicUnarySymbol(exprID) != fnSym {
			return false
		}
		unary, ok := tc.builder.Exprs.Unary(exprID)
		if !ok || unary == nil || len(params) != 1 {
			return false
		}
		return tc.isParamRef(unary.Operand, params[0])
	default:
		return false
	}
}

func (tc *typeChecker) callArgsMatchParams(call *ast.ExprCallData, params []symbols.SymbolID) bool {
	if call == nil {
		return false
	}
	if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
		if !tc.isStaticMemberTarget(member.Target) {
			if len(params) != len(call.Args)+1 {
				return false
			}
			if !tc.isParamRef(member.Target, params[0]) {
				return false
			}
			return tc.argsMatchParams(call.Args, params[1:])
		}
	}
	return tc.argsMatchParams(call.Args, params)
}

func (tc *typeChecker) argsMatchParams(args []ast.CallArg, params []symbols.SymbolID) bool {
	if len(args) != len(params) {
		return false
	}
	for i, arg := range args {
		if !tc.isParamRef(arg.Value, params[i]) {
			return false
		}
	}
	return true
}

func (tc *typeChecker) isParamRef(exprID ast.ExprID, paramSym symbols.SymbolID) bool {
	if !exprID.IsValid() || !paramSym.IsValid() || tc.builder == nil {
		return false
	}
	exprID = tc.stripTrivialWrappers(exprID)
	if !exprID.IsValid() {
		return false
	}
	expr := tc.builder.Exprs.Get(exprID)
	if expr == nil || expr.Kind != ast.ExprIdent {
		return false
	}
	return tc.symbolForExpr(exprID) == paramSym
}

func (tc *typeChecker) stripTrivialWrappers(exprID ast.ExprID) ast.ExprID {
	for exprID.IsValid() {
		expr := tc.builder.Exprs.Get(exprID)
		if expr == nil || expr.Kind != ast.ExprGroup {
			return exprID
		}
		group, ok := tc.builder.Exprs.Group(exprID)
		if !ok || group == nil || !group.Inner.IsValid() {
			return exprID
		}
		exprID = group.Inner
	}
	return ast.NoExprID
}

func (tc *typeChecker) isStaticMemberTarget(exprID ast.ExprID) bool {
	if tc.moduleSymbolForExpr(exprID) != nil {
		return true
	}
	symID := tc.symbolForExpr(exprID)
	if !symID.IsValid() {
		return false
	}
	sym := tc.symbolFromID(symID)
	return sym != nil && sym.Kind == symbols.SymbolType
}

func (tc *typeChecker) magicUnarySymbol(exprID ast.ExprID) symbols.SymbolID {
	if tc.result == nil || tc.result.MagicUnarySymbols == nil {
		return symbols.NoSymbolID
	}
	return tc.result.MagicUnarySymbols[exprID]
}

func (tc *typeChecker) magicBinarySymbol(exprID ast.ExprID) symbols.SymbolID {
	if tc.result == nil || tc.result.MagicBinarySymbols == nil {
		return symbols.NoSymbolID
	}
	return tc.result.MagicBinarySymbols[exprID]
}
