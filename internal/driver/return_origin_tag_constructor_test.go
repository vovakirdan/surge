package driver

import (
	"crypto/sha256"
	"fmt"
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

func TestAnalyzeTypedReturnOriginTagConstructors(t *testing.T) {
	for _, tc := range []struct {
		name, digest, text, target, exit string
	}{
		{"borrowed_input", "25f3f740e919d753e31d42d71aaafa6d27d8f476bb84a4499bc699beafbb5bb7", `pragma no_std;
tag Hold<T>(T);
type Held<T> = Hold(T) | nothing;
fn probe(value: &string) -> Held<&string> {
    return Hold::<&string>(value);
}
`, "Hold", ""},
		{"local_owner_escape", "3cbab0c97157fbc8d3cf3bc67c7034ee4891a4a822534d61df5b25a796c58efc", `pragma no_std;
tag Hold<T>(T);
type Held<T> = Hold(T) | nothing;
fn probe() -> Held<&string> {
    let owned: string = "local";
    return Hold::<&string>(&owned);
}
`, "Hold", "return Hold::<&string>(&owned);"},
		{"ref_free_child_effect", "6138be540ac2e46514aeb9b22ae9e95011dacaab371831a55259c00805b1a72d", `pragma no_std;
tag Number(int);
type Numeric = Number(int) | nothing;
fn probe(outside: &int64) -> int64 {
    let mut saved: &int64 = outside;
    let result = Number({ let owned: int64 = 1; saved = &owned; ret 1; });
    return 1;
}
`, "Number", "ret 1;"},
		{"non_tag_global_const", "3403613251ec8b9e898443e3dbeddea6028f64056e7caf6eadb7e86a1514bcae", `pragma no_std;
const Number: int = 7;
fn probe() -> int {
    return Number;
}
`, "Number", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
				t.Fatalf("PRECONDITION: frozen source changed: %s", got)
			}
			logReturnOriginCallEvidence(t, map[string]any{"stage": "tag_source", "case": tc.name, "sha256": tc.digest, "source": tc.text})
			// Reuse real parsing, symbols, sema and publication. The helper retains
			// old SEM3139 evidence and refuses unrelated parser/type diagnostics.
			res := returnOriginTypedFixtureWithEscapeEvidence(t, tc.text, tc.exit != "")
			closureErr := FinalizeInstantiationClosure(t.Context(), res, 64)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "tag_closure", "case": tc.name,
				"error": errorReturnOriginCallText(closureErr), "closure": res.Sema.InstantiationClosure,
				"roots": res.Sema.InstantiationGraph.Roots(), "edges": res.Sema.InstantiationGraph.Edges(),
				"candidates": res.Sema.CallableCandidates, "identity_present": res.Sema.InstantiationIdentity != nil,
				"raw_bag": res.Bag.Items()})
			if closureErr != nil || res.Sema.InstantiationIdentity == nil || res.Sema.InstantiationClosure == nil {
				t.Fatal("PRECONDITION: source lacks its real finalized instantiation authority")
			}
			inputs, err := collectReturnOriginUnits(res)
			if err != nil || len(inputs.units) != 1 {
				t.Fatalf("PRECONDITION: one original typed source unit required: %v", err)
			}
			unit := inputs.units[0]
			targetSpan := returnOriginTagSpan(t, res.File.ID, tc.text, tc.target, true)
			var target, callID ast.ExprID
			callCount := 0
			for raw := uint32(1); raw <= res.Builder.Exprs.Arena.Len(); raw++ {
				id := ast.ExprID(raw)
				node := res.Builder.Exprs.Get(id)
				if node.Kind == ast.ExprIdent && node.Span == targetSpan {
					if target.IsValid() {
						t.Fatal("PRECONDITION: ambiguous target")
					}
					target = id
				}
				if node.Kind == ast.ExprCall {
					callID, callCount = id, callCount+1
				}
			}
			wantKind, wantCalls := symbols.SymbolTag, 1
			if tc.name == "non_tag_global_const" {
				wantKind, wantCalls = symbols.SymbolConst, 0
			}
			selected := unit.Symbols.ExprSymbols[target]
			sym := unit.Symbols.Table.Symbols.Get(selected)
			if !target.IsValid() || sym == nil || sym.Kind != wantKind || sym.Type == types.NoTypeID ||
				res.Sema.ExprTypes[target] == types.NoTypeID || callCount != wantCalls {
				t.Fatalf("PRECONDITION: original target kind/type/call shape: id=%d symbol=%+v calls=%d", target, sym, callCount)
			}
			scope := unit.Symbols.Table.Scopes.Get(sym.Scope)
			if scope == nil || sym.Decl.SourceFile != res.File.ID || sym.Span.File != res.File.ID {
				t.Fatal("PRECONDITION: original declaration/scope missing")
			}
			metadata := map[string]any{"stage": "tag_typed", "case": tc.name, "raw_bag": res.Bag.Items(),
				"target_id": target, "target_node": res.Builder.Exprs.Get(target), "target_type": res.Sema.ExprTypes[target],
				"selected_id": selected, "original_symbol": sym, "original_scope": scope,
				"symbols": unit.Symbols.Table.Symbols.Data(), "scopes": unit.Symbols.Table.Scopes.Data(),
				"binding_types": res.Sema.BindingTypes,
				"expr_types":    res.Sema.ExprTypes, "expr_symbols": unit.Symbols.ExprSymbols, "publication": unit.Publication}
			var callSpan source.Span
			if wantCalls == 1 {
				call, _ := res.Builder.Exprs.Call(callID)
				callSpan = res.Builder.Exprs.Get(callID).Span
				callType := res.Sema.ExprTypes[callID]
				tag, tagPresent := res.Builder.Items.Tag(sym.Decl.Item)
				info, infoPresent := res.Sema.TypeInterner.UnionInfo(callType)
				fn, fnPresent := res.Sema.TypeInterner.FnInfo(sym.Type)
				metadata["call"] = map[string]any{"id": callID, "span": callSpan, "type": callType,
					"payload": call, "symbol": unit.Symbols.ExprSymbols[callID], "tag_declaration": tag,
					"union": info, "fn_info": fn, "fn_present": fnPresent}
				if call == nil || call.Target != target || unit.Symbols.ExprSymbols[callID] != selected ||
					callType == types.NoTypeID || !tagPresent || tag == nil || fnPresent ||
					!infoPresent || info == nil || len(info.Members) != 1 {
					logReturnOriginCallEvidence(t, metadata)
					t.Fatal("PRECONDITION: selected original tag/typed result descriptor missing")
				}
				member := info.Members[0]
				if member.Kind != types.UnionMemberTag || member.TagName != sym.Name || len(member.TagArgs) != len(call.Args) {
					t.Fatal("PRECONDITION: actual constructor tag/payload identity differs")
				}
				if tc.target == "Hold" {
					rootPresent, instancePresent := false, false
					for _, root := range res.Sema.InstantiationGraph.Roots() {
						rootPresent = rootPresent || root.Kind == sema.InstantiationTag && root.Template == selected && slices.Equal(root.TemplateArgs, member.TagArgs)
					}
					for _, instance := range res.Sema.InstantiationClosure.Instances {
						instancePresent = instancePresent || instance.Kind == sema.InstantiationTag && instance.Template == selected && slices.Equal(instance.TemplateArgs, member.TagArgs)
					}
					if !rootPresent || !instancePresent {
						t.Fatal("PRECONDITION: original concrete tag root/instance missing")
					}
				}
				payloadTypes := []any{}
				for i, arg := range call.Args {
					if res.Sema.ExprTypes[arg.Value] == types.NoTypeID || member.TagArgs[i] != res.Sema.ExprTypes[arg.Value] {
						t.Fatal("PRECONDITION: actual argument/payload descriptors differ")
					}
					typ, present := res.Sema.TypeInterner.Lookup(member.TagArgs[i])
					if !present || tc.target == "Hold" && (typ.Kind != types.KindReference || typ.Elem != res.Sema.TypeInterner.Builtins().String) {
						t.Fatal("PRECONDITION: original borrowed payload descriptor missing")
					}
					payloadTypes = append(payloadTypes, typ)
				}
				metadata["payload_types"] = payloadTypes
			} else if sym.Type != res.Sema.TypeInterner.Builtins().Int || res.Sema.ExprTypes[target] != sym.Type {
				t.Fatal("PRECONDITION: global const lost its actual int type")
			}
			logReturnOriginCallEvidence(t, metadata)
			analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
			logReturnOriginCallEvidence(t, map[string]any{"stage": "tag_analysis", "case": tc.name, "analysis": analysis,
				"error": errorReturnOriginCallText(err), "raw_bag": res.Bag.Items()})
			if err != nil || analysis == nil {
				t.Fatalf("analyzer failed before result assertions: %v", err)
			}
			summary := requireReturnOriginSummary(t, analysis, "probe")
			captured := "captured binding requires origin finalization"
			missingCall := "call needs an exact body, canonical core contract, or opaque declaration promise"
			foundCaptured := false
			for _, pending := range analysis.Pending {
				if pending.SourceKey != unit.SourceKey {
					t.Fatalf("unexpected foreign Pending: %+v", pending)
				}
				foundCaptured = foundCaptured || pending.Span == targetSpan && pending.Reason == captured
				if wantCalls == 1 && (pending.Span == targetSpan && pending.Reason == captured || pending.Span == callSpan && pending.Reason == missingCall) {
					t.Errorf("tag constructor still enters captured/callable refusal: %+v", pending)
				}
			}
			if summary.NoNormalReturn {
				t.Errorf("source has a normal return: %+v", summary)
			}
			switch tc.name {
			case "borrowed_input":
				if !analysis.Complete() || summary.Unknown || !slices.Equal(summary.ParamSlots, []uint32{0}) {
					t.Errorf("borrowed tag payload lost exact input source: %+v pending=%+v", summary, analysis.Pending)
				}
			case "local_owner_escape":
				if !summary.Unknown || len(summary.ParamSlots) != 0 {
					t.Errorf("local owner became a proven result source: %+v", summary)
				}
			case "ref_free_child_effect":
				if !analysis.Complete() || summary.Unknown || len(summary.ParamSlots) != 0 {
					t.Errorf("RefFree result has unfinished constructor transfer: %+v pending=%+v", summary, analysis.Pending)
				}
			case "non_tag_global_const":
				if !foundCaptured || analysis.Complete() || !summary.Unknown || len(summary.ParamSlots) != 0 {
					t.Errorf("non-tag global refusal was weakened: %+v pending=%+v", summary, analysis.Pending)
				}
			}
			if tc.exit == "" {
				if len(analysis.Diagnostics) != 0 {
					t.Errorf("unexpected origin diagnostics: %+v", analysis.Diagnostics)
				}
				return
			}
			var owner *symbols.Symbol
			for i := range unit.Symbols.Table.Symbols.Data() {
				candidate := unit.Symbols.Table.Symbols.Get(symbols.SymbolID(i + 1))
				name, _ := res.Builder.StringsInterner.Lookup(candidate.Name)
				if candidate.Kind == symbols.SymbolLet && name == "owned" {
					if owner != nil {
						t.Fatal("PRECONDITION: ambiguous original owner")
					}
					owner = candidate
				}
			}
			if owner == nil || owner.Span.File != res.File.ID {
				t.Fatal("PRECONDITION: original local owner missing")
			}
			exitSpan := returnOriginTagSpan(t, res.File.ID, tc.text, tc.exit, false)
			foundEscape := false
			for _, d := range analysis.Diagnostics {
				if d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError ||
					d.Message != "borrow of 'owned' outlives its owner when this scope exits" {
					t.Errorf("unexpected origin diagnostic: %+v", d)
				}
				for _, note := range d.Notes {
					foundEscape = foundEscape || d.Code == diag.SemaBorrowEscapesReturn && d.Primary == exitSpan &&
						note.Span == owner.Span && note.Msg == "'owned' owns storage that ends in this scope"
				}
			}
			if !foundEscape {
				t.Errorf("missing exact SEM3139 at %v with owner note %v", exitSpan, owner.Span)
			}
		})
	}
}

func returnOriginTagSpan(t *testing.T, file source.FileID, text, needle string, last bool) source.Span {
	t.Helper()
	start := strings.Index(text, needle)
	if last {
		start = strings.LastIndex(text, needle)
	}
	if start < 0 {
		t.Fatalf("PRECONDITION: source span %q missing", needle)
	}
	return source.Span{File: file, Start: uint32(start), End: uint32(start + len(needle))}
}
