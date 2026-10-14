package sema

import (
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// A synchronous entrypoint is a root activation, not an ordinary callable.
// Reject references as well as direct calls, so aliases and function arguments
// cannot bypass the root coroutine boundary. Explicit async functions retain
// their ordinary Task-returning call contract.
func (tc *typeChecker) refuseEntrypointValue(sym *symbols.Symbol, span source.Span) {
	if sym == nil || sym.Kind != symbols.SymbolFunction ||
		sym.Flags&symbols.SymbolFlagEntrypoint == 0 ||
		(sym.Signature != nil && sym.Signature.Async) {
		return
	}
	if b := diag.ReportError(tc.reporter, diag.SemaEntrypointValue, span,
		"a synchronous @entrypoint can only be invoked by the runtime; it cannot be called or used as a function value"); b != nil {
		b.WithNote(sym.Span, "this function is the program's root task activation")
		b.WithHelp(span, "move reusable logic into a separate function or async fn and call that function instead")
		b.Emit()
	}
}
