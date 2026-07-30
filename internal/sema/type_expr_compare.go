package sema

import (
	"fmt"
	"strings"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// inferComparePatternTypes type-checks pattern (an arm's compare
// pattern) against subject, and appends the SymbolID of every
// identifier binding it introduces to *bound — the caller uses this to
// forbid the guard from moving them (see pushCompareGuardBindings).
// bound may be nil when the caller has no such need.
func (tc *typeChecker) inferComparePatternTypes(pattern ast.ExprID, subject types.TypeID, bound *[]symbols.SymbolID) {
	if !pattern.IsValid() || tc.builder == nil {
		return
	}
	subjectValue := subject
	if subjectValue != types.NoTypeID {
		subjectValue = tc.resolveAlias(tc.stripOwnType(subjectValue))
		literalExpected := subjectValue
		if tc.types != nil {
			if tt, ok := tc.types.Lookup(subjectValue); ok && tt.Kind == types.KindReference {
				literalExpected = tt.Elem
			}
		}
		if applied, _ := tc.materializeNumericLiteral(pattern, literalExpected); applied {
			return
		}
	}
	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil {
		return
	}
	switch expr.Kind {
	case ast.ExprIdent:
		symID := tc.symbolForExpr(pattern)
		tc.setBindingType(symID, subject)
		if bound != nil && symID.IsValid() {
			*bound = append(*bound, symID)
		}
	case ast.ExprMember:
		if member, ok := tc.builder.Exprs.Member(pattern); ok && member != nil {
			if enumType := tc.enumTypeForExpr(member.Target); enumType != types.NoTypeID {
				ty := tc.typeOfEnumVariant(enumType, member.Field, expr.Span)
				if ty != types.NoTypeID {
					tc.result.ExprTypes[pattern] = ty
				}
			}
		}
	case ast.ExprCall:
		call, _ := tc.builder.Exprs.Call(pattern)
		if call == nil {
			return
		}
		tagName := source.NoStringID
		if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
			tagName = ident.Name
		} else if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
			if tc.moduleSymbolForExpr(member.Target) != nil {
				tagName = member.Field
			}
		}
		argTypes := tc.unionTagPayloadTypes(subjectValue, tagName)
		for i, arg := range call.Args {
			argType := types.NoTypeID
			if i < len(argTypes) {
				argType = argTypes[i]
			}
			tc.inferComparePatternTypes(arg.Value, argType, bound)
		}
	case ast.ExprTuple:
		tuple, _ := tc.builder.Exprs.Tuple(pattern)
		if tuple == nil {
			return
		}
		var elemTypes []types.TypeID
		if subjectValue != types.NoTypeID && tc.types != nil {
			if info, ok := tc.types.TupleInfo(subjectValue); ok && info != nil {
				elemTypes = info.Elems
			}
		}
		for i, elem := range tuple.Elements {
			elemType := types.NoTypeID
			if i < len(elemTypes) {
				elemType = elemTypes[i]
			}
			tc.inferComparePatternTypes(elem, elemType, bound)
		}
	}
}

// registerComparePayloadDroppables gives an arm's pattern bindings the
// scope-exit obligation every other binding has.
//
// A payload extracted into a binding is that binding's to release: for an OWNED
// scrutinee the payload's reference moves out of the envelope, which is then
// freed shallowly on exactly that premise, and for a BORROWED one the extraction
// retains so the binding holds a reference of its own. Either way one release is
// owed, and nothing owed it — only the `let` walk reaches
// registerDroppableBinding, so a heap payload bound and used locally inside its
// arm was simply abandoned.
//
// A binding the arm moves ONWARD is skipped later by liveDroppables, which is
// why the ubiquitous `Success(x) => x` shape hid this for so long: it moves, so
// it never needed the obligation that was missing.
func (tc *typeChecker) registerComparePayloadDroppables(bindings []symbols.SymbolID) {
	for _, symID := range bindings {
		tc.registerDroppableBinding(symID)
	}
}

