package driver

import (
	"crypto/sha256"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
)

// Existing clone Pending cannot prove that a particular authority record was
// checked. Each mutation needs its own exact source-site refusal.
func TestAnalyzeTypedDeferredCloneAuthority(t *testing.T) {
	const probes = "fn owned_probe(value: int64) -> int64 { return duplicate(value); }\n" +
		"fn borrowed_probe(value: &string) -> &string { return duplicate::<&string>(value); }\n"
	const mixed = "fn duplicate<T>(value: T) -> T { return clone(&value); }\n" + probes
	const unvisited = "fn duplicate<T>(value: T, result: T) -> T { return result; return clone(&value); }\n" +
		"fn owned_probe(value: int64, result: int64) -> int64 { return duplicate(value, result); }\n" +
		"fn borrowed_probe(value: &string, result: &string) -> &string { return duplicate::<&string>(value, result); }\n"
	for _, tc := range []struct{ name, src, reason string }{
		{"missing_original_use", mixed, "deferred clone lacks its original typed use"},
		{"missing_resolved_outcome", mixed, "deferred clone lacks its finalized outcome"},
		{"receiver_binding", mixed, "deferred clone outcome disagrees with its caller binding"},
		{"duplicate_resolution", mixed, "deferred clone has duplicate finalized outcomes"},
		{"missing_unvisited_outcome", unvisited, "deferred clone lacks its finalized outcome"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_CLONE_AUTHORITY_SOURCE case=%s sha256=%x source=%q", tc.name, sha256.Sum256([]byte(tc.src)), tc.src)
			res := returnOriginStdlibFixture(t, tc.src, false)
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			inputs, unitsErr := collectReturnOriginUnits(res)
			var originalUseLog []map[string]any
			for ref, use := range res.Sema.DeferredCallableUses {
				originalUseLog = append(originalUseLog, map[string]any{"ref": ref, "use": use})
			}
			logReturnOriginCallEvidence(t, map[string]any{"closure_error": errorReturnOriginCallText(closureErr), "unit_error": errorReturnOriginCallText(unitsErr),
				"closure": res.Sema.InstantiationClosure, "roots": res.Sema.InstantiationGraph.Roots(), "deferred_edges": res.Sema.InstantiationGraph.DeferredCallables(),
				"original_uses": originalUseLog, "candidates": res.Sema.CallableCandidates, "expr_types": res.Sema.ExprTypes, "binding_types": res.Sema.BindingTypes})
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 11 || res.Sema.InstantiationIdentity == nil || res.Sema.InstantiationClosure == nil {
				t.Fatal("PRECONDITION: missing real full-module typed input or finalized authority")
			}
			checkReturnOriginStdlibBags(t, res, false)
			rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
			ref, edge, borrowed, owned := checkReturnOriginCloneAuthority(t, res, rootKey, tc.src, tc.name == "missing_unvisited_outcome")
			want := sema.ReturnOriginPending{SourceKey: rootKey, Span: edge.Witness.Site, Reason: tc.reason}
			original, originalUses := res.Sema.InstantiationClosure, res.Sema.DeferredCallableUses
			snapshot := func(closure *sema.InstantiationClosure, uses map[sema.DeferredUseRef]sema.DeferredUseID) []byte {
				var entries []struct {
					Ref sema.DeferredUseRef
					Use sema.DeferredUseID
				}
				for key, use := range uses {
					entries = append(entries, struct {
						Ref sema.DeferredUseRef
						Use sema.DeferredUseID
					}{key, use})
				}
				slices.SortFunc(entries, func(a, b struct {
					Ref sema.DeferredUseRef
					Use sema.DeferredUseID
				}) int {
					return strings.Compare(string(a.Use), string(b.Use))
				})
				raw, err := json.Marshal(map[string]any{"closure": closure, "uses": entries, "roots": res.Sema.InstantiationGraph.Roots(),
					"edges": res.Sema.InstantiationGraph.Edges(), "deferred": res.Sema.InstantiationGraph.DeferredCallables(),
					"expr_types": res.Sema.ExprTypes, "binding_types": res.Sema.BindingTypes})
				if err != nil {
					t.Fatal(err)
				}
				return raw
			}
			originalRaw := snapshot(original, originalUses)
			before, beforeErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "before": before, "before_error": errorReturnOriginCallText(beforeErr), "original_input": json.RawMessage(originalRaw)})
			if !slices.Equal(originalRaw, snapshot(original, originalUses)) {
				t.Fatal("intact analysis mutated original authority or typed maps")
			}
			if beforeErr != nil || before == nil || slices.Contains(before.Pending, want) {
				t.Fatal("PRECONDITION: intact input failed analysis or already reported the selected corruption")
			}
			raw, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var changed sema.InstantiationClosure
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			res.Sema.InstantiationClosure = &changed
			res.Sema.DeferredCallableUses = maps.Clone(originalUses)
			switch tc.name {
			case "missing_original_use":
				delete(res.Sema.DeferredCallableUses, ref)
			case "missing_resolved_outcome", "missing_unvisited_outcome":
				changed.ResolvedDeferredCalls = slices.Delete(changed.ResolvedDeferredCalls, borrowed, borrowed+1)
			case "receiver_binding":
				changed.ResolvedDeferredCalls[borrowed].Receiver = original.ResolvedDeferredCalls[owned].Receiver
			case "duplicate_resolution":
				changed.ResolvedDeferredCalls = append(changed.ResolvedDeferredCalls, sema.CloneResolvedDeferredCallForConsumer(&original.ResolvedDeferredCalls[borrowed]))
			}
			changedRaw := snapshot(&changed, res.Sema.DeferredCallableUses)
			after, afterErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"case": tc.name, "expected_pending": want, "mutated_input": json.RawMessage(changedRaw),
				"after": after, "after_error": errorReturnOriginCallText(afterErr), "typed_diagnostics": res.Bag.Items()})
			if !slices.Equal(originalRaw, snapshot(original, originalUses)) || !slices.Equal(changedRaw, snapshot(&changed, res.Sema.DeferredCallableUses)) {
				t.Fatal("corrupted-input analysis mutated the original or detached input snapshot")
			}
			if afterErr != nil || after == nil || after.Complete() || !slices.Contains(after.Pending, want) {
				t.Fatalf("missing exact deferred clone authority refusal %+v; unrelated Pending is not proof: analysis=%+v error=%v", want, after, afterErr)
			}
		})
	}
}

