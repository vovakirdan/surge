package sema

import (
	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// A choice whose values are `Some(x)` and `nothing` -- `c ? Some(1) : nothing`,
// `compare k { 1 => Some(1); _ => nothing; }` -- yields an Option. A ternary
// took the Some branch's own type, `Some<int>`, whose only case is `Some`, so
// the value the `nothing` branch produced had no case to live in: the VM
// refused it (VM1003) and the native build failed after the `nothing` arm's
// test had been folded to false. A compare refused the same join outright.
// The join is now `Option<T>`, the Some value is upcast into it, and a union
// that cannot hold `nothing` and has no Option to widen to is refused.

// choiceNothingJoin is the type a choice yields when one value is `nothing`
// and another has type other: other itself when it can hold `nothing` or is
// no union, `Option<T>` for `Some<T>`. ok is false when other is a union
// with neither.
func (tc *typeChecker) choiceNothingJoin(other types.TypeID, span source.Span) (types.TypeID, bool) {
	if other == types.NoTypeID || tc.types == nil {
		return other, true
	}
	info, isUnion := tc.types.UnionInfo(tc.resolveAlias(other))
	if !isUnion || info == nil {
		return other, true
	}
	for _, member := range info.Members {
		if member.Kind == types.UnionMemberNothing {
			return other, true
		}
	}
	if len(info.Members) == 1 && info.Members[0].Kind == types.UnionMemberTag &&
		tc.lookupName(info.Members[0].TagName) == "Some" && len(info.Members[0].TagArgs) == 1 {
		option := tc.resolveOptionType(info.Members[0].TagArgs[0], span, tc.scopeOrFile(tc.currentScope()))
		if option != types.NoTypeID && tc.canTagUnionUpcast(other, option) {
			return option, true
		}
	}
	return types.NoTypeID, false
}

// ternaryNothingJoin settles a ternary one of whose branches is `nothing`:
// the annotated union it initializes when there is one and the other branch
// fits it, else choiceNothingJoin. The other branch is upcast into the result.
func (tc *typeChecker) ternaryNothingJoin(id ast.ExprID, tern *ast.ExprTernaryData, trueType, falseType, unified types.TypeID, span source.Span) types.TypeID {
	nothing := tc.types.Builtins().Nothing
	other, otherExpr, nothingExpr := trueType, tern.TrueExpr, tern.FalseExpr
	switch {
	case trueType == nothing && falseType != nothing:
		other, otherExpr, nothingExpr = falseType, tern.FalseExpr, tern.TrueExpr
	case falseType != nothing || trueType == nothing || trueType == types.NoTypeID:
		return unified
	}
	result := types.NoTypeID
	if expected := tc.expectedTypeForExpr(id); expected != types.NoTypeID {
		if joined, ok := tc.choiceNothingJoin(expected, span); ok && joined == expected && tc.canTagUnionUpcast(other, expected) {
			result = expected
		}
	}
	if result == types.NoTypeID {
		joined, ok := tc.choiceNothingJoin(other, span)
		if !ok {
			tc.report(diag.SemaTypeMismatch, tc.exprSpan(nothingExpr),
				"cannot assign nothing to %s", tc.typeLabel(other))
			return types.NoTypeID
		}
		result = joined
	}
	if result != other {
		tc.recordTagUnionUpcast(otherExpr, other, result)
	}
	return result
}