// releaseArmResultObligations takes back the obligation an arm earned for a
// payload binding it RETURNED, once the compare's own value turns out to be
// consumed.
//
// Whether an arm handed its payload onward is not knowable while the arm is
// being typed. `let out = compare v { Payload(s) => s; ... }` consumes the
// result and the binding must NOT also drop it; `peek(compare v { ... })` with
// a `&string` parameter only borrows it, and then the binding's drop is the
// only thing that reclaims it; a discarded compare is the same. The arm is
// typed before any of those are known.
//
// So the arm keeps the obligation by default and the consuming context takes
// it away here — the leak-over-double-free direction while the answer is
// unknown, which is the direction the rest of this file errs in too. This runs
// from observeMove, which is exactly the set of positions that consume a
// value.
//
// Nested compares recurse: an arm whose result is itself a compare hands ITS
// arms' results onward by the same argument. An arm whose result is a BLOCK
// needs nothing here — a block's tail expression already observes its own move
// while the arm is being typed, so the binding never earned the obligation.
func (tc *typeChecker) releaseArmResultObligations(expr ast.ExprID) {
	if tc.builder == nil || tc.result == nil {
		return
	}
	cmp, ok := tc.builder.Exprs.Compare(tc.unwrapGroups(expr))
	if !ok || cmp == nil {
		return
	}

	// The arms are branches of one choice, exactly as a ternary's two are: one
	// runs and hands its result onward. An arm returning a binding declared
	// OUTSIDE the compare needs that whole treatment — the move, so the
	// binding's scope-exit drop stands down, and a drop on every sibling arm,
	// which still holds it.
	results := make([]ast.ExprID, 0, len(cmp.Arms))
	for _, arm := range cmp.Arms {
		if !arm.Result.IsValid() || tc.compareArmAbruptExit(arm.Result) {
			// An arm that exits abruptly hands nothing to the compare; its own
			// `return` already accounted for what it moved.
			continue
		}
		results = append(results, arm.Result)
	}
	tc.consumeBranchResults(results)

	if len(tc.result.ArmDropsExpr) == 0 {
		return
	}
	for _, arm := range cmp.Arms {
		if !arm.Result.IsValid() {
			continue
		}
		symID := tc.symbolForExpr(arm.Result)
		if !symID.IsValid() {
			continue
		}
		drops := tc.result.ArmDropsExpr[arm.Result]
		kept := drops[:0:0]
		for _, owed := range drops {
			if owed != symID {
				kept = append(kept, owed)
			}
		}
		if len(kept) == 0 {
			delete(tc.result.ArmDropsExpr, arm.Result)
			continue
		}
		tc.result.ArmDropsExpr[arm.Result] = kept
	}
}

func (tc *typeChecker) unionTagPayloadTypes(subject types.TypeID, tag source.StringID) []types.TypeID {
	if tag == source.NoStringID || tc.types == nil {
		return nil
	}
	normalized := tc.resolveAlias(tc.stripOwnType(subject))
	isRef := false
	refMut := false
	if tt, ok := tc.types.Lookup(normalized); ok && tt.Kind == types.KindReference {
		isRef = true
		refMut = tt.Mutable
		normalized = tc.resolveAlias(tc.stripOwnType(tt.Elem))
	}
	info, ok := tc.types.UnionInfo(normalized)
	if !ok || info == nil {
		return nil
	}
	for _, member := range info.Members {
		if member.Kind != types.UnionMemberTag {
			continue
		}
		if member.TagName == tag {
			if !isRef {
				return member.TagArgs
			}
			payload := make([]types.TypeID, len(member.TagArgs))
			for i, arg := range member.TagArgs {
				payload[i] = arg
				if arg == types.NoTypeID {
					continue
				}
				resolved := tc.resolveAlias(arg)
				if tt, ok := tc.types.Lookup(resolved); ok && tt.Kind == types.KindReference {
					continue
				}
				payload[i] = tc.types.Intern(types.MakeReference(arg, refMut))
			}
			return payload
		}
	}
	return nil
}

// checkCompareExhausiveness validates that all variants of unions are covered
func (tc *typeChecker) checkCompareExhausiveness(cmp *ast.ExprCompareData, subjectType types.TypeID, span source.Span) {
	if cmp == nil || tc.types == nil {
		return
	}

	// Get union info - skip non-unions
	normalized := tc.compareUnionSubjectType(subjectType)
	unionInfo, ok := tc.types.UnionInfo(normalized)
	if !ok || unionInfo == nil || len(unionInfo.Members) == 0 {
		return
	}

	// Track remaining members through all arms
	remaining := tc.unionMembers(subjectType)
	hasFinally := false

	for _, arm := range cmp.Arms {
		if arm.IsFinally {
			hasFinally = true
			remaining = nil
			break
		}
		remaining = tc.consumeCompareMembers(remaining, arm)
	}

	// Check for non-exhaustive match
	if len(remaining) > 0 && !hasFinally {
		tc.emitNonExhaustiveMatchForMembers(span, remaining)
	}

	// Check for redundant finally (all members already matched before finally)
	if hasFinally {
		remainingWithoutFinally := tc.unionMembers(subjectType)
		for _, arm := range cmp.Arms {
			if arm.IsFinally {
				break
			}
			remainingWithoutFinally = tc.consumeCompareMembers(remainingWithoutFinally, arm)
		}
		if len(remainingWithoutFinally) == 0 {
			tc.emitRedundantFinally(span)
		}
	}
}

