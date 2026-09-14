package driver

import (
	"crypto/sha256"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// These real single-unit sources separate unused generic bodies from concrete
// identity instances. They do not certify the full core/module analysis.
func TestAnalyzeTypedGenericReturnOrigins(t *testing.T) {
	const marked = "fn identity<T>(@return_source value: T) -> T { return value; }\n"
	for _, tc := range []struct{ name, src string }{
		{"identity_template", "fn identity<T>(value: T) -> T { return value; }\n"},
		{"alias_template", "fn identity<T>(value: T) -> T { let alias: T = value; return alias; }\n"},
		{"local_address_template", "fn identity<T>(value: T) -> &T { return &value; }\n"},
		{"ref_free_inner_escape_template", "fn identity<T>(value: T) -> int64 { let alias = { let owned: int64 = 7; ret &owned; }; return 1; }\n"},
		{"marked_template", marked},
		{"marked_owned_use", marked + "fn probe(value: int64) -> int64 { return identity(value); }\n"},
		{"marked_borrowed_use", marked + "fn probe(value: &string) -> &string { return identity::<&string>(value); }\n"},
		{"marked_owned_and_borrowed_uses", marked + "fn owned_probe(value: int64) -> int64 { return identity(value); }\nfn borrowed_probe(value: &string) -> &string { return identity::<&string>(value); }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_GENERIC_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			res := returnOriginTypedFixtureWithEscapeEvidence(t, tc.src, true)
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			inputs, unitsErr := collectReturnOriginUnits(res)
			bodies := make(map[symbols.SymbolID]*ast.FnItem)
			var bodySymbols [][]symbols.SymbolID
			for _, item := range res.Builder.Files.Get(res.FileID).Items {
				fn, ok := res.Builder.Items.Fn(item)
				if !ok || fn == nil {
					continue
				}
				ids := res.Symbols.ItemSymbols[item]
				bodySymbols = append(bodySymbols, ids)
				if len(ids) == 1 {
					bodies[ids[0]] = fn
				}
			}
			var candidates []sema.CallableCandidate
			var identity sema.CallableCandidate
			for _, candidate := range res.Sema.CallableCandidates {
				if bodies[candidate.Symbol] != nil {
					candidates = append(candidates, candidate)
					if candidate.Name == "identity" {
						identity = candidate
					}
				}
			}
			var analysis *sema.ReturnOriginAnalysis
			var analysisErr error
			if closureErr == nil && unitsErr == nil {
				analysis, analysisErr = sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			}
			var declarations []map[string]any
			for _, request := range res.Sema.ReturnSourceDeclarations {
				declarations = append(declarations, map[string]any{"request": request, "params": request.Params(), "result": request.Result(),
					"slots": request.Syntax.Sources().Slots(), "validation": sema.ValidateDeclaredReturnSources(res.Sema.TypeInterner, request)})
			}
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "body_symbols": bodySymbols, "candidates": candidates,
				"identity_present": res.Sema.InstantiationIdentity != nil, "closure": res.Sema.InstantiationClosure,
				"closure_error": errorReturnOriginCallText(closureErr), "unit_count": len(inputs.units), "unit_error": errorReturnOriginCallText(unitsErr),
				"expr_types": res.Sema.ExprTypes, "binding_types": res.Sema.BindingTypes, "declarations": declarations,
				"analysis": analysis, "analysis_error": errorReturnOriginCallText(analysisErr), "typed_diagnostics": res.Bag.Items()})
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 1 || res.Sema.InstantiationIdentity == nil || res.Sema.InstantiationClosure == nil {
				t.Fatal("PRECONDITION: source did not retain its real unit and finalized generic authority")
			}
			if len(candidates) != len(bodySymbols) || len(bodies) != len(bodySymbols) || !identity.Symbol.IsValid() || len(identity.TemplateParams) != 1 {
				t.Fatal("PRECONDITION: original functions lack unique typed candidates or identity<T>")
			}
			for _, candidate := range candidates {
				fn := bodies[candidate.Symbol]
				if candidate.BodyKey == "" || candidate.SourceKey != inputs.units[0].SourceKey || !candidate.HasBody ||
					!fn.Body.IsValid() || candidate.Source != fn.NameSpan || candidate.DeclKeyword != fn.FnKeywordSpan {
					t.Fatalf("PRECONDITION: candidate lost original body/source identity: %+v", candidate)
				}
			}
			checkReturnOriginGenericInstances(t, res, identity, candidates)
			if strings.HasPrefix(tc.name, "marked_") {
				checkReturnOriginGenericPromise(t, res, identity)
			}
			if analysisErr != nil || analysis == nil || len(analysis.Summaries) == 0 {
				t.Fatalf("PRECONDITION: typed body analysis did not produce evidence: %v", analysisErr)
			}
			body := requireReturnOriginSummary(t, analysis, "identity")
			if body.BodyKey != identity.BodyKey || body.Source != identity.Source {
				t.Fatal("body summary was detached from its exact original declaration")
			}
			if tc.name == "local_address_template" || tc.name == "ref_free_inner_escape_template" {
				owner, statement := "value", "return &value;"
				if tc.name == "ref_free_inner_escape_template" {
					owner, statement = "owned", "ret &owned;"
					if body.Unknown || body.NoNormalReturn || len(body.ParamSlots) != 0 {
						t.Fatalf("reference-free result changed the inner-escape subject: %+v", body)
					}
				}
				checkReturnOriginGenericEscape(t, res, analysis, tc.src, owner, statement)
				return
			}
			if !analysis.Complete() || len(analysis.Diagnostics) != 0 || body.Unknown || body.NoNormalReturn || !slices.Equal(body.ParamSlots, []uint32{0}) {
				t.Fatalf("generic incoming content lacks a complete Param0 proof: %+v", analysis)
			}
			for _, candidate := range candidates {
				if candidate.Symbol == identity.Symbol {
					continue
				}
				probe := requireReturnOriginSummary(t, analysis, candidate.Name)
				var slots []uint32
				if typ, _ := res.Sema.TypeInterner.Lookup(candidate.ResultType); typ.Kind == types.KindReference {
					slots = []uint32{0}
				}
				if probe.BodyKey != candidate.BodyKey || probe.Source != candidate.Source || probe.Unknown || probe.NoNormalReturn || !slices.Equal(probe.ParamSlots, slots) {
					t.Fatalf("concrete probe lost its actual borrowed-content result: %+v", probe)
				}
			}
		})
	}
}

