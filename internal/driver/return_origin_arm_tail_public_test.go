package driver

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
)

type publicTailEscape struct {
	start, end, declStart, declEnd uint32
	owner, text, decl              string
}

type publicTailCase struct {
	name, digest, text string
	escape             publicTailEscape
}

func publicTailCases() []publicTailCase {
	return []publicTailCase{
		{name: "tail_value_then_block_exit", digest: "5bc46ebc01ddb20e424998d27a32ad566e23287b17a25bca13b97a03a462f652", text: `pragma no_std;
fn probe(flag: bool) -> int {
    let n: int = compare flag { true => { 1; } false => { 2; } };
    let escaped: &string = {
        let owned: string = "owned";
        ret &owned;
    };
    return n;
}
`, escape: publicTailEscape{185, 196, 148, 176, "owned", "ret &owned;", `let owned: string = "owned";`}},
		{name: "tag_local_owner_return", digest: "3cbab0c97157fbc8d3cf3bc67c7034ee4891a4a822534d61df5b25a796c58efc", text: `pragma no_std;
tag Hold<T>(T);
type Held<T> = Hold(T) | nothing;
fn probe() -> Held<&string> {
    let owned: string = "local";
    return Hold::<&string>(&owned);
}
`, escape: publicTailEscape{132, 163, 99, 127, "owned", "return Hold::<&string>(&owned);", `let owned: string = "local";`}},
	}
}

// A finished core-free public diagnosis publishes exactly the analysis refusal
// and builds no HIR, whether or not HIR was requested.
func TestDiagnoseArmTailAndRefusedReturn(t *testing.T) {
	for _, emitHIR := range []bool{false, true} {
		t.Run(fmt.Sprintf("emit_hir_%t", emitHIR), func(t *testing.T) {
			for _, tc := range publicTailCases() {
				t.Run(tc.name, func(t *testing.T) { checkPublicTailCase(t, tc, emitHIR) })
			}
		})
	}
}

func checkPublicTailCase(t *testing.T, tc publicTailCase, emitHIR bool) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	e := tc.escape
	if int(e.end) > len(tc.text) || tc.text[e.start:e.end] != e.text || tc.text[e.declStart:e.declEnd] != e.decl {
		t.Fatal("PRECONDITION: frozen spans moved")
	}
	t.Setenv("SURGE_STDLIB", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "origin.sg")
	if err := os.WriteFile(path, []byte(tc.text), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := DiagnoseOptions{Stage: DiagnoseStageAll, BaseDir: dir, MaxDiagnostics: 64,
		IgnoreWarnings: true, KeepArtifacts: true, EmitHIR: emitHIR}
	res, err := DiagnoseWithOptions(t.Context(), path, &opts)
	var unfinished *returnOriginUnfinishedError
	if errors.As(err, &unfinished) {
		for _, pending := range unfinished.Pending {
			if strings.HasPrefix(pending.SourceKey, "core/") {
				t.Fatalf("PRECONDITION: core was loaded: %+v", pending)
			}
		}
	}
	if err != nil {
		t.Fatalf("public diagnosis did not finish: %v", err)
	}
	if res == nil || res.Bag == nil || res.File == nil {
		t.Fatal("public diagnosis returned no result")
	}
	for key, rec := range res.moduleRecords {
		if strings.HasPrefix(key, "core") || rec != nil && rec.Meta != nil && strings.HasPrefix(rec.Meta.Path, "core") {
			t.Fatalf("PRECONDITION: core module %s was loaded", key)
		}
	}
	items, marshalErr := json.Marshal(res.Bag.Items())
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	t.Logf("RETURN_ORIGIN_TAIL_PUBLIC case=%s emit_hir=%t diagnostics=%s", tc.name, emitHIR, items)
	var errs []*diag.Diagnostic
	for _, d := range res.Bag.Items() {
		if d != nil && d.Severity >= diag.SevError {
			errs = append(errs, d)
		}
	}
	primary := source.Span{File: res.File.ID, Start: e.start, End: e.end}
	notes := []diag.Note{{Span: source.Span{File: res.File.ID, Start: e.declStart, End: e.declEnd},
		Msg: fmt.Sprintf("'%s' owns storage that ends in this scope", e.owner)}}
	if len(errs) != 1 || errs[0].Code != diag.SemaBorrowEscapesReturn || errs[0].Primary != primary ||
		errs[0].Message != fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", e.owner) ||
		!slices.Equal(errs[0].Notes, notes) {
		t.Fatalf("published errors=%s, want exactly one SEM3139 at %v", items, primary)
	}
	if res.HIR != nil {
		t.Fatal("refused source reached HIR")
	}
}

