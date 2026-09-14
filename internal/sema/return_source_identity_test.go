package sema

import (
	"context"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/symbols"
	"surge/internal/types"
)

func returnSourceIdentityFixture(t *testing.T, marked bool) (*symbols.FunctionSignature, CallableCandidate) {
	t.Helper()
	mark := ""
	if marked {
		mark = "@return_source "
	}
	builder, file, parseBag := parseSource(t, "fn subject("+mark+"a: &string, b: &string) -> &string { return a; }")
	if parseBag.HasErrors() {
		t.Fatal(diagnosticsSummary(parseBag))
	}
	syms := resolveSymbols(t, builder, file)
	bag := diag.NewBag(32)
	result := Check(context.Background(), builder, file, Options{Symbols: syms, Reporter: &diag.BagReporter{Bag: bag}})
	if bag.HasErrors() || len(result.CallableCandidates) != 1 {
		t.Fatalf("fixture not typed: %s, candidates=%d", diagnosticsSummary(bag), len(result.CallableCandidates))
	}
	candidate := result.CallableCandidates[0]
	sym := syms.Table.Symbols.Get(candidate.Symbol)
	info, ok := result.TypeInterner.FnInfo(sym.Type)
	if !ok || !candidate.ReturnSources.Equal(info.ReturnSources()) {
		t.Fatal("rememberCallableCandidate lost the typed declared promise")
	}
	return sym.Signature, candidate
}

func TestReturnSourceCallableIdentity(t *testing.T) {
	for _, name := range []string{"signature", "body_key", "candidate_copy", "candidate_equivalence", "legacy_all"} {
		t.Run(name, func(t *testing.T) {
			sig, candidate := returnSourceIdentityFixture(t, true)
			plainSig, _ := returnSourceIdentityFixture(t, false)
			plain := candidate
			plain.ReturnSources = types.ReturnSources{}
			switch name {
			case "signature":
				if canonicalSignatureIdentity(sig) == canonicalSignatureIdentity(plainSig) {
					t.Fatal("declaration identities collapsed distinct promises")
				}
			case "body_key":
				if canonicalCallableBodyKey(&candidate) == canonicalCallableBodyKey(&plain) {
					t.Fatal("body identity omitted declared sources")
				}
			case "candidate_copy":
				copy := cloneCallableCandidate(&candidate)
				slots := copy.ReturnSources.Slots()
				slots[0] = 99
				if !copy.ReturnSources.Equal(candidate.ReturnSources) || copy.ReturnSources.Slots()[0] != 0 {
					t.Fatal("candidate clone exposed mutable promise storage")
				}
			case "candidate_equivalence":
				candidate.BodyKey, plain.BodyKey = "same", "same"
				if callableCandidateRecordsEquivalent(&candidate, &plain) {
					t.Fatal("manual candidate equivalence ignored declared sources")
				}
				copy := cloneCallableCandidate(&candidate)
				if !callableCandidateRecordsEquivalent(&candidate, &copy) {
					t.Fatal("equal candidate clone no longer equivalent")
				}
			case "legacy_all":
				want := encodeInstantiationIdentity([]string{"2", "&string", "false", "&string", "false", "&string", "false"})
				if canonicalSignatureIdentity(plainSig) != want {
					t.Fatal("AllInputs changed the legacy signature bytes")
				}
				if strings.Contains(canonicalCallableBodyKey(&plain), "@return_source") {
					t.Fatal("legacy body key gained metadata")
				}
			}
		})
	}
}
