package sema

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func returnOriginPublicationFixture(t *testing.T, src string, allowOldEscape bool) (*Result, ReturnOriginUnit) {
	t.Helper()
	builder, file, parseBag := parseSnippet(t, src)
	if parseBag.HasErrors() {
		t.Fatalf("parse refused origin fixture: %s", diagnosticsSummary(parseBag))
	}
	bag := diag.NewBag(64)
	resolved := symbols.ResolveFile(builder, file, &symbols.ResolveOptions{Reporter: &diag.BagReporter{Bag: bag}})
	result := Check(t.Context(), builder, file, Options{Symbols: &resolved, Reporter: &diag.BagReporter{Bag: bag}})
	for _, d := range bag.Items() {
		if d.Severity == diag.SevError && (!allowOldEscape || d.Code != diag.SemaBorrowEscapesReturn) {
			t.Fatalf("unrelated source refusal: %+v", *d)
		}
	}
	const key = "origin.sg"
	if err := CanonicalizeInstantiationGraphSources(&result, func(source.FileID) (string, error) { return key, nil }); err != nil {
		t.Fatal(err)
	}
	publication := FinalizationPublication{SourceKey: key}
	for _, candidate := range result.CallableCandidates {
		publication.LocalCallables = append(publication.LocalCallables, FinalizationCallableIdentity{
			Symbol: candidate.Symbol, BodyKey: candidate.BodyKey, SourceKey: candidate.SourceKey,
		})
	}
	return &result, ReturnOriginUnit{Builder: builder, FileID: file, Sema: &result,
		Symbols: &resolved, SourceKey: key, Publication: publication}
}

func TestReturnOriginPublicationUsesCanonicalLocalBody(t *testing.T) {
	result, unit := returnOriginPublicationFixture(t, `fn same(value: &string) -> &string { return value; }`, false)
	authority := *result
	authority.CallableCandidates = slices.Clone(result.CallableCandidates)
	unit.Publication.RootToLocalSymbols = make(map[symbols.SymbolID][]symbols.SymbolID)
	for i := range authority.CallableCandidates {
		candidate := &authority.CallableCandidates[i]
		local := candidate.Symbol
		candidate.Symbol += 10000
		unit.Publication.RootToLocalSymbols[candidate.Symbol] = []symbols.SymbolID{local}
	}
	analysis, err := AnalyzeReturnOrigins(t.Context(), &authority, []ReturnOriginUnit{unit})
	if err != nil {
		t.Fatal(err)
	}
	if !analysis.Complete() || len(analysis.Diagnostics) != 0 || len(analysis.Summaries) != 1 ||
		!slices.Equal(analysis.Summaries[0].ParamSlots, []uint32{0}) {
		t.Fatalf("root/local allocation change lost the actual body: %+v", analysis)
	}
}

func TestReturnOriginPublicationRejectsMissingOrAmbiguousIdentity(t *testing.T) {
	for _, name := range []string{"missing_local_body", "foreign_source", "ambiguous_body"} {
		t.Run(name, func(t *testing.T) {
			result, unit := returnOriginPublicationFixture(t, `fn same(value: &string) -> &string { return value; }`, false)
			if len(unit.Publication.LocalCallables) != 1 {
				t.Fatal("fixture did not retain exactly one declaration")
			}
			switch name {
			case "missing_local_body":
				unit.Publication.LocalCallables = nil
			case "foreign_source":
				unit.Publication.LocalCallables[0].SourceKey = "other.sg"
			case "ambiguous_body":
				other := unit.Publication.LocalCallables[0]
				other.BodyKey += "/other"
				unit.Publication.LocalCallables = append(unit.Publication.LocalCallables, other)
			}
			if analysis, err := AnalyzeReturnOrigins(t.Context(), result, []ReturnOriginUnit{unit}); err == nil || analysis != nil {
				t.Fatalf("unproved publication became a body: analysis=%+v error=%v", analysis, err)
			}
		})
	}
}

func TestReturnOriginBodylessDeclarationDoesNotInventBodyObligation(t *testing.T) {
	result, unit := returnOriginPublicationFixture(t, `fn opaque(value: int) -> int;`, false)
	analysis, err := AnalyzeReturnOrigins(t.Context(), result, []ReturnOriginUnit{unit})
	if err != nil {
		t.Fatal(err)
	}
	if !analysis.Complete() || len(analysis.Diagnostics) != 0 || len(analysis.Summaries) != 0 {
		t.Fatalf("reference-free declaration fabricated a missing body or summary: %+v", analysis)
	}
}

