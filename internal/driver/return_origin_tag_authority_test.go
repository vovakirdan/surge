package driver

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"surge/internal/ast"
	"surge/internal/sema"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Original-source transfer and finalized-use validation are separate duties.
// Corrupt only detached closure records after the actual typed fixture closes.
func TestAnalyzeTypedReturnOriginTagAuthority(t *testing.T) {
	const borrowed = `pragma no_std;
tag Hold<T>(T);
type Held<T> = Hold(T) | nothing;
fn probe(value: &string) -> Held<&string> {
    return Hold::<&string>(value);
}
`
	const unvisited = `pragma no_std;
tag Hold<T>(T);
type Held<T> = Hold(T) | nothing;
fn probe(value: &string) -> &string {
    return value;
    let held = Hold::<&string>(value);
}
`
	const generic = `pragma no_std;
tag Hold<T>(T);
type Held<T> = Hold(T) | nothing;
fn generic_probe<U>(ignored: U, value: &string) -> Held<&string> {
    return Hold::<&string>(value);
}
`
	for _, tc := range []struct{ name, text, digest, reason string }{
		{"missing_use", borrowed, "25f3f740e919d753e31d42d71aaafa6d27d8f476bb84a4499bc699beafbb5bb7", "generic call lacks its finalized concrete use"},
		{"missing_instance", borrowed, "25f3f740e919d753e31d42d71aaafa6d27d8f476bb84a4499bc699beafbb5bb7", "generic use lacks its finalized callee instance"},
		{"duplicate_use", borrowed, "25f3f740e919d753e31d42d71aaafa6d27d8f476bb84a4499bc699beafbb5bb7", "generic use has duplicate or contradictory finalized authority"},
		{"rebound_args", borrowed, "25f3f740e919d753e31d42d71aaafa6d27d8f476bb84a4499bc699beafbb5bb7", "generic use disagrees with its finalized callee instance"},
		{"intact_unvisited", unvisited, "6c3d554fd9812e4c9423142ea0f5f9a7755d398daffe3da85a9b29eb73f9bdc2", ""},
		{"missing_unvisited", unvisited, "6c3d554fd9812e4c9423142ea0f5f9a7755d398daffe3da85a9b29eb73f9bdc2", "generic call lacks its finalized concrete use"},
		{"original_generic_body", generic, "cda3bb425fd49ab4b54f99eb4fb4443b8142fbaf2c2c206e197cbb7c7fd1fceb", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
				t.Fatalf("PRECONDITION: frozen source changed: %s", got)
			}
			logReturnOriginCallEvidence(t, map[string]any{"stage": "tag_authority_source", "case": tc.name, "source": tc.text, "sha256": tc.digest})
			res := returnOriginTypedFixtureWithEscapeEvidence(t, tc.text, false)
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			inputs, unitsErr := collectReturnOriginUnits(res)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "tag_authority_closure", "case": tc.name,
				"closure_error": errorReturnOriginCallText(closureErr), "units_error": errorReturnOriginCallText(unitsErr), "raw_bag": res.Bag.Items(),
				"roots": res.Sema.InstantiationGraph.Roots(), "edges": res.Sema.InstantiationGraph.Edges(), "closure": res.Sema.InstantiationClosure,
				"candidates": res.Sema.CallableCandidates, "identity_present": res.Sema.InstantiationIdentity != nil})
			if closureErr != nil || unitsErr != nil || len(inputs.units) != 1 || res.Sema.InstantiationClosure == nil || res.Sema.InstantiationIdentity == nil || res.Bag.HasErrors() {
				t.Fatal("PRECONDITION: real source admission/closure/unit missing")
			}
			unit := inputs.units[0]
			span := returnOriginTagSpan(t, res.File.ID, tc.text, "Hold::<&string>(value)", true)
			targetSpan := returnOriginTagSpan(t, res.File.ID, tc.text, "Hold", true)
			var callID ast.ExprID
			for id := range res.Sema.ExprTypes {
				if node := res.Builder.Exprs.Get(id); node != nil && node.Span == span && node.Kind == ast.ExprCall {
					if callID.IsValid() {
						t.Fatal("PRECONDITION: duplicate original call")
					}
					callID = id
				}
			}
			call, ok := res.Builder.Exprs.Call(callID)
			if !ok || call == nil || len(call.Args) != 1 || len(call.TypeArgs) != 1 || call.HasNamedArgs() {
				t.Fatal("PRECONDITION: original explicit-reference call missing")
			}
			selected := unit.Symbols.ExprSymbols[callID]
			sym := unit.Symbols.Table.Symbols.Get(selected)
			target := res.Builder.Exprs.Get(call.Target)
			if sym == nil || sym.Kind != symbols.SymbolTag || sym.Decl.SourceFile != res.File.ID || sym.Decl.ASTFile != res.FileID ||
				target == nil || target.Kind != ast.ExprIdent || target.Span != targetSpan || unit.Symbols.ExprSymbols[call.Target] != selected ||
				res.Sema.ExprTypes[call.Target] == types.NoTypeID || !slices.Contains(unit.Symbols.ItemSymbols[sym.Decl.Item], selected) {
				t.Fatal("PRECONDITION: original selected tag/target/declaration missing")
			}
			tag, tagOK := res.Builder.Items.Tag(sym.Decl.Item)
			union, unionOK := res.Sema.TypeInterner.UnionInfo(res.Sema.ExprTypes[callID])
			_, fnPresent := res.Sema.TypeInterner.FnInfo(sym.Type)
			if !tagOK || tag == nil || tag.NameSpan != sym.Span || len(tag.Payload) != 1 || len(tag.Generics) != 1 ||
				sym.Signature == nil || len(sym.Signature.Params) != 1 || sym.Signature.Params[0] != "T" || sym.Signature.Result != "Hold<T>" ||
				len(sym.TypeParams) != 1 || sym.TypeParams[0] != tag.Generics[0] || fnPresent || !unionOK || union == nil || len(union.Members) != 1 {
				t.Fatal("PRECONDITION: exact original tag payload/signature or typed result missing")
			}
			member := union.Members[0]
			if member.Kind != types.UnionMemberTag || member.Type != types.NoTypeID || member.TagName != sym.Name || len(member.TagArgs) != 1 {
				t.Fatal("PRECONDITION: actual tag member shape missing")
			}
			payload := member.TagArgs[0]
			typ, typeOK := res.Sema.TypeInterner.Lookup(payload)
			if !typeOK || typ.Kind != types.KindReference || typ.Mutable || typ.Elem != res.Sema.TypeInterner.Builtins().String ||
				res.Sema.ExprTypes[call.Args[0].Value] != payload {
				t.Fatal("PRECONDITION: exact borrowed payload/argument descriptor missing")
			}
			var probe sema.CallableCandidate
			name := "probe"
			if tc.name == "original_generic_body" {
				name = "generic_probe"
			}
			for _, candidate := range res.Sema.CallableCandidates {
				if candidate.Name == name && candidate.SourceKey == unit.SourceKey && candidate.Source.File == res.File.ID {
					probe = candidate
				}
			}
			if !probe.Symbol.IsValid() || !probe.HasBody || probe.BodyKey == "" {
				t.Fatal("PRECONDITION: original caller missing")
			}
			original := res.Sema.InstantiationClosure
			roots, edges := res.Sema.InstantiationGraph.Roots(), res.Sema.InstantiationGraph.Edges()
			logReturnOriginCallEvidence(t, map[string]any{"stage": "tag_authority_typed", "case": tc.name, "call_id": callID, "call_span": span,
				"call": call, "target": target, "selected": selected, "symbol": sym, "tag": tag, "union": union, "payload_type": typ,
				"probe": probe, "scope": unit.Symbols.Table.Scopes.Get(sym.Scope), "expr_types": res.Sema.ExprTypes, "expr_symbols": unit.Symbols.ExprSymbols,
				"binding_types": res.Sema.BindingTypes, "publication": unit.Publication})
			if tc.name == "original_generic_body" {
				if len(roots) != 0 || len(edges) != 1 || len(original.Instances) != 0 || len(original.UseSites) != 0 || len(original.LiveCallables) != 0 ||
					len(probe.TemplateParams) != 1 || len(probe.ParamTypes) != 2 || probe.ParamTypes[0] != probe.TemplateParams[0] || probe.ParamTypes[1] != payload {
					t.Fatal("PRECONDITION: original generic-only body acquired current instances or lost its exact formals/edge")
				}
				edge := edges[0]
				param, present := res.Sema.TypeInterner.TypeParamInfo(probe.TemplateParams[0])
				if !present || param == nil || edge.Kind != sema.InstantiationTag || edge.Caller != probe.Symbol || edge.Callee != selected ||
					edge.CallerTemplateArity != 1 || len(edge.CallerBindings) != 1 || edge.CallerBindings[0].Param != probe.TemplateParams[0] ||
					edge.CallerBindings[0].Owner != symbols.SymbolID(param.Owner) || edge.CallerBindings[0].ParamIndex != param.Index || edge.CallerBindings[0].ArgIndex != 0 ||
					!slices.Equal(edge.CalleeTemplateArgs, []types.TypeID{payload}) || edge.Witness.Site != span || edge.Witness.Caller != probe.Symbol || edge.Witness.SourceKey != unit.SourceKey {
					t.Fatal("PRECONDITION: original generic caller/parameter/payload witness differs")
				}
			} else {
				if len(roots) != 1 || len(edges) != 0 || len(original.Instances) != 1 || len(original.UseSites) != 1 ||
					!slices.Contains(original.LiveCallables, probe.Symbol) || len(probe.TemplateParams) != 0 {
					t.Fatal("PRECONDITION: current tag census is not exactly one root/use/instance")
				}
				root, use, instance := roots[0], original.UseSites[0], original.Instances[0]
				key, keyErr := sema.NewInstanceKey(*res.Sema.InstantiationIdentity, selected, []types.TypeID{payload})
				if keyErr != nil || root.Kind != sema.InstantiationTag || use.Kind != root.Kind || instance.Kind != root.Kind ||
					root.Template != selected || use.CalleeTemplate != selected || instance.Template != selected || use.Callee != key || instance.Key != key ||
					!slices.Equal(root.TemplateArgs, []types.TypeID{payload}) || !slices.Equal(use.TemplateArgs, root.TemplateArgs) || !slices.Equal(instance.TemplateArgs, root.TemplateArgs) ||
					root.Witness.Caller != probe.Symbol || use.CallerTemplate != probe.Symbol || use.Caller != (sema.InstanceKey{}) || len(use.CallerTemplateArgs) != 0 ||
					root.Witness.Site != span || use.Site != span || root.Witness.SourceKey != unit.SourceKey || use.SourceKey != unit.SourceKey {
					t.Fatal("PRECONDITION: exact current caller/root/use/instance identity differs")
				}
			}
			if tc.name == "intact_unvisited" || tc.name == "missing_unvisited" {
				var body *ast.BlockStmt
				for _, item := range res.Builder.Files.Get(res.FileID).Items {
					if fn, found := res.Builder.Items.Fn(item); found && fn != nil && fn.NameSpan == probe.Source {
						body = res.Builder.Stmts.Block(fn.Body)
					}
				}
				if body == nil || len(body.Stmts) != 2 {
					t.Fatal("PRECONDITION: unvisited source lost return then let")
				}
				first, second := res.Builder.Stmts.Return(body.Stmts[0]), res.Builder.Stmts.Let(body.Stmts[1])
				if first == nil || second == nil || second.Value != callID || res.Builder.Stmts.Get(body.Stmts[0]).Span.End >= span.Start ||
					unit.Symbols.ExprSymbols[first.Expr] != unit.Symbols.ExprSymbols[call.Args[0].Value] {
					t.Fatal("PRECONDITION: retained original call is not after explicit input return")
				}
			}
			before, beforeErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "tag_authority_intact", "case": tc.name, "analysis": before,
				"error": errorReturnOriginCallText(beforeErr), "raw_bag": res.Bag.Items()})
			if beforeErr != nil || before == nil {
				t.Fatalf("intact analyzer failed: %v", beforeErr)
			}
			slots := []uint32{0}
			if tc.name == "original_generic_body" {
				slots[0] = 1
			}
			summary := requireReturnOriginSummary(t, before, name)
			if !before.Complete() || summary.BodyKey != probe.BodyKey || summary.Unknown || summary.NoNormalReturn || !slices.Equal(summary.ParamSlots, slots) || len(before.Diagnostics) != 0 {
				t.Errorf("intact original tag transfer incomplete: summary=%+v pending=%+v diagnostics=%+v", summary, before.Pending, before.Diagnostics)
			}
			if tc.reason == "" {
				return
			}
			raw, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var changed sema.InstantiationClosure
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			use := original.UseSites[0]
			want := sema.ReturnOriginPending{SourceKey: use.SourceKey, Span: use.Site, Reason: tc.reason}
			if slices.Contains(before.Pending, want) {
				t.Fatal("PRECONDITION: intact authority already reports selected corruption")
			}
			switch tc.name {
			case "missing_use", "missing_unvisited":
				changed.UseSites = nil
			case "missing_instance":
				changed.Instances = nil
			case "duplicate_use":
				changed.UseSites = append(changed.UseSites, changed.UseSites[0])
			case "rebound_args":
				changed.UseSites[0].TemplateArgs = []types.TypeID{res.Sema.TypeInterner.Builtins().Int}
			}
			res.Sema.InstantiationClosure = &changed
			after, afterErr := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "tag_authority_mutated", "case": tc.name, "original_closure": original,
				"changed_closure": changed, "expected_pending": want, "analysis": after, "error": errorReturnOriginCallText(afterErr), "raw_bag": res.Bag.Items()})
			retained, err := json.Marshal(original)
			if err != nil || !slices.Equal(raw, retained) {
				t.Fatal("original closure changed")
			}
			if afterErr != nil || after == nil || after.Complete() || !slices.Contains(after.Pending, want) {
				t.Fatalf("missing exact tag authority refusal: want=%+v analysis=%+v error=%v", want, after, afterErr)
			}
			for _, pending := range after.Pending {
				if pending.Reason == "generic constructor authority needs its owning payload transfer" || pending.Reason == "captured binding requires origin finalization" ||
					pending.Reason == "call needs an exact body, canonical core contract, or opaque declaration promise" {
					t.Errorf("unrelated constructor refusal remains: %+v", pending)
				}
			}
		})
	}
}
