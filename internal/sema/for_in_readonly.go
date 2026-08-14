package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
)

// `for-in` ITERATES, it does not lend out places to write through. That is a
// settled property of the language, not a limitation waiting to be lifted, and
// the diagnostics below must never promise otherwise.
//
// Two spellings ask for a write and neither delivers one:
//
//   - assigning to the loop binding. `for s in strs { s = mk(7); }` writes into
//     the loop's word-wise COPY, so the container never sees the value and
//     nothing owns the new one — measured at 192 bytes lost, one block per
//     iteration, with the container unchanged. The place spelling
//     `for it in xs { it.name = mk(7); }` behaves the same way.
//   - `&mut` in the ITERABLE position. `for h in &mut hs { ... }` measures
//     BYTE-IDENTICALLY to the plain form: asking for mutability explicitly buys
//     nothing, the write still lands in a copy and still leaks. A reader would
//     reasonably expect that spelling to be the one that works, which makes
//     silently accepting it the only disposition that is definitely wrong.
//
// The language was already half of this way before the rule existed:
// `set_name(&mut it, ...)` inside the loop is refused with "cannot take mutable
// borrow of 'it'", so the borrow checker already held that this binding may not
// be mutated through. Only the direct store slipped past.
//
// Measured before the rule was written: the whole corpus — `testdata`,
// `showcases`, `stdlib`, `core`, `internal/vm/testdata`, `modules_test` —
// contains ZERO assignments to a for-in binding and ZERO `for ... in &mut`, so
// a uniform refusal (Copy element types included) breaks nothing that exists.

// rejectWriteToLoopBinding refuses a store whose target is a `for-in` binding,
// whether the store rebinds the whole thing or reaches a field of it.
//
// The predicate is isNonOwningBinding, which TODAY means "loop binding" because
// markNonOwningBinding has exactly one caller. That is what lets the message
// name a loop; a second producer would turn the headline into a lie, which is
// why a golden fixture pins the shape rather than only the rule.
func (tc *typeChecker) rejectWriteToLoopBinding(desc placeDescriptor, span source.Span) bool {
	if !desc.Base.IsValid() || !tc.isNonOwningBinding(desc.Base) {
		return false
	}
	name := tc.bindingName(desc.Base)
	headline := fmt.Sprintf("cannot assign through `%s`: a `for` loop binding is read-only", name)
	if tc.reporter == nil {
		tc.report(diag.SemaWriteToLoopBinding, span, "%s", headline)
		return true
	}
	b := diag.ReportError(tc.reporter, diag.SemaWriteToLoopBinding, span, headline)
	if b == nil {
		return true
	}
	b.WithNote(span, fmt.Sprintf(
		"`%s` is a copy of the element the container still owns, so this store would be lost and the value it assigned would leak",
		name))
	b.WithNote(span,
		"hint: write to the container instead - `xs[i] = v` - iterating over its indices if you need them")
	b.Emit()
	return true
}

// rejectMutableIterable refuses `&mut` in the iterable position.
//
// The test is on the `&mut expr` SYNTAX, deliberately NOT on "the iterable has
// reference type": a `fn f(xs: &mut Item[]) { for it in xs { ... } }` passes a
// reference the caller made and must keep working — the mistake is asking for
// mutability AT the loop, where it cannot be honoured.
func (tc *typeChecker) rejectMutableIterable(iterable ast.ExprID) {
	if !iterable.IsValid() {
		return
	}
	node := tc.builder.Exprs.Get(iterable)
	if node == nil || node.Kind != ast.ExprUnary {
		return
	}
	unary := tc.builder.Exprs.Unaries.Get(uint32(node.Payload))
	if unary == nil || unary.Op != ast.ExprUnaryRefMut {
		return
	}
	span := tc.exprSpan(iterable)
	headline := "cannot iterate over `&mut`: `for` loops only read what they iterate"
	if tc.reporter == nil {
		tc.report(diag.SemaMutableIterable, span, "%s", headline)
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaMutableIterable, span, headline)
	if b == nil {
		return
	}
	b.WithNote(span,
		"the loop binding is a copy of each element whatever the iterable is, so this `&mut` is accepted and then ignored - the write lands in the copy")
	b.WithNote(span,
		"hint: drop the `&mut` to iterate, and write to the container itself - `xs[i] = v` - where you need to change an element")
	b.Emit()
}
