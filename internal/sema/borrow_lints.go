package sema

import (
	"fmt"
	"slices"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func (tc *typeChecker) placeLabel(place Place) string {
	base := tc.symbolLabel(place.Base)
	if tc.borrow == nil {
		return base
	}
	strings := tc.builder.StringsInterner
	if strings == nil && tc.symbols != nil && tc.symbols.Table != nil {
		strings = tc.symbols.Table.Strings
	}
	return tc.borrow.formatPlaceLabel(place, base, strings)
}

// plainPlaceLabel renders a place the way source spells it — `o.inner`, with no
// quoting. placeLabel quotes the base (`'o'`) because the borrow diagnostics it
// serves read that way; a message that wraps its own labels in backticks would
// print `'o'.inner`.
func (tc *typeChecker) plainPlaceLabel(place Place) string {
	base := tc.bindingName(place.Base)
	if tc.borrow == nil {
		return base
	}
	strs := tc.builder.StringsInterner
	if strs == nil && tc.symbols != nil && tc.symbols.Table != nil {
		strs = tc.symbols.Table.Strings
	}
	return formatPlaceSegments(base, tc.borrow.placeSegments(place), strs)
}

func (tc *typeChecker) symbolLabel(symID symbols.SymbolID) string {
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return "value"
	}
	name := tc.lookupName(sym.Name)
	if name == "" {
		return "value"
	}
	return fmt.Sprintf("'%s'", name)
}

func (tc *typeChecker) reportBorrowConflict(place Place, span source.Span, issue BorrowIssue, kind BorrowKind) {
	if issue.Kind == BorrowIssueNone {
		return
	}
	label := tc.placeLabel(place)
	var msg string
	switch issue.Kind {
	case BorrowIssueConflictMut:
		if kind == BorrowShared {
			msg = fmt.Sprintf("cannot take shared borrow of %s while an exclusive borrow is active", label)
		} else {
			msg = fmt.Sprintf("cannot take mutable borrow of %s while another mutable borrow is active", label)
		}
	case BorrowIssueConflictShared:
		msg = fmt.Sprintf("cannot take mutable borrow of %s while a shared borrow is active", label)
	default:
		msg = fmt.Sprintf("cannot borrow %s due to an active borrow", label)
	}
	tc.emitBorrowDiag(diag.SemaBorrowConflict, span, msg, issue.Borrow, label)
}

func (tc *typeChecker) reportBorrowMutation(place Place, span source.Span, issue BorrowIssue) {
	if issue.Kind == BorrowIssueNone {
		return
	}
	label := tc.placeLabel(place)
	var msg string
	switch issue.Kind {
	case BorrowIssueFrozen:
		msg = fmt.Sprintf("cannot mutate %s while it is shared-borrowed", label)
	case BorrowIssueTaken:
		msg = fmt.Sprintf("cannot mutate %s while an exclusive borrow is active", label)
	default:
		msg = fmt.Sprintf("cannot mutate %s due to an active borrow", label)
	}
	tc.emitBorrowDiag(diag.SemaBorrowMutation, span, msg, issue.Borrow, label)
}

func (tc *typeChecker) reportBorrowMove(place Place, span source.Span, issue BorrowIssue) {
	if issue.Kind == BorrowIssueNone {
		return
	}
	label := tc.placeLabel(place)
	var msg string
	switch issue.Kind {
	case BorrowIssueFrozen:
		msg = fmt.Sprintf("cannot move %s while it is shared-borrowed", label)
	case BorrowIssueTaken:
		msg = fmt.Sprintf("cannot move %s while an exclusive borrow is active", label)
	default:
		msg = fmt.Sprintf("cannot move %s due to an active borrow", label)
	}
	tc.emitBorrowDiag(diag.SemaBorrowMove, span, msg, issue.Borrow, label)
}

func (tc *typeChecker) reportSpawnThreadEscape(symID symbols.SymbolID, span source.Span, borrow BorrowID) {
	label := tc.symbolLabel(symID)
	msg := fmt.Sprintf("cannot send %s to a task", label)
	tc.emitBorrowDiag(diag.SemaBorrowThreadEscape, span, msg, borrow, label)
}

func (tc *typeChecker) emitBorrowDiag(code diag.Code, span source.Span, msg string, borrow BorrowID, label string) {
	if tc.reporter == nil {
		return
	}
	builder := diag.ReportError(tc.reporter, code, span, msg)
	if builder == nil {
		return
	}
	if tc.borrow != nil {
		if info := tc.borrow.Info(borrow); info != nil {
			note := fmt.Sprintf("previous borrow of %s occurs here", label)
			builder.WithNote(info.Span, note)
		}
	}
	builder.Emit()
}

