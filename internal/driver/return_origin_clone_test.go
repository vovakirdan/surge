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

// Clone's source target is not its selected operation. These cases require the
// existing typed deferred edge before asking about content or owner escape.
// This is a root-component proof; full core Pending remains visible below.
func TestAnalyzeTypedDeferredCloneOrigins(t *testing.T) {
	for _, tc := range []struct{ name, src, escape string }{
		{"template_content", "fn duplicate<T>(value: T) -> T { return clone(&value); }\n", ""},
		{"owned_copy", "fn duplicate<T>(value: T) -> T { return clone(&value); }\nfn probe(value: int64) -> int64 { return duplicate(value); }\n", ""},
		{"borrowed_copy", "fn duplicate<T>(value: T) -> T { return clone(&value); }\nfn probe(value: &string) -> &string { return duplicate::<&string>(value); }\n", ""},
		{"local_value_alias", "fn duplicate<T>(value: T) -> T { let saved = value; return clone(&saved); }\n", ""},
		{"local_shared_alias", "fn duplicate<T>(value: T) -> T { let borrowed = &value; return clone(borrowed); }\n", ""},
		{"local_address_rejected", "fn duplicate<T>(value: T) -> &T { let saved = clone(&value); return &saved; }\n", "return &saved;"},
		{"ref_free_inner_escape", "fn duplicate<T>(value: T) -> int64 { let alias = { let saved = clone(&value); ret &saved; }; return 0:int64; }\n", "ret &saved;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_CLONE_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			res := returnOriginStdlibFixture(t, tc.src, tc.escape != "")
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			inputs, unitsErr := collectReturnOriginUnits(res)
			var deferredUses []map[string]any
			for ref, use := range res.Sema.DeferredCallableUses {
				deferredUses = append(deferredUses, map[string]any{"ref": ref, "use": use})
			}
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "closure_error": errorReturnOriginCallText(closureErr),
				"unit_error": errorReturnOriginCallText(unitsErr), "closure": res.Sema.InstantiationClosure,
				"deferred_uses": deferredUses, "deferred_edges": res.Sema.InstantiationGraph.DeferredCallables(),
				"roots": res.Sema.InstantiationGraph.Roots(), "candidates": res.Sema.CallableCandidates,
				"expr_types": res.Sema.ExprTypes, "binding_types": res.Sema.BindingTypes, "typed_diagnostics": res.Bag.Items()})
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 || res.Sema.InstantiationIdentity == nil || res.Sema.InstantiationClosure == nil {
				t.Fatal("PRECONDITION: source lacks its original unit and real finalized authority")
			}
			checkReturnOriginStdlibBags(t, res, tc.escape != "")
			rootKey, coreKeys := checkReturnOriginCloneUnits(t, res, inputs.units)
			var duplicate, probe sema.CallableCandidate
			for _, candidate := range res.Sema.CallableCandidates {
				if candidate.Source.File != res.File.ID || candidate.SourceKey != rootKey {
					continue
				}
				if candidate.Name == "duplicate" {
					duplicate = candidate
				} else if candidate.Name == "probe" {
					probe = candidate
				}
			}
			var body *ast.FnItem
			for _, item := range res.Builder.Files.Get(res.FileID).Items {
				if fn, ok := res.Builder.Items.Fn(item); ok && fn != nil && fn.NameSpan == duplicate.Source {
					body = fn
				}
			}
			if body == nil || !body.Body.IsValid() || !duplicate.HasBody || duplicate.BodyKey == "" ||
				len(duplicate.TemplateParams) != 1 || !slices.Equal(duplicate.ParamTypes, duplicate.TemplateParams) {
				t.Fatal("PRECONDITION: original duplicate<T> body or exact T input is missing")
			}
			param := duplicate.TemplateParams[0]
			paramInfo, paramOK := res.Sema.TypeInterner.TypeParamInfo(param)
			var edges []sema.DeferredCallableEdge
			for _, edge := range res.Sema.InstantiationGraph.DeferredCallables() {
				if edge.Witness.SourceKey == rootKey && edge.Caller == duplicate.Symbol {
					edges = append(edges, edge)
				}
			}
			if !paramOK || paramInfo == nil || len(edges) != 1 || len(res.Sema.DeferredCallableUses) != 1 {
				t.Fatal("PRECONDITION: source lacks its unique original clone use/edge/type parameter")
			}
			edge := edges[0]
			var callID ast.ExprID
			for ref, use := range res.Sema.DeferredCallableUses {
				if ref.Kind == sema.DeferredCloneCall && use == edge.UseID {
					callID = ref.Expr
				}
			}
			call, callOK := res.Builder.Exprs.Call(callID)
			if !callOK || call == nil || len(call.Args) != 1 || res.Sema.ExprTypes[callID] != param {
				t.Fatal("PRECONDITION: original clone use is not the typed one-argument T result")
			}
			span := res.Builder.Exprs.Get(callID).Span
			arg := call.Args[0].Value
			argType, argOK := res.Sema.TypeInterner.Lookup(res.Sema.ExprTypes[arg])
			if !argOK || argType.Kind != types.KindReference || argType.Mutable || argType.Elem != param ||
				edge.Kind != sema.DeferredCloneCall || edge.UseID == "" || edge.Caller != duplicate.Symbol || edge.Witness.Caller != duplicate.Symbol ||
				edge.Witness.SourceKey != rootKey || edge.Witness.Site != span || edge.Receiver != param || edge.ExpectedResult != param ||
				edge.Method != "__clone" || edge.StaticReceiver || len(edge.Args) != 0 || len(edge.ExplicitTypeArgs) != 0 ||
				edge.CallerTemplateArity != 1 || len(edge.CallerBindings) != 1 {
				t.Fatal("PRECONDITION: clone lost its original caller/source/receiver/result/argument authority")
			}
			binding := edge.CallerBindings[0]
			if binding.Param != param || uint32(binding.Owner) != paramInfo.Owner || binding.ParamIndex != paramInfo.Index || binding.ArgIndex != 0 {
				t.Fatal("PRECONDITION: clone lost its original type-parameter owner/index binding")
			}
			var operand ast.ExprID
			if tc.name == "local_shared_alias" {
				operand = arg
			} else {
				unary, ok := res.Builder.Exprs.Unary(arg)
				if !ok || unary == nil || res.Sema.ExprTypes[unary.Operand] != param {
					t.Fatal("PRECONDITION: clone no longer borrows the whole original T value")
				}
				operand = unary.Operand
			}
			operandSymbol := res.Symbols.Table.Symbols.Get(res.Symbols.ExprSymbols[operand])
			if operandSymbol == nil || operandSymbol.Span.File != res.File.ID || operandSymbol.Type != res.Sema.ExprTypes[operand] {
				t.Fatal("PRECONDITION: cloned binding lacks its actual original type and owner")
			}
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "typed_call": callID, "call": call, "call_span": span,
				"target": res.Builder.Exprs.Get(call.Target), "target_type": res.Sema.ExprTypes[call.Target], "target_symbol": res.Symbols.ExprSymbols[call.Target],
				"operand": operand, "operand_owner": operandSymbol, "original_param": paramInfo, "edge": edge})
			closure := res.Sema.InstantiationClosure
			var instances []sema.InstantiationInstance
			var uses []sema.ConcreteInstantiationUse
			var resolutions []sema.ResolvedDeferredCall
			var roots []sema.InstantiationRoot
			for _, instance := range closure.Instances {
				if instance.Template == duplicate.Symbol {
					instances = append(instances, instance)
				}
			}
			for _, use := range closure.UseSites {
				if use.CalleeTemplate == duplicate.Symbol {
					uses = append(uses, use)
				}
			}
			for _, resolution := range closure.ResolvedDeferredCalls {
				if resolution.CallerTemplate == duplicate.Symbol || resolution.UseID == edge.UseID {
					resolutions = append(resolutions, resolution)
				}
			}
			for _, root := range res.Sema.InstantiationGraph.Roots() {
				if root.Template == duplicate.Symbol {
					roots = append(roots, root)
				}
			}
			logReturnOriginCallEvidence(t, map[string]any{"root_component_instances": instances, "root_component_uses": uses,
				"root_component_resolutions": resolutions, "root_component_roots": roots})
			if !probe.Symbol.IsValid() {
				if len(instances) != 0 || len(uses) != 0 || len(resolutions) != 0 || len(roots) != 0 {
					t.Fatal("PRECONDITION: generic-only source acquired a hidden concrete use")
				}
			} else {
				if len(probe.ParamTypes) != 1 || len(instances) != 1 || len(uses) != 1 || len(resolutions) != 1 || len(roots) != 1 {
					t.Fatal("PRECONDITION: concrete source lost its root/instance/use/resolved-clone census")
				}
				actualType := probe.ParamTypes[0]
				instance, use, resolved := instances[0], uses[0], resolutions[0]
				root := roots[0]
				key, keyErr := sema.NewInstanceKey(*res.Sema.InstantiationIdentity, duplicate.Symbol, []types.TypeID{actualType})
				if keyErr != nil || instance.Key != key || instance.Template != duplicate.Symbol || instance.Kind != sema.InstantiationFunction ||
					!slices.Equal(instance.TemplateArgs, []types.TypeID{actualType}) || use.Callee != key || use.CalleeTemplate != duplicate.Symbol ||
					use.CallerTemplate != probe.Symbol || use.Caller != (sema.InstanceKey{}) || use.SourceKey != edge.Witness.SourceKey ||
					!slices.Equal(use.TemplateArgs, instance.TemplateArgs) || root.Template != duplicate.Symbol || root.Witness.Caller != probe.Symbol ||
					root.Witness.Site != use.Site || !slices.Equal(root.TemplateArgs, instance.TemplateArgs) || !slices.Contains(closure.LiveCallables, probe.Symbol) {
					t.Fatal("PRECONDITION: concrete generic use lost its exact original root and instance identity")
				}
				if resolved.UseID != edge.UseID || resolved.Caller != key || resolved.CallerTemplate != duplicate.Symbol ||
					!slices.Equal(resolved.CallerTemplateArgs, instance.TemplateArgs) || resolved.Kind != sema.DeferredCloneCall ||
					resolved.SourceKey != edge.Witness.SourceKey || resolved.Site != span || resolved.Receiver != actualType || resolved.ExpectedResult != actualType ||
					resolved.Outcome != sema.DeferredCallableBuiltinCopy || resolved.Callee.IsValid() || resolved.CalleeKey != "builtin/copy" ||
					len(resolved.CalleeTemplateArgs) != 0 || len(resolved.CalleeParamTypes) != 0 || resolved.CalleeResultType != types.NoTypeID ||
					len(resolved.Args) != 0 || resolved.StaticReceiver || !res.Sema.TypeInterner.IsCopy(actualType) {
					t.Fatal("PRECONDITION: concrete clone lacks its exact existing BuiltinCopy outcome")
				}
				typedUse := false
				for id, typ := range res.Sema.ExprTypes {
					if node := res.Builder.Exprs.Get(id); node != nil && node.Kind == ast.ExprCall && node.Span == use.Site && typ == actualType && res.Symbols.ExprSymbols[id] == duplicate.Symbol {
						typedUse = true
					}
				}
				if !typedUse {
					t.Fatal("PRECONDITION: finalized generic root is not an actual typed source call")
				}
			}
			var escapeSpan, ownerSpan source.Span
			if tc.escape != "" {
				start := strings.Index(tc.src, tc.escape)
				for _, symbol := range res.Symbols.Table.Symbols.Data() {
					if name, _ := res.Symbols.Table.Strings.Lookup(symbol.Name); name == "saved" && symbol.Span.File == res.File.ID {
						ownerSpan = symbol.Span
					}
				}
				if start < 0 || ownerSpan.Empty() || ownerSpan.File != res.File.ID {
					t.Fatal("PRECONDITION: frozen negative lost its original statement or saved owner")
				}
				escapeSpan = source.Span{File: res.File.ID, Start: uint32(start), End: uint32(start + len(tc.escape))}
				logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "expected_escape": escapeSpan, "expected_owner": ownerSpan})
			}
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "analysis": analysis, "analysis_error": errorReturnOriginCallText(err), "typed_diagnostics": res.Bag.Items()})
			if err != nil || analysis == nil {
				t.Fatalf("deferred clone content analysis did not reach its source result: %v", err)
			}
			var componentPending []sema.ReturnOriginPending
			corePending := make(map[string]map[string]int)
			for _, pending := range analysis.Pending {
				if pending.SourceKey == rootKey {
					componentPending = append(componentPending, pending)
					continue
				}
				if !coreKeys[pending.SourceKey] {
					t.Fatalf("PRECONDITION: Pending lost its original owning source: %+v", pending)
				}
				if corePending[pending.SourceKey] == nil {
					corePending[pending.SourceKey] = make(map[string]int)
				}
				corePending[pending.SourceKey][pending.Reason]++
			}
			logReturnOriginCallEvidence(t, map[string]any{"root_component_pending": componentPending, "unfinished_core_by_source_reason": corePending, "full_complete": analysis.Complete()})
			summary := requireReturnOriginSummary(t, analysis, "duplicate")
			if summary.BodyKey != duplicate.BodyKey || summary.Source != duplicate.Source {
				t.Fatal("clone summary lost its original body identity")
			}
			if tc.escape != "" {
				if tc.name == "ref_free_inner_escape" && (summary.Unknown || summary.NoNormalReturn || len(summary.ParamSlots) != 0) {
					t.Fatalf("reference-free result changed the inner owner-escape subject: %+v", summary)
				}
				found := false
				for _, d := range analysis.Diagnostics {
					if d.Code == diag.SemaBorrowEscapesReturn && d.Severity == diag.SevError && d.Primary == escapeSpan &&
						d.Message == "borrow of 'saved' outlives its owner when this scope exits" && len(d.Help) != 0 &&
						slices.ContainsFunc(d.Notes, func(note diag.Note) bool { return note.Span == ownerSpan && strings.Contains(note.Msg, "owns storage") }) {
						found = true
					}
				}
				if !found {
					t.Fatalf("clone lost exact saved owner escape at %v: %+v", escapeSpan, analysis.Diagnostics)
				}
				return
			}
			if len(componentPending) != 0 || len(analysis.Diagnostics) != 0 || summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, []uint32{0}) {
				t.Fatalf("root clone component lost its Param0 proof (full core completion is separate): %+v", analysis)
			}
			if probe.Symbol.IsValid() {
				want := []uint32(nil)
				if tc.name == "borrowed_copy" {
					want = []uint32{0}
				}
				actual := requireReturnOriginSummary(t, analysis, "probe")
				if actual.BodyKey != probe.BodyKey || actual.Source != probe.Source || actual.Unknown || actual.NoNormalReturn || !slices.Equal(actual.ParamSlots, want) {
					t.Fatalf("concrete clone result lost its actual incoming content: %+v", actual)
				}
			}
		})
	}
}
