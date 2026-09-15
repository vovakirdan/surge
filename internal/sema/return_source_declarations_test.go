package sema

import (
	"context"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

// Each broken @return_source rule has its own code; attribute placement errors
// stay with the general attribute check.
var returnSourceRuleCodes = map[string]diag.Code{
	"@return_source does not accept arguments":              diag.SemaReturnSourceArgument,
	"@return_source requires a reference-bearing parameter": diag.SemaReturnSourceOwnedParam,
	"@return_source requires a reference-bearing result":    diag.SemaReturnSourceOwnedResult,
}

func TestReturnSourceDeclarationValidation(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		message string
	}{
		{"named_valid", "fn subject(@return_source a: &string, b: &string) -> &string { return a; }", ""},
		{"callback_valid", "type Subject = fn(@return_source &string, &string) -> &string;", ""},
		{"union_payload_result", "tag Some<T>(T); type Option<T> = Some(T) | nothing; @intrinsic fn subject(@return_source a: &string) -> Option<&string>;", ""},
		{"owned_input_invalid", "fn subject(@return_source a: string, b: &string) -> &string { return b; }", "@return_source requires a reference-bearing parameter"},
		{"owned_result_invalid", "fn subject(@return_source a: &string) -> int { return 1; }", "@return_source requires a reference-bearing result"},
		{"arity_invalid", "fn subject(@return_source(1) a: &string) -> &string { return a; }", "@return_source does not accept arguments"},
		{"wrong_fn_target", "@return_source fn subject(a: &string) -> &string { return a; }", "attribute '@return_source' is not allowed here"},
		{"wrong_type_target", "@return_source type Subject = string;", "attribute '@return_source' is not allowed here"},
		{"callback_wrong_target", "type Subject = fn(@allow_to &string) -> &string;", "attribute '@allow_to' is not allowed here"},
		{"callback_unknown_attr", "type Subject = fn(@not_an_attribute &string) -> &string;", "unknown attribute '@not_an_attribute'"},
		{"existing_allow_to_named", "fn subject(@allow_to a: &string) -> &string { return a; }", ""},
		{"existing_arena_named", "fn subject(@arena a: &string) -> &string { return a; }", ""},
		{"generic_deferred", "@intrinsic fn subject<T>(@return_source a: T) -> T;", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			builder, file, parseBag := parseSource(t, test.src)
			if parseBag.HasErrors() {
				t.Fatalf("parse failed: %s", diagnosticsSummary(parseBag))
			}
			syms := resolveSymbols(t, builder, file)
			bag := diag.NewBag(64)
			result := Check(context.Background(), builder, file, Options{Symbols: syms, Reporter: &diag.BagReporter{Bag: bag}})
			if test.message != "" {
				code, promise := returnSourceRuleCodes[test.message]
				if !promise {
					code = diag.SemaError
				}
				if bag.Len() != 1 || bag.Items()[0].Code != code || bag.Items()[0].Message != test.message {
					t.Fatalf("want exactly %s %q, got %s", code, test.message, diagnosticsSummary(bag))
				}
				item := bag.Items()[0]
				if item.Primary.End <= item.Primary.Start {
					t.Fatal("attribute diagnostic has no source location")
				}
				if promise && (len(item.Notes) != 1 || len(item.Help) != 1 || item.Notes[0].Span != item.Primary || item.Help[0].Msg == "") {
					t.Fatalf("return-source diagnostic lacks its rule note or repair help: %+v", item)
				}
				return
			}
			if bag.HasErrors() {
				t.Fatalf("valid declaration rejected: %s", diagnosticsSummary(bag))
			}
			var subject *symbols.Symbol
			data := syms.Table.Symbols.Data()
			for i := range data {
				sym := &data[i]
				if sym != nil {
					name := builder.StringsInterner.MustLookup(sym.Name)
					if name == "subject" || name == "Subject" {
						subject = sym
					}
				}
			}
			if subject == nil {
				t.Fatal("subject has no symbol")
			}
			checker := typeChecker{types: result.TypeInterner}
			info, ok := result.TypeInterner.FnInfo(checker.resolveAlias(subject.Type))
			if !ok || info == nil {
				t.Fatal("accepted declaration has no typed function contract")
			}
			unmarked := test.name == "existing_allow_to_named" || test.name == "existing_arena_named"
			if info.ReturnSources().IsAllInputs() != unmarked || (!unmarked && !slices.Equal(info.ReturnSources().Slots(), []uint32{0})) {
				t.Fatalf("wrong typed sources: %+v", info.ReturnSources())
			}
			if unmarked {
				return
			}
			if len(result.ReturnSourceDeclarations) != 1 {
				t.Fatalf("typed requests = %d, want 1", len(result.ReturnSourceDeclarations))
			}
			request := result.ReturnSourceDeclarations[0]
			if request.Result() != info.Result || !slices.Equal(request.Params(), info.Params) ||
				len(request.Syntax.Markers()) != 1 || request.Syntax.Markers()[0].Span.End <= request.Syntax.Markers()[0].Span.Start {
				t.Fatal("request lost original typed roots or marker span")
			}
			want := ReturnSourcesValid
			if test.name == "generic_deferred" {
				want = ReturnSourcesDeferred
			}
			if state := ValidateDeclaredReturnSources(result.TypeInterner, request).Status; state != want {
				t.Fatalf("original eligibility = %v, want %v", state, want)
			}
			if test.name == "generic_deferred" {
				checkConditionalReturnSource(t, result.TypeInterner, request)
				checkReturnSourceAliasSpecializations(t)
			}
		})
	}
}

