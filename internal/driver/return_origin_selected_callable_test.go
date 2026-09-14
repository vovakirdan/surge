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
	"surge/internal/symbols"
)

const selectedCallableDependency = "pragma module::dep, no_std;\npub fn first(a: &string, b: &string) -> &string { return a; }\n"
const selectedCallableApp = "pragma module::app, no_std;\nimport dep as Other;\n"
const selectedCallableDirect = "fn probe(a: &string, b: &string) -> &string { return Other.first(a, b); }\n"
const selectedCallableWide = `fn probe(a: &string, b: &string) -> &string {
    let wide: fn(&string, &string) -> &string = Other.first;
    let copied = wide;
    return copied(a, b);
}
`

// BEFORE records actual dispatch/type/publication gates before asserting the
// requested result. Full core obligations remain visible and are not waived.
func TestAnalyzeTypedSelectedCallableAuthority(t *testing.T) {
	for _, name := range []string{"selected_map", "imported_body", "missing_mapping", "rebound_mapping", "ambiguous_mapping", "widened_destination"} {
		t.Run(name, func(t *testing.T) {
			var res *DiagnoseResult
			var mapCase storageP0Case
			imported := name == "imported_body" || name == "widened_destination"
			if imported {
				body := selectedCallableDirect
				if name == "widened_destination" {
					body = selectedCallableWide
				}
				res = selectedCallableModuleFixture(t, selectedCallableApp+body)
			} else {
				for _, tc := range storageP0Cases {
					if tc.name == "p11_replace_effects" {
						mapCase = tc
					}
				}
				if mapCase.source == "" {
					t.Fatal("PRECONDITION: frozen P11 source is missing")
				}
				res = returnOriginStdlibFixture(t, mapCase.source, false)
			}
			logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_source", "case": name, "source": string(res.File.Content), "sha256": sha256.Sum256(res.File.Content)})
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_closure", "case": name, "error": errorReturnOriginCallText(closureErr)})
			checkReturnOriginStdlibBags(t, res, false)
			inputs, unitErr := collectReturnOriginUnits(res)
			wantUnits := 11
			if imported {
				wantUnits = 12
			}
			if closureErr != nil || unitErr != nil || len(inputs.units) != wantUnits || res.Sema.InstantiationClosure == nil {
				t.Fatalf("PRECONDITION: full original input/closure: units=%d want=%d error=%v/%v", len(inputs.units), wantUnits, closureErr, unitErr)
			}
			rootIndex := -1
			seen := make(map[string]bool)
			for i, u := range inputs.units {
				file := res.FileSet.Get(u.Builder.Files.Get(u.FileID).Span.File)
				logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_unit", "source_key": u.SourceKey, "sha256": sha256.Sum256(file.Content), "bag": inputs.bags[file.ID].Items()})
				if seen[u.SourceKey] || inputs.bags[file.ID].HasErrors() {
					t.Fatal("PRECONDITION: duplicate owner or original source error")
				}
				seen[u.SourceKey] = true
				if file.ID == res.File.ID {
					rootIndex = i
				}
			}
			if rootIndex < 0 {
				t.Fatal("PRECONDITION: original caller unit missing")
			}
			root := &inputs.units[rootIndex]
			if strings.HasSuffix(name, "_mapping") {
				for i := range inputs.units {
					if inputs.units[i].SourceKey == "core/map.sg" {
						root = &inputs.units[i]
					}
				}
			}
			for _, key := range []string{"array", "base", "entrypoint", "format", "intrinsics", "map", "option", "result", "string", "sync"} {
				if !seen["core/"+key+".sg"] {
					t.Fatal("PRECONDITION: original core owner was filtered out")
				}
			}
			for _, witness := range res.Sema.InstantiationGraph.Roots() {
				if witness.Witness.Site.File == root.Builder.Files.Get(root.FileID).Span.File {
					logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_original_root", "root": witness})
				}
			}
			for _, caller := range res.Sema.CallableCandidates {
				if caller.Source.File == root.Builder.Files.Get(root.FileID).Span.File {
					logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_caller", "candidate": caller, "calls": res.Sema.FunctionCallEdges[caller.Symbol]})
				}
			}
			for _, use := range res.Sema.InstantiationClosure.UseSites {
				if use.Site.File == root.Builder.Files.Get(root.FileID).Span.File {
					instance, _ := res.Sema.InstantiationClosure.Lookup(use.Callee)
					logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_current_use", "use": use, "instance": instance})
				}
			}
			text, kind := "m.insert(clone(&key), value)", ast.ExprCall
			if root.SourceKey == "core/map.sg" {
				text = "rt_map_insert(self, key, value)"
			}
			if imported {
				text, kind = "Other.first", ast.ExprMember
			}
			id := selectedCallableSite(t, res, *root, text, kind)
			selected := root.Symbols.ExprSymbols[id]
			if name == "imported_body" {
				callID := selectedCallableSite(t, res, *root, "Other.first(a, b)", ast.ExprCall)
				selected = root.Symbols.ExprSymbols[callID]
				captureSelectedOwner(t, res, *root, callID)
			}
			if imported {
				captureSelectedOwner(t, res, *root, id)
			}
			candidate := selectedCallableCandidateFact(t, res, inputs.units, *root, selected)
			promises := selectedCallablePromises(t, *root, selected)
			if imported {
				member, _ := root.Builder.Exprs.Member(id)
				target := root.Symbols.Table.Symbols.Get(root.Symbols.ExprSymbols[member.Target])
				if target == nil || target.Kind != symbols.SymbolModule || candidate.HasSelf {
					t.Fatal("PRECONDITION: imported function is not a module-selected nonreceiver value")
				}
				if !candidate.ReturnSources.IsAllInputs() {
					t.Fatal("PRECONDITION: unmarked imported first lost its original all-input promise")
				}
				if name == "widened_destination" {
					for _, binding := range []string{"wide", "copied"} {
						_, info := returnOriginValueFunction(t, res, binding)
						if !info.ReturnSources().IsAllInputs() {
							t.Fatal("PRECONDITION: widened destination/copy lost its declared all-input promise")
						}
					}
				}
			} else if name == "selected_map" {
				for _, target := range mapCase.targets {
					captureStorageP0Target(t, res, inputs.units, root.SourceKey, name, target)
				}
			}
			before := selectedCallableDigest(t, root.Publication)
			original := root.Publication
			wantReason := ""
			if strings.HasSuffix(name, "_mapping") {
				otherID := selectedCallableSite(t, res, *root, "rt_map_remove(self, key)", ast.ExprCall)
				foreign := selectedCallableCandidateFact(t, res, inputs.units, *root, root.Symbols.ExprSymbols[otherID])
				if candidate.Symbol == foreign.Symbol || candidate.Source == foreign.Source || len(original.RootToLocalSymbols) == 0 {
					t.Fatal("PRECONDITION: mapping mutation lacks two distinct real declarations")
				}
				root.Publication.RootToLocalSymbols = maps.Clone(original.RootToLocalSymbols)
				for key, locals := range original.RootToLocalSymbols {
					root.Publication.RootToLocalSymbols[key] = slices.Clone(locals)
				}
				if name != "ambiguous_mapping" {
					root.Publication.RootToLocalSymbols[candidate.Symbol] = slices.DeleteFunc(root.Publication.RootToLocalSymbols[candidate.Symbol], func(s symbols.SymbolID) bool { return s == selected })
				}
				wantReason = "selected callable lacks its published callable authority"
				if name != "missing_mapping" {
					root.Publication.RootToLocalSymbols[foreign.Symbol] = append(root.Publication.RootToLocalSymbols[foreign.Symbol], selected)
					wantReason = "selected callable disagrees with its original typed signature"
				}
				if name == "ambiguous_mapping" {
					wantReason = "selected callable has ambiguous canonical authority"
				}
				logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_mapping_delta", "case": name, "selected": selected, "candidate": candidate, "foreign": foreign,
					"before": original.LocalSymbols(candidate.Symbol), "after": root.Publication.LocalSymbols(candidate.Symbol), "foreign_before": original.LocalSymbols(foreign.Symbol), "foreign_after": root.Publication.LocalSymbols(foreign.Symbol)})
			}
			effective := selectedCallableDigest(t, root.Publication)
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_actual_analysis", "case": name, "analysis": analysis, "error": errorReturnOriginCallText(err), "expected_reason": wantReason,
				"site": root.Builder.Exprs.Get(id).Span, "publication_before": before, "publication_effective": effective})
			if before != selectedCallableDigest(t, original) || effective != selectedCallableDigest(t, root.Publication) {
				t.Fatal("analysis or mutation modified retained publication")
			}
			if promises != selectedCallablePromises(t, *root, selected) {
				t.Fatal("analysis changed the original selected descriptor/promise")
			}
			checkReturnOriginStdlibBags(t, res, false)
			if err != nil || analysis == nil {
				t.Fatalf("selected source analysis did not reach a result: %v", err)
			}
			for _, d := range analysis.Diagnostics {
				if d.Primary.File == root.Builder.Files.Get(root.FileID).Span.File {
					t.Errorf("admitted caller acquired an origin diagnostic: %+v", d)
				}
			}
			if wantReason != "" {
				want := sema.ReturnOriginPending{SourceKey: root.SourceKey, Span: root.Builder.Exprs.Get(id).Span, Reason: wantReason}
				if !slices.Contains(analysis.Pending, want) || analysis.Complete() {
					t.Errorf("detached selection lacks exact refusal: want=%+v actual=%+v", want, analysis.Pending)
				}
			} else if imported {
				want := []uint32{0}
				if name == "widened_destination" {
					want = []uint32{0, 1}
				}
				summary := requireReturnOriginSummary(t, analysis, "probe")
				if summary.Source.File != res.File.ID || summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, want) {
					t.Errorf("imported body/value lost its exact result promise: want=%v actual=%+v", want, summary)
				}
			} else {
				for _, pending := range analysis.Pending {
					if pending.SourceKey == root.SourceKey && pending.Span == root.Builder.Exprs.Get(id).Span && (strings.HasPrefix(pending.Reason, "return origins: no local callable identity") || strings.HasPrefix(pending.Reason, "selected callable ") || pending.Reason == "generic original call lacks its selected declaration and physical formals") {
						t.Errorf("certified selected call still lacks identity: %+v", pending)
					}
				}
			}
		})
	}
}

