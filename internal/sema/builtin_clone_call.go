package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A call spelled `clone` is the builtin clone only when its callee is the core
// declaration `clone<T>(value: &T) -> T`. The spelling alone decides nothing: a
// local binding, a parameter, an import or a user overload named clone is an
// ordinary callee and its call is an ordinary call.

// coreClone returns the symbol when symID is a function named clone that core
// declares: seen through the core prelude (the only source of builtin
// function symbols), or declared while checking core itself.
func (tc *typeChecker) coreClone(symID symbols.SymbolID) *symbols.Symbol {
	sym := tc.symbolFromID(symID)
	if sym == nil || sym.Kind != symbols.SymbolFunction || tc.lookupName(sym.Name) != "clone" {
		return nil
	}
	if sym.Flags&symbols.SymbolFlagBuiltin == 0 && !isCoreRuntimeModulePath(sym.ModulePath) &&
		!isCoreRuntimeModulePath(tc.modulePath) {
		return nil
	}
	return sym
}

// isCoreFreeClone reports whether symID is the core generic free clone: a
// body-less core function with one type parameter T, no receiver, one `&T`
// formal and result T.
func (tc *typeChecker) isCoreFreeClone(symID symbols.SymbolID) bool {
	sym := tc.coreClone(symID)
	if sym == nil || sym.ReceiverKey != "" || sym.Flags&symbols.SymbolFlagMethod != 0 || len(sym.TypeParams) != 1 {
		return false
	}
	sig := sym.Signature
	if sig == nil || sig.HasBody || sig.HasSelf || sig.Async || len(sig.Params) != 1 {
		return false
	}
	param := tc.lookupName(sym.TypeParams[0])
	return param != "" && string(sig.Params[0]) == "&"+param && string(sig.Result) == param
}

// callsOnlyCoreClone reports whether a one-argument call whose callee is the
// identifier `name` can only mean the core clone: the identifier resolves to a
// function, every function the nearest overload set holds under that name is
// a core clone (the free one or a core method such as the task handle's), and
// the free one is among them. A binding or parameter named clone resolves to
// itself, not to a function, even though the overload walk would look past it.
func (tc *typeChecker) callsOnlyCoreClone(target ast.ExprID, name source.StringID, args []callArg) bool {
	if len(args) != 1 {
		return false
	}
	resolvedID := tc.symbolForExpr(target)
	candidates := tc.functionCandidates(name)
	if !resolvedID.IsValid() && len(candidates) == 0 {
		// Nothing at all is named clone here -- a unit checked without the
		// core prelude -- so no user declaration can be what the call means.
		return true
	}
	resolved := tc.symbolFromID(resolvedID)
	if resolved == nil || resolved.Kind != symbols.SymbolFunction || len(candidates) == 0 {
		return false
	}
	free := false
	for _, id := range candidates {
		if tc.coreClone(id) == nil {
			return false
		}
		free = free || tc.isCoreFreeClone(id)
	}
	return free
}

// builtinCloneCall types a call already known to mean the core clone and marks
// it, so HIR lowers exactly the calls the checker typed as the builtin.
func (tc *typeChecker) builtinCloneCall(callID ast.ExprID, args []callArg, span source.Span) types.TypeID {
	result := tc.handleCloneCall(callID, args, span)
	if result == types.NoTypeID {
		return result
	}
	if tc.result.BuiltinCloneCalls == nil {
		tc.result.BuiltinCloneCalls = make(map[ast.ExprID]struct{})
	}
	tc.result.BuiltinCloneCalls[callID] = struct{}{}
	return result
}

// selectedCoreClone reports whether ordinary overload resolution over a set
// that mixes the core clone with user declarations chose the core clone.
func (tc *typeChecker) selectedCoreClone(sel candidateSelection, args []callArg) bool {
	return sel.ok && !sel.ambiguous && len(args) == 1 && tc.isCoreFreeClone(sel.sym)
}
