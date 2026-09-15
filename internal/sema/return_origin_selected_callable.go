package sema

import (
	"slices"

	"surge/internal/symbols"
	"surge/internal/types"
)

// A selected symbol belongs to its caller's vocabulary. It reaches a
// declaration only through the one canonical candidate its unit published for
// it, and only while the two still describe the same typed callable. Missing,
// rebound or ambiguous publication refuses: once a non-empty publication has
// missed, nothing falls back to comparing raw symbol IDs.
func (u *returnOriginUnitIndex) selectedCallableCandidate(selected symbols.SymbolID) (*CallableCandidate, string) {
	var candidate *CallableCandidate
	for i := range u.authority.CallableCandidates {
		c := &u.authority.CallableCandidates[i]
		mapped := c.Symbol == selected
		if len(u.Publication.RootToLocalSymbols) != 0 {
			mapped = slices.Contains(u.Publication.RootToLocalSymbols[c.Symbol], selected)
		}
		if !mapped {
			continue
		}
		if candidate != nil {
			return nil, "selected callable has ambiguous canonical authority"
		}
		candidate = c
	}
	sym := u.Symbols.Table.Symbols.Get(selected)
	if !selected.IsValid() || candidate == nil || sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		return nil, "selected callable lacks its published callable authority"
	}
	info := returnOriginFnInfo(u.Sema.TypeInterner, sym.Type)
	if info == nil || sym.Span != candidate.Source || !selectedCallableAgrees(sym.Signature, info, candidate) {
		return nil, "selected callable disagrees with its original typed signature"
	}
	return candidate, ""
}

// The physical declaration behind a certified selection: the one indexed
// function owning the candidate's symbol, body and source, still admitted by
// its own unit's declaration certificate, whose original signature the
// selection agrees with. Named and default slots come from that original.
func (a *returnOriginAnalyzer) selectedCallableFunction(u *returnOriginUnitIndex, selected symbols.SymbolID) (*returnOriginFunction, string) {
	candidate, reason := u.selectedCallableCandidate(selected)
	if reason != "" {
		return nil, reason
	}
	fn := a.functionForTemplate(candidate.Symbol)
	if fn == nil || fn.candidate == nil || fn.key != candidate.BodyKey || fn.canonicalSourceKey != candidate.SourceKey {
		return nil, "selected callable lacks its indexed physical declaration"
	}
	identity, err := fn.unit.owningCallableIdentity(fn.item, fn.symbol, fn.info)
	original := fn.unit.Symbols.Table.Symbols.Get(fn.symbol)
	chosen := u.Symbols.Table.Symbols.Get(selected)
	if err != nil || identity.BodyKey != fn.key || identity.SourceKey != fn.canonicalSourceKey ||
		original == nil || original.Signature == nil || !selectedCallableAgrees(original.Signature, fn.info, candidate) ||
		!moduleFunctionSignaturesEqual(chosen.Signature, original.Signature) {
		return nil, "selected callable disagrees with its physical declaration"
	}
	return fn, ""
}

// A signature and its typed formals against a published candidate, slot by
// slot; a missing default or variadic flag reads as false on both sides.
func selectedCallableAgrees(sig *symbols.FunctionSignature, info *types.FnInfo, c *CallableCandidate) bool {
	if sig.HasSelf != c.HasSelf || sig.HasBody != c.HasBody || info.Result != c.ResultType ||
		!slices.Equal(info.Params, c.ParamTypes) || !info.ReturnSources().Equal(c.ReturnSources) ||
		!sig.ReturnSourceSyntax.Sources().Equal(c.ReturnSources) {
		return false
	}
	for i := range info.Params {
		if boolAt(sig.Defaults, i) != boolAt(c.Defaults, i) || boolAt(sig.Variadic, i) != boolAt(c.Variadic, i) {
			return false
		}
	}
	return true
}
