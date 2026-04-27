package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) recordMethodCallSymbol(callID ast.ExprID, member *ast.ExprMemberData, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID, staticReceiver bool) symbols.SymbolID {
	if callID == ast.NoExprID || member == nil || tc.symbols == nil {
		return symbols.NoSymbolID
	}
	if tc.symbols.ExprSymbols == nil {
		return symbols.NoSymbolID
	}
	symID := tc.resolveMethodCallSymbol(member, recv, recvExpr, args, argExprs, staticReceiver)
	if !symID.IsValid() && tc.magic != nil {
		name := tc.lookupExportedName(member.Field)
		if name != "" {
			sig, _, _, _, _ := tc.matchMethodSignature(name, recv, recvExpr, args, argExprs, staticReceiver)
			if sig != nil {
				symID = tc.magicSymbolForSignature(sig)
				if !symID.IsValid() {
					symID = tc.ensureExportedMethodSymbol(name, sig, tc.exprSpan(callID))
				}
			}
		}
	}
	if symID.IsValid() {
		tc.symbols.ExprSymbols[callID] = symID
	}
	return symID
}

func (tc *typeChecker) ensureExportedMethodSymbol(name string, sig *symbols.FunctionSignature, fallback source.Span) symbols.SymbolID {
	if name == "" || sig == nil || tc.exports == nil || tc.symbols == nil || tc.symbols.Table == nil || tc.builder == nil || tc.builder.StringsInterner == nil {
		return symbols.NoSymbolID
	}
	if symID := tc.magicSymbolForSignature(sig); symID.IsValid() {
		return symID
	}

	nameID := tc.builder.StringsInterner.Intern(name)
	for modulePath, exports := range tc.exports {
		if exports == nil {
			continue
		}
		exported := exports.Lookup(name)
		for i := range exported {
			exp := &exported[i]
			if exp.Kind != symbols.SymbolFunction || exp.ReceiverKey == "" {
				continue
			}
			if exp.Flags&symbols.SymbolFlagBuiltin != 0 {
				continue
			}
			if !functionSignaturesMatch(exp.Signature, sig) {
				continue
			}
			sym := tc.exportedSymbolToSymbol(exp, modulePath)
			if sym == nil {
				return symbols.NoSymbolID
			}
			sym.Scope = tc.fileScope()
			if sym.Span == (source.Span{}) {
				sym.Span = fallback
			}
			sym.Name = nameID
			sym.ImportName = nameID
			id := tc.symbols.Table.Symbols.New(sym)
			if scope := tc.symbols.Table.Scopes.Get(tc.fileScope()); scope != nil {
				scope.Symbols = append(scope.Symbols, id)
				if scope.NameIndex == nil {
					scope.NameIndex = make(map[source.StringID][]symbols.SymbolID)
				}
				scope.NameIndex[nameID] = append(scope.NameIndex[nameID], id)
			}
			if tc.magicSymbols == nil {
				tc.magicSymbols = make(map[*symbols.FunctionSignature]symbols.SymbolID)
			}
			tc.magicSymbols[sig] = id
			return id
		}
	}
	return symbols.NoSymbolID
}

func functionSignaturesMatch(a, b *symbols.FunctionSignature) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil || len(a.Params) != len(b.Params) {
		return false
	}
	if !typeKeyEqual(a.Result, b.Result) {
		return false
	}
	for i := range a.Params {
		if !typeKeyEqual(a.Params[i], b.Params[i]) {
			return false
		}
	}
	return true
}

func (tc *typeChecker) exportedMethodSymbolForSignature(name string, sig *symbols.FunctionSignature, fallback source.Span) *symbols.Symbol {
	if name == "" || sig == nil || tc.exports == nil {
		return nil
	}
	for modulePath, exports := range tc.exports {
		if exports == nil {
			continue
		}
		exported := exports.Lookup(name)
		for i := range exported {
			exp := &exported[i]
			if exp.Kind != symbols.SymbolFunction || exp.ReceiverKey == "" {
				continue
			}
			if !functionSignaturesMatch(exp.Signature, sig) {
				continue
			}
			sym := tc.exportedSymbolToSymbol(exp, modulePath)
			if sym == nil {
				return nil
			}
			if sym.Span == (source.Span{}) {
				sym.Span = fallback
			}
			if tc.builder != nil && tc.builder.StringsInterner != nil {
				nameID := tc.builder.StringsInterner.Intern(name)
				sym.Name = nameID
				sym.ImportName = nameID
			}
			return sym
		}
	}
	return nil
}

