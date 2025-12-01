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

func (tc *typeChecker) inferComparePatternTypes(pattern ast.ExprID, subject types.TypeID) {
	if !pattern.IsValid() || tc.builder == nil {
		return
	}
	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil {
		return
	}
	switch expr.Kind {
	case ast.ExprIdent:
		symID := tc.symbolForExpr(pattern)
		tc.setBindingType(symID, subject)
	case ast.ExprCall:
		call, _ := tc.builder.Exprs.Call(pattern)
		if call == nil {
			return
		}
		tagName := source.NoStringID
		if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
			tagName = ident.Name
		}
		argTypes := tc.unionTagPayloadTypes(subject, tagName)
		for i, arg := range call.Args {
			argType := types.NoTypeID
			if i < len(argTypes) {
				argType = argTypes[i]
			}
			tc.inferComparePatternTypes(arg, argType)
		}
	case ast.ExprTuple:
		tuple, _ := tc.builder.Exprs.Tuple(pattern)
		if tuple == nil {
			return
		}
		for _, elem := range tuple.Elements {
			tc.inferComparePatternTypes(elem, types.NoTypeID)
		}
	}
}

func (tc *typeChecker) unionTagPayloadTypes(subject types.TypeID, tag source.StringID) []types.TypeID {
	if tag == source.NoStringID || tc.types == nil {
		return nil
	}
	normalized := tc.resolveAlias(subject)
	info, ok := tc.types.UnionInfo(normalized)
	if !ok || info == nil {
		return nil
	}
	for _, member := range info.Members {
		if member.Kind != types.UnionMemberTag {
			continue
		}
		if member.TagName == tag {
			return member.TagArgs
		}
	}
	return nil
}

// checkCompareExhausiveness validates that all variants of tagged unions are covered
func (tc *typeChecker) checkCompareExhausiveness(cmp *ast.ExprCompareData, subjectType types.TypeID, span source.Span) {
	if cmp == nil || tc.types == nil {
		return
	}

	// Enable exhaustiveness checking for Erring<T, E>
	// The new model has only one generic tag Success<T>, so exhaustiveness should work properly

	// Only check exhaustiveness for tagged unions
	normalized := tc.resolveAlias(subjectType)
	unionInfo, ok := tc.types.UnionInfo(normalized)
	if !ok || unionInfo == nil {
		return
	}

	// Collect all tagged variants from the union
	allTags := make(map[source.StringID]bool)
	hasOnlyTags := true
	for _, member := range unionInfo.Members {
		if member.Kind == types.UnionMemberTag {
			allTags[member.TagName] = true
		} else {
			hasOnlyTags = false
		}
	}

	// Only check exhaustiveness for unions that contain only tags
	if !hasOnlyTags || len(allTags) == 0 {
		return
	}

	// Collect covered patterns and check for finally/wildcard
	coveredTags := make(map[source.StringID]bool)
	hasFinally := false
	hasWildcard := false

	for _, arm := range cmp.Arms {
		if arm.IsFinally {
			hasFinally = true
			continue
		}

		if tc.isWildcardPattern(arm.Pattern) {
			hasWildcard = true
			continue
		}

		if tagName := tc.extractTagPattern(arm.Pattern); tagName != source.NoStringID {
			coveredTags[tagName] = true
		}
	}

	// Find missing tags
	var missingTags []source.StringID
	for tagName := range allTags {
		if !coveredTags[tagName] {
			missingTags = append(missingTags, tagName)
		}
	}

	// Debug: log what we found
	if tc.reporter != nil && len(allTags) > 0 {
		var allTagNames, coveredTagNames []string
		for tag := range allTags {
			allTagNames = append(allTagNames, tc.lookupName(tag))
		}
		for tag := range coveredTags {
			coveredTagNames = append(coveredTagNames, tc.lookupName(tag))
		}
		// This debug info would go in logs if we had them
		_ = allTagNames
		_ = coveredTagNames
	}

	// Check for non-exhaustive match
	if len(missingTags) > 0 && !hasFinally && !hasWildcard {
		tc.emitNonExhaustiveMatch(span, missingTags)
	}

	// Check for redundant finally or wildcard
	if len(missingTags) == 0 && hasFinally {
		tc.emitRedundantFinally(span)
	}
}

// extractTagPattern attempts to extract a tag name from a pattern expression
func (tc *typeChecker) extractTagPattern(pattern ast.ExprID) source.StringID {
	if !pattern.IsValid() || tc.builder == nil {
		return source.NoStringID
	}

	expr := tc.builder.Exprs.Get(pattern)
	if expr == nil {
		return source.NoStringID
	}

	switch expr.Kind {
	case ast.ExprIdent:
		if ident, ok := tc.builder.Exprs.Ident(pattern); ok && ident != nil {
			// Check if this identifier resolves to a tag constructor
			if tc.isTagConstructor(ident.Name) {
				return ident.Name
			}
		}
	case ast.ExprCall:
		if call, ok := tc.builder.Exprs.Call(pattern); ok && call != nil {
			if ident, ok := tc.builder.Exprs.Ident(call.Target); ok && ident != nil {
				// This handles patterns like Ok(v), Err(_), etc.
				return ident.Name
			}
		}
	}

	return source.NoStringID
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

	// For now, also accept any valid name as potentially being a tag
	// This is a fallback for cases where symbol resolution isn't complete
	return true
}

// emitNonExhaustiveMatch reports a diagnostic for missing patterns
func (tc *typeChecker) emitNonExhaustiveMatch(span source.Span, missingTags []source.StringID) {
	if tc.reporter == nil || len(missingTags) == 0 {
		return
	}

	tagNames := make([]string, 0, len(missingTags))
	for _, tag := range missingTags {
		tagNames = append(tagNames, tc.lookupName(tag))
	}

	message := fmt.Sprintf("non-exhaustive pattern match: missing patterns for %s", strings.Join(tagNames, ", "))

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
