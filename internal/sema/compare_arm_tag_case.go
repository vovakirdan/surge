package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// A compare arm may only name a tag the matched union actually HAS.
//
//	tag T1<A>(A); tag T2<B>(B); tag Outer1<C>(C);
//	type Inner<A, B> = T1(A) | T2(B);
//	type Outer<A, B, C> = Outer1(C) | Inner<A, B>;
//
//	compare o { Outer1(v) => ...; T1(v) => ...; }   // T1 is Inner's case, not Outer's
//
// A union-typed member of a union is NESTED — the inner union sits whole, with
// its own tag, inside the outer one — so `Outer` has two cases, `Outer1` and
// `Inner`, and `T1` is not one of them. Sema accepted the hoisted `T1` anyway
// and every consumer behind it then refused the same program.
//
// WHICH REFUSAL ARRIVED DEPENDED ON ARM ORDER, and that is the sharpest
// evidence that the front end was the one in the wrong. Measured on the program
// above at b6159bf3, varying nothing but the order of the two arms:
//
//	T1 arm SECOND  -> `Outer1(v)` consumes the tag member, the nested member is
//	                  all that remains, narrowCompareSubjectType collapses the
//	                  subject to `Inner`, and the arm type-checks against the
//	                  INNER union. The VM runs it; LLVM refuses to emit it:
//	                  `missing finalized union case 2 for type#1531`.
//	T1 arm FIRST   -> nothing has been consumed, so the subject is still `Outer`,
//	                  `unionTagPayloadTypes` finds no `T1` case and returns nil,
//	                  and the payload binding is left with no type at all:
//	                  `MIR validation failed: local L5 (v): unknown type`.
//
// So the narrowing was the front end's own half of the flattening the owner
// ruled against, and the untyped binding was the same mistake with nothing left
// to paper over it. The predicate below is therefore asked of the union the
// compare NAMES — `compareUnionSubjectType(valueType)`, before any arm has
// consumed anything — so its answer cannot depend on the arms that precede it.
//
// THE PREDICATE IS NOT "ANY PATTERN THAT IS NOT A CASE". It fires only on a TAG
// pattern, and only when the name really resolves to a tag constructor:
//
//   - a NAMED BINDING is not a tag pattern, so `Erring<T, E>`'s bare `E` member
//     is still caught by `err` and `err.message` still types — that spelling is
//     what makes `E` reachable at all and it must stay legal;
//   - `nothing`, `_` and `finally` are not tag patterns;
//   - a tag matched against ITS OWN single-member union type is legal, which is
//     what a scrutinee already narrowed to one tag looks like;
//   - a subject that is not a union at all is not this rule's business.
//
// It does not descend into a pattern's arguments. A nested `Outer1(Some(x))`
// asks the same question one level down and is not answered here.

// rejectArmTagNotAUnionCase refuses a compare arm whose tag is not a case of
// the union the compare names.
//
// `subject` must be the compare's OWN subject type, not the per-arm narrowed
// one: narrowing is what made the same arm well-typed or untyped depending on
// which arm preceded it, so re-asking it here would inherit that.
func (tc *typeChecker) rejectArmTagNotAUnionCase(arm ast.ExprCompareArm, subject types.TypeID) {
	if arm.IsFinally || tc.types == nil || tc.reporter == nil {
		return
	}
	tagName, span, ok := tc.armPatternTagName(arm.Pattern)
	if !ok {
		return
	}
	normalized := tc.compareUnionSubjectType(subject)
	info, infoOK := tc.types.UnionInfo(normalized)
	if !infoOK || info == nil || len(info.Members) == 0 {
		return
	}
	// The scrutinee is already this very tag's own single-member union.
	if info.Name == tagName {
		return
	}
	if len(tc.matchUnionTagMembers(tagName, info.Members)) > 0 {
		return
	}
	tc.reportArmTagNotAUnionCase(tagName, normalized, info.Members, span)
}

// armPatternTagName names the tag an arm pattern matches, when it matches one.
//
// Only the unqualified spellings are answered. A module-qualified pattern is
// left alone on purpose: isTagConstructor can only see the file scope, so an
// imported tag would look like no tag at all and this rule would refuse a
// legitimate arm.
func (tc *typeChecker) armPatternTagName(pattern ast.ExprID) (source.StringID, source.Span, bool) {
	if !pattern.IsValid() || tc.builder == nil {
		return source.NoStringID, source.Span{}, false
	}
	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil {
		return source.NoStringID, source.Span{}, false
	}
	target := pattern
	if expr.Kind == ast.ExprCall {
		call, ok := tc.builder.Exprs.Call(pattern)
		if !ok || call == nil {
			return source.NoStringID, source.Span{}, false
		}
		target = call.Target
	} else if expr.Kind != ast.ExprIdent {
		return source.NoStringID, source.Span{}, false
	}
	ident, ok := tc.builder.Exprs.Ident(target)
	if !ok || ident == nil || ident.Name == source.NoStringID {
		return source.NoStringID, source.Span{}, false
	}
	switch tc.lookupName(ident.Name) {
	case "", "_", "nothing":
		return source.NoStringID, source.Span{}, false
	}
	if !tc.isTagConstructor(ident.Name) {
		return source.NoStringID, source.Span{}, false
	}
	return ident.Name, expr.Span, true
}

// reportArmTagNotAUnionCase names the union, the tag, and the cases that do
// exist, because the cases are the whole answer to "then what should I write".
func (tc *typeChecker) reportArmTagNotAUnionCase(
	tagName source.StringID,
	union types.TypeID,
	members []types.UnionMember,
	span source.Span,
) {
	tag := tc.lookupName(tagName)
	unionLabel := tc.typeLabel(union)
	message := fmt.Sprintf("%s is not a case of %s", tag, unionLabel)
	b := diag.ReportError(tc.reporter, diag.SemaArmTagNotAUnionCase, span, message)
	if b == nil {
		return
	}
	if cases := tc.unionCaseNames(members); len(cases) > 0 {
		b.WithNote(span, fmt.Sprintf("%s has %s", unionLabel, strings.Join(cases, ", ")))
	}
	b.Emit()
}

// unionCaseNames spells a union's cases the way an arm would have to write
// them. A union-typed member is named by its TYPE, because that member is a
// nested union matched whole and not by any tag inside it.
func (tc *typeChecker) unionCaseNames(members []types.UnionMember) []string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		switch member.Kind {
		case types.UnionMemberTag:
			if name := tc.lookupName(member.TagName); name != "" {
				names = append(names, name)
			}
		case types.UnionMemberType:
			names = append(names, tc.typeLabel(member.Type))
		case types.UnionMemberNothing:
			names = append(names, "nothing")
		}
	}
	return names
}
