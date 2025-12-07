package sema

import (
	"surge/internal/ast"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// ImplicitConversion records an implicit __to call for an expression.
// This information is used by later compilation phases (such as codegen) to emit
// the actual __to function call.
type ImplicitConversion struct {
	Source types.TypeID // Original type T
	Target types.TypeID // Target type U
	Span   source.Span  // Location of the expression
}

// tryImplicitConversion attempts to find a __to conversion from source to target.
// It returns:
//   - (target, true, false) if exactly one __to(source -> target) exists
//   - (NoTypeID, false, true) if multiple candidates exist (ambiguous)
//   - (NoTypeID, false, false) if no candidate found
//
// This function only attempts implicit conversion if the types are not already
// assignable. It does NOT chain conversions (T -> X -> U is not allowed).
func (tc *typeChecker) tryImplicitConversion(source, target types.TypeID) (types.TypeID, bool, bool) {
	if source == types.NoTypeID || target == types.NoTypeID {
		return types.NoTypeID, false, false
	}

	// Fast path: already assignable (don't use conversion)
	// This ensures we don't apply conversion when types already match
	if tc.typesAssignable(target, source, true) {
		return target, false, false // found=false because no conversion needed
	}

	candidates := tc.collectToMethods(source, target)
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
func (tc *typeChecker) collectToMethods(source, target types.TypeID) []*symbols.FunctionSignature {
	var results []*symbols.FunctionSignature
	seen := make(map[*symbols.FunctionSignature]struct{})
	targetCandidates := tc.typeKeyCandidates(target)

	for _, sc := range tc.typeKeyCandidates(source) {
		if sc.key == "" {
			continue
		}
		methods := tc.lookupMagicMethods(sc.key, "__to")
		for _, sig := range methods {
			if sig == nil || len(sig.Params) < 2 {
				continue
			}
			// Check if second parameter matches target type
			for _, tgt := range targetCandidates {
				if tgt.key != "" && typeKeyEqual(sig.Params[1], tgt.key) {
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

// recordImplicitConversion records an implicit conversion for codegen.
// This stores the conversion in Result.ImplicitConversions so that later
// phases can emit the actual __to function call.
func (tc *typeChecker) recordImplicitConversion(expr ast.ExprID, source, target types.TypeID) {
	if !expr.IsValid() || source == types.NoTypeID || target == types.NoTypeID {
		return
	}
	if tc.result.ImplicitConversions == nil {
		tc.result.ImplicitConversions = make(map[ast.ExprID]ImplicitConversion)
	}
	tc.result.ImplicitConversions[expr] = ImplicitConversion{
		Source: source,
		Target: target,
		Span:   tc.exprSpan(expr),
	}
}