func (tc *typeChecker) ensureMagicMethodSymbol(name string, sig *symbols.FunctionSignature, fallback source.Span) symbols.SymbolID {
	if symID := tc.magicSymbolForSignature(sig); symID.IsValid() {
		return symID
	}
	return tc.ensureExportedMethodSymbol(name, sig, fallback)
}

func (tc *typeChecker) recordMethodCallInstantiation(symID symbols.SymbolID, recv types.TypeID, explicitArgs []types.TypeID, span source.Span) {
	if !symID.IsValid() {
		return
	}
	tc.checkDeprecatedSymbol(symID, "function", span)
	sym := tc.symbolFromID(symID)
	if sym == nil || len(sym.TypeParams) == 0 {
		return
	}
	recvArgs := tc.receiverTypeArgs(recv)
	typeArgs := make([]types.TypeID, 0, len(recvArgs)+len(explicitArgs))
	typeArgs = append(typeArgs, recvArgs...)
	typeArgs = append(typeArgs, explicitArgs...)
	if len(typeArgs) == 0 || len(typeArgs) != len(sym.TypeParams) {
		return
	}
	tc.rememberFunctionInstantiation(symID, typeArgs, span, "call")
}

func (tc *typeChecker) receiverTypeArgs(recv types.TypeID) []types.TypeID {
	if recv == types.NoTypeID || tc.types == nil {
		return nil
	}
	resolved := tc.resolveAlias(recv)
	tt, ok := tc.types.Lookup(resolved)
	if !ok {
		return nil
	}
	if tt.Kind == types.KindOwn || tt.Kind == types.KindReference || tt.Kind == types.KindPointer {
		if tt.Elem != types.NoTypeID {
			resolved = tc.resolveAlias(tt.Elem)
		}
	}
	return tc.typeArgsForType(resolved)
}

func (tc *typeChecker) resolveMethodCallSymbol(member *ast.ExprMemberData, recv types.TypeID, recvExpr ast.ExprID, args []types.TypeID, argExprs []ast.ExprID, staticReceiver bool) symbols.SymbolID {
	if member == nil || recv == types.NoTypeID {
		return symbols.NoSymbolID
	}
	if tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return symbols.NoSymbolID
	}
	name := tc.lookupExportedName(member.Field)
	if name == "" {
		return symbols.NoSymbolID
	}
	data := tc.symbols.Table.Symbols.Data()
	if data == nil {
		return symbols.NoSymbolID
	}
	for _, recvCand := range tc.typeKeyCandidates(recv) {
		if recvCand.key == "" {
			continue
		}
		for i := len(data) - 1; i >= 0; i-- {
			sym := &data[i]
			if sym.Kind != symbols.SymbolFunction || sym.ReceiverKey == "" || sym.Signature == nil {
				continue
			}
			if tc.symbolName(sym.Name) != name {
				continue
			}
			if !typeKeyMatchesWithGenerics(sym.ReceiverKey, recvCand.key) {
				continue
			}
			sig := sym.Signature
			subst := tc.methodSubst(recv, recvCand.key, sig)
			switch {
			case sig.HasSelf:
				if !tc.selfParamCompatible(recv, sig.Params[0], recvCand.key) {
					continue
				}
				if !tc.selfParamAddressable(sig.Params[0], recv, recvExpr, nil) {
					continue
				}
				if len(sig.Params)-1 != len(args) {
					continue
				}
				if !tc.methodParamsMatchWithSubst(sig.Params[1:], args, subst) {
					continue
				}
			case staticReceiver:
				if len(sig.Params) != len(args) {
					continue
				}
				if !tc.methodParamsMatchWithSubst(sig.Params, args, subst) {
					if name != "from_str" || !tc.methodParamsMatchWithImplicitBorrow(sig.Params, args, argExprs, subst, nil) {
						continue
					}
				}
			default:
				continue
			}
			return symbols.SymbolID(i + 1) //nolint:gosec
		}
	}
	return symbols.NoSymbolID
}

func (tc *typeChecker) methodSubst(recv types.TypeID, recvKey symbols.TypeKey, sig *symbols.FunctionSignature) map[string]symbols.TypeKey {
	if sig != nil && sig.HasSelf && len(sig.Params) > 0 {
		if subst := tc.buildTypeParamSubst(recv, sig.Params[0]); len(subst) > 0 {
			return subst
		}
	}
	if subst := tc.buildTypeParamSubst(recv, recvKey); len(subst) > 0 {
		return subst
	}
	return tc.receiverTypeParamSubst(recv)
}
