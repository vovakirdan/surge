package sema

import (
	"testing"

	"surge/internal/source"
	"surge/internal/symbols"
)

// An imported copy stands for a body only when its unit published it as a
// copy of exactly that body and it carries the merge key of the body's own
// symbol. Two identities for one copy, a body published under another source,
// a canonical symbol owning two records, a copy keyed under another module
// path or name, or a unit selecting in another vocabulary reach nothing.
func TestReturnOriginImportedCallableIdentity(t *testing.T) {
	strs := source.NewInterner()
	table := symbols.NewTable(symbols.Hints{}, strs)
	span := source.Span{File: 3, Start: 10, End: 15}
	sig := &symbols.FunctionSignature{Params: []symbols.TypeKey{"&string"}, Result: "&string"}
	add := func(name, module string) symbols.SymbolID {
		return table.Symbols.New(&symbols.Symbol{Name: strs.Intern(name), Kind: symbols.SymbolFunction, Flags: symbols.SymbolFlagImported,
			Span: span, Signature: sig, ModulePath: module})
	}
	canonical, copySym, secondCopy := add("first", "dep"), add("first", "dep"), add("first", "dep")
	otherPath, aliased := add("first", "dep/main"), add("pick", "dep")
	other := table.Symbols.New(&symbols.Symbol{Name: strs.Intern("second"), Kind: symbols.SymbolFunction, Flags: symbols.SymbolFlagImported,
		Span: source.Span{File: 3, Start: 30, End: 36}, Signature: sig, ModulePath: "dep"})
	body := CallableCandidate{Symbol: canonical, BodyKey: "dep|dep/main.sg:10:15|first", SourceKey: "dep/main.sg"}
	foreign := CallableCandidate{Symbol: other, BodyKey: "dep|dep/main.sg:30:36|second", SourceKey: "dep/main.sg"}
	copyOf := func(sym symbols.SymbolID, c CallableCandidate) FinalizationCallableIdentity {
		return FinalizationCallableIdentity{Symbol: sym, BodyKey: c.BodyKey, SourceKey: c.SourceKey}
	}
	published := copyOf(copySym, body)
	// Identities listed twice for one copy publish the empty identity, as the
	// driver's capture does.
	unit := func(candidates []CallableCandidate, identities ...FinalizationCallableIdentity) *returnOriginUnitIndex {
		published := make(map[symbols.SymbolID]FinalizationCallableIdentity, len(identities))
		for _, identity := range identities {
			if _, twice := published[identity.Symbol]; twice {
				identity = FinalizationCallableIdentity{Symbol: identity.Symbol}
			}
			published[identity.Symbol] = identity
		}
		return &returnOriginUnitIndex{
			ReturnOriginUnit: ReturnOriginUnit{Symbols: &symbols.Result{Table: table},
				Publication: FinalizationPublication{SourceKey: "app/main.sg", ImportedCallables: published}},
			authority: &Result{CallableCandidates: candidates},
		}
	}
	both := []CallableCandidate{body, foreign}
	for _, tc := range []struct {
		name       string
		candidates []CallableCandidate
		identities []FinalizationCallableIdentity
		template   symbols.SymbolID
		want       bool
	}{
		{"published_sibling_copy", both, []FinalizationCallableIdentity{published}, copySym, true},
		{"canonical_itself", both, nil, canonical, true},
		{"unpublished_copy", both, nil, copySym, false},
		{"copy_of_another_body", both, []FinalizationCallableIdentity{copyOf(copySym, foreign)}, copySym, false},
		{"same_key_other_source", both, []FinalizationCallableIdentity{{Symbol: copySym, BodyKey: body.BodyKey, SourceKey: "builtin"}}, copySym, false},
		{"two_identities_for_one_copy", both, []FinalizationCallableIdentity{published, copyOf(copySym, foreign)}, copySym, false},
		{"canonical_owns_two_records", []CallableCandidate{body, {Symbol: canonical, BodyKey: foreign.BodyKey, SourceKey: foreign.SourceKey}}, []FinalizationCallableIdentity{published}, copySym, false},
		{"another_symbol_published", both, []FinalizationCallableIdentity{copyOf(secondCopy, body)}, copySym, false},
		{"copy_keyed_under_another_module_path", both, []FinalizationCallableIdentity{copyOf(otherPath, body)}, otherPath, false},
		{"aliased_copy", both, []FinalizationCallableIdentity{copyOf(aliased, body)}, aliased, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := unit(tc.candidates, tc.identities...).templateNames(tc.template, canonical); got != tc.want {
				t.Fatalf("templateNames(%d, %d) = %v, want %v", tc.template, canonical, got, tc.want)
			}
		})
	}
	t.Run("copy_outside_the_authority_vocabulary", func(t *testing.T) {
		u := unit(both, published)
		u.Publication.RootToLocalSymbols = map[symbols.SymbolID][]symbols.SymbolID{canonical: {3}}
		if u.templateNames(copySym, canonical) || u.importedCopyOf(copySym, canonical) {
			t.Fatal("a copy counts only for a unit that selects in the authority's vocabulary")
		}
	})
	t.Run("instance_template_bodies", func(t *testing.T) {
		root := unit(both, published, copyOf(secondCopy, body), copyOf(otherPath, body))
		a := &returnOriginAnalyzer{units: []*returnOriginUnitIndex{root}}
		for _, pair := range [][2]symbols.SymbolID{{copySym, secondCopy}, {copySym, canonical}, {canonical, canonical}} {
			if !a.sameTemplateBody(pair[0], pair[1]) {
				t.Fatalf("templates %v name one body", pair)
			}
		}
		for _, pair := range [][2]symbols.SymbolID{{copySym, other}, {copySym, otherPath}, {copySym, 99}, {99, 98}} {
			if a.sameTemplateBody(pair[0], pair[1]) {
				t.Fatalf("templates %v name different or no bodies", pair)
			}
		}
		imported := unit(both, published)
		imported.Publication.RootToLocalSymbols = map[symbols.SymbolID][]symbols.SymbolID{canonical: {3}}
		if (&returnOriginAnalyzer{units: []*returnOriginUnitIndex{imported}}).sameTemplateBody(copySym, canonical) {
			t.Fatal("only a unit selecting in the authority's vocabulary vouches for a copy template")
		}
		twin := unit(both, copyOf(copySym, foreign))
		if (&returnOriginAnalyzer{units: []*returnOriginUnitIndex{unit(both, published), twin}}).sameTemplateBody(copySym, canonical) {
			t.Fatal("units that publish different bodies for one copy vouch for none")
		}
	})
	t.Run("selection_through_identity", func(t *testing.T) {
		u := unit(both, published)
		if !u.importedCallableNames(copySym, &u.authority.CallableCandidates[0]) || u.importedCallableNames(copySym, &u.authority.CallableCandidates[1]) {
			t.Fatal("a published copy must name exactly its own body")
		}
	})
}