// reportPartialMoveNeedsOwn rejects taking a projection out of a live binding
// when the source wrote a plain read.
//
// The read is accepted in every other position — as a borrow, as a copy — so
// the text alone cannot say which one this is, and the consequence is
// invisible: `o` is no longer whole afterwards, and the next line that reads it
// fails for a reason nothing at this line announced. `own` is the marker that
// already means "I am taking this" at call sites and crossings, so the fix is
// to say it here too rather than to learn a new rule.
func (tc *typeChecker) reportPartialMoveNeedsOwn(desc placeDescriptor, expr ast.ExprID, span source.Span) {
	if span == (source.Span{}) {
		span = tc.exprSpan(expr)
	}
	strs := tc.builder.StringsInterner
	if strs == nil && tc.symbols != nil && tc.symbols.Table != nil {
		strs = tc.symbols.Table.Strings
	}
	base := tc.bindingName(desc.Base)
	label := formatPlaceSegments(base, desc.Segments, strs)
	if tc.reporter == nil {
		return
	}
	b := diag.ReportError(tc.reporter, diag.SemaPartialMoveNeedsOwn, span,
		fmt.Sprintf("taking `%s` out of `%s` empties it, so write `own %s`", label, base, label))
	if b == nil {
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"this hands the value in `%s` to its new owner: `%s` keeps its other fields and can still be read, "+
			"but reading `%s` or `%s` as a whole after this is an error",
		label, base, label, base))
	b.WithNote(span, fmt.Sprintf(
		"hint: if you did not mean to empty it, borrow the field instead (`&%s`) or clone what you need",
		label))
	// The marker is the ONLY thing missing, so this is one of the few fixes a
	// compiler can offer without guessing: this diagnostic is reached after the
	// path is known enumerable, the place is known still present, and the read is
	// known to be a projection of a named binding at a resolved move-only type.
	// `own` in front of it is exactly the form that would have been accepted —
	// which is why the applicability is the always-safe default rather than a
	// heuristic.
	b.WithFixSuggestion(fix.InsertText(
		fmt.Sprintf("insert `own` to take `%s` out of `%s`", label, base),
		span.ZeroideToStart(), "own ", "", fix.Preferred()))
	b.Emit()
}

// rejectMoveOutOfSharedBorrow refuses reading a heap-owning value out of a
// shared reference into a position that takes ownership of it.
//
// The objection is the SECOND OWNER, and that is why this does not reuse the
// partial-move diagnostic next door. Nothing is being taken out of an aggregate
// and no residual has to be named — the whole pointee is read. Saying it under
// its own number is also what lets the message name `r.__clone()`: the
// partial-move hint says to move the value at the place that owns it, and
// following that here turns the error into a use-after-move as soon as the
// owner is read again, which the six-line reproducer for this defect does on
// the very next line.
//
// A type that owns no heap is left alone. Two bindings for one `int` is not a
// double free, and the check would otherwise fire on shapes with nothing wrong
// with them.
func (tc *typeChecker) rejectMoveOutOfSharedBorrow(expr ast.ExprID, span source.Span, exprType types.TypeID) {
	if tc.reporter == nil || !tc.ownsHeap(exprType) {
		return
	}
	if span == (source.Span{}) {
		span = tc.exprSpan(expr)
	}
	name := tc.derefOperandName(expr)
	label := tc.typeLabel(exprType)
	subject := "this reference"
	if name != "" {
		subject = fmt.Sprintf("`%s`", name)
	}
	b := diag.ReportError(tc.reporter, diag.SemaMoveOutOfSharedBorrow, span,
		fmt.Sprintf("cannot take the `%s` out of %s: a shared reference only borrows what it points at", label, subject))
	if b == nil {
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"the value already has an owner, and reading it here would make a second one — both would free the same `%s`", label))
	if name == "" {
		b.WithNote(span, "hint: clone it with `.__clone()` to pay for a copy, or keep working through the borrow")
		b.Emit()
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"hint: write `%s.__clone()` to pay for a copy, or keep working through the borrow", name))
	// Offered rather than preferred: `__clone()` allocates, and whether that
	// cost is wanted is the author's call — the other way out is to stop taking
	// the value at all, which no edit here can guess. The guard means the fix
	// simply does not apply when the source spells the deref some other way.
	b.WithFixSuggestion(fix.ReplaceSpan(
		fmt.Sprintf("copy the value: `%s.__clone()`", name),
		span, fmt.Sprintf("%s.__clone()", name), fmt.Sprintf("*%s", name),
		fix.WithApplicability(diag.FixApplicabilityManualReview)))
	b.Emit()
}