func selectedCallableDigest(t *testing.T, value any) [32]byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func selectedCallablePromises(t *testing.T, u sema.ReturnOriginUnit, selected symbols.SymbolID) [32]byte {
	t.Helper()
	var facts []any
	selectedSymbol := u.Symbols.Table.Symbols.Get(selected)
	for _, sym := range u.Symbols.Table.Symbols.Data() {
		if (sym.Span != selectedSymbol.Span || sym.Type != selectedSymbol.Type) && sym.Decl.SourceFile != u.Builder.Files.Get(u.FileID).Span.File {
			continue
		}
		if info, known := u.Sema.TypeInterner.FnInfo(sym.Type); known {
			facts = append(facts, map[string]any{"symbol": sym, "params": info.Params, "result": info.Result, "all_inputs": info.ReturnSources().IsAllInputs(), "slots": info.ReturnSources().Slots()})
		}
	}
	return selectedCallableDigest(t, facts)
}

// Find the first exact source occurrence; repeated calls keep their own records.
func selectedCallableSite(t *testing.T, res *DiagnoseResult, u sema.ReturnOriginUnit, text string, kind ast.ExprKind) ast.ExprID {
	t.Helper()
	var found ast.ExprID
	file := res.FileSet.Get(u.Builder.Files.Get(u.FileID).Span.File)
	for i, node := range u.Builder.Exprs.Arena.Slice() {
		if node.Kind != kind || node.Span.File != file.ID || string(file.Content[node.Span.Start:node.Span.End]) != text {
			continue
		}
		id := ast.ExprID(i + 1)
		if !found.IsValid() || node.Span.Start < u.Builder.Exprs.Get(found).Span.Start {
			found = id
		}
		children := []ast.ExprID{id}
		if member, ok := u.Builder.Exprs.Member(id); ok {
			children = append(children, member.Target)
		}
		if call, ok := u.Builder.Exprs.Call(id); ok {
			children = append(children, call.Target)
		}
		for _, child := range children {
			typ, typed := u.Sema.ExprTypes[child]
			selected, hasSymbol := u.Symbols.ExprSymbols[child]
			info, _ := u.Sema.TypeInterner.FnInfo(typ)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_expression", "expr": child, "node": u.Builder.Exprs.Get(child), "type_present": typed, "type": typ, "fn_info": info,
				"symbol_present": hasSymbol, "selected": selected, "symbol": u.Symbols.Table.Symbols.Get(selected)})
		}
	}
	if !found.IsValid() {
		t.Fatalf("PRECONDITION: source lacks exact %q kind=%v", text, kind)
	}
	return found
}