// isWildcardPattern checks if the pattern is a wildcard that matches everything
func (tc *typeChecker) isWildcardPattern(pattern ast.ExprID) bool {
	if !pattern.IsValid() || tc.builder == nil {
		return false
	}

	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil {
		return false
	}

	// Check for wildcard identifier '_'
	if expr.Kind == ast.ExprIdent {
		if ident, ok := tc.builder.Exprs.Ident(pattern); ok && ident != nil {
			wildcard := tc.lookupName(ident.Name)
			return wildcard == "_"
		}
	}

	return false
}

// isTagConstructor checks if the given name resolves to a tag constructor
func (tc *typeChecker) isTagConstructor(name source.StringID) bool {
	if tc.symbols == nil || tc.symbols.Table == nil || name == source.NoStringID {
		return false
	}

	// Look up the symbol in the current scope and file scope
	if tc.symbols.ExprSymbols != nil {
		// Try to find a symbol with this name that is a tag
		for _, symID := range tc.symbols.Table.Scopes.Get(tc.symbols.FileScope).NameIndex[name] {
			if sym := tc.symbols.Table.Symbols.Get(symID); sym != nil && sym.Kind == symbols.SymbolTag {
				return true
			}
		}
	}

	return false
}

// isNamedBindingPattern checks if the pattern is a named binding (catches all remaining members)
func (tc *typeChecker) isNamedBindingPattern(pattern ast.ExprID) bool {
	if !pattern.IsValid() || tc.builder == nil {
		return false
	}

	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil || expr.Kind != ast.ExprIdent {
		return false
	}

	ident, ok := tc.builder.Exprs.Ident(pattern)
	if !ok || ident == nil {
		return false
	}

	name := tc.lookupName(ident.Name)
	if name == "_" {
		return false // wildcard handled separately
	}
	if name == "nothing" {
		return false // nothing literal handled separately
	}

	return !tc.isTagConstructor(ident.Name)
}

func (tc *typeChecker) compareArmAbruptExit(result ast.ExprID) bool {
	if !result.IsValid() || tc.builder == nil {
		return false
	}
	expr := tc.builder.Exprs.Get(result)
	if expr == nil {
		return false
	}
	if expr.Kind != ast.ExprBlock {
		return tc.exprAbruptExit(result)
	}
	block, ok := tc.builder.Exprs.Block(result)
	if !ok || block == nil {
		return false
	}
	return tc.blockAbruptStatus(block.Stmts) == returnClosed
}

func (tc *typeChecker) compareExprAbruptExit(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	expr = tc.unwrapGroupExpr(expr)
	cmp, ok := tc.builder.Exprs.Compare(expr)
	if !ok || cmp == nil || len(cmp.Arms) == 0 {
		return false
	}
	for _, arm := range cmp.Arms {
		if !tc.compareArmAbruptExit(arm.Result) {
			return false
		}
	}
	return tc.compareAlwaysMatches(cmp)
}

func (tc *typeChecker) compareAlwaysMatches(cmp *ast.ExprCompareData) bool {
	if cmp == nil || len(cmp.Arms) == 0 {
		return false
	}
	for _, arm := range cmp.Arms {
		if arm.Guard.IsValid() {
			continue
		}
		if arm.IsFinally || tc.isWildcardPattern(arm.Pattern) || tc.isNamedBindingPattern(arm.Pattern) {
			return true
		}
	}
	subjectType, ok := tc.compareSubjectType(cmp)
	if !ok {
		return false
	}
	if tc.isBoolType(subjectType) {
		return tc.compareBoolAlwaysMatches(cmp)
	}
	remaining := tc.unionMembers(subjectType)
	if len(remaining) == 0 {
		return false
	}
	for _, arm := range cmp.Arms {
		if arm.Guard.IsValid() {
			continue
		}
		remaining = tc.consumeCompareMembers(remaining, arm)
		if len(remaining) == 0 {
			return true
		}
	}
	return false
}

func (tc *typeChecker) compareSubjectType(cmp *ast.ExprCompareData) (types.TypeID, bool) {
	if tc == nil || tc.types == nil || cmp == nil || !cmp.Value.IsValid() {
		return types.NoTypeID, false
	}
	subjectType := tc.result.ExprTypes[cmp.Value]
	if subjectType == types.NoTypeID {
		return types.NoTypeID, false
	}
	return tc.compareUnionSubjectType(subjectType), true
}

