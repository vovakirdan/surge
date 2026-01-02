package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ImplicitConversionKind distinguishes different types of implicit conversions.
type ImplicitConversionKind int

const (
	// ImplicitConversionTo represents a __to method call (e.g., int to string)
	ImplicitConversionTo ImplicitConversionKind = iota
	// ImplicitConversionSome represents wrapping in Some() for Option<T>
	ImplicitConversionSome
	// ImplicitConversionSuccess represents wrapping in Success() for Erring<T, E>
	ImplicitConversionSuccess
	// ImplicitConversionTagUnion represents upcasting a tag type to its containing union.
	ImplicitConversionTagUnion
)

// ImplicitConversion records an implicit conversion for an expression.
// This information is used by later compilation phases (such as HIR lowering
// and codegen) to emit the actual conversion code.
type ImplicitConversion struct {
	Kind   ImplicitConversionKind // Type of conversion
	Source types.TypeID           // Original type T
	Target types.TypeID           // Target type U (Option<T> or Erring<T, E>)
	Span   source.Span            // Location of the expression
}

// tryImplicitConversion attempts to find a __to conversion from source to target.
// It returns:
//   - (target, true, false) if exactly one __to(source -> target) exists
//   - (NoTypeID, false, true) if multiple candidates exist (ambiguous)
//   - (NoTypeID, false, false) if no candidate found
//
// This function only attempts implicit conversion if the types are not already
// assignable. It does NOT chain conversions (T -> X -> U is not allowed).
func (tc *typeChecker) tryImplicitConversion(src, target types.TypeID) (types.TypeID, bool, bool) {
	if src == types.NoTypeID || target == types.NoTypeID {
		return types.NoTypeID, false, false
	}

	// Fast path: already assignable (don't use conversion)
	// This ensures we don't apply conversion when types already match
	if tc.typesAssignable(target, src, true) {
		return target, false, false // found=false because no conversion needed
	}
	if tc.types != nil {
		if tc.valueType(src) == tc.valueType(target) {
			// Avoid implicit conversions that only adjust ownership/reference wrappers.
			return types.NoTypeID, false, false
		}
	}
	if tc.isNumericType(src) && tc.isNumericType(target) {
		return types.NoTypeID, false, false
	}

	candidates := tc.collectToMethods(src, target)
	switch len(candidates) {
	case 0:
		return types.NoTypeID, false, false
	case 1:
		return target, true, false
	default:
		return types.NoTypeID, false, true // ambiguous
	}
}

// collectToMethods collects all __to methods for (source -> target) pair.
// It looks up __to functions with signature: fn __to(self: source, _: target) -> target
// This includes both @intrinsic and user-defined __to functions from the current
// module and all visible imports.
func (tc *typeChecker) collectToMethods(src, target types.TypeID) []*symbols.FunctionSignature {
	var results []*symbols.FunctionSignature
	seen := make(map[*symbols.FunctionSignature]struct{})
	targetCandidates := tc.filterTargetCandidates(target, tc.typeKeyCandidates(target))

	for _, sc := range tc.typeKeyCandidates(src) {
		if sc.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(sc.key, "__to")
		for _, sig := range methods {
			if sig == nil || len(sig.Params) < 2 {
				continue
			}
			// Check if second parameter matches target type AND result matches target type
			// __to signature must be: fn __to(self: source, _: target) -> target
			for _, tgt := range targetCandidates {
				if tgt.key != "" && typeKeyEqual(sig.Params[1], tgt.key) && typeKeyEqual(sig.Result, tgt.key) {
					// Deduplicate: only add each signature once
					if _, dup := seen[sig]; !dup {
						seen[sig] = struct{}{}
						results = append(results, sig)
					}
					break // Found a match for this sig, no need to check other target candidates
				}
			}
		}
	}
	return results
}

// filterTargetCandidates removes family fallbacks (e.g., uint8 -> uint) so that
// implicit conversions only match the exact requested target type.
func (tc *typeChecker) filterTargetCandidates(target types.TypeID, candidates []typeKeyCandidate) []typeKeyCandidate {
	if len(candidates) == 0 {
		return candidates
	}
	filtered := make([]typeKeyCandidate, 0, len(candidates))
	for _, cand := range candidates {
		if cand.key == "" {
			continue
		}
		baseKey := tc.typeKeyForType(cand.base)
		if family := tc.familyKeyForType(cand.base); family != "" && cand.key == family && baseKey != "" && baseKey != cand.key {
			continue
		}
		filtered = append(filtered, cand)
	}
	if target != types.NoTypeID && tc.types != nil {
		if tt, ok := tc.types.Lookup(target); ok && (tt.Kind == types.KindOwn || tt.Kind == types.KindReference || tt.Kind == types.KindPointer) {
			// Don't match base-type __to methods when target is a wrapper (own/&/*).
			wrapped := filtered[:0]
			for _, cand := range filtered {
				if cand.base == target {
					wrapped = append(wrapped, cand)
				}
			}
			filtered = wrapped
		}
	}
	return filtered
}