func checkReturnOriginGenericInstances(t *testing.T, res *DiagnoseResult, identity sema.CallableCandidate, candidates []sema.CallableCandidate) {
	t.Helper()
	wanted := make(map[types.TypeID]symbols.SymbolID)
	for _, candidate := range candidates {
		if candidate.Symbol == identity.Symbol {
			continue
		}
		if len(candidate.TemplateParams) != 0 || len(candidate.ParamTypes) != 1 || candidate.ResultType != candidate.ParamTypes[0] ||
			!slices.Contains(res.Sema.InstantiationClosure.LiveCallables, candidate.Symbol) {
			t.Fatalf("PRECONDITION: actual nongeneric probe root is missing: %+v", candidate)
		}
		wanted[candidate.ParamTypes[0]] = candidate.Symbol
	}
	var instances []sema.InstantiationInstance
	var uses []sema.ConcreteInstantiationUse
	for _, instance := range res.Sema.InstantiationClosure.Instances {
		if instance.Template == identity.Symbol {
			instances = append(instances, instance)
		}
	}
	for _, use := range res.Sema.InstantiationClosure.UseSites {
		if use.CalleeTemplate == identity.Symbol {
			uses = append(uses, use)
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"identity_instances": instances, "identity_uses": uses, "nongeneric_probe_roots": wanted})
	if len(instances) != len(wanted) || len(uses) != len(wanted) {
		t.Fatalf("PRECONDITION: actual identity instances/uses differ from source probe types: %d/%d want %d", len(instances), len(uses), len(wanted))
	}
	seen := make(map[types.TypeID]bool)
	for _, instance := range instances {
		if len(instance.TemplateArgs) != 1 || !wanted[instance.TemplateArgs[0]].IsValid() || seen[instance.TemplateArgs[0]] {
			t.Fatalf("PRECONDITION: identity specialization lost or repeated a source argument: %+v", instance)
		}
		arg := instance.TemplateArgs[0]
		seen[arg] = true
		key, err := sema.NewInstanceKey(*res.Sema.InstantiationIdentity, identity.Symbol, []types.TypeID{arg})
		if err != nil || instance.Key != key {
			t.Fatalf("PRECONDITION: identity specialization has a noncanonical key: %+v error=%v", instance, err)
		}
		matched := 0
		for _, use := range uses {
			if use.Callee != instance.Key {
				continue
			}
			matched++
			if use.SourceKey != identity.SourceKey || !slices.Equal(use.TemplateArgs, instance.TemplateArgs) || use.Site.File != res.File.ID {
				t.Fatal("PRECONDITION: concrete use lost original source or type arguments")
			}
			callTyped := false
			for expr, typ := range res.Sema.ExprTypes {
				if node := res.Builder.Exprs.Get(expr); node != nil && node.Span == use.Site {
					call, ok := res.Builder.Exprs.Call(expr)
					callTyped = ok && len(call.Args) == 1 && typ == arg && res.Sema.ExprTypes[call.Args[0].Value] == arg
				}
			}
			if !callTyped {
				t.Fatal("PRECONDITION: concrete use is not the typed identity call in the source")
			}
		}
		if matched != 1 {
			t.Fatal("PRECONDITION: identity instance lacks exactly its source call")
		}
	}
}