func selectedCallableCandidateFact(t *testing.T, res *DiagnoseResult, units []sema.ReturnOriginUnit, u sema.ReturnOriginUnit, selected symbols.SymbolID) sema.CallableCandidate {
	t.Helper()
	var found *sema.CallableCandidate
	for i := range res.Sema.CallableCandidates {
		candidate := &res.Sema.CallableCandidates[i]
		mapped := slices.Contains(u.Publication.LocalSymbols(candidate.Symbol), selected)
		if len(u.Publication.RootToLocalSymbols) == 0 {
			mapped = candidate.Symbol == selected
		}
		if mapped {
			if found != nil {
				t.Fatal("PRECONDITION: selected callable has ambiguous original publication")
			}
			found = candidate
		}
	}
	sym := u.Symbols.Table.Symbols.Get(selected)
	logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_candidate_lookup", "source_key": u.SourceKey, "selected": selected, "symbol": sym, "candidate": found, "mapping_count": len(u.Publication.RootToLocalSymbols)})
	if found == nil || !selected.IsValid() || sym == nil || sym.Kind != symbols.SymbolFunction || sym.Signature == nil {
		t.Fatal("PRECONDITION: selected callable lacks its real typed symbol/candidate")
	}
	info, ok := u.Sema.TypeInterner.FnInfo(sym.Type)
	logReturnOriginCallEvidence(t, map[string]any{"stage": "selected_local_signature", "candidate": found, "symbol": sym, "fn_info": info, "local_id": selected})
	if !ok || info == nil || sym.Span != found.Source || sym.Signature.HasSelf != found.HasSelf || sym.Signature.HasBody != found.HasBody ||
		!slices.Equal(info.Params, found.ParamTypes) || info.Result != found.ResultType || !info.ReturnSources().Equal(found.ReturnSources) {
		t.Fatal("PRECONDITION: original selected descriptor disagrees with its canonical declaration")
	}
	captureStorageP0Selection(t, res, units, u, "selected_callable", selected, found.Name)
	return *found
}