func TestReturnOriginRefFreeResultStillChecksNestedScopeEscape(t *testing.T) {
	const src = `fn read(value: &string) -> int { return 1; }
fn probe() -> int {
    let outside: string = "outside";
    let escaped: &string = {
        let owned: string = "owned";
        ret &OWNER;
    };
    return read(escaped);
}`
	for _, name := range []string{"local_owner", "outer_owner"} {
		t.Run(name, func(t *testing.T) {
			owner := "outside"
			if name == "local_owner" {
				owner = "owned"
			}
			result, unit := returnOriginPublicationFixture(t, strings.ReplaceAll(src, "OWNER", owner), name == "local_owner")
			analysis, err := AnalyzeReturnOrigins(t.Context(), result, []ReturnOriginUnit{unit})
			if err != nil {
				t.Fatal(err)
			}
			if !analysis.Complete() || (len(analysis.Diagnostics) != 0) != (name == "local_owner") {
				t.Fatalf("owned function result hid a nested lifetime obligation: %+v", analysis)
			}
			for _, d := range analysis.Diagnostics {
				if d.Code != diag.SemaBorrowEscapesReturn || d.Primary.Start >= d.Primary.End {
					t.Fatalf("wrong nested owner diagnostic: %+v", d)
				}
			}
		})
	}
}

func TestReturnOriginUnresolvedReferenceEffectStaysPending(t *testing.T) {
	for _, fixture := range []struct {
		name           string
		src            string
		allowOldEscape bool
	}{
		{"reference_content_write", `fn replace(dst: &mut &string, value: &string) -> nothing;
fn probe(dst: &mut &string, value: &string) -> nothing { return replace(dst, value); }`, false},
	} {
		result, unit := returnOriginPublicationFixture(t, fixture.src, fixture.allowOldEscape)
		analysis, err := AnalyzeReturnOrigins(t.Context(), result, []ReturnOriginUnit{unit})
		if err != nil {
			t.Fatal(err)
		}
		if analysis.Complete() || len(analysis.Pending) == 0 {
			t.Fatalf("unresolved reference effect became complete for %s: %+v", fixture.src, analysis)
		}
		t.Logf("RETURN_ORIGIN_EFFECT_CASE_COMPLETE name=%s source_sha256=%x pending=%d",
			fixture.name, sha256.Sum256([]byte(fixture.src)), len(analysis.Pending))
	}
}

func TestReturnOriginDeferredMethodTargetEvidence(t *testing.T) {
	const src = `contract Read<T> { fn read(self: &T) -> int; }
fn probe<T: Read<T>>(self: &T) -> int { return self.read(); }`
	for _, name := range []string{"syntactic_target_untyped", "runtime_receiver_untyped"} {
		t.Run(name, func(t *testing.T) {
			result, unit := returnOriginPublicationFixture(t, src, false)
			var id ast.ExprID
			for ref := range result.DeferredCallableUses {
				if ref.Kind == DeferredMethodCall {
					if id.IsValid() {
						t.Fatal("fixture has multiple deferred method calls")
					}
					id = ref.Expr
				}
			}
			if !id.IsValid() {
				t.Fatal("source checker did not record the deferred method")
			}
			call, ok := unit.Builder.Exprs.Call(id)
			if !ok || call == nil || result.ExprTypes[id] == types.NoTypeID || result.ExprTypes[call.Target] != types.NoTypeID {
				t.Fatal("fixture lacks a typed call with an untyped selector")
			}
			member, ok := unit.Builder.Exprs.Member(call.Target)
			if !ok || member == nil || result.ExprTypes[member.Target] == types.NoTypeID {
				t.Fatal("fixture has no typed runtime receiver")
			}
			if name == "runtime_receiver_untyped" {
				delete(result.ExprTypes, member.Target)
			}
			analysis, err := AnalyzeReturnOrigins(t.Context(), result, []ReturnOriginUnit{unit})
			if name == "runtime_receiver_untyped" {
				if err == nil || !strings.Contains(err.Error(), "is not typed") || analysis != nil {
					t.Fatalf("missing runtime type was accepted: analysis=%+v error=%v", analysis, err)
				}
				return
			}
			want := ReturnOriginPending{SourceKey: unit.SourceKey, Span: unit.Builder.Exprs.Get(id).Span, Reason: "deferred method lacks finalized instance authority"}
			if err != nil || analysis == nil || analysis.Complete() || len(analysis.Pending) == 0 || !slices.Contains(analysis.Pending, want) {
				t.Fatalf("selector syntax hid or aborted an unresolved call: analysis=%+v error=%v", analysis, err)
			}
		})
	}
}

