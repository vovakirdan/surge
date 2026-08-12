package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
)

// reinitializeAssignedPlace handles a store INTO a place - a struct field, a
// field one level down, a tuple element, or a target behind `&mut` - as opposed
// to a store over a whole binding, which `handleAssignment` keeps inline.
//
// Two things happen here and their ORDER is the whole point.
//
// The store owes a drop for the value it displaces, and the place has to come
// back to life afterwards because assigning into it reinitializes it: a field
// that was given away returns, and with nothing else moved under the binding the
// whole value is readable again. Reviving is what makes the second half true,
// and reviving DELETES every moved place the target covers - which is exactly
// the set the drop record's own guard has to read to know whether this place was
// already given away. So the record is taken first. Reversed, `@drop o.inner`
// followed by `o.inner = Inner{...}` frees the same storage twice.
func (tc *typeChecker) reinitializeAssignedPlace(
	exprID ast.ExprID,
	op ast.ExprBinaryOp,
	left, right ast.ExprID,
	desc placeDescriptor,
	span source.Span,
) {
	reviveTarget := desc
	if !tc.isWriteThroughMutRef(desc) {
		reviveTarget, _ = tc.expandPlaceDescriptor(desc)
	}
	target := tc.canonicalPlace(reviveTarget)
	if !target.IsValid() {
		return
	}
	// Only a plain `=`. A compound assignment reaches here too, and its lowering
	// never consults the overwritten-value drop, so a record made for one would
	// be an obligation nothing discharges. Whether `s.f += x` leaks the value it
	// displaces is a separate question with a separate answer.
	if op == ast.ExprBinaryAssign {
		tc.recordPlaceOldDrop(exprID, left, right, desc, target)
	}
	if blockedBy, blockedSpan, revived := tc.revivePlace(target); !revived {
		tc.reportAssignIntoMovedValue(target, blockedBy, span, blockedSpan)
	}
}