func checkReturnOriginGenericPromise(t *testing.T, res *DiagnoseResult, identity sema.CallableCandidate) {
	t.Helper()
	if len(res.Sema.ReturnSourceDeclarations) != 1 {
		t.Fatal("PRECONDITION: marked identity lost its original declaration request")
	}
	original := res.Sema.ReturnSourceDeclarations[0]
	if original.Owner != identity.Symbol || !slices.Equal(original.Params(), identity.TemplateParams) || original.Result() != identity.TemplateParams[0] ||
		!slices.Equal(original.Syntax.Sources().Slots(), []uint32{0}) || !identity.ReturnSources.Equal(original.Syntax.Sources()) || len(original.Syntax.Markers()) != 1 ||
		sema.ValidateDeclaredReturnSources(res.Sema.TypeInterner, original).Status != sema.ReturnSourcesDeferred {
		t.Fatal("PRECONDITION: marked identity lost its original generic roots, source slot or deferred eligibility")
	}
	marker := original.Syntax.Markers()[0]
	if marker.Slot != 0 || marker.Span.File != res.File.ID || marker.Span.End <= marker.Span.Start || marker.ArgumentCount != 0 {
		t.Fatal("PRECONDITION: original marker lost its physical slot or source span")
	}
	for _, instance := range res.Sema.InstantiationClosure.Instances {
		if instance.Template != identity.Symbol {
			continue
		}
		arg := instance.TemplateArgs[0]
		validation := sema.ValidateInstantiatedReturnSources(res.Sema.TypeInterner, original, []types.TypeID{arg}, arg)
		typ, _ := res.Sema.TypeInterner.Lookup(arg)
		logReturnOriginCallEvidence(t, map[string]any{"instance": instance.Key, "actual_type": typ, "original": original, "validation": validation})
		if validation.Status != sema.ReturnSourcesValid {
			t.Fatalf("conditional generic promise rejected its actual specialization: %+v", validation)
		}
	}
}

func checkReturnOriginGenericEscape(t *testing.T, res *DiagnoseResult, analysis *sema.ReturnOriginAnalysis, src, owner, statement string) {
	t.Helper()
	start := strings.Index(src, statement)
	if start < 0 {
		t.Fatal("PRECONDITION: frozen source lost its escaping statement")
	}
	want := source.Span{File: res.File.ID, Start: uint32(start), End: uint32(start + len(statement))}
	var ownerSpan source.Span
	for _, symbol := range res.Symbols.Table.Symbols.Data() {
		if name, _ := res.Symbols.Table.Strings.Lookup(symbol.Name); name == owner {
			ownerSpan = symbol.Span
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"escape_statement": want, "owner": owner, "original_owner_span": ownerSpan})
	if ownerSpan.File != res.File.ID || ownerSpan.Empty() {
		t.Fatal("PRECONDITION: escape owner has no original declaration span")
	}
	for _, diagnostic := range analysis.Diagnostics {
		if diagnostic.Code != diag.SemaBorrowEscapesReturn || diagnostic.Severity != diag.SevError || diagnostic.Primary != want ||
			diagnostic.Message != "borrow of '"+owner+"' outlives its owner when this scope exits" || len(diagnostic.Help) == 0 {
			continue
		}
		for _, note := range diagnostic.Notes {
			if note.Span == ownerSpan && strings.Contains(note.Msg, "owns storage") {
				return
			}
		}
	}
	t.Fatalf("generic body lost the actual %s local-storage escape at %v: %+v", owner, want, analysis)
}
