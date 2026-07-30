package sema

import (
	"surge/internal/ast"
	"surge/internal/types"
)

// Statement-end temporaries: owned rvalues no binding ever owns (borrowed
// concat operands, discarded results, condition temporaries). Sema
// publishes the FINAL set — an expression has exactly one syntactic use
// position, so "produces an owned value and nothing consumes it" is
// decidable during the walk; HIR wraps the flagged expressions and MIR
// frees them at their evaluation region's end. Consumption never needs
// runtime deregistration because a consumed expression is simply never
// in the set.

type tempFrame struct {
	flags map[ast.ExprID]struct{}
	// tainted marks statements containing suspension-bearing constructs
	// (await/select/spawn/crossing/blocking): temp lifetimes across
	// suspension belong to the crossing vertical, so the whole frame is
	// discarded (leak — the pre-epic status quo — never a free).
	tainted bool
}

func (tc *typeChecker) pushTempFrame() {
	tc.tempFrames = append(tc.tempFrames, tempFrame{})
}

// popTempFrame publishes the frame's surviving flags unless tainted, each with
// the plan that reclaims what is LEFT of it.
//
// The plan is computed here rather than at the move because this is the point
// where the answer is complete: a temporary cannot be named again, so every
// path that will ever be taken out of it was taken inside the region now
// closing. A temporary nothing was taken from carries no plan, which is the
// ordinary whole release and every case there was before fields could move.
func (tc *typeChecker) popTempFrame() {
	if len(tc.tempFrames) == 0 {
		return
	}
	frame := tc.tempFrames[len(tc.tempFrames)-1]
	tc.tempFrames = tc.tempFrames[:len(tc.tempFrames)-1]
	if frame.tainted || len(frame.flags) == 0 {
		return
	}
	if tc.result.TempDrops == nil {
		tc.result.TempDrops = make(map[ast.ExprID][]DropStep)
	}
	for id := range frame.flags {
		tc.result.TempDrops[id] = tc.temporaryResidualPlan(tc.result.ExprTypes[id], tc.tempTaken[id])
	}
}

// recordTemporaryTaken notes that a path was moved out of an evaluation nothing
// holds, so its statement-end release can be narrowed to the remainder.
func (tc *typeChecker) recordTemporaryTaken(base ast.ExprID, path []PlaceSegment) {
	if !base.IsValid() || len(path) == 0 {
		return
	}
	if tc.tempTaken == nil {
		tc.tempTaken = make(map[ast.ExprID][][]PlaceSegment)
	}
	tc.tempTaken[base] = append(tc.tempTaken[base], path)
}

// pendingTempCandidate reports whether this evaluation is still an owned
// temporary nobody has consumed — the only kind whose release this pass may
// narrow, since anything else already has an owner that frees it whole.
func (tc *typeChecker) pendingTempCandidate(exprID ast.ExprID) bool {
	if !exprID.IsValid() {
		return false
	}
	exprID = tc.unwrapTempCandidate(exprID)
	for i := len(tc.tempFrames) - 1; i >= 0; i-- {
		if tc.tempFrames[i].flags == nil {
			continue
		}
		if _, ok := tc.tempFrames[i].flags[exprID]; ok {
			return true
		}
	}
	return false
}

func (tc *typeChecker) taintTempFrame() {
	if len(tc.tempFrames) == 0 {
		return
	}
	tc.tempFrames[len(tc.tempFrames)-1].tainted = true
}

// consumeTempCandidate removes an evaluation from the pending set: its
// value transferred somewhere that now owns it (binding, return, callee,
// aggregate position, outer control-flow expression) or escaped through
// an explicit borrow.
func (tc *typeChecker) consumeTempCandidate(exprID ast.ExprID) {
	if !exprID.IsValid() {
		return
	}
	exprID = tc.unwrapTempCandidate(exprID)
	for i := len(tc.tempFrames) - 1; i >= 0; i-- {
		if tc.tempFrames[i].flags != nil {
			delete(tc.tempFrames[i].flags, exprID)
		}
	}
}

// unwrapTempCandidate sees through grouping parentheses so consumption
// reaches the flagged node.
func (tc *typeChecker) unwrapTempCandidate(exprID ast.ExprID) ast.ExprID {
	for {
		node := tc.builder.Exprs.Get(exprID)
		if node == nil || node.Kind != ast.ExprGroup {
			return exprID
		}
		group, ok := tc.builder.Exprs.Group(exprID)
		if !ok || group == nil || !group.Inner.IsValid() {
			return exprID
		}
		exprID = group.Inner
	}
}

// noteTempCandidate runs at typeExpr's single exit: flag evaluations that
// PRODUCE an owned value. Place reads (idents, member/element access,
// derefs) are owned by their containers and never flagged; assign-shaped
// binaries yield the assigned PLACE and never flag; suspension constructs
// taint the whole statement instead.
func (tc *typeChecker) noteTempCandidate(exprID ast.ExprID, kind ast.ExprKind, ty types.TypeID) {
	if len(tc.tempFrames) == 0 || tc.dropObligationsSuppressed() {
		return
	}

	switch kind {
	case ast.ExprAwait, ast.ExprSelect, ast.ExprRace, ast.ExprAsync,
		ast.ExprBlocking, ast.ExprOn, ast.ExprTask, ast.ExprSpawn:
		tc.taintTempFrame()
		return
	}

	if !tc.isDroppableType(ty) {
		return
	}

	producer := false
	switch kind {
	case ast.ExprCall, ast.ExprCast, ast.ExprStruct, ast.ExprArray,
		ast.ExprMap, ast.ExprTuple:
		producer = true
	// Control-flow expressions (ternary/compare/block) NEVER produce:
	// their value can forward a PLACE from any arm (an assignment arm
	// yields its target's live value — dropping that is a use-after-
	// free through the binding, caught by the VM sanitizer on the
	// compare-arm-mutation row). Their arm results stay consumed, so
	// fresh values built in arms leak when the outer value is
	// discarded — the safe direction, recorded as a bound.
	case ast.ExprLit:
		producer = true // string literals heap-allocate per use
	case ast.ExprBinary:
		if data, ok := tc.builder.Exprs.Binary(exprID); ok && data != nil {
			if _, isAssign := tc.assignmentBaseOp(data.Op); !isAssign && data.Op != ast.ExprBinaryAssign {
				producer = true
			}
		}
	case ast.ExprUnary:
		if tc.result.MagicUnarySymbols != nil {
			_, producer = tc.result.MagicUnarySymbols[exprID]
		}
	case ast.ExprIndex:
		// Only slice expressions MINT a value (an owned view header);
		// element reads stay owned by the array.
		producer = tc.isArrayViewExpr(exprID)
	}
	if !producer {
		return
	}

	top := &tc.tempFrames[len(tc.tempFrames)-1]
	if top.flags == nil {
		top.flags = make(map[ast.ExprID]struct{})
	}
	top.flags[exprID] = struct{}{}
}