// derefOperandName names the reference a `*r` reads through, when it is a plain
// identifier. A deref of anything else — a field, a call result — has no name to
// put in a message, and the caller says so differently rather than inventing one.
func (tc *typeChecker) derefOperandName(expr ast.ExprID) string {
	if tc.builder == nil {
		return ""
	}
	unary, ok := tc.builder.Exprs.Unary(expr)
	if !ok || unary == nil {
		return ""
	}
	ident, ok := tc.builder.Exprs.Ident(unary.Operand)
	if !ok || ident == nil {
		return ""
	}
	return tc.lookupName(ident.Name)
}

// rejectArmHandingOutBorrowedPayload refuses an arm that returns a payload it
// only borrowed.
//
// This is the second half of the rule that refuses `let b: string = *r`, and it
// exists because the first half cannot see it. A compare over a borrowed union
// takes nothing, so the scrutinee `*arg` is allowed; but an arm that answers
// with its own payload binding hands the caller a value the union still owns,
// and the caller frees it. Measured on both backends at `5ebb0cec`:
// `compare *arg { IStr(s) => out + s; ... }` prints correctly twice and is
// valgrind-clean, while the same program with `IStr(s) => s;` prints once and
// dies with `free(): double free detected in tcache 2`.
//
// The existing machinery agrees that nothing is owed here and that is exactly
// the problem: armHandsOutItsPayload answers from the arm's OBLIGATIONS, a
// binding out of a borrowed subject earns none, so the retraction it performs
// never runs and no drop is emitted either way. Sema hands MIR one owner and
// the second release appears at the consuming sink. There is nothing to fix in
// the obligations; the program is asking for something it cannot have.
//
// Only a DIRECT hand-out is caught — `=> s`, the shape measured. A payload
// laundered through a call is the deep-laundering residual this project has
// recorded elsewhere and is not answered here.
func (tc *typeChecker) rejectArmHandingOutBorrowedPayload(
	result ast.ExprID,
	bindings []symbols.SymbolID,
	resultType types.TypeID,
) {
	if !result.IsValid() || len(bindings) == 0 || tc.reporter == nil {
		return
	}
	if !tc.ownsHeap(resultType) {
		return
	}
	inner := tc.unwrapGroups(result)
	symID := tc.symbolForExpr(inner)
	if !symID.IsValid() || !slices.Contains(bindings, symID) {
		return
	}
	// A payload that took its OWN reference on the way out is the arm's to give.
	// Only the reference-counted scalars do — the extraction retains them, which
	// is the same distinction registerComparePayloadDroppables makes one line
	// apart when it decides whether the binding owes a release. A binding that
	// owes one has something to hand over; a binding that owes nothing is
	// looking at the union's storage under another name.
	if tc.payloadTakesItsOwnReference(symID) {
		return
	}
	span := tc.exprSpan(result)
	// The bare name, not symbolLabel's quoted form: it goes inside backticks in
	// the message and into the quick-fix text, and neither wants quotes.
	sym := tc.symbolFromID(symID)
	if sym == nil {
		return
	}
	name := tc.lookupName(sym.Name)
	if name == "" {
		return
	}
	label := tc.typeLabel(resultType)
	b := diag.ReportError(tc.reporter, diag.SemaMoveOutOfSharedBorrow, span,
		fmt.Sprintf("cannot hand `%s` out of this arm: the compare only borrows the value it was matched against", name))
	if b == nil {
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"`%s` names storage the matched value still owns, so returning it would leave the caller and that owner both freeing the same `%s`",
		name, label))
	b.WithNote(span, fmt.Sprintf(
		"hint: write `%s.__clone()` to pay for a copy, or build the answer from `%s` without giving it away", name, name))
	b.WithFixSuggestion(fix.ReplaceSpan(
		fmt.Sprintf("copy the payload: `%s.__clone()`", name),
		span, fmt.Sprintf("%s.__clone()", name), name,
		fix.WithApplicability(diag.FixApplicabilityManualReview)))
	b.Emit()
}

