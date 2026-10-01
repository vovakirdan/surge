package sema

import (
	"fmt"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/fix"
	"surge/internal/source"
	"surge/internal/types"
)

// A fixed array `T[N]` never becomes a dynamic `T[]` implicitly (LANGUAGE.md
// §2.2): the two have different layouts -- the fixed array's elements are
// inline, the dynamic array is a handle to a growable buffer -- and neither
// backend converts one into the other in place, so a value taken as the other
// kind is read through the wrong layout. The copy is the author's to write:
// `x.to_array()`. An array LITERAL is not a conversion: written where a `T[]`
// is expected it is typed as that `T[]` and builds it directly.

// fixedArrayWhereDynamic reports whether actual is a fixed array whose
// elements a dynamic array expected would accept, which is the one array
// shape the rule refuses and explains.
func (tc *typeChecker) fixedArrayWhereDynamic(expected, actual types.TypeID) bool {
	expElem, _, expFixed, okExp := tc.arrayInfo(expected)
	actElem, _, actFixed, okAct := tc.arrayInfo(actual)
	return okExp && okAct && !expFixed && actFixed && tc.typesAssignable(expElem, actElem, true)
}

// fixedArrayInside finds a fixed array standing where a dynamic one is
// expected at any depth of the two types: the types themselves, an array's
// elements, a tuple's elements, a generic record's or a union's type arguments
// (`Some<int[2]>` against `Option<int[]>`). It answers the pair it found.
func (tc *typeChecker) fixedArrayInside(expected, actual types.TypeID) (exp, act types.TypeID, ok bool) {
	return tc.fixedArrayInsideDepth(expected, actual, 0)
}

func (tc *typeChecker) fixedArrayInsideDepth(expected, actual types.TypeID, depth int) (exp, act types.TypeID, ok bool) {
	if tc.types == nil || expected == types.NoTypeID || actual == types.NoTypeID || depth > 8 {
		return types.NoTypeID, types.NoTypeID, false
	}
	expected, actual = tc.resolveAlias(expected), tc.resolveAlias(actual)
	if tc.fixedArrayWhereDynamic(expected, actual) {
		return expected, actual, true
	}
	if expElem, _, _, okExp := tc.arrayInfo(expected); okExp {
		if actElem, _, _, okAct := tc.arrayInfo(actual); okAct {
			return tc.fixedArrayInsideDepth(expElem, actElem, depth+1)
		}
		return types.NoTypeID, types.NoTypeID, false
	}
	if expTuple, okExp := tc.types.TupleInfo(expected); okExp {
		if actTuple, okAct := tc.types.TupleInfo(actual); okAct && len(expTuple.Elems) == len(actTuple.Elems) {
			for i := range expTuple.Elems {
				if e, a, found := tc.fixedArrayInsideDepth(expTuple.Elems[i], actTuple.Elems[i], depth+1); found {
					return e, a, true
				}
			}
		}
		return types.NoTypeID, types.NoTypeID, false
	}
	if expStruct, okExp := tc.types.StructInfo(expected); okExp && expStruct != nil {
		// The same generic record at other arguments (`Box<int[2]>` against
		// `Box<int[]>`): the arguments are paired.
		if actStruct, okAct := tc.types.StructInfo(actual); okAct && actStruct != nil &&
			expStruct.Name == actStruct.Name && expStruct.Decl == actStruct.Decl {
			return tc.fixedArrayInsidePairs(expStruct.TypeArgs, actStruct.TypeArgs, depth)
		}
		return types.NoTypeID, types.NoTypeID, false
	}
	return tc.fixedArrayInsideUnion(expected, actual, depth)
}

func (tc *typeChecker) fixedArrayInsidePairs(exps, acts []types.TypeID, depth int) (exp, act types.TypeID, ok bool) {
	if len(exps) != len(acts) {
		return types.NoTypeID, types.NoTypeID, false
	}
	for i := range exps {
		if e, a, found := tc.fixedArrayInsideDepth(exps[i], acts[i], depth+1); found {
			return e, a, true
		}
	}
	return types.NoTypeID, types.NoTypeID, false
}

