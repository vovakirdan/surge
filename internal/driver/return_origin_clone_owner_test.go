package driver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These are full source inputs with one detached publication association
// changed. Other unfinished core transfers remain visible in the analysis.
func TestAnalyzeTypedDeferredCloneOwnerPublication(t *testing.T) {
	const src = "fn probe(value: &string) -> &string { return value; }\n"
	const reason = "deferred clone disagrees with its original parameter owner"
	for _, name := range []string{"intact", "foreign_owner", "missing_owner", "ambiguous_owner"} {
		t.Run(name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_CLONE_OWNER_SOURCE case=%s sha256=%x source=%q", name, sha256.Sum256([]byte(src)), src)
			res := returnOriginStdlibFixture(t, src, false)
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			inputs, unitsErr := collectReturnOriginUnits(res)
			logReturnOriginCallEvidence(t, map[string]any{"case": name, "closure_error": errorReturnOriginCallText(closureErr),
				"unit_error": errorReturnOriginCallText(unitsErr), "closure": res.Sema.InstantiationClosure})
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 || res.Sema.InstantiationClosure == nil {
				t.Fatal("PRECONDITION: missing complete original source input or finalized authority")
			}
			checkReturnOriginStdlibBags(t, res, false)
			checkReturnOriginCloneUnits(t, res, inputs.units)
			coreIndex, edges, owners := checkReturnOriginCloneOwners(t, res, inputs.units)
			original := returnOriginCloneOwnerSnapshot(t, res, inputs.units, coreIndex, edges)
			changed := slices.Clone(inputs.units)
			publication := &changed[coreIndex].Publication
			publication.RootToLocalSymbols = make(map[symbols.SymbolID][]symbols.SymbolID)
			for root, locals := range inputs.units[coreIndex].Publication.RootToLocalSymbols {
				publication.RootToLocalSymbols[root] = slices.Clone(locals)
			}
			first, foreign := owners[0], owners[1]
			if name == "foreign_owner" || name == "missing_owner" {
				publication.RootToLocalSymbols[first.canonical] = slices.DeleteFunc(publication.RootToLocalSymbols[first.canonical],
					func(local symbols.SymbolID) bool { return local == first.local })
			}
			if name == "foreign_owner" || name == "ambiguous_owner" {
				publication.RootToLocalSymbols[foreign.canonical] = append(publication.RootToLocalSymbols[foreign.canonical], first.local)
			}
			var want []sema.ReturnOriginPending
			if name != "intact" {
				for _, edge := range edges[:4] {
					want = append(want, sema.ReturnOriginPending{SourceKey: "core/array.sg", Span: edge.Witness.Site, Reason: reason})
				}
			}
			mutated := returnOriginCloneOwnerSnapshot(t, res, changed, coreIndex, edges)
			if !slices.Equal(original, returnOriginCloneOwnerSnapshot(t, res, inputs.units, coreIndex, edges)) {
				t.Fatal("PRECONDITION: detached publication mutation changed original input")
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, changed)
			logReturnOriginCallEvidence(t, map[string]any{"case": name, "original_input": json.RawMessage(original),
				"effective_input": json.RawMessage(mutated), "expected_owner_pending": want,
				"analysis": analysis, "analysis_error": errorReturnOriginCallText(err), "analyzed_units": len(changed)})
			if !slices.Equal(original, returnOriginCloneOwnerSnapshot(t, res, inputs.units, coreIndex, edges)) ||
				!slices.Equal(mutated, returnOriginCloneOwnerSnapshot(t, res, changed, coreIndex, edges)) {
				t.Fatal("analysis mutated original authority, original symbols, descriptors or detached publication")
			}
			if err != nil || analysis == nil {
				t.Fatalf("owner publication analysis failed: %v", err)
			}
			var got []sema.ReturnOriginPending
			for _, pending := range analysis.Pending {
				if pending.Reason == reason {
					got = append(got, pending)
				}
			}
			slices.SortFunc(got, func(a, b sema.ReturnOriginPending) int {
				if a.Span.Start < b.Span.Start {
					return -1
				}
				if a.Span.Start > b.Span.Start {
					return 1
				}
				return 0
			})
			if !slices.Equal(got, want) {
				t.Fatalf("wrong exact parameter-owner refusals: got %+v, want %+v; the two untouched owner sites must stay unaffected", got, want)
			}
		})
	}
}

type returnOriginCloneOwner struct {
	local, canonical symbols.SymbolID
}

