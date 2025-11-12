package sema

import (
	"fmt"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

func (tc *typeChecker) placeLabel(place Place) string {
	if !place.Base.IsValid() {
		return "value"
	}
	return tc.symbolLabel(place.Base)
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
	msg := fmt.Sprintf("cannot send %s to a spawned task", label)
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