func checkReturnOriginCloneAuthority(t *testing.T, res *DiagnoseResult, rootKey, src string, unvisited bool) (sema.DeferredUseRef, sema.DeferredCallableEdge, int, int) {
	t.Helper()
	var duplicate sema.CallableCandidate
	var probes []sema.CallableCandidate
	for _, candidate := range res.Sema.CallableCandidates {
		if candidate.Source.File == res.File.ID && candidate.SourceKey == rootKey {
			if candidate.Name == "duplicate" {
				duplicate = candidate
			} else {
				probes = append(probes, candidate)
			}
		}
	}
	var body *ast.FnItem
	for _, item := range res.Builder.Files.Get(res.FileID).Items {
		if fn, ok := res.Builder.Items.Fn(item); ok && fn != nil && fn.NameSpan == duplicate.Source {
			body = fn
		}
	}
	if body == nil || !body.Body.IsValid() || !duplicate.HasBody || duplicate.BodyKey == "" || len(probes) != 2 ||
		len(duplicate.TemplateParams) != 1 || duplicate.ResultType != duplicate.TemplateParams[0] {
		t.Fatal("PRECONDITION: mixed source lost its original duplicate<T> declaration or two real probes")
	}
	param := duplicate.TemplateParams[0]
	wantParams := []types.TypeID{param}
	if unvisited {
		wantParams = append(wantParams, param)
	}
	if !slices.Equal(duplicate.ParamTypes, wantParams) || int(body.ParamsCount) != len(wantParams) {
		t.Fatal("PRECONDITION: original physical parameter slots changed")
	}
	paramInfo, paramOK := res.Sema.TypeInterner.TypeParamInfo(param)
	var edges []sema.DeferredCallableEdge
	for _, edge := range res.Sema.InstantiationGraph.DeferredCallables() {
		if edge.Caller == duplicate.Symbol && edge.Witness.SourceKey == rootKey {
			edges = append(edges, edge)
		}
	}
	if !paramOK || paramInfo == nil || len(edges) != 1 || len(res.Sema.DeferredCallableUses) != 1 {
		t.Fatal("PRECONDITION: missing unique original clone use/edge/type parameter")
	}
	edge := edges[0]
	var ref sema.DeferredUseRef
	for key, use := range res.Sema.DeferredCallableUses {
		if key.Kind == sema.DeferredCloneCall && use == edge.UseID {
			ref = key
		}
	}
	call, ok := res.Builder.Exprs.Call(ref.Expr)
	start := strings.Index(src, "clone(&value)")
	span := source.Span{File: res.File.ID, Start: uint32(start), End: uint32(start + len("clone(&value)"))}
	if !ok || call == nil || len(call.Args) != 1 || start < 0 || res.Builder.Exprs.Get(ref.Expr).Span != span || res.Sema.ExprTypes[ref.Expr] != param ||
		edge.Kind != sema.DeferredCloneCall || edge.UseID == "" || edge.Witness.Caller != duplicate.Symbol || edge.Witness.Site != span ||
		edge.Receiver != param || edge.ExpectedResult != param || edge.Method != "__clone" || edge.StaticReceiver || len(edge.Args) != 0 ||
		len(edge.ExplicitTypeArgs) != 0 || edge.CallerTemplateArity != 1 || len(edge.CallerBindings) != 1 {
		t.Fatal("PRECONDITION: original clone lost its exact typed call and caller authority")
	}
	arg := call.Args[0].Value
	argType, argOK := res.Sema.TypeInterner.Lookup(res.Sema.ExprTypes[arg])
	unary, unaryOK := res.Builder.Exprs.Unary(arg)
	binding := edge.CallerBindings[0]
	if !argOK || argType.Kind != types.KindReference || argType.Mutable || argType.Elem != param || !unaryOK || unary == nil ||
		res.Sema.ExprTypes[unary.Operand] != param || binding.Param != param || uint32(binding.Owner) != paramInfo.Owner || binding.ParamIndex != paramInfo.Index || binding.ArgIndex != 0 {
		t.Fatal("PRECONDITION: original clone receiver lost its whole T binding or parameter owner/index")
	}
	owner := res.Symbols.Table.Symbols.Get(res.Symbols.ExprSymbols[unary.Operand])
	if owner == nil || owner.Type != param || owner.Span.File != res.File.ID {
		t.Fatal("PRECONDITION: borrowed value lacks its original storage owner")
	}
	if unvisited {
		block := res.Builder.Stmts.Block(body.Body)
		if block == nil || len(block.Stmts) != 2 {
			t.Fatal("PRECONDITION: post-return body lost its two original statements")
		}
		first, second := res.Builder.Stmts.Return(block.Stmts[0]), res.Builder.Stmts.Return(block.Stmts[1])
		if first == nil || second == nil || second.Expr != ref.Expr || res.Symbols.ExprSymbols[first.Expr] == res.Symbols.ExprSymbols[unary.Operand] ||
			res.Sema.ExprTypes[first.Expr] != param || res.Builder.Stmts.Get(block.Stmts[0]).Span.End >= span.Start {
			t.Fatal("PRECONDITION: retained clone does not follow a return of a distinct T value")
		}
		params := res.Builder.Items.GetFnParamIDs(body)
		valueParam, resultParam := res.Builder.Items.FnParam(params[0]), res.Builder.Items.FnParam(params[1])
		returned := res.Symbols.Table.Symbols.Get(res.Symbols.ExprSymbols[first.Expr])
		if valueParam == nil || resultParam == nil || returned == nil || returned.Type != param || returned.Span != resultParam.Span || returned.Name != resultParam.Name || owner.Span != valueParam.Span || owner.Name != valueParam.Name {
			t.Fatal("PRECONDITION: return result and clone value lost their distinct original parameter owners")
		}
		logReturnOriginCallEvidence(t, map[string]any{"returned_param": resultParam, "returned_owner": returned, "cloned_param": valueParam, "cloned_owner": owner})
	}
	closure := res.Sema.InstantiationClosure
	instances, uses, roots, resolutions := 0, 0, 0, 0
	for _, instance := range closure.Instances {
		if instance.Template == duplicate.Symbol {
			instances++
		}
	}
	for _, use := range closure.UseSites {
		if use.CalleeTemplate == duplicate.Symbol {
			uses++
		}
	}
	for _, root := range res.Sema.InstantiationGraph.Roots() {
		if root.Template == duplicate.Symbol {
			roots++
		}
	}
	borrowed, owned := -1, -1
	for i, resolved := range closure.ResolvedDeferredCalls {
		if resolved.CallerTemplate != duplicate.Symbol && resolved.UseID != edge.UseID {
			continue
		}
		resolutions++
		actual := resolved.Receiver
		wantActual := []types.TypeID{actual}
		if unvisited {
			wantActual = append(wantActual, actual)
		}
		key, err := sema.NewInstanceKey(*res.Sema.InstantiationIdentity, duplicate.Symbol, []types.TypeID{actual})
		instance, found := closure.Lookup(key)
		if err != nil || !found || instance.Kind != sema.InstantiationFunction || instance.Template != duplicate.Symbol || !slices.Equal(instance.TemplateArgs, []types.TypeID{actual}) ||
			resolved.Caller != key || resolved.CallerTemplate != duplicate.Symbol || !slices.Equal(resolved.CallerTemplateArgs, instance.TemplateArgs) ||
			resolved.UseID != edge.UseID || resolved.Kind != sema.DeferredCloneCall || resolved.Site != span || resolved.SourceKey != rootKey || resolved.ExpectedResult != actual ||
			resolved.Outcome != sema.DeferredCallableBuiltinCopy || resolved.Callee.IsValid() || resolved.CalleeKey != "builtin/copy" || len(resolved.CalleeTemplateArgs) != 0 ||
			len(resolved.CalleeParamTypes) != 0 || resolved.CalleeResultType != types.NoTypeID || len(resolved.Args) != 0 || resolved.StaticReceiver || !res.Sema.TypeInterner.IsCopy(actual) {
			t.Fatal("PRECONDITION: clone outcome lost its exact canonical caller instance or BuiltinCopy shape")
		}
		matched := 0
		for _, use := range closure.UseSites {
			if use.Callee != key {
				continue
			}
			for _, probe := range probes {
				if use.CallerTemplate != probe.Symbol {
					continue
				}
				if use.Kind != sema.InstantiationFunction || use.CalleeTemplate != duplicate.Symbol || use.Caller != (sema.InstanceKey{}) || len(use.CallerTemplateArgs) != 0 ||
					use.SourceKey != rootKey || !slices.Equal(use.TemplateArgs, instance.TemplateArgs) || !slices.Equal(probe.ParamTypes, wantActual) || probe.ResultType != actual || !slices.Contains(closure.LiveCallables, probe.Symbol) {
					t.Fatal("PRECONDITION: clone instance lacks its original source caller")
				}
				for _, root := range res.Sema.InstantiationGraph.Roots() {
					if root.Template != duplicate.Symbol || root.Witness.Caller != probe.Symbol || root.Witness.Site != use.Site || !slices.Equal(root.TemplateArgs, instance.TemplateArgs) {
						continue
					}
					for expr, typ := range res.Sema.ExprTypes {
						if node := res.Builder.Exprs.Get(expr); node != nil && node.Kind == ast.ExprCall && node.Span == use.Site && typ == actual && res.Symbols.ExprSymbols[expr] == duplicate.Symbol {
							selected, ok := res.Builder.Exprs.Call(expr)
							if ok && selected != nil && len(selected.Args) == len(wantActual) {
								var argTypes []types.TypeID
								for _, arg := range selected.Args {
									argTypes = append(argTypes, res.Sema.ExprTypes[arg.Value])
								}
								if slices.Equal(argTypes, wantActual) {
									matched++
								}
							}
						}
					}
				}
			}
		}
		if matched != 1 {
			t.Fatal("PRECONDITION: clone instance lacks its unique actual typed root/use")
		}
		typ, _ := res.Sema.TypeInterner.Lookup(actual)
		if typ.Kind == types.KindReference {
			borrowed = i
		} else {
			owned = i
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"component_instances": instances, "component_uses": uses, "component_roots": roots, "component_resolutions": resolutions,
		"original_ref": ref, "original_edge": edge, "original_param": paramInfo, "storage_owner": owner, "call": call, "borrowed_outcome_index": borrowed, "owned_outcome_index": owned, "unvisited": unvisited})
	if instances != 2 || uses != 2 || roots != 2 || resolutions != 2 || borrowed < 0 || owned < 0 || borrowed == owned {
		t.Fatal("PRECONDITION: mixed source lost its two real distinct roots/uses/instances/clone outcomes")
	}
	return ref, edge, borrowed, owned
}
