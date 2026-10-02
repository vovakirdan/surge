package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// A choice that joins a plain value with `nothing` is an Option (owner ruling
// 2026-09-29): `c ? 5 : nothing` and `compare k { 1 => 5; _ => nothing; }`
// are `Option<int>`, the plain value wrapped in `Some`. The ternary used to
// take the plain branch's own type, `int`, so the `nothing` path produced a
// value `int` cannot hold: natively a 0, on the VM a panic in the first use.
// A plain value joined with an Option it fits is wrapped the same way, which
// is what a nested choice needs: `c ? 5 : d ? 6 : nothing`. An annotated
// Option target wins, with its own payload (`let x: int8? = c ? 5 : nothing`),
// and so does a union target with both cases (`type M = int | nothing`); any
// other target refuses the join (`let x: int = ...`).
//
// One rule with the union join (choice_nothing_join.go): T ⊔ nothing is
// Option<T>, a plain T wrapped in `Some` here and a `Some<T>` upcast there;
// a union that holds `nothing` is itself; any other union is refused. This
// file runs first and answers every plain branch, including one meeting a
// `Some<T>` or an Option (`c ? 3 : d ? Some(4) : nothing`); the union join
// then settles a choice whose branches are all unions or `nothing`.

// plainChoiceValue reports whether ty is a value a choice may wrap in `Some`:
// known, not `nothing`, and no union.
func (tc *typeChecker) plainChoiceValue(ty types.TypeID) bool {
	if ty == types.NoTypeID || tc.types == nil || ty == tc.types.Builtins().Nothing {
		return false
	}
	_, isUnion := tc.types.UnionInfo(tc.resolveAlias(ty))
	return !isUnion
}

// plainOptionJoin is the Option a plain value of type plain joins with a
// value of type other: `Option<plain>` for `nothing`, other itself for an
// Option plain can be injected into. ok is false for anything else.
func (tc *typeChecker) plainOptionJoin(plain, other types.TypeID, span source.Span) (types.TypeID, bool) {
	if !tc.plainChoiceValue(plain) || other == types.NoTypeID {
		return types.NoTypeID, false
	}
	if other == tc.types.Builtins().Nothing {
		option := tc.resolveOptionType(plain, span, tc.scopeOrFile(tc.currentScope()))
		return option, option != types.NoTypeID
	}
	other = tc.someAsOption(other, span)
	if !tc.isOptionType(other) {
		return types.NoTypeID, false
	}
	if _, kind, ok := tc.tryTagInjection(plain, other); ok && kind == ImplicitConversionSome {
		return other, true
	}
	return types.NoTypeID, false
}

// ternaryPlainOptionJoin settles a ternary whose one branch is a plain value
// and whose other is `nothing` or an Option, before the branches are unified:
// the plain branch is wrapped in `Some` and its type becomes the Option, so
// unification yields the Option. A discarded ternary keeps its branch types:
// no value leaves it.
func (tc *typeChecker) ternaryPlainOptionJoin(id ast.ExprID, tern *ast.ExprTernaryData, trueType, falseType types.TypeID, span source.Span) (joinedTrue, joinedFalse types.TypeID) {
	if tern == nil || tc.isExprDiscarded(id) {
		return trueType, falseType
	}
	plainExpr, plain, other := tern.TrueExpr, trueType, falseType
	if !tc.plainChoiceValue(plain) {
		plainExpr, plain, other = tern.FalseExpr, falseType, trueType
	}
	if other != tc.types.Builtins().Nothing && !tc.isOptionType(tc.someAsOption(other, span)) {
		return trueType, falseType
	}
	otherExpr := tern.FalseExpr
	if plainExpr == tern.FalseExpr {
		otherExpr = tern.TrueExpr
	}
	if tc.choiceBranchLeaves(otherExpr) {
		// `skip ? { continue; } : f(i)` leaves instead of answering `nothing`.
		return trueType, falseType
	}
	expected := tc.expectedTypeForExpr(id)
	if payload, isOption := tc.optionPayload(expected); isOption {
		// An untyped literal takes the target's payload, as `let n: int8 = 6` does.
		// A literal out of the payload's range is reported there and still
		// takes the payload type, so the binding does not report it again.
		if applied, _ := tc.materializeNumericLiteral(plainExpr, payload); applied {
			plain = tc.result.ExprTypes[plainExpr]
		}
	}
	option, ok := tc.plainOptionJoin(plain, other, span)
	if !ok {
		return trueType, falseType
	}
	switch {
	case expected == types.NoTypeID:
		tc.recordImplicitConversionWithKind(plainExpr, plain, option, ImplicitConversionSome)
	case tc.isOptionType(expected):
		option = expected
		tc.ensureChoiceTarget(id, option, plain, plainExpr)
	case tc.unionHoldsNothing(expected) && other == tc.types.Builtins().Nothing && tc.typesAssignable(expected, plain, true):
		// A target union with both cases takes each branch directly: no Option.
		option = expected
		tc.ensureChoiceTarget(id, option, plain, plainExpr)
	case tc.unionHoldsNothing(expected):
		// Reported once, here; the choice then has the target's type.
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(plainExpr), "cannot assign %s to %s",
			tc.typeLabel(plain), tc.typeLabel(expected))
		option = expected
	default:
		// The Option is not what the target holds, and no conversion may make
		// it so: `Option<T> to int` is an exit code, not the value.
		tc.report(diag.SemaTypeMismatch, tc.exprSpan(otherExpr), "cannot assign %s to %s",
			tc.typeLabel(other), tc.typeLabel(expected))
		return trueType, falseType
	}
	if other != tc.types.Builtins().Nothing && other != option {
		tc.recordTagUnionUpcast(otherExpr, other, option)
	}
	if plainExpr == tern.TrueExpr {
		return option, falseType
	}
	return trueType, option
}

