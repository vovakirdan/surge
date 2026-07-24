package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
)

// reportModuleLevelLet rejects `let` and `let mut` declared at module scope.
//
// A module-level binding lowers to one storage slot initialized once at startup
// and read by every shard, and there is no boundary at which a per-shard copy of
// it could be made. `const` is a different mechanism despite the surface
// similarity: it is inlined at each use site, so each use materializes its own
// value inside the shard that runs the code and nothing is shared.
func (tc *typeChecker) reportModuleLevelLet(letItem *ast.LetItem) {
	if tc.reporter == nil || letItem == nil {
		return
	}

	keyword := "let"
	if letItem.IsMut {
		keyword = "let mut"
	}
	span := letItem.LetSpan
	if span.Empty() {
		span = letItem.Span
	}

	msg := "module-level `" + keyword + "` is not allowed; `const` is the only declaration a module may hold"
	b := diag.ReportError(tc.reporter, diag.SemaModuleLevelLet, span, msg)
	if b == nil {
		return
	}
	b.WithNote(span, "a module-level binding is one slot every shard reads, and there is no point at which each shard could be given its own copy")
	if letItem.IsMut {
		b.WithNote(span, "there is no `const mut`: move the state into a function and pass it where it is needed, or share it through a channel")
	} else {
		b.WithNote(span, "for a compile-time number or string, write `const` instead; otherwise move the binding into the function that uses it")
	}
	b.Emit()
}
