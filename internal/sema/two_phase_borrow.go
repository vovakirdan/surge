package sema

import (
	"surge/internal/ast"
)

// Two-phase argument borrows (the f(&mut a, len(a)) fix): a `&mut x` written
// DIRECTLY as a call argument does not need exclusive access while the
// SIBLING arguments are still being evaluated — every argument is fully
// evaluated before the callee runs. The borrow is therefore RESERVED during
// the argument list (it blocks writes, moves, and other mutable borrows, but
// coexists with shared reads like the implicit borrow inside `len(a)`), and
// ACTIVATED once all arguments are checked. Activation still rejects any
// borrow that is genuinely alive at the call — an explicit `&a` sibling, or
// a loan kept alive through a reference-returning call — so
// f(&mut a, &a) stays an error.
//
// Only the direct-argument shape reserves: a `&mut` nested deeper inside an
// argument expression can be USED before the call, so it keeps today's
// activate-immediately semantics.

type twoPhaseFrame struct {
	// reserved collects the reservation ids created while this call's
	// arguments were being checked, in argument order.
	reserved []BorrowID
	// marked remembers which expressions this frame made eligible, so an
	// abandoned or error-recovered argument list cannot leave stale
	// eligibility behind (a later re-type of the same node must not
	// resurrect a dead call context).
	marked []ast.ExprID
}

// beginTwoPhaseArgs marks each direct `&mut place` argument of the call as
// eligible for reservation and returns the frame that collects them. Nested
// calls push their own frames; eligibility is keyed by the unary expression
// id, so an inner call's arguments never leak into the outer frame.
func (tc *typeChecker) beginTwoPhaseArgs(args []ast.CallArg) *twoPhaseFrame {
	if tc.builder == nil {
		return nil
	}
	frame := (*twoPhaseFrame)(nil)
	for _, arg := range args {
		inner := tc.unwrapGroupExpr(arg.Value)
		if !inner.IsValid() {
			continue
		}
		node := tc.builder.Exprs.Get(inner)
		if node == nil || node.Kind != ast.ExprUnary {
			continue
		}
		unary := tc.builder.Exprs.Unaries.Get(uint32(node.Payload))
		if unary == nil || unary.Op != ast.ExprUnaryRefMut {
			continue
		}
		if frame == nil {
			frame = &twoPhaseFrame{}
		}
		if tc.twoPhaseEligible == nil {
			tc.twoPhaseEligible = make(map[ast.ExprID]*twoPhaseFrame)
		}
		tc.twoPhaseEligible[inner] = frame
		frame.marked = append(frame.marked, inner)
	}
	return frame
}

// reserveTwoPhaseBorrow consumes the eligibility of a direct `&mut` argument
// expression. It returns the frame the reservation must join, or nil when
// the expression is not a direct argument of the call currently checking its
// arguments.
func (tc *typeChecker) reserveTwoPhaseBorrow(exprID ast.ExprID) *twoPhaseFrame {
	if tc.twoPhaseEligible == nil {
		return nil
	}
	frame, ok := tc.twoPhaseEligible[exprID]
	if !ok {
		return nil
	}
	delete(tc.twoPhaseEligible, exprID)
	return frame
}

// activateTwoPhaseArgs upgrades every reservation of the finished argument
// list. All sibling temporaries have released their loans by now, so a
// conflict here names a borrow that truly overlaps the callee's exclusive
// access.
func (tc *typeChecker) activateTwoPhaseArgs(frame *twoPhaseFrame) {
	if frame == nil || tc.borrow == nil {
		return
	}
	for _, expr := range frame.marked {
		delete(tc.twoPhaseEligible, expr)
	}
	frame.marked = frame.marked[:0]
	for _, id := range frame.reserved {
		info := tc.borrow.Info(id)
		if info == nil {
			continue
		}
		issue := tc.borrow.Activate(id)
		if issue.Kind != BorrowIssueNone {
			tc.reportBorrowConflict(info.Place, info.Span, issue, BorrowMut)
		}
	}
	frame.reserved = frame.reserved[:0]
}