func checkReturnSourceAliasSpecializations(t *testing.T) {
	t.Helper()
	for _, src := range []string{
		"type F<T> = fn(@return_source T) -> T; fn use(a: F<int>, b: F<int>) {}",
		"fn use(a: F<int>) {} type F<T> = fn(@return_source T) -> T;",
		"type Forward = F<int>; type F<T> = fn(@return_source T) -> T; fn use(a: Forward) {}",
		"type Forward = Outer<int>; type Outer<T> = fn(F<T>) -> nothing; type F<T> = fn(@return_source T) -> T; fn use(a: Forward) {}",
	} {
		builder, file, parseBag := parseSource(t, src)
		if parseBag.HasErrors() {
			t.Fatalf("alias source parse failed: %s", diagnosticsSummary(parseBag))
		}
		syms := resolveSymbols(t, builder, file)
		bag := diag.NewBag(64)
		result := Check(context.Background(), builder, file, Options{Symbols: syms, Reporter: &diag.BagReporter{Bag: bag}})
		if bag.HasErrors() {
			t.Fatalf("conditional alias rejected: %s\n%s", diagnosticsSummary(bag), src)
		}
		if len(result.ReturnSourceDeclarations) != 1 {
			t.Fatalf("original alias requests = %d, want 1", len(result.ReturnSourceDeclarations))
		}
		original := result.ReturnSourceDeclarations[0]
		if !original.TypeExpr.IsValid() || ValidateDeclaredReturnSources(result.TypeInterner, original).Status != ReturnSourcesDeferred ||
			!types.ContainsGenericParam(result.TypeInterner, original.Result()) {
			t.Fatal("alias lost original AST identity or generic result")
		}
		foundOwned := false
		for _, use := range result.ReturnSourceInstantiations {
			if use.Result() == result.TypeInterner.Builtins().Int {
				foundOwned = true
				if use.Declaration.TypeExpr != original.TypeExpr || use.Declaration.Result() != original.Result() ||
					ValidateInstantiatedReturnSources(result.TypeInterner, use.Declaration, use.Params(), use.Result()).Status != ReturnSourcesValid {
					t.Fatal("owned alias specialization lost its conditional original promise")
				}
			}
		}
		if !foundOwned {
			t.Fatal("source never reached an owned callback specialization")
		}
		t.Logf("validated alias specialization: %s", src)
	}
}

func checkConditionalReturnSource(t *testing.T, in *types.Interner, original ReturnSourceDeclarationRequest) {
	t.Helper()
	owned := in.Builtins().String
	borrowed := in.Intern(types.MakeReference(owned, false))
	for _, test := range []struct {
		params []types.TypeID
		result types.TypeID
		want   ReturnSourceValidationStatus
	}{
		{[]types.TypeID{owned}, owned, ReturnSourcesValid},
		{[]types.TypeID{borrowed}, borrowed, ReturnSourcesValid},
		{[]types.TypeID{owned}, borrowed, ReturnSourcesInvalid},
		{original.Params(), original.Result(), ReturnSourcesDeferred},
	} {
		if got := ValidateInstantiatedReturnSources(in, original, test.params, test.result); got.Status != test.want {
			t.Fatalf("conditional eligibility = %+v, want %v", got, test.want)
		}
	}
	directOwned := NewReturnSourceDeclarationRequest(original.Syntax, []types.TypeID{owned}, owned)
	if ValidateInstantiatedReturnSources(in, directOwned, []types.TypeID{owned}, owned).Status != ReturnSourcesInvalid {
		t.Fatal("owned specialization erased an invalid original concrete declaration")
	}
	params := original.Params()
	params[0] = owned
	if original.Params()[0] == owned {
		t.Fatal("request exposed its original typed input storage")
	}
	union := in.RegisterUnion(in.Strings.Intern("ReturnSourceTestPayload"), original.Syntax.Span())
	in.SetUnionMembers(union, []types.UnionMember{{Kind: types.UnionMemberTag, TagArgs: []types.TypeID{borrowed}}})
	if got := ValidateDeclaredReturnSources(in, NewReturnSourceDeclarationRequest(original.Syntax, []types.TypeID{borrowed}, union)); got.Status != ReturnSourcesValid {
		t.Fatalf("tag payload borrow lost: %+v", got)
	}
}