// A view stored into a reference-free field loses its loan with the
// payload-free refusal, not with the place-store taint.
func TestAnalyzeReturnOriginPlaceStoreLoanDiscard(t *testing.T) {
	const text = `type Holder = { view: uint64[] };
fn probe() -> nothing {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut h: Holder = Holder { view = [] };
    h.view = xs[[1..3]];
    return nothing;
}
`
	const store = "h.view = xs[[1..3]]"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != "63e42fdfd2988ddfbe5f7c4b208f8a48fe4c9b11e3e7d32b82d2ee2dbabc1a35" {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	if text[174:193] != store {
		t.Fatal("PRECONDITION: frozen store span moved")
	}
	res := returnOriginStdlibFixture(t, text, false)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatalf("PRECONDITION: owning units: %v", err)
	}
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	logReturnOriginCallEvidence(t, map[string]any{"case": "place_store_loan_discard", "analysis": analysis,
		"error": errorReturnOriginCallText(err), "typed_diagnostics": res.Bag.Items()})
	if err != nil || analysis == nil {
		t.Fatalf("analysis did not run: %v", err)
	}
	at := source.Span{File: res.File.ID, Start: 174, End: 193}
	discard := sema.ReturnOriginPending{SourceKey: rootKey, Span: at, Reason: backingLoanDiscard}
	taint := sema.ReturnOriginPending{SourceKey: rootKey, Span: at, Reason: "store through a place needs reference-content transfer"}
	if !slices.Contains(analysis.Pending, discard) || slices.Contains(analysis.Pending, taint) {
		t.Fatalf("store %q: want the loan-discard Pending and no taint, got %+v", store, analysis.Pending)
	}
}

// An implicit __to may return a view of its own receiver; the store never
// evaluates the conversion, so a reference-free field store keeps its taint.
// Returning h is refused by the eager borrow checker (SEM3020), so h stays local.
func TestAnalyzeReturnOriginPlaceStoreKeepsConversionTaint(t *testing.T) {
	const text = `type Src = { items: uint64[4] };
type Holder = { view: uint64[] };
extern<Src> {
    fn __to(self: &Src, _: uint64[]) -> uint64[] {
        return self.items[[0..4]];
    }
}
fn probe() -> nothing {
    let src: Src = Src { items = [1:uint64, 2:uint64, 3:uint64, 4:uint64] };
    let mut h: Holder = Holder { view = [] };
    h.view = src;
    return nothing;
}
`
	const store = "h.view = src"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != "bfbca5e493aa7a8d914d00c68b0111aa4507ecb0c1c30cc5385918a89f53b5be" {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	if text[326:338] != store {
		t.Fatal("PRECONDITION: frozen store span moved")
	}
	res := returnOriginStdlibFixture(t, text, false)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatalf("PRECONDITION: owning units: %v", err)
	}
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	var converted bool
	for expr, conversion := range res.Sema.ImplicitConversions {
		node := res.Builder.Exprs.Get(expr)
		converted = converted || node != nil && node.Span.File == res.File.ID && node.Span.Start == 335 && node.Span.End == 338 &&
			conversion.Kind == sema.ImplicitConversionTo
	}
	if !converted {
		t.Fatal("PRECONDITION: the stored value lost its implicit __to conversion")
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	logReturnOriginCallEvidence(t, map[string]any{"case": "place_store_conversion_taint", "analysis": analysis,
		"error": errorReturnOriginCallText(err), "typed_diagnostics": res.Bag.Items()})
	if err != nil || analysis == nil {
		t.Fatalf("analysis did not run: %v", err)
	}
	at := source.Span{File: res.File.ID, Start: 326, End: 338}
	discard := sema.ReturnOriginPending{SourceKey: rootKey, Span: at, Reason: backingLoanDiscard}
	taint := sema.ReturnOriginPending{SourceKey: rootKey, Span: at, Reason: "store through a place needs reference-content transfer"}
	if !slices.Contains(analysis.Pending, taint) || slices.Contains(analysis.Pending, discard) {
		t.Fatalf("store %q: want the place-store taint and no loan discard, got %+v", store, analysis.Pending)
	}
}
