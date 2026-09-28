package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// checkBodylessFunctions refuses a free function declared without a body when
// it is not `@intrinsic` (or core) and no `@override` implementation with the same
// signature completes it in its module. Such a declaration is a forward
// declaration: when nothing completes it there is no code a call could run.
// Members of `extern<T>` blocks and contract requirements are not checked here.
func (tc *typeChecker) checkBodylessFunctions(file *ast.File) {
	if file == nil || tc.symbols == nil || tc.symbols.Table == nil || tc.symbols.Table.Symbols == nil {
		return
	}
	overridden := tc.overrideAttemptNames(file)
	for _, itemID := range file.Items {
		item := tc.builder.Items.Get(itemID)
		// Only free functions: a body-less member of an `extern<T>` block is a
		// declaration the language allows (LANGUAGE.md §4.4.1), and a call of
		// one that nothing provides is refused by the backends instead.
		if item == nil || item.Kind != ast.ItemFn {
			continue
		}
		for _, symID := range tc.symbols.ItemSymbols[itemID] {
			sym := tc.symbols.Table.Symbols.Get(symID)
			if !tc.isUncompletedBodyless(sym) {
				continue
			}
			// An `@override` of this name that the resolver refused already
			// carries its own diagnostic; the missing implementation is its
			// consequence, not a second mistake.
			if _, attempted := overridden[sym.Name]; attempted {
				continue
			}
			tc.reportBodylessFunction(sym)
		}
	}
}

func (tc *typeChecker) isUncompletedBodyless(sym *symbols.Symbol) bool {
	if sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil || sym.Signature.HasBody {
		return false
	}
	// The builtin flag marks every `@intrinsic` declaration and every core
	// symbol, so both are exempt; an entrypoint without a body has its own
	// diagnostic.
	if sym.Flags&(symbols.SymbolFlagBuiltin|symbols.SymbolFlagEntrypoint) != 0 {
		return false
	}
	if sym.Decl.ASTFile != tc.fileID {
		return false
	}
	return !tc.bodylessCompleted(sym)
}

// bodylessCompleted reports whether a function with a body and the same name,
// receiver and signature sits beside the declaration: the `@override` that
// completes a forward declaration is declared into the same scope.
func (tc *typeChecker) bodylessCompleted(sym *symbols.Symbol) bool {
	scope := tc.symbols.Table.Scopes.Get(sym.Scope)
	if scope == nil {
		return false
	}
	key := tc.candidateKey(sym)
	for _, id := range scope.NameIndex[sym.Name] {
		other := tc.symbols.Table.Symbols.Get(id)
		if other == nil || other == sym || other.Kind != symbols.SymbolFunction || other.Signature == nil || !other.Signature.HasBody {
			continue
		}
		if other.ReceiverKey == sym.ReceiverKey && tc.candidateKey(other) == key {
			return true
		}
	}
	return false
}

func (tc *typeChecker) reportBodylessFunction(sym *symbols.Symbol) {
	if tc.reporter == nil {
		return
	}
	name := tc.lookupName(sym.Name)
	if name == "" {
		name = "_"
	}
	msg := fmt.Sprintf("function '%s' is declared without a body, and nothing in its module implements it", name)
	b := diag.ReportError(tc.reporter, diag.SemaBodylessFunction, sym.Span, msg)
	if b == nil {
		return
	}
	b.WithHelp(sym.Span, "give it a body, mark it @intrinsic, or add its @override implementation with the same signature in this module")
	b.Emit()
}

// overrideAttemptNames collects the names of the free functions with a body
// that are written with `@override` in this file.
func (tc *typeChecker) overrideAttemptNames(file *ast.File) map[source.StringID]struct{} {
	out := make(map[source.StringID]struct{})
	for _, itemID := range file.Items {
		fn, ok := tc.builder.Items.Fn(itemID)
		if !ok || fn == nil || !fn.Body.IsValid() {
			continue
		}
		for _, attr := range tc.builder.Items.CollectAttrs(fn.AttrStart, fn.AttrCount) {
			if tc.lookupName(attr.Name) == "override" {
				out[fn.Name] = struct{}{}
			}
		}
	}
	return out
}
