package sema

import (
	"slices"

	"surge/internal/symbols"
)

// importedCallableNames answers whether the imported copy `selected` stands
// for the body c records. Two facts must agree: the unit published the copy as
// a copy of exactly that body, and the copy carries the key the program's
// merge bound the declaration under, so it is a sibling of c's own symbol in
// this vocabulary. A copy whose module path, name or signature spelling the
// merge keyed differently (a directory module imported by a file path, an
// aliased import, the methods of an intrinsic stdlib type) was never bound
// to that body by the merge, and the backends do not reach it either.
func (u *returnOriginUnitIndex) importedCallableNames(selected symbols.SymbolID, c *CallableCandidate) bool {
	body, listed := u.importedBody(selected)
	return listed && body.BodyKey != "" && body.BodyKey == c.BodyKey && body.SourceKey == c.SourceKey &&
		u.mergedSibling(selected, c.Symbol)
}

// mergedSibling answers whether two symbols of the authority's vocabulary are
// imported copies of one declaration under one merge key: the same span,
// kind, name, receiver, signature spelling, type-parameter count and module
// path. Only a unit that selects in the authority's own vocabulary holds both.
func (u *returnOriginUnitIndex) mergedSibling(dup, merged symbols.SymbolID) bool {
	if len(u.Publication.RootToLocalSymbols) != 0 || u.Symbols == nil || u.Symbols.Table == nil || u.Symbols.Table.Symbols == nil {
		return false
	}
	x, y := u.Symbols.Table.Symbols.Get(dup), u.Symbols.Table.Symbols.Get(merged)
	if x == nil || y == nil || x.Signature == nil || y.Signature == nil || x.Kind != symbols.SymbolFunction || y.Kind != x.Kind ||
		x.Flags&symbols.SymbolFlagImported == 0 || y.Flags&symbols.SymbolFlagImported == 0 {
		return false
	}
	return x.Span == y.Span && x.Name == y.Name && x.ModulePath == y.ModulePath && x.ReceiverKey == y.ReceiverKey &&
		len(x.TypeParams) == len(y.TypeParams) && x.Signature.Result == y.Signature.Result &&
		slices.Equal(x.Signature.Params, y.Signature.Params) &&
		x.Signature.ReturnSourceSyntax.Sources().Equal(y.Signature.ReturnSourceSyntax.Sources())
}

// importedBody is the body published for a copy, and whether the copy is
// listed at all; a copy whose facts named two bodies has the empty body.
func (u *returnOriginUnitIndex) importedBody(dup symbols.SymbolID) (FinalizationCallableIdentity, bool) {
	body, listed := u.Publication.ImportedCallables[dup]
	return body, listed
}

// importedCopyOf answers whether the imported copy `local` was published as a
// copy of the one body the canonical symbol owns in the merged catalog.
func (u *returnOriginUnitIndex) importedCopyOf(local, canonical symbols.SymbolID) bool {
	var owner *CallableCandidate
	for i := range u.authority.CallableCandidates {
		if c := &u.authority.CallableCandidates[i]; c.Symbol == canonical {
			if owner != nil {
				return false
			}
			owner = c
		}
	}
	return owner != nil && u.importedCallableNames(local, owner)
}

// templateNames answers whether a template recorded in the authority's
// vocabulary is the canonical callee itself, or an imported copy that stands
// for the callee's one body (importedCallableNames).
func (u *returnOriginUnitIndex) templateNames(template, callee symbols.SymbolID) bool {
	return template == callee || u.importedCopyOf(template, callee)
}

// importedTemplateFunction is the indexed declaration behind an imported copy
// that a finalized use names as its template: the one catalog record the copy
// stands for (importedCallableNames), owned by exactly one indexed function.
func (a *returnOriginAnalyzer) importedTemplateFunction(u *returnOriginUnitIndex, template symbols.SymbolID) *returnOriginFunction {
	var owner *CallableCandidate
	for i := range u.authority.CallableCandidates {
		if c := &u.authority.CallableCandidates[i]; u.importedCallableNames(template, c) {
			if owner != nil {
				return nil
			}
			owner = c
		}
	}
	if owner == nil {
		return nil
	}
	fn := a.functionForTemplate(owner.Symbol)
	if fn == nil || fn.candidate == nil || fn.key != owner.BodyKey || fn.canonicalSourceKey != owner.SourceKey {
		return nil
	}
	return fn
}

// sameTemplateBody answers whether two templates of the authority's
// vocabulary name one body: the same symbol, or symbols whose one catalog
// record or published copy identity names the same body. A finalized instance
// is keyed by its template's declaration, so the copy one file recorded and
// the copy another file selected meet in one instance.
func (a *returnOriginAnalyzer) sameTemplateBody(x, y symbols.SymbolID) bool {
	if x == y {
		return true
	}
	bx, okx := a.templateBody(x)
	by, oky := a.templateBody(y)
	return okx && oky && bx == by
}

// templateBody is the body identity of a template in the authority's
// vocabulary: its one catalog record, or else the one record every unit
// selecting in that vocabulary finds the template standing for as a copy.
func (a *returnOriginAnalyzer) templateBody(template symbols.SymbolID) (FinalizationCallableIdentity, bool) {
	var body FinalizationCallableIdentity
	authority := a.units[0].authority
	for i := range authority.CallableCandidates {
		if c := &authority.CallableCandidates[i]; c.Symbol == template {
			if body.BodyKey != "" {
				return body, false
			}
			body = FinalizationCallableIdentity{BodyKey: c.BodyKey, SourceKey: c.SourceKey}
		}
	}
	if body.BodyKey != "" {
		return body, true
	}
	for _, u := range a.units {
		if _, listed := u.importedBody(template); !listed || len(u.Publication.RootToLocalSymbols) != 0 {
			continue
		}
		found := 0
		for i := range authority.CallableCandidates {
			c := &authority.CallableCandidates[i]
			if !u.importedCallableNames(template, c) {
				continue
			}
			published := FinalizationCallableIdentity{BodyKey: c.BodyKey, SourceKey: c.SourceKey}
			found++
			if found > 1 || (body.BodyKey != "" && body != published) {
				return FinalizationCallableIdentity{}, false
			}
			body = published
		}
		if found == 0 {
			return FinalizationCallableIdentity{}, false
		}
	}
	return body, body.BodyKey != ""
}