// reportUnnameableResidual rejects a partial move out of a place the compiler
// cannot list the survivors of. See rejectUnnameableResidual for why the list
// is what decides this.
func (tc *typeChecker) reportUnnameableResidual(desc placeDescriptor, kind PlaceSegmentKind, expr ast.ExprID, span source.Span) {
	if span == (source.Span{}) {
		span = tc.exprSpan(expr)
	}
	strs := tc.builder.StringsInterner
	if strs == nil && tc.symbols != nil && tc.symbols.Table != nil {
		strs = tc.symbols.Table.Strings
	}
	base := tc.bindingName(desc.Base)
	label := formatPlaceSegments(base, desc.Segments, strs)
	if tc.reporter == nil {
		return
	}
	what := "an element"
	if kind == PlaceSegmentDeref {
		what = "a place behind a reference"
	}
	b := diag.ReportError(tc.reporter, diag.SemaPartialMoveNotEnumerable, span,
		fmt.Sprintf("cannot take `%s` out of `%s`: it names %s, and what would be left cannot be listed", label, base, what))
	if b == nil {
		return
	}
	if kind == PlaceSegmentDeref {
		b.WithNote(span, "a reference borrows what it points at, so there is no value here to give away")
		b.WithNote(span, fmt.Sprintf("hint: move the value that `%s` refers to, at the place that owns it", base))
		b.Emit()
		return
	}
	b.WithNote(span,
		"emptying one place means releasing the rest one at a time at every exit, and elements are chosen by "+
			"position rather than by name, so there is no list to release — a struct field has one, an array or "+
			"tuple element does not")
	b.WithNote(span, fmt.Sprintf(
		"hint: move `%s` as a whole, or take a field of it if that is what you need; taking an element out "+
			"and leaving a hole is not expressible",
		base))
	b.Emit()
}

// reportPlaceUseAfterMove reports reading a place that a PARTIAL move emptied.
//
// Worth its own wording rather than reusing the binding-level message: the
// value that went and the value being read are different names, and a reader
// told only "use of moved value 'o'" would go looking for a move of `o` that
// is not in the source. The two spans plus both labels are the whole
// explanation — this place went, that one is what you asked for.
func (tc *typeChecker) reportPlaceUseAfterMove(read, moved Place, span, moveSpan source.Span) {
	if tc.reporter == nil {
		return
	}
	readLabel := tc.plainPlaceLabel(read)
	movedLabel := tc.plainPlaceLabel(moved)
	var msg string
	switch {
	case read == moved:
		msg = fmt.Sprintf("use of moved value `%s`", readLabel)
	case len(read.Path) < len(moved.Path):
		// Reading a container part of which has gone.
		msg = fmt.Sprintf("cannot use `%s` as a whole: `%s` was moved out of it", readLabel, movedLabel)
	default:
		// Reading something under a place that has gone.
		msg = fmt.Sprintf("cannot use `%s`: `%s` was moved", readLabel, movedLabel)
	}
	b := diag.ReportError(tc.reporter, diag.SemaUseAfterMove, span, msg)
	if b == nil {
		return
	}
	if moveSpan != (source.Span{}) {
		b.WithNote(moveSpan, fmt.Sprintf("`%s` gave its value away here", movedLabel))
	}
	if read != moved && len(read.Path) < len(moved.Path) {
		b.WithNote(span, fmt.Sprintf(
			"hint: the fields `%s` still holds can be read individually; reading `%s` whole needs `%s` back",
			readLabel, readLabel, movedLabel))
	}
	b.Emit()
}

// reportAssignIntoMovedValue rejects storing into part of a value that has
// already been given away whole.
//
// The store has nowhere to land: `o` is gone, so `o.inner` names storage that
// is no longer this binding's. The way out is to reinitialize the whole value,
// and saying so is the point of the message — the reader's next question is
// always "then how do I fix it".
func (tc *typeChecker) reportAssignIntoMovedValue(target, movedWhole Place, span, moveSpan source.Span) {
	if tc.reporter == nil {
		return
	}
	targetLabel := tc.plainPlaceLabel(target)
	movedLabel := tc.plainPlaceLabel(movedWhole)
	b := diag.ReportError(tc.reporter, diag.SemaUseAfterMove, span,
		fmt.Sprintf("cannot assign to `%s`: `%s` was moved, so there is nothing to assign into", targetLabel, movedLabel))
	if b == nil {
		return
	}
	if moveSpan != (source.Span{}) {
		b.WithNote(moveSpan, fmt.Sprintf("`%s` gave its value away here", movedLabel))
	}
	b.WithNote(span, fmt.Sprintf(
		"hint: give `%s` a whole value first (`%s = ...`), then its fields can be assigned individually",
		movedLabel, movedLabel))
	b.Emit()
}