func checkReturnOriginCloneOwners(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit) (int, []sema.DeferredCallableEdge, [2]returnOriginCloneOwner) {
	t.Helper()
	coreIndex := -1
	for i := range units {
		if units[i].SourceKey == "core/array.sg" {
			coreIndex = i
		}
	}
	if coreIndex < 0 {
		t.Fatal("PRECONDITION: missing original array unit")
	}
	core := &units[coreIndex]
	file := res.FileSet.Get(core.Builder.Files.Get(core.FileID).Span.File)
	if file == nil || fmt.Sprintf("%x", sha256.Sum256(file.Content)) != "532a6cd1bc46d2d665d71f13f988358e42fe30dd39810117278afd46b967ffbc" {
		t.Fatal("PRECONDITION: core array source differs from the frozen six clone sites")
	}
	var edges []sema.DeferredCallableEdge
	for _, edge := range res.Sema.InstantiationGraph.DeferredCallables() {
		if edge.Kind == sema.DeferredCloneCall {
			edges = append(edges, edge)
		}
	}
	slices.SortFunc(edges, func(a, b sema.DeferredCallableEdge) int { return int(a.Witness.Site.Start) - int(b.Witness.Site.Start) })
	spans := [][2]uint32{{1486, 1499}, {2762, 2777}, {3222, 3236}, {3260, 3274}, {7414, 7427}, {8033, 8047}}
	if len(edges) != len(spans) {
		t.Fatal("PRECONDITION: graph does not contain the six original core clone edges")
	}
	var owners [2]returnOriginCloneOwner
	for i, edge := range edges {
		span := source.Span{File: file.ID, Start: spans[i][0], End: spans[i][1]}
		if edge.Witness.SourceKey != core.SourceKey || edge.Witness.Site != span || edge.Witness.Caller != edge.Caller ||
			string(edge.UseID) != fmt.Sprintf("core/array.sg/%d:%d/3/0", span.Start, span.End) {
			t.Fatal("PRECONDITION: clone site/canonical caller/use differs from the original source")
		}
		group, arity := 0, 1
		if i >= 4 {
			group, arity = 1, 2
		}
		var candidate *sema.CallableCandidate
		for j := range res.Sema.CallableCandidates {
			c := &res.Sema.CallableCandidates[j]
			if c.Symbol == edge.Caller {
				if candidate != nil {
					t.Fatal("PRECONDITION: duplicate canonical clone caller")
				}
				candidate = c
			}
		}
		if candidate == nil || !candidate.HasBody || candidate.SourceKey != core.SourceKey || candidate.Source.File != file.ID ||
			len(candidate.TemplateParams) != arity || int(edge.CallerTemplateArity) != arity || len(edge.CallerBindings) != arity {
			t.Fatal("PRECONDITION: original clone caller lost its exact template arity")
		}
		registered := core.Sema.TypeInterner.ArrayNominalType()
		if group == 1 {
			registered = core.Sema.TypeInterner.ArrayFixedNominalType()
		}
		nominal, nominalOK := core.Sema.TypeInterner.StructInfo(registered)
		receiver, receiverOK := core.Sema.TypeInterner.StructInfo(candidate.ReceiverType)
		if registered == types.NoTypeID || !nominalOK || nominal == nil || !receiverOK || receiver == nil ||
			receiver.Name != nominal.Name || receiver.Decl != nominal.Decl || !slices.Equal(receiver.TypeArgs, candidate.TemplateParams) ||
			candidate.ReceiverTemplateArity != arity {
			t.Fatal("PRECONDITION: extern receiver differs from the exact registered array nominal and template arguments")
		}
		logReturnOriginCallEvidence(t, map[string]any{"registered_type": registered, "registered_nominal": nominal, "extern_receiver": receiver})
		localCallers := 0
		for _, identity := range core.Publication.LocalCallables {
			if identity.BodyKey != candidate.BodyKey || identity.SourceKey != candidate.SourceKey ||
				!slices.Contains(core.Publication.LocalSymbols(candidate.Symbol), identity.Symbol) {
				continue
			}
			sym := core.Symbols.Table.Symbols.Get(identity.Symbol)
			if sym == nil || sym.Span != candidate.Source || sym.Decl.SourceFile != file.ID || sym.Decl.ASTFile != core.FileID {
				t.Fatal("PRECONDITION: canonical caller does not retain its original source symbol")
			}
			receiverSyntax := core.Builder.Types.Get(sym.Receiver)
			if receiverSyntax == nil || receiverSyntax.Span.File != file.ID || receiverSyntax.Span.Start >= receiverSyntax.Span.End {
				t.Fatal("PRECONDITION: extern callable lost its original source receiver declaration")
			}
			localCallers++
			logReturnOriginCallEvidence(t, map[string]any{"edge": edge, "canonical_caller": candidate, "original_callable": identity,
				"original_symbol": sym, "original_receiver_syntax": receiverSyntax})
		}
		uses := 0
		for ref, use := range core.Sema.DeferredCallableUses {
			if ref.Kind != sema.DeferredCloneCall || use != edge.UseID {
				continue
			}
			node := core.Builder.Exprs.Get(ref.Expr)
			call, ok := core.Builder.Exprs.Call(ref.Expr)
			if node == nil || node.Span != span || !ok || call == nil || len(call.Args) != 1 || edge.ExpectedResult == types.NoTypeID ||
				edge.ExpectedResult != edge.Receiver || core.Sema.ExprTypes[ref.Expr] != edge.ExpectedResult {
				t.Fatal("PRECONDITION: original clone call/type/use is missing")
			}
			arg, ok := core.Sema.TypeInterner.Lookup(core.Sema.ExprTypes[call.Args[0].Value])
			if !ok || arg.Kind != types.KindReference || arg.Mutable || arg.Elem != edge.Receiver {
				t.Fatal("PRECONDITION: original clone lacks its typed shared receiver")
			}
			uses++
		}
		if localCallers != 1 || uses != 1 {
			t.Fatal("PRECONDITION: original callable or local typed use is not unique")
		}
		for j, binding := range edge.CallerBindings {
			info, ok := core.Sema.TypeInterner.TypeParamInfo(binding.Param)
			if !ok || info == nil || info.Index != uint32(j) || binding.ParamIndex != info.Index || binding.ArgIndex != uint32(j) ||
				candidate.TemplateParams[j] != binding.Param || info.IsConst != (j == 1) {
				t.Fatal("PRECONDITION: original parameter descriptor/index/argument slot differs from the typed caller")
			}
			owner := returnOriginCloneOwner{local: symbols.SymbolID(info.Owner), canonical: binding.Owner}
			if !owners[group].local.IsValid() {
				owners[group] = owner
			}
			if owner != owners[group] || owner.local == owner.canonical || !owner.local.IsValid() || !owner.canonical.IsValid() {
				t.Fatal("PRECONDITION: original and canonical owner classes are not distinct and stable")
			}
			var roots []symbols.SymbolID
			for root, locals := range core.Publication.RootToLocalSymbols {
				if slices.Contains(locals, owner.local) {
					roots = append(roots, root)
				}
			}
			sym := core.Symbols.Table.Symbols.Get(owner.local)
			logReturnOriginCallEvidence(t, map[string]any{"site": span, "binding": binding, "descriptor": info,
				"original_parameter_owner": sym, "canonical_owner_candidates": roots})
			// The built-in nominal is registered without a source declaration.
			// Its extern callable and receiver syntax carry the source identity above.
			if len(roots) != 1 || roots[0] != owner.canonical || sym == nil || sym.Kind != symbols.SymbolType ||
				sym.Flags&symbols.SymbolFlagBuiltin == 0 || sym.Type != registered || sym.Name != nominal.Name || sym.Span != nominal.Decl {
				t.Fatal("PRECONDITION: original registered type owner lacks its exact nominal or unique canonical publication")
			}
		}
	}
	if owners[0].local == owners[1].local || owners[0].canonical == owners[1].canonical {
		t.Fatal("PRECONDITION: foreign owner is not a distinct actual original type")
	}
	return coreIndex, edges, owners
}