// fixedArrayInsideUnion pairs a union's type arguments with the actual's: the
// same generic union (`Option<int[2]>` against `Option<int[]>`), or a tag of
// the expected union (`Some<int[2]>` against `Option<int[]>`).
func (tc *typeChecker) fixedArrayInsideUnion(expected, actual types.TypeID, depth int) (exp, act types.TypeID, ok bool) {
	expUnion, okExp := tc.types.UnionInfo(expected)
	actUnion, okAct := tc.types.UnionInfo(actual)
	if !okExp || expUnion == nil {
		return types.NoTypeID, types.NoTypeID, false
	}
	if !okAct || actUnion == nil {
		// A bare value injected into a tag of the union (`int[2]` given
		// where an `Option<int[]>` is expected lands in `Some`).
		for _, member := range expUnion.Members {
			if member.Kind == types.UnionMemberTag && len(member.TagArgs) == 1 && tc.fixedArrayWhereDynamic(member.TagArgs[0], actual) {
				return tc.resolveAlias(member.TagArgs[0]), actual, true
			}
		}
		return types.NoTypeID, types.NoTypeID, false
	}
	if expUnion.Name == actUnion.Name {
		return tc.fixedArrayInsidePairs(expUnion.TypeArgs, actUnion.TypeArgs, depth)
	}
	actName := tc.lookupName(actUnion.Name)
	for _, member := range expUnion.Members {
		if member.Kind == types.UnionMemberTag && tc.lookupName(member.TagName) == actName {
			return tc.fixedArrayInsidePairs(member.TagArgs, actUnion.TypeArgs, depth)
		}
	}
	return types.NoTypeID, types.NoTypeID, false
}

// reportFixedArrayToDynamic reports a fixed array given where a dynamic array
// is expected, directly or inside a tuple, an array or a union's payload, as
// SEM3015 at span, with the copy that says what the author meant. It reports
// and answers true only for that shape; any other mismatch is left to the
// caller's own report.
func (tc *typeChecker) reportFixedArrayToDynamic(expected, actual types.TypeID, expr ast.ExprID, span source.Span) bool {
	if tc.reporter == nil {
		return false
	}
	exp, act, ok := tc.fixedArrayInside(expected, actual)
	if !ok {
		return false
	}
	if span == (source.Span{}) && expr.IsValid() {
		span = tc.exprSpan(expr)
	}
	direct := exp == tc.resolveAlias(expected) && act == tc.resolveAlias(actual)
	msg := fmt.Sprintf("expected %s, got %s", tc.typeLabel(expected), tc.typeLabel(actual))
	if !direct {
		msg += fmt.Sprintf(": %s is not %s", tc.typeLabel(act), tc.typeLabel(exp))
	}
	b := diag.ReportError(tc.reporter, diag.SemaTypeMismatch, span, msg)
	if b == nil {
		return true
	}
	b.WithHelp(span, fmt.Sprintf("a fixed array %s does not convert to %s implicitly; `.to_array()` copies it into a new %s",
		tc.typeLabel(act), tc.typeLabel(exp), tc.typeLabel(exp)))
	// The copy is offered where the value itself is the fixed array: given
	// directly or injected into a tag, not when it sits inside a tuple or a
	// payload the author has to take apart first.
	if act == tc.resolveAlias(actual) && expr.IsValid() && tc.builder != nil {
		if node := tc.builder.Exprs.Get(expr); node != nil {
			switch node.Kind {
			case ast.ExprIdent, ast.ExprCall, ast.ExprMember, ast.ExprIndex, ast.ExprGroup:
				b.WithFixSuggestion(fix.InsertText("insert `.to_array()`", tc.exprSpan(expr).ZeroideToEnd(), ".to_array()", "",
					fix.WithKind(diag.FixKindRefactorRewrite), fix.WithApplicability(diag.FixApplicabilityManualReview)))
			}
		}
	}
	b.Emit()
	return true
}

// materializeTupleArrayLiterals types the array literals of a tuple literal by
// the tuple elements they land in, so `([6, 7], 1)` written as an
// `(int[], int)` holds an `int[]` it builds directly. It answers applied only
// when some element was an array or tuple literal it retyped; the tuple then
// takes the element types as they now stand.
func (tc *typeChecker) materializeTupleArrayLiterals(expr ast.ExprID, expected types.TypeID) (applied, ok bool) {
	if tc.builder == nil {
		return false, false
	}
	inner := tc.unwrapGroups(expr)
	tuple, isTuple := tc.builder.Exprs.Tuple(inner)
	expInfo, isExpTuple := tc.types.TupleInfo(tc.resolveAlias(expected))
	if !isTuple || tuple == nil || !isExpTuple || len(expInfo.Elems) != len(tuple.Elements) {
		return false, false
	}
	ok = true
	for i, elem := range tuple.Elements {
		if elemApplied, elemOK := tc.materializeArrayLiteral(elem, expInfo.Elems[i]); elemApplied {
			applied = true
			ok = ok && elemOK
		}
	}
	if !applied || !ok {
		return applied, ok
	}
	elems := make([]types.TypeID, len(tuple.Elements))
	for i, elem := range tuple.Elements {
		if elems[i] = tc.result.ExprTypes[elem]; elems[i] == types.NoTypeID {
			return true, false
		}
	}
	tupleType := tc.types.RegisterTuple(elems)
	tc.result.ExprTypes[expr] = tupleType
	tc.result.ExprTypes[inner] = tupleType
	return true, true
}