// choiceBranchLeaves reports whether a branch typed `nothing` leaves instead
// of answering: an abrupt exit, or a block that breaks or continues its loop.
func (tc *typeChecker) choiceBranchLeaves(branch ast.ExprID) bool {
	if tc.compareArmAbruptExit(branch) {
		return true
	}
	block, ok := tc.builder.Exprs.Block(tc.unwrapGroupExpr(branch))
	return ok && block != nil && tc.stmtsLeaveLoop(block.Stmts)
}

func (tc *typeChecker) stmtsLeaveLoop(stmts []ast.StmtID) bool {
	for _, id := range stmts {
		stmt := tc.builder.Stmts.Get(id)
		if stmt == nil {
			continue
		}
		switch stmt.Kind {
		case ast.StmtBreak, ast.StmtContinue:
			return true
		case ast.StmtBlock:
			if inner := tc.builder.Stmts.Block(id); inner != nil && tc.stmtsLeaveLoop(inner.Stmts) {
				return true
			}
		}
	}
	return false
}

// unionHoldsNothing reports whether ty is a union with a `nothing` case.
func (tc *typeChecker) unionHoldsNothing(ty types.TypeID) bool {
	if ty == types.NoTypeID || tc.types == nil {
		return false
	}
	info, ok := tc.types.UnionInfo(tc.resolveAlias(ty))
	if !ok || info == nil {
		return false
	}
	for _, member := range info.Members {
		if member.Kind == types.UnionMemberNothing {
			return true
		}
	}
	return false
}

// typeChoiceBranch types one ternary branch. A nested choice under a target
// that can hold `nothing` is typed against that target, so its literals and
// its own join answer the target's type: `let z: int8? = c ? 7 : d ? 8 : nothing`.
func (tc *typeChecker) typeChoiceBranch(id, branch ast.ExprID) types.TypeID {
	if id == tc.callChoiceExpr {
		return tc.typeExpr(branch)
	}
	expected := tc.expectedTypeForExpr(id)
	if node := tc.builder.Exprs.Get(tc.unwrapGroupExpr(branch)); node != nil && tc.unionHoldsNothing(expected) &&
		(node.Kind == ast.ExprTernary || node.Kind == ast.ExprCompare) {
		return tc.typeExprWithExpected(branch, expected)
	}
	return tc.typeTagCallUnder(branch, expected)
}

// someAsOption widens `Some<T>` to `Option<T>` as the union join does beside
// `nothing` (choiceNothingJoin), so a plain value joins either the same way.
func (tc *typeChecker) someAsOption(ty types.TypeID, span source.Span) types.TypeID {
	if ty == types.NoTypeID || ty == tc.types.Builtins().Nothing || tc.isOptionType(ty) {
		return ty
	}
	if joined, ok := tc.choiceNothingJoin(ty, span); ok && tc.isOptionType(joined) {
		return joined
	}
	return ty
}

func (tc *typeChecker) isOptionType(ty types.TypeID) bool {
	_, ok := tc.optionPayload(ty)
	return ok
}

// compareArmOptionJoin is the compare's arm unification step for a plain arm
// meeting an Option arm, in either order: the Option, or ok false.
func (tc *typeChecker) compareArmOptionJoin(id ast.ExprID, resultType, armResult types.TypeID, span source.Span) (types.TypeID, bool) {
	if tc.isExprDiscarded(id) {
		// A discarded compare keeps its old typing, as comparePlainOptionJoin
		// and a discarded ternary do: no arm is wrapped, so none may be joined.
		return types.NoTypeID, false
	}
	if joined, ok := tc.plainOptionJoin(armResult, resultType, span); ok {
		return joined, true
	}
	if joined, ok := tc.plainOptionJoin(resultType, armResult, span); ok {
		return joined, true
	}
	return types.NoTypeID, false
}

// comparePlainOptionJoin settles an unannotated, used compare after its arms:
// a plain result that some value arm answers with `nothing` becomes
// `Option<result>`, and every value arm of a plain type is wrapped in `Some`.
func (tc *typeChecker) comparePlainOptionJoin(id ast.ExprID, cmp *ast.ExprCompareData, armTypes []types.TypeID, armClosed []bool, resultType types.TypeID, span source.Span) types.TypeID {
	if cmp == nil || resultType == types.NoTypeID || tc.isExprDiscarded(id) {
		return resultType
	}
	nothing := tc.types.Builtins().Nothing
	option := resultType
	if tc.plainChoiceValue(resultType) {
		for i := range cmp.Arms {
			if !armClosed[i] && armTypes[i] == nothing && !tc.choiceBranchLeaves(cmp.Arms[i].Result) {
				option, _ = tc.plainOptionJoin(resultType, nothing, span)
				break
			}
		}
	}
	if !tc.isOptionType(option) {
		return resultType
	}
	for i, arm := range cmp.Arms {
		if armClosed[i] || !tc.plainChoiceValue(armTypes[i]) {
			continue
		}
		if joined, ok := tc.plainOptionJoin(armTypes[i], option, span); ok && joined == option {
			tc.recordImplicitConversionWithKind(arm.Result, armTypes[i], option, ImplicitConversionSome)
		}
	}
	for i, arm := range cmp.Arms {
		// A `Some<T>` arm beside a plain one is upcast into the Option.
		if !armClosed[i] && armTypes[i] != option && armTypes[i] != nothing && !tc.plainChoiceValue(armTypes[i]) {
			tc.recordTagUnionUpcast(arm.Result, armTypes[i], option)
		}
	}
	return option
}
