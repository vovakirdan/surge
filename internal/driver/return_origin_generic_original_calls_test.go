package driver

import (
	"crypto/sha256"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/types"
)

// An original call has source authority even when no concrete instance uses
// its enclosing generic body. Current-use corruption controls remain separate.
func TestAnalyzeOriginalGenericCallsWithoutInstances(t *testing.T) {
	const identity = "fn identity<T>(value: T) -> T { return value; }\n"
	for _, tc := range []struct {
		name, body string
		symbolic   bool
		sources    []uint32
		escape     bool
	}{
		{"symbolic_edge", "fn bridge<T>(value: T) -> T { return identity::<T>(value); }\n", true, []uint32{0}, false},
		{"owned_edge", "fn bridge<T>(value: int64, unused: T) -> int64 { return identity::<int64>(value); }\n", false, nil, false},
		{"borrowed_edge", "fn bridge<T>(value: &string, unused: T) -> &string { return identity::<&string>(value); }\n", false, []uint32{0}, false},
		{"inner_escape_edge", "fn bridge<T>(value: T) -> int64 { let escaped = { let owned: int64 = 7; ret identity::<&int64>(&owned); }; return 1; }\n", false, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := identity + tc.body
			t.Logf("RETURN_ORIGIN_ORIGINAL_GENERIC_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(src)), src)
			res := returnOriginTypedFixtureWithEscapeEvidence(t, src, tc.escape)
			if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
				t.Fatalf("PRECONDITION: original source closure failed: %v", err)
			}
			inputs, err := collectReturnOriginUnits(res)
			if err != nil || len(inputs.units) != 1 || res.Sema.InstantiationClosure == nil || res.Sema.InstantiationIdentity == nil {
				t.Fatalf("PRECONDITION: original source lost its unit or finalized authority: %v", err)
			}
			closure := res.Sema.InstantiationClosure
			var callee, caller sema.CallableCandidate
			for _, c := range res.Sema.CallableCandidates {
				if c.Name == "identity" {
					callee = c
				} else if c.Name == "bridge" {
					caller = c
				}
			}
			if len(res.Sema.CallableCandidates) != 2 || !callee.Symbol.IsValid() || !caller.Symbol.IsValid() ||
				len(callee.TemplateParams) != 1 || len(caller.TemplateParams) != 1 || len(callee.ParamTypes) != 1 ||
				callee.ParamTypes[0] != callee.TemplateParams[0] || callee.ResultType != callee.TemplateParams[0] ||
				!callee.HasBody || !caller.HasBody || callee.SourceKey != inputs.units[0].SourceKey || caller.SourceKey != callee.SourceKey ||
				len(closure.Instances) != 0 || len(closure.UseSites) != 0 || len(closure.ResolvedDeferredCalls) != 0 {
				t.Fatal("PRECONDITION: source is not two original generic bodies with zero concrete instances")
			}
			var expression ast.ExprID
			for id, typ := range res.Sema.ExprTypes {
				if node := res.Builder.Exprs.Get(id); node != nil && node.Kind == ast.ExprCall {
					if expression.IsValid() || typ == types.NoTypeID {
						t.Fatal("PRECONDITION: original source lacks exactly one typed call")
					}
					expression = id
				}
			}
			call, ok := res.Builder.Exprs.Call(expression)
			if !ok || call == nil || len(call.Args) != 1 || res.Symbols.ExprSymbols[expression] != callee.Symbol ||
				res.Sema.ExprTypes[call.Args[0].Value] != res.Sema.ExprTypes[expression] {
				t.Fatal("PRECONDITION: call lost its selected identity or exact typed direct argument")
			}
			span := res.Builder.Exprs.Get(expression).Span
			roots, edges := res.Sema.InstantiationGraph.Roots(), res.Sema.InstantiationGraph.Edges()
			if len(roots) != 0 || len(edges) != 1 || edges[0].Caller != caller.Symbol || edges[0].Callee != callee.Symbol ||
				edges[0].Witness.Site != span || edges[0].Witness.SourceKey != caller.SourceKey ||
				edges[0].CallerTemplateArity != 1 || len(edges[0].CallerBindings) != 1 ||
				edges[0].CallerBindings[0].Param != caller.TemplateParams[0] {
				t.Fatal("PRECONDITION: call lost its unique original generic edge")
			}
			args := edges[0].CalleeTemplateArgs
			if !slices.Equal(args, []types.TypeID{res.Sema.ExprTypes[expression]}) ||
				(tc.symbolic && args[0] != caller.TemplateParams[0]) || (!tc.symbolic && types.ContainsGenericParam(res.Sema.TypeInterner, args[0])) {
				t.Fatal("PRECONDITION: original call graph argument differs from its actual typed source")
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "original_generic_analysis", "case": tc.name,
				"caller": caller, "callee": callee, "roots": roots, "edges": edges, "closure": closure,
				"call": call, "call_span": span, "expr_types": res.Sema.ExprTypes, "publication": inputs.units[0].Publication,
				"original_diagnostics": res.Bag.Items(), "analysis": analysis, "error": errorReturnOriginCallText(err)})
			if err != nil || analysis == nil {
				t.Fatalf("PRECONDITION: original body traversal failed: %v", err)
			}
			bridge := requireReturnOriginSummary(t, analysis, "bridge")
			if bridge.BodyKey != caller.BodyKey || bridge.Source != caller.Source || bridge.NoNormalReturn || bridge.Unknown || !slices.Equal(bridge.ParamSlots, tc.sources) {
				t.Errorf("original bridge lost its actual return sources: %+v want=%v", bridge, tc.sources)
			}
			if tc.escape {
				checkReturnOriginGenericEscape(t, res, analysis, src, "owned", "ret identity::<&int64>(&owned);")
				return
			}
			if !analysis.Complete() || len(analysis.Diagnostics) != 0 {
				t.Fatalf("original call without concrete instances has no complete proof: %+v", analysis)
			}
		})
	}
}