func (tc *typeChecker) isBoolType(typ types.TypeID) bool {
	if tc == nil || tc.types == nil || typ == types.NoTypeID {
		return false
	}
	return tc.resolveAlias(tc.stripOwnType(typ)) == tc.types.Builtins().Bool
}

func (tc *typeChecker) compareBoolAlwaysMatches(cmp *ast.ExprCompareData) bool {
	if cmp == nil {
		return false
	}
	matchedTrue := false
	matchedFalse := false
	for _, arm := range cmp.Arms {
		if arm.Guard.IsValid() || !arm.Pattern.IsValid() || tc.builder == nil {
			continue
		}
		pattern := tc.unwrapGroupExpr(arm.Pattern)
		node := tc.builder.Exprs.Get(pattern)
		if node == nil || node.Kind != ast.ExprLit {
			continue
		}
		lit, ok := tc.builder.Exprs.Literal(pattern)
		if !ok || lit == nil {
			continue
		}
		switch lit.Kind {
		case ast.ExprLitTrue:
			matchedTrue = true
		case ast.ExprLitFalse:
			matchedFalse = true
		}
	}
	return matchedTrue && matchedFalse
}

func (tc *typeChecker) exprAbruptExit(expr ast.ExprID) bool {
	if !expr.IsValid() || tc.builder == nil {
		return false
	}
	expr = tc.unwrapGroupExpr(expr)
	if tc.compareExprAbruptExit(expr) {
		return true
	}
	call, ok := tc.builder.Exprs.Call(expr)
	if !ok || call == nil {
		return false
	}
	return tc.isAbruptCallTarget(call.Target)
}

func (tc *typeChecker) isAbruptCallTarget(target ast.ExprID) bool {
	if !target.IsValid() || tc.builder == nil {
		return false
	}
	target = tc.unwrapGroupExpr(target)
	if ident, ok := tc.builder.Exprs.Ident(target); ok && ident != nil {
		name := tc.lookupName(ident.Name)
		return name == "panic" || name == "exit"
	}
	if member, ok := tc.builder.Exprs.Member(target); ok && member != nil && tc.moduleSymbolForExpr(member.Target) != nil {
		name := tc.lookupName(member.Field)
		return name == "panic" || name == "exit"
	}
	return false
}

// emitNonExhaustiveMatchForMembers reports a diagnostic for uncovered union members (tags, types, or nothing)
func (tc *typeChecker) emitNonExhaustiveMatchForMembers(span source.Span, missing []types.UnionMember) {
	if tc.reporter == nil || len(missing) == 0 {
		return
	}

	var parts []string
	for _, member := range missing {
		switch member.Kind {
		case types.UnionMemberTag:
			tagName := tc.lookupName(member.TagName)
			if tagName != "" {
				parts = append(parts, tagName)
			}
		case types.UnionMemberType:
			typeName := tc.typeLabel(member.Type)
			parts = append(parts, fmt.Sprintf("type %s", typeName))
		case types.UnionMemberNothing:
			parts = append(parts, "nothing")
		}
	}

	if len(parts) == 0 {
		return
	}

	message := fmt.Sprintf("non-exhaustive pattern match: missing patterns for %s", strings.Join(parts, ", "))

	if b := diag.ReportError(tc.reporter, diag.SemaNonexhaustiveMatch, span, message); b != nil {
		b.WithNote(span, "consider adding patterns for the missing variants or a 'finally' clause")
		b.Emit()
	}
}

// emitRedundantFinally reports a diagnostic for unnecessary finally clause
func (tc *typeChecker) emitRedundantFinally(span source.Span) {
	if tc.reporter == nil {
		return
	}

	message := "redundant 'finally' clause: all variants are already covered"

	if b := diag.ReportWarning(tc.reporter, diag.SemaRedundantFinally, span, message); b != nil {
		b.WithNote(span, "consider removing the 'finally' clause")
		b.Emit()
	}
}

// unionMembers returns a copy of union members for the given type (if any).
func (tc *typeChecker) unionMembers(subject types.TypeID) []types.UnionMember {
	if tc.types == nil {
		return nil
	}
	normalized := tc.compareUnionSubjectType(subject)
	info, ok := tc.types.UnionInfo(normalized)
	if !ok || info == nil || len(info.Members) == 0 {
		return nil
	}
	members := make([]types.UnionMember, len(info.Members))
	copy(members, info.Members)
	return members
}

