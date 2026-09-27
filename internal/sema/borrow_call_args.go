package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// refArgFrame holds the reference arguments of one call: the places each can
// reach, and whether it is `&mut`.
type refArgFrame struct {
	args []refArg
}

type refArg struct {
	reach   []reached
	mutable bool
}

// reached is a place a reference argument can reach. passedOn: it is reached
// through an existing reference -- a reference place, or one a call returned
// from such a place -- which takes no loan of its own. A place reached through
// a borrow written in the call (`&mut a`, `ident(&a)`) holds that borrow's
// loan, and the loan conflicts on its own.
type reached struct {
	place    Place
	passedOn bool
}

// beginMutArgs opens the frame of a call whose argument ownership is about to
// be applied; the caller restores the returned frame with endMutArgs.
func (tc *typeChecker) beginMutArgs() *refArgFrame {
	prev := tc.mutArgs
	tc.mutArgs = &refArgFrame{}
	return prev
}

func (tc *typeChecker) endMutArgs(prev *refArgFrame) {
	tc.mutArgs = prev
}

// noteSharedRefArg notes a reference handed to a `&` parameter of the call
// whose frame is open, so a `&mut` argument reaching the same referent is
// refused: `rd2(r, r)` with `rd2(a: &int[], b: &mut int[])` lets the callee
// grow the array its other parameter reads.
func (tc *typeChecker) noteSharedRefArg(expr ast.ExprID, exprType types.TypeID, span source.Span) {
	if tc.mutArgs == nil || !tc.isReferenceType(exprType) {
		return
	}
	tc.noteRefArg(tc.unwrapGroupExpr(expr), false, span)
}

// noteRefArg refuses a reference argument that reaches a place an earlier
// reference argument of the same call reaches, when either is `&mut` and
// either was passed on: `app2(r, r)`, `app2(r, &mut *r)`, `app2(id(r), r)`
// and `rd2(r, r)` give the callee an exclusive reference and a second one to
// the same referent, and a passed-on reference takes no loan for the other to
// conflict with. Two borrows written in the call take loans, and the
// two-phase check answers them.
func (tc *typeChecker) noteRefArg(expr ast.ExprID, mutable bool, span source.Span) {
	frame := tc.mutArgs
	if frame == nil || tc.borrow == nil {
		return
	}
	arg := refArg{mutable: mutable, reach: tc.referentReach(expr, true, make(map[symbols.SymbolID]bool))}
	for _, prev := range frame.args {
		if !arg.mutable && !prev.mutable {
			continue
		}
		for _, a := range arg.reach {
			for _, b := range prev.reach {
				if (a.passedOn || b.passedOn) && placesOverlap(a.place, b.place) {
					tc.reportBorrowConflict(a.place, span, BorrowIssue{Kind: BorrowIssueConflictMut}, BorrowMut)
					return
				}
			}
		}
	}
	frame.args = append(frame.args, arg)
}

// referentReach names the places a reference argument can reach: the place a
// borrow or a reference place stands on, what a reference binding was given
// (`let q = id(r)` reaches what `id(r)` reaches), and for a call whose result
// carries a reference, everything its receiver and its reference arguments
// reach -- the result may point into any of them.
func (tc *typeChecker) referentReach(expr ast.ExprID, passedOn bool, seen map[symbols.SymbolID]bool) []reached {
	expr = tc.unwrapGroupExpr(expr)
	if !expr.IsValid() || tc.builder == nil || tc.result == nil {
		return nil
	}
	if unary, ok := tc.builder.Exprs.Unary(expr); ok && unary != nil &&
		(unary.Op == ast.ExprUnaryRef || unary.Op == ast.ExprUnaryRefMut) {
		return tc.referentReach(unary.Operand, false, seen)
	}
	if desc, ok := tc.resolvePlace(expr); ok && desc.Base.IsValid() {
		var out []reached
		if place, _ := tc.loanPlace(desc); place.IsValid() {
			out = append(out, reached{place: place, passedOn: passedOn})
		}
		if !seen[desc.Base] {
			seen[desc.Base] = true
			for _, src := range tc.lentValues.sources[desc.Base] {
				out = append(out, tc.referentReach(src, passedOn, seen)...)
			}
		}
		return out
	}
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		return nil
	}
	if _, carries := tc.carriedReferenceType(tc.result.ExprTypes[expr]); !carries {
		return nil
	}
	var out []reached
	if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
		out = append(out, tc.referentReach(member.Target, passedOn, seen)...)
	}
	for _, arg := range call.Args {
		if _, carries := tc.carriedReferenceType(tc.result.ExprTypes[arg.Value]); carries {
			out = append(out, tc.referentReach(arg.Value, passedOn, seen)...)
		}
	}
	return out
}
