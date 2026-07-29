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

// reportPartialMove rejects taking a projection out of a live binding.
//
// The diagnostic has to carry the whole explanation, because the thing that
// goes wrong is invisible from the program text: without this rejection the
// extraction compiles and produces a second NAME for the container's field
// rather than a value, so writing through it is visible through the original.
// Naming the eventual spelling matters too — this is a "not yet", not a "no",
// and a reader who is told only that the move is unsupported has no way to
// tell which.
func (tc *typeChecker) reportPartialMove(desc placeDescriptor, expr ast.ExprID, span source.Span) {
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
	b := diag.ReportError(tc.reporter, diag.SemaPartialMoveUnsupported, span,
		fmt.Sprintf("cannot move `%s` out of `%s`: `%s` would stay usable beside it", label, base, base))
	if b == nil {
		return
	}
	b.WithNote(span, fmt.Sprintf(
		"moving one place out of a live value is a PARTIAL move, and moves are tracked per binding rather than per place, "+
			"so `%s` can be neither invalidated on its own nor left holding only what remains",
		label))
	b.WithNote(span, fmt.Sprintf(
		"hint: borrow it instead (`&%s`), copy the whole value, or move `%s` itself if you are finished with it; "+
			"`own %s` becomes the way to take just this place once partial moves are tracked",
		label, base, label))
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