func (tc *typeChecker) compareUnionSubjectType(subject types.TypeID) types.TypeID {
	if tc.types == nil || subject == types.NoTypeID {
		return subject
	}
	normalized := tc.resolveAlias(tc.stripOwnType(subject))
	if tt, ok := tc.types.Lookup(normalized); ok && tt.Kind == types.KindReference {
		normalized = tc.resolveAlias(tt.Elem)
	}
	return normalized
}

// narrowCompareSubjectType chooses a more specific subject type for the current arm.
func (tc *typeChecker) narrowCompareSubjectType(fallback types.TypeID, remaining []types.UnionMember) types.TypeID {
	if narrowed := tc.narrowUnionMembers(remaining); narrowed != types.NoTypeID {
		return narrowed
	}
	return fallback
}

// narrowUnionMembers collapses a single-member union to its payload type.
func (tc *typeChecker) narrowUnionMembers(members []types.UnionMember) types.TypeID {
	if len(members) != 1 || tc.types == nil {
		return types.NoTypeID
	}
	member := members[0]
	switch member.Kind {
	case types.UnionMemberType:
		return member.Type
	case types.UnionMemberNothing:
		return tc.types.Builtins().Nothing
	default:
		return types.NoTypeID
	}
}

func (tc *typeChecker) consumeCompareMembers(remaining []types.UnionMember, arm ast.ExprCompareArm) []types.UnionMember {
	if len(remaining) == 0 {
		return remaining
	}
	matched := tc.matchedUnionMembers(arm.Pattern, remaining, arm.IsFinally)
	if len(matched) == 0 {
		return remaining
	}
	return tc.dropUnionMembers(remaining, matched)
}

func (tc *typeChecker) matchedUnionMembers(pattern ast.ExprID, members []types.UnionMember, isFinally bool) []int {
	if len(members) == 0 {
		return nil
	}
	// Wildcards, finally, and named bindings all match everything remaining
	if isFinally || tc.isWildcardPattern(pattern) || tc.isNamedBindingPattern(pattern) {
		indexes := make([]int, 0, len(members))
		for i := range members {
			indexes = append(indexes, i)
		}
		return indexes
	}
	if !pattern.IsValid() || tc.builder == nil {
		return nil
	}
	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil {
		return nil
	}
	switch expr.Kind {
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(pattern); ok && call != nil {
			if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
				if idxs := tc.matchUnionTagMembers(ident.Name, members); len(idxs) > 0 {
					return idxs
				}
			} else if member, ok := tc.builder.Exprs.Member(call.Target); ok && member != nil {
				if tc.moduleSymbolForExpr(member.Target) != nil {
					if idxs := tc.matchUnionTagMembers(member.Field, members); len(idxs) > 0 {
						return idxs
					}
				}
			}
		}
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(pattern); ok && ident != nil {
			// Check for "nothing" identifier (matches nothing member)
			if tc.lookupName(ident.Name) == "nothing" {
				return tc.matchUnionNothingMembers(members)
			}
			if idxs := tc.matchUnionTagMembers(ident.Name, members); len(idxs) > 0 {
				return idxs
			}
		}
	case ast.ExprMember:
		if member, ok := tc.builder.Exprs.Member(pattern); ok && member != nil {
			if tc.moduleSymbolForExpr(member.Target) != nil {
				if idxs := tc.matchUnionTagMembers(member.Field, members); len(idxs) > 0 {
					return idxs
				}
			}
		}
	case ast.ExprLit:
		if lit, ok := tc.builder.Exprs.Literal(pattern); ok && lit != nil && lit.Kind == ast.ExprLitNothing {
			return tc.matchUnionNothingMembers(members)
		}
	}
	return nil
}

func (tc *typeChecker) matchUnionTagMembers(tag source.StringID, members []types.UnionMember) []int {
	if tag == source.NoStringID {
		return nil
	}
	indexes := make([]int, 0, len(members))
	for i, member := range members {
		if member.Kind == types.UnionMemberTag && member.TagName == tag {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (tc *typeChecker) matchUnionNothingMembers(members []types.UnionMember) []int {
	indexes := make([]int, 0, len(members))
	for i, member := range members {
		if member.Kind == types.UnionMemberNothing {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func (tc *typeChecker) dropUnionMembers(members []types.UnionMember, matched []int) []types.UnionMember {
	if len(members) == 0 || len(matched) == 0 {
		return members
	}
	drop := make(map[int]struct{}, len(matched))
	for _, idx := range matched {
		drop[idx] = struct{}{}
	}
	filtered := make([]types.UnionMember, 0, len(members)-len(drop))
	for i, member := range members {
		if _, ok := drop[i]; ok {
			continue
		}
		filtered = append(filtered, member)
	}
	return filtered
}