// tryTagInjection attempts implicit tag wrapping for Option<T> and Erring<T, E> types.
// This enables syntax like: let x: int? = 1; (instead of let x: int? = Some(1);)
//
// For Option<T>:
//   - If expected is Option<T> and actual is T, return (target, ImplicitConversionSome, true)
//
// For Erring<T, E>:
//   - If expected is Erring<T, E> and actual is T, return (target, ImplicitConversionSuccess, true)
//   - If expected is Erring<T, E> and actual is E, wrapping to Error is NOT implicit
//     (we only wrap success values, not errors, to avoid ambiguity)
//
// Returns (NoTypeID, 0, false) if no tag injection is possible.
func (tc *typeChecker) tryTagInjection(src, target types.TypeID) (types.TypeID, ImplicitConversionKind, bool) {
	if src == types.NoTypeID || target == types.NoTypeID {
		return types.NoTypeID, 0, false
	}

	// Try Option<T>: if target is Option<T> and src is assignable to T
	if payload, ok := tc.optionPayload(target); ok {
		if tc.typesAssignable(payload, src, true) {
			return target, ImplicitConversionSome, true
		}
	}

	// Try Erring<T, E>: if target is Erring<T, E> and src is assignable to T
	// Note: we do NOT implicitly wrap errors (E) - only success values (T)
	if okType, _, ok := tc.resultPayload(target); ok {
		if tc.typesAssignable(okType, src, true) {
			return target, ImplicitConversionSuccess, true
		}
	}

	return types.NoTypeID, 0, false
}

// recordImplicitConversion records an implicit __to conversion for codegen.
// This stores the conversion in Result.ImplicitConversions so that later
// phases can emit the actual __to function call.
func (tc *typeChecker) recordImplicitConversion(expr ast.ExprID, src, target types.TypeID) {
	tc.recordImplicitConversionWithKind(expr, src, target, ImplicitConversionTo)
}

// recordImplicitConversionWithKind records an implicit conversion of any kind.
func (tc *typeChecker) recordImplicitConversionWithKind(expr ast.ExprID, src, target types.TypeID, kind ImplicitConversionKind) {
	if !expr.IsValid() || src == types.NoTypeID || target == types.NoTypeID {
		return
	}
	if tc.result.ImplicitConversions == nil {
		tc.result.ImplicitConversions = make(map[ast.ExprID]ImplicitConversion)
	}
	tc.result.ImplicitConversions[expr] = ImplicitConversion{
		Kind:   kind,
		Source: src,
		Target: target,
		Span:   tc.exprSpan(expr),
	}
	if kind == ImplicitConversionTo {
		tc.recordToSymbol(expr, src, target)
	}

	// For tag injection (Some/Success), we need to register the instantiation
	// so that mono knows about this tag constructor call.
	if kind == ImplicitConversionSome || kind == ImplicitConversionSuccess {
		tc.recordTagInstantiationForInjection(kind, src, tc.exprSpan(expr))
	}
}

func (tc *typeChecker) recordTagUnionUpcast(expr ast.ExprID, src, target types.TypeID) bool {
	if !expr.IsValid() || src == types.NoTypeID || target == types.NoTypeID {
		return false
	}
	if !tc.canTagUnionUpcast(src, target) {
		return false
	}
	tc.recordImplicitConversionWithKind(expr, src, target, ImplicitConversionTagUnion)
	return true
}

func (tc *typeChecker) canTagUnionUpcast(src, target types.TypeID) bool {
	if tc.types == nil || src == types.NoTypeID || target == types.NoTypeID {
		return false
	}
	srcVal := tc.valueType(src)
	targetVal := tc.valueType(target)
	if srcVal == types.NoTypeID || targetVal == types.NoTypeID || srcVal == targetVal {
		return false
	}
	info, ok := tc.types.UnionInfo(targetVal)
	if !ok || info == nil {
		return false
	}
	for _, member := range info.Members {
		if member.Kind != types.UnionMemberTag {
			continue
		}
		if tc.isTagTypeMatch(srcVal, member.TagName, member.TagArgs) {
			return true
		}
	}
	return false
}

func (tc *typeChecker) recordToSymbol(expr ast.ExprID, src, target types.TypeID) {
	if !expr.IsValid() || tc.result == nil {
		return
	}
	if tc.result.ToSymbols == nil {
		tc.result.ToSymbols = make(map[ast.ExprID]symbols.SymbolID)
	}
	symID := tc.resolveToSymbol(expr, src, target)
	tc.result.ToSymbols[expr] = symID
	if symID.IsValid() {
		tc.recordMethodCallInstantiation(symID, src, nil, tc.exprSpan(expr))
	}
}

func (tc *typeChecker) resolveToSymbol(expr ast.ExprID, src, target types.TypeID) symbols.SymbolID {
	if tc == nil || tc.builder == nil || tc.builder.StringsInterner == nil {
		return symbols.NoSymbolID
	}
	if src == types.NoTypeID || target == types.NoTypeID {
		return symbols.NoSymbolID
	}
	nameID := tc.builder.StringsInterner.Intern("__to")
	member := &ast.ExprMemberData{Field: nameID}
	return tc.resolveMethodCallSymbol(member, src, expr, []types.TypeID{target}, nil, false)
}

// recordTagInstantiationForInjection registers a tag instantiation for implicit tag injection.
// This ensures mono knows about the Some<T> or Success<T> call we'll generate.
func (tc *typeChecker) recordTagInstantiationForInjection(kind ImplicitConversionKind, payloadType types.TypeID, span source.Span) {
	if tc.builder == nil || tc.builder.StringsInterner == nil {
		return
	}

	var tagName string
	switch kind {
	case ImplicitConversionSome:
		tagName = "Some"
	case ImplicitConversionSuccess:
		tagName = "Success"
	default:
		return
	}

	nameID := tc.builder.StringsInterner.Intern(tagName)
	scope := tc.scopeOrFile(tc.currentScope())
	tagSymID := tc.lookupTagSymbol(nameID, scope)
	if !tagSymID.IsValid() {
		return
	}

	// Register the instantiation with the payload type as the type argument
	args := []types.TypeID{payloadType}
	tc.rememberFunctionInstantiation(tagSymID, args, span, "tag-injection")
}
