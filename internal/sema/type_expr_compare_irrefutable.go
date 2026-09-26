package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/types"
)

// Owner ruling 2026-09-25 (option B): a compare arm covers a union variant only
// when it is UNGUARDED and its pattern is IRREFUTABLE for that variant. A
// pattern is irrefutable against a type exactly when it is one of:
//
//   - `_`;
//   - a named binding (an identifier that is not `_`, `nothing` or a tag);
//   - a tuple pattern of the tuple's arity whose every element is irrefutable
//     against its element type;
//   - a tag pattern (`T`, `T(p1, ..., pn)`, `mod.T(...)`) or `nothing` that
//     names every member of the type's union — so the union has that one
//     member — and whose every payload sub-pattern is irrefutable against the
//     payload type.
//
// Everything else is refutable: literals (numbers, strings, bools), enum
// variants (`Color.Red`), a tag of a union with more than one member, and any
// expression form not listed above (a parenthesised pattern included). A
// variant's arm with a refutable payload therefore does not cover the variant.

// consumeCompareMembers drops the members this arm is certain to catch. Owner
// ruling 2026-09-25: only an IRREFUTABLE, UNGUARDED arm covers its variant. A
// guarded arm consumes nothing, and a tag pattern consumes its member only when
// every payload sub-pattern is irrefutable (see comparePatternIrrefutable).
func (tc *typeChecker) consumeCompareMembers(remaining []types.UnionMember, arm ast.ExprCompareArm) []types.UnionMember {
	if len(remaining) == 0 {
		return remaining
	}
	if !arm.IsFinally && arm.Guard.IsValid() {
		return remaining
	}
	matched := tc.matchedUnionMembers(arm.Pattern, remaining, arm.IsFinally)
	if len(matched) == 0 {
		return remaining
	}
	if !arm.IsFinally {
		matched = tc.irrefutablyMatchedMembers(arm.Pattern, remaining, matched)
		if len(matched) == 0 {
			return remaining
		}
	}
	return tc.dropUnionMembers(remaining, matched)
}

// irrefutablyMatchedMembers keeps the matched members whose payload the arm's
// pattern accepts in full. `matched` indexes `members`.
func (tc *typeChecker) irrefutablyMatchedMembers(pattern ast.ExprID, members []types.UnionMember, matched []int) []int {
	if tc.isWildcardPattern(pattern) || tc.isNamedBindingPattern(pattern) {
		return matched
	}
	args := tc.compareTagPatternArgs(pattern)
	kept := make([]int, 0, len(matched))
	for _, idx := range matched {
		if idx < 0 || idx >= len(members) {
			continue
		}
		if tc.comparePayloadIrrefutable(args, members[idx]) {
			kept = append(kept, idx)
		}
	}
	return kept
}

// compareTagPatternArgs returns the payload sub-patterns of a tag call
// pattern, or nil for a bare tag, `nothing` or anything else.
func (tc *typeChecker) compareTagPatternArgs(pattern ast.ExprID) []ast.CallArg {
	if !pattern.IsValid() || tc.builder == nil {
		return nil
	}
	if call, ok := tc.builder.Exprs.Call(pattern); ok && call != nil {
		return call.Args
	}
	return nil
}

// comparePayloadIrrefutable reports whether every payload sub-pattern is
// irrefutable against the member's payload type at the same position.
func (tc *typeChecker) comparePayloadIrrefutable(args []ast.CallArg, member types.UnionMember) bool {
	for i, arg := range args {
		payload := types.NoTypeID
		if member.Kind == types.UnionMemberTag && i < len(member.TagArgs) {
			payload = member.TagArgs[i]
		}
		if !tc.comparePatternIrrefutable(arg.Value, payload) {
			return false
		}
	}
	return true
}

// comparePatternIrrefutable is the rule quoted at the top of this file.
func (tc *typeChecker) comparePatternIrrefutable(pattern ast.ExprID, ty types.TypeID) bool {
	if !pattern.IsValid() || tc.builder == nil {
		return false
	}
	if tc.isWildcardPattern(pattern) || tc.isNamedBindingPattern(pattern) {
		return true
	}
	if ty == types.NoTypeID || tc.types == nil {
		return false
	}
	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil {
		return false
	}
	subject := tc.compareUnionSubjectType(ty)
	switch expr.Kind {
	case ast.ExprTuple:
		tuple, ok := tc.builder.Exprs.Tuple(pattern)
		if !ok || tuple == nil {
			return false
		}
		info, ok := tc.types.TupleInfo(subject)
		if !ok || info == nil || len(info.Elems) != len(tuple.Elements) {
			return false
		}
		for i, elem := range tuple.Elements {
			if !tc.comparePatternIrrefutable(elem, info.Elems[i]) {
				return false
			}
		}
		return true
	case ast.ExprCall, ast.ExprIdent, ast.ExprMember, ast.ExprLit:
		members := tc.unionMembers(subject)
		if len(members) == 0 {
			return false
		}
		matched := tc.matchedUnionMembers(pattern, members, false)
		if len(matched) != len(members) {
			return false
		}
		return len(tc.irrefutablyMatchedMembers(pattern, members, matched)) == len(members)
	}
	return false
}

// subtractUnionMembers returns the members of `from` that are not in `drop`.
func (tc *typeChecker) subtractUnionMembers(from, drop []types.UnionMember) []types.UnionMember {
	out := make([]types.UnionMember, 0, len(from))
	for _, m := range from {
		found := false
		for _, d := range drop {
			if m.Kind == d.Kind && m.TagName == d.TagName && m.Type == d.Type {
				found = true
				break
			}
		}
		if !found {
			out = append(out, m)
		}
	}
	return out
}

// emitNonExhaustiveGuardedMatch reports variants that some arm names but every
// arm naming them can miss (guard or refutable payload). The help shows the
// irrefutable arm that would close each of them.
func (tc *typeChecker) emitNonExhaustiveGuardedMatch(span source.Span, partial []types.UnionMember) {
	if tc.reporter == nil || len(partial) == 0 {
		return
	}
	names := make([]string, 0, len(partial))
	arms := make([]string, 0, len(partial))
	for _, member := range partial {
		switch member.Kind {
		case types.UnionMemberTag:
			name := tc.lookupName(member.TagName)
			if name == "" {
				continue
			}
			names = append(names, "`"+name+"`")
			if len(member.TagArgs) == 0 {
				arms = append(arms, fmt.Sprintf("`%s => ...`", name))
			} else {
				holes := strings.TrimSuffix(strings.Repeat("_, ", len(member.TagArgs)), ", ")
				arms = append(arms, fmt.Sprintf("`%s(%s) => ...`", name, holes))
			}
		case types.UnionMemberNothing:
			names = append(names, "`nothing`")
			arms = append(arms, "`nothing => ...`")
		}
	}
	if len(names) == 0 {
		return
	}
	message := fmt.Sprintf("non-exhaustive pattern match: some %s values match no arm", strings.Join(names, ", "))
	if b := diag.ReportError(tc.reporter, diag.SemaNonexhaustiveGuardedMatch, span, message); b != nil {
		b.WithNote(span, fmt.Sprintf("every arm for %s has a guard or a payload pattern that can fail, and only an unguarded arm whose pattern cannot fail covers a variant", strings.Join(names, ", ")))
		b.WithHelp(span, fmt.Sprintf("add %s after those arms, or a `finally` arm", strings.Join(arms, ", ")))
		b.Emit()
	}
}
