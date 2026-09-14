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
	"surge/internal/types"
)

// The last use is recorded by ordinary SEMA after an explicit return, although
// origin flow has already ended. Its contract still belongs to the closure.
func TestAnalyzeTypedGenericReturnOriginCoverage(t *testing.T) {
	const original = "fn choose<T>(@return_source a: T, b: T) -> T { return b; }\n"
	for _, tc := range []struct{ name, tail string }{
		{"template_only", ""},
		{"owned_vacuous", "fn probe(a: int64, b: int64) -> int64 { return choose(a, b); }\n"},
		{"borrowed_rejected", "fn probe(a: &string, b: &string) -> &string { return choose::<&string>(a, b); }\n"},
		{"unvisited_borrowed_rejected", "fn probe(a: &string, b: &string) -> &string { return a; return choose::<&string>(a, b); }\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := original + tc.tail
			t.Logf("RETURN_ORIGIN_GENERIC_COVERAGE_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(src)), src)
			res := returnOriginTypedFixtureWithEscapeEvidence(t, src, false)
			if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
				t.Fatalf("PRECONDITION: actual generic closure: %v", err)
			}
			inputs, err := collectReturnOriginUnits(res)
			if err != nil || len(inputs.units) != 1 || res.Sema.InstantiationIdentity == nil || res.Sema.InstantiationClosure == nil {
				t.Fatalf("PRECONDITION: missing real owning unit or finalized authority: %v", err)
			}
			var choose, probe sema.CallableCandidate
			for _, candidate := range res.Sema.CallableCandidates {
				if candidate.Source.File == res.File.ID && candidate.SourceKey == inputs.units[0].SourceKey {
					if candidate.Name == "choose" {
						choose = candidate
					} else if candidate.Name == "probe" {
						probe = candidate
					}
				}
			}
			closure := res.Sema.InstantiationClosure
			roots := res.Sema.InstantiationGraph.Roots()
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "choose": choose, "probe": probe, "closure": closure,
				"roots": roots, "expr_types": res.Sema.ExprTypes, "original_requests": res.Sema.ReturnSourceDeclarations, "typed_diagnostics": res.Bag.Items()})
			if !choose.Symbol.IsValid() || !choose.HasBody || choose.BodyKey == "" || len(choose.TemplateParams) != 1 || len(res.Sema.ReturnSourceDeclarations) != 1 {
				t.Fatal("PRECONDITION: original generic declaration/body is missing")
			}
			request := res.Sema.ReturnSourceDeclarations[0]
			param := choose.TemplateParams[0]
			marker := source.Span{File: res.File.ID, Start: 13, End: 27}
			if request.Owner != choose.Symbol || !slices.Equal(request.Params(), []types.TypeID{param, param}) || request.Result() != param ||
				!slices.Equal(choose.ParamTypes, request.Params()) || choose.ResultType != param || !request.Syntax.Sources().Equal(choose.ReturnSources) ||
				!slices.Equal(request.Syntax.Sources().Slots(), []uint32{0}) || len(request.Syntax.Markers()) != 1 || request.Syntax.Markers()[0].Span != marker ||
				sema.ValidateDeclaredReturnSources(res.Sema.TypeInterner, request).Status != sema.ReturnSourcesDeferred {
				t.Fatal("PRECONDITION: original T roots, marked slot, marker or deferred eligibility changed")
			}
			var primary source.Span
			if tc.tail == "" {
				if len(closure.Instances) != 0 || len(closure.UseSites) != 0 || len(roots) != 0 || probe.Symbol.IsValid() {
					t.Fatal("PRECONDITION: template-only source acquired a concrete use")
				}
			} else {
				if !probe.Symbol.IsValid() || !slices.Contains(closure.LiveCallables, probe.Symbol) || len(closure.Instances) != 1 || len(closure.UseSites) != 1 || len(roots) != 1 {
					t.Fatal("PRECONDITION: actual probe/root/instance/use census is missing")
				}
				use, instance, root := closure.UseSites[0], closure.Instances[0], roots[0]
				if len(use.TemplateArgs) != 1 || len(probe.ParamTypes) != 2 {
					t.Fatal("PRECONDITION: actual generic use/formal arity changed")
				}
				arg := use.TemplateArgs[0]
				callText := "choose::<&string>(a, b)"
				if tc.name == "owned_vacuous" {
					callText = "choose(a, b)"
				}
				start := strings.Index(src, callText)
				if start < 0 {
					t.Fatal("PRECONDITION: frozen concrete source call is missing")
				}
				primary = source.Span{File: res.File.ID, Start: uint32(start), End: uint32(start + len(callText))}
				key, keyErr := sema.NewInstanceKey(*res.Sema.InstantiationIdentity, choose.Symbol, []types.TypeID{arg})
				rootSource := root.Witness.SourceKey
				if rootSource == "" {
					rootSource, err = res.Sema.InstantiationIdentity.ResolveSource(root.Witness.Site.File)
				}
				if keyErr != nil || err != nil || use.Callee != key || instance.Key != key || instance.Template != choose.Symbol ||
					use.CalleeTemplate != choose.Symbol || root.Template != choose.Symbol || use.Caller != (sema.InstanceKey{}) ||
					use.CallerTemplate != probe.Symbol || root.Witness.Caller != probe.Symbol || use.Kind != sema.InstantiationFunction ||
					instance.Kind != use.Kind || root.Kind != use.Kind || !slices.Equal(use.TemplateArgs, instance.TemplateArgs) || !slices.Equal(root.TemplateArgs, use.TemplateArgs) ||
					use.SourceKey != inputs.units[0].SourceKey || rootSource != use.SourceKey || use.Site != primary || root.Witness.Site != primary {
					t.Fatal("PRECONDITION: concrete use lost its original root/caller/source/instance authority")
				}
				var callID ast.ExprID
				for id, typ := range res.Sema.ExprTypes {
					if node := res.Builder.Exprs.Get(id); node != nil && node.Span == primary && node.Kind == ast.ExprCall && typ == arg {
						callID = id
					}
				}
				call, ok := res.Builder.Exprs.Call(callID)
				if !ok || call == nil || len(call.Args) != 2 || res.Symbols.ExprSymbols[callID] != choose.Symbol ||
					res.Sema.ExprTypes[call.Args[0].Value] != arg || res.Sema.ExprTypes[call.Args[1].Value] != arg ||
					!slices.Equal(probe.ParamTypes, []types.TypeID{arg, arg}) || probe.ResultType != arg {
					t.Fatal("PRECONDITION: recorded call lacks its actual typed expressions")
				}
				validation := sema.ValidateInstantiatedReturnSources(res.Sema.TypeInterner, request, []types.TypeID{arg, arg}, arg)
				logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "original": request, "concrete_eligibility": validation, "typed_call": callID})
				if validation.Status != sema.ReturnSourcesValid {
					t.Fatal("PRECONDITION: relation witness instead failed conditional input/result eligibility")
				}
				if tc.name == "unvisited_borrowed_rejected" {
					var body *ast.BlockStmt
					for _, item := range res.Builder.Files.Get(res.FileID).Items {
						if fn, ok := res.Builder.Items.Fn(item); ok && fn != nil && fn.NameSpan == probe.Source {
							body = res.Builder.Stmts.Block(fn.Body)
						}
					}
					if body == nil || len(body.Stmts) != 2 {
						t.Fatal("PRECONDITION: probe lacks its two actual sequential returns")
					}
					first, second := res.Builder.Stmts.Return(body.Stmts[0]), res.Builder.Stmts.Return(body.Stmts[1])
					if first == nil || second == nil || second.Expr != callID || res.Symbols.ExprSymbols[first.Expr] != res.Symbols.ExprSymbols[call.Args[0].Value] ||
						res.Builder.Stmts.Get(body.Stmts[0]).Span.End >= primary.Start {
						t.Fatal("PRECONDITION: retained concrete call is not after the explicit return of input a")
					}
				}
			}
			analysis, analysisErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "analysis": analysis, "error": errorReturnOriginCallText(analysisErr), "primary": primary, "marker": marker})
			if analysisErr != nil || analysis == nil || !analysis.Complete() {
				t.Fatalf("conditional source relation did not complete: %+v error=%v", analysis, analysisErr)
			}
			actual := requireReturnOriginSummary(t, analysis, "choose")
			if actual.BodyKey != choose.BodyKey || actual.Source != choose.Source || actual.Unknown || actual.NoNormalReturn || !slices.Equal(actual.ParamSlots, []uint32{1}) {
				t.Fatalf("original generic actual sources were replaced by its declared bound: %+v", actual)
			}
			if tc.tail != "" {
				var want []uint32
				if tc.name == "borrowed_rejected" {
					want = []uint32{1}
				} else if tc.name == "unvisited_borrowed_rejected" {
					want = []uint32{0}
				}
				value := requireReturnOriginSummary(t, analysis, "probe")
				if value.BodyKey != probe.BodyKey || value.Source != probe.Source || value.Unknown || value.NoNormalReturn || !slices.Equal(value.ParamSlots, want) {
					t.Fatalf("probe flow lost the actual reachable result: %+v", value)
				}
			}
			if tc.name == "template_only" || tc.name == "owned_vacuous" {
				if len(analysis.Diagnostics) != 0 {
					t.Fatalf("conditional borrowed promise rejected template or owned result: %+v", analysis.Diagnostics)
				}
				return
			}
			if len(analysis.Diagnostics) != 1 {
				t.Fatalf("conditional borrowed use lacks its unique wrong-source diagnostic: %+v", analysis.Diagnostics)
			}
			d := analysis.Diagnostics[0]
			if d.Code != diag.SemaError || d.Severity != diag.SevError || d.Message != returnOriginWrongInputMessage || d.Primary != primary || len(d.Help) == 0 ||
				!slices.ContainsFunc(d.Notes, func(note diag.Note) bool {
					return note.Span == marker && note.Msg == "this marker limits the possible input sources of returned references"
				}) {
				t.Fatalf("conditional borrowed-source diagnostic lost its actual call or original marker: %+v", d)
			}
		})
	}
}
