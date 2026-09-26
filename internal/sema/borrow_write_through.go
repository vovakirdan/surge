package sema

import (
	"surge/internal/source"
	"surge/internal/symbols"
)

// checkAssignmentWrite records an assignment's write event and refuses the
// write when a live loan holds the place. A write through a `&mut`
// reference (writeThroughMutRef) is checked on the referent (checkedMutRefPlace,
// when valid) and authorized by the reference's reborrow chain (mutRefParent).
func (tc *typeChecker) checkAssignmentWrite(place, checkedMutRefPlace Place, writeThroughMutRef bool, mutRefParent BorrowID, span source.Span) {
	checkedPlace := place
	if writeThroughMutRef && checkedMutRefPlace.IsValid() {
		checkedPlace = checkedMutRefPlace
	}
	// The exclusive loans that give `&mut` permission -- the reference's
	// own and the rest of its reborrow chain (`let r2 = &mut *r` writes
	// through r2 while r's loan also covers the place) -- are not a
	// conflict with its write. A child shared loan still is: a BytesView
	// returned from `view(s)` is such a child, and must keep `*s = ...`
	// frozen while the view lives.
	var issue BorrowIssue
	if writeThroughMutRef {
		issue = tc.borrow.WriteThroughAllowed(checkedPlace, mutRefParent)
	} else {
		issue = tc.borrow.MutationAllowed(checkedPlace)
	}
	eventPlace := place
	if issue.Kind != BorrowIssueNone {
		eventPlace = checkedPlace
	}
	note := ""
	if writeThroughMutRef && issue.Kind == BorrowIssueNone {
		note = "write_through_mut_ref"
	}
	tc.recordBorrowEvent(&BorrowEvent{
		Kind:        BorrowEvWrite,
		Place:       eventPlace,
		Span:        span,
		Scope:       tc.currentScope(),
		Issue:       issue.Kind,
		IssueBorrow: issue.Borrow,
		Note:        note,
	})
	tc.refuseWriteToHeldPlace(checkedPlace, span, issue)
}

// exclusiveOffChain returns an exclusive loan overlapping place that a new
// borrow reborrowed through parent conflicts with, or NoBorrowID.
//
// A reborrow chain stacks one exclusive loan per level on overlapping places:
// `let r = &mut s; let r2 = &mut *r` holds s through r's loan and s.* through
// r2's. A borrow through r2 is authorized by BOTH, so every ancestor of parent
// is excused, not only parent. Asking only the first exclusive loan found --
// which one is first depends on map order -- made `&mut *r2` and a view of r2
// conflict with r's loan on some runs and not on others. Any other exclusive
// loan, including one reborrowed FROM parent that is still live, conflicts;
// the lowest id is reported so the diagnostic does not depend on map order.
func (bt *BorrowTable) exclusiveOffChain(place Place, parent BorrowID) BorrowID {
	held := NoBorrowID
	for p, state := range bt.placeState {
		if state.mut == NoBorrowID || !bt.placesOverlap(place, p) || bt.isAncestorOrSelf(state.mut, parent) {
			continue
		}
		if held == NoBorrowID || state.mut < held {
			held = state.mut
		}
	}
	return held
}

// isAncestorOrSelf reports whether loan is id or one of the loans id was
// reborrowed through.
func (bt *BorrowTable) isAncestorOrSelf(loan, id BorrowID) bool {
	for ; id != NoBorrowID; id = bt.parentOf(id) {
		if id == loan {
			return true
		}
	}
	return false
}

// WriteThroughAllowed verifies a write through a `&mut` reference whose own
// loan is holder (NoBorrowID when the reference is a parameter or otherwise
// carries no loan of this function; place is then rooted at the reference).
//
// The reference's reborrow chain is the write's own authority, not a conflict:
// every ancestor it was reborrowed through (`let r2 = &mut *r` writes through
// r2 while r's loan is on the chain) and every exclusive reborrow taken from
// it. The descendants are excused because loans are lexical here: rejecting
// `*r = x` after a finished `*r2 = y` would refuse code whose reborrow is
// dead, which the checker admitted before shared views were tracked.
//
// Any live SHARED loan of the place still freezes it -- a BytesView or a
// `&*r` read taken from the chain is exactly such a loan -- and so does an
// exclusive loan off the chain.
func (bt *BorrowTable) WriteThroughAllowed(place Place, holder BorrowID) BorrowIssue {
	if bt == nil || !place.IsValid() {
		return BorrowIssue{}
	}
	state := bt.combinedState(place)
	if len(state.shared) > 0 {
		return BorrowIssue{Kind: BorrowIssueFrozen, Borrow: state.shared[0]}
	}
	for p, st := range bt.placeState {
		if st.mut == NoBorrowID || !bt.placesOverlap(place, p) {
			continue
		}
		if !bt.onReborrowChain(st.mut, holder, place.Base) {
			return BorrowIssue{Kind: BorrowIssueTaken, Borrow: st.mut}
		}
	}
	return BorrowIssue{}
}

// onReborrowChain reports whether loan is holder, an ancestor of holder, or
// reborrowed (transitively) from holder. With no holder, a loan whose chain
// starts on base -- the reference binding itself -- was taken through it.
func (bt *BorrowTable) onReborrowChain(loan, holder BorrowID, base symbols.SymbolID) bool {
	if bt.isAncestorOrSelf(loan, holder) {
		return true
	}
	root := loan
	for id := loan; id != NoBorrowID; id = bt.parentOf(id) {
		if id == holder {
			return true
		}
		root = id
	}
	if holder != NoBorrowID {
		return false
	}
	info := bt.Info(root)
	return info != nil && info.Place.Base == base
}

// parentOf returns the loan id was reborrowed through. A parent is always
// created before its child, so a parent id smaller than id is the only one
// accepted; anything else ends the walk instead of looping.
func (bt *BorrowTable) parentOf(id BorrowID) BorrowID {
	info := bt.Info(id)
	if info == nil || info.Parent >= id {
		return NoBorrowID
	}
	return info.Parent
}
