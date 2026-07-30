package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
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
