package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// untypedReceiverMethodResult answers a selected method whose result key the fallback could not type.
// A prelude module holds a prelude copy and an injected copy of each core export. They share its
// signature and carry no magic ID, so strict selection finds no declaration; copies that agree on the
// type and arity substitution reads are that one declaration, and its result is substituted quietly.
// A call still untyped is refused only when its receiver's own arguments are type parameters and its
// declaration's header has resolved; any other untyped call keeps the result it has today.
func (tc *typeChecker) untypedReceiverMethodResult(name string, sig *symbols.FunctionSignature, recv types.TypeID, recvCand typeKeyCandidate, resultKey symbols.TypeKey, span source.Span, checkpoint int) types.TypeID {
	if tc.types == nil || sig == nil || tc.hasErrorsSince(checkpoint) {
		return types.NoTypeID
	}
	sym := tc.selectedMethodResultSymbol(sig)
	if sym == nil {
		sym = tc.agreeingExportCopy(sig)
		if res := tc.quietSelectedSymbolResult(name, sym, recvCand, span); res != types.NoTypeID {
			return res
		}
	}
	if (sym != nil && sym.Type == types.NoTypeID) || !tc.receiverHasTypeParam(recv) {
		return types.NoTypeID
	}
	help := "call it where the receiver's type is concrete"
	if _, union := tc.types.UnionInfo(tc.valueType(recvCand.base)); union {
		help += ", or match on the value with `compare` instead of calling the method"
	}
	b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, span, "cannot work out what `"+name+"` returns here: its result `"+string(resultKey)+"` depends on a type parameter this call cannot pin down yet").WithHelp(span, help)
	if sym != nil && sym.Span != (source.Span{}) {
		b.WithNote(sym.Span, "declared here")
	}
	b.Emit()
	return types.NoTypeID
}

// agreeingExportCopy answers the first table copy of a signature when every copy agrees on the type and
// arity substitution reads; a copy that disagrees belongs to another declaration.
func (tc *typeChecker) agreeingExportCopy(sig *symbols.FunctionSignature) *symbols.Symbol {
	if tc.symbols == nil || tc.symbols.Table == nil {
		return nil
	}
	var found *symbols.Symbol
	for i := range tc.symbols.Table.Symbols.Data() {
		sym := tc.symbols.Table.Symbols.Get(symbols.SymbolID(i + 1))
		if sym.Kind != symbols.SymbolFunction || sym.Signature != sig {
			continue
		}
		if found == nil {
			found = sym
		} else if found.Type != sym.Type || len(found.TypeParams) != len(sym.TypeParams) {
			return nil
		}
	}
	return found
}

// quietSelectedSymbolResult substitutes a recovered declaration's result without reporting. A
// descriptor it cannot bind leaves the call as untyped as the fallback left it.
func (tc *typeChecker) quietSelectedSymbolResult(name string, sym *symbols.Symbol, recvCand typeKeyCandidate, span source.Span) types.TypeID {
	if sym == nil {
		return types.NoTypeID
	}
	reporter, errors := tc.reporter, tc.errorCount
	tc.reporter = &diagnosticCountingReporter{inner: &externHeaderReporter{}, errorCount: &tc.errorCount}
	res, handled := tc.selectedSymbolResult(name, sym, recvCand, span)
	failed := tc.hasErrorsSince(errors)
	tc.reporter, tc.errorCount = reporter, errors
	if !handled || failed {
		return types.NoTypeID
	}
	return res
}

// receiverHasTypeParam reports whether one of the receiver's own type arguments is a type parameter.
func (tc *typeChecker) receiverHasTypeParam(recv types.TypeID) bool {
	for _, arg := range tc.receiverTypeArgs(recv) {
		if tt, ok := tc.types.Lookup(tc.resolveAlias(arg)); ok && tt.Kind == types.KindGenericParam {
			return true
		}
	}
	return false
}