func TestReturnOriginBuiltinDeclarationKeepsOwningSource(t *testing.T) {
	const src = "@intrinsic fn rt_string_len_bytes(s: &string) -> uint;\nfn probe(s: &string) -> uint { return rt_string_len_bytes(s); }\n"
	for _, name := range []string{"valid_builtin", "missing_identity", "wrong_original_span", "missing_builtin_certificate"} {
		t.Run(name, func(t *testing.T) {
			t.Logf("RETURN_ORIGIN_BUILTIN_SOURCE case=%s sha256=%x source=%q", name, sha256.Sum256([]byte(src)), src)
			result, unit := returnOriginPublicationFixture(t, src, false)
			file := unit.Builder.Files.Get(unit.FileID)
			if file == nil || len(file.Items) != 2 || len(unit.Symbols.ItemSymbols[file.Items[0]]) != 1 {
				t.Fatal("PRECONDITION: source lacks its two original declarations")
			}
			fn, ok := unit.Builder.Items.Fn(file.Items[0])
			id := unit.Symbols.ItemSymbols[file.Items[0]][0]
			sym := unit.Symbols.Table.Symbols.Get(id)
			if !ok || fn == nil || fn.Body.IsValid() || sym == nil || sym.Kind != symbols.SymbolFunction ||
				sym.Flags&symbols.SymbolFlagBuiltin == 0 || sym.Decl.ASTFile != unit.FileID ||
				sym.Decl.SourceFile != fn.NameSpan.File || sym.Span != fn.NameSpan || sym.Signature == nil || sym.Signature.HasBody {
				t.Fatal("PRECONDITION: intrinsic does not retain its actual source declaration")
			}
			info, ok := result.TypeInterner.FnInfo(sym.Type)
			if !ok || info == nil || len(info.Params) != 1 || !returnOriginIsReference(result.TypeInterner, info.Params[0]) || info.Result != result.TypeInterner.Builtins().Uint {
				t.Fatal("PRECONDITION: intrinsic does not have the checked reference-input/uint-result signature")
			}
			candidateIndex, identityIndex := -1, -1
			for i, candidate := range result.CallableCandidates {
				if candidate.Symbol == id {
					if candidateIndex >= 0 {
						t.Fatal("PRECONDITION: ambiguous original candidate")
					}
					candidateIndex = i
				}
			}
			for i, identity := range unit.Publication.LocalCallables {
				if identity.Symbol == id {
					if identityIndex >= 0 {
						t.Fatal("PRECONDITION: ambiguous original publication")
					}
					identityIndex = i
				}
			}
			if candidateIndex < 0 || identityIndex < 0 {
				t.Fatal("PRECONDITION: actual intrinsic authority was not retained")
			}
			original := result.CallableCandidates[candidateIndex]
			identity := unit.Publication.LocalCallables[identityIndex]
			if !original.Builtin || !original.Intrinsic || original.HasBody || original.Source != fn.NameSpan ||
				original.DeclKeyword != fn.FnKeywordSpan || !slices.Equal(original.ParamTypes, info.Params) || original.ResultType != info.Result ||
				original.SourceKey != "builtin" || identity.SourceKey != "builtin" || identity.BodyKey != original.BodyKey ||
				identity.BodyKey == "" || unit.SourceKey == identity.SourceKey || unit.Publication.SourceKey != unit.SourceKey {
				t.Fatal("PRECONDITION: canonical builtin key and physical owning declaration were not both proven")
			}
			var callID ast.ExprID
			for expr, typ := range result.ExprTypes {
				if call, found := unit.Builder.Exprs.Call(expr); found && call != nil {
					if callID.IsValid() || typ != info.Result || unit.Symbols.ExprSymbols[expr] != id || len(call.Args) != 1 || result.ExprTypes[call.Args[0].Value] != info.Params[0] {
						t.Fatal("PRECONDITION: source call does not use the exact typed intrinsic")
					}
					callID = expr
				}
			}
			if !callID.IsValid() {
				t.Fatal("PRECONDITION: intrinsic call was not typed")
			}
			authority := *result
			authority.CallableCandidates = slices.Clone(result.CallableCandidates)
			switch name {
			case "missing_identity":
				unit.Publication.LocalCallables = slices.Delete(slices.Clone(unit.Publication.LocalCallables), identityIndex, identityIndex+1)
			case "wrong_original_span":
				authority.CallableCandidates[candidateIndex].Source.Start++
			case "missing_builtin_certificate":
				authority.CallableCandidates[candidateIndex].Builtin = false
			}
			analysis, err := AnalyzeReturnOrigins(t.Context(), &authority, []ReturnOriginUnit{unit})
			evidence, marshalErr := json.Marshal(map[string]any{"case": name, "source": src, "owning_source": unit.SourceKey,
				"symbol": sym, "fn_info": info, "call": callID, "call_type": result.ExprTypes[callID], "original_candidate": original,
				"original_identity": identity, "effective_candidate": authority.CallableCandidates[candidateIndex],
				"effective_publication": unit.Publication, "analysis": analysis})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			t.Logf("RETURN_ORIGIN_BUILTIN_EVIDENCE=%s error=%v", evidence, err)
			if name != "valid_builtin" {
				if err == nil || analysis != nil {
					t.Fatal("corrupted builtin authority was accepted")
				}
				return
			}
			if err != nil || analysis == nil || !analysis.Complete() || len(analysis.Diagnostics) != 0 || len(analysis.Summaries) != 1 {
				t.Fatalf("canonical builtin identity lost its owning source: analysis=%+v error=%v", analysis, err)
			}
			summary := analysis.Summaries[0]
			if summary.Name != "probe" || summary.NoNormalReturn || summary.Unknown || len(summary.ParamSlots) != 0 {
				t.Fatalf("typed intrinsic call did not produce the actual normal RefFree body summary: %+v", summary)
			}
		})
	}
}