func returnOriginCloneOwnerSnapshot(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, coreIndex int, edges []sema.DeferredCallableEdge) []byte {
	t.Helper()
	publications := make(map[string]sema.FinalizationPublication)
	for _, unit := range units {
		publications[unit.SourceKey] = unit.Publication
	}
	core := &units[coreIndex]
	var parameters, originalSymbols []any
	for _, edge := range edges {
		for _, binding := range edge.CallerBindings {
			info, _ := core.Sema.TypeInterner.TypeParamInfo(binding.Param)
			parameters = append(parameters, map[string]any{"param": binding.Param, "descriptor": info})
			if info != nil {
				originalSymbols = append(originalSymbols, core.Symbols.Table.Symbols.Get(symbols.SymbolID(info.Owner)))
			}
		}
		for _, identity := range core.Publication.LocalCallables {
			for _, candidate := range res.Sema.CallableCandidates {
				if candidate.Symbol == edge.Caller && identity.BodyKey == candidate.BodyKey && identity.SourceKey == candidate.SourceKey {
					originalSymbols = append(originalSymbols, core.Symbols.Table.Symbols.Get(identity.Symbol))
				}
			}
		}
	}
	raw, err := json.Marshal(map[string]any{"publications": publications, "original_symbols": originalSymbols, "parameter_descriptors": parameters,
		"roots": res.Sema.InstantiationGraph.Roots(), "edges": res.Sema.InstantiationGraph.Edges(), "deferred": res.Sema.InstantiationGraph.DeferredCallables(),
		"closure": res.Sema.InstantiationClosure, "candidates": res.Sema.CallableCandidates})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
