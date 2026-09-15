package sema

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/source"
	"surge/internal/symbols"
)

// returnOriginTailSpan is a [start,end) byte range computed over the frozen source bytes.
type returnOriginTailSpan struct {
	start, end uint32
	text       string
}

type returnOriginTailEscape struct {
	at    returnOriginTailSpan
	owner string
	decl  returnOriginTailSpan
}

type returnOriginTailPending struct {
	at     returnOriginTailSpan
	reason string
}

// returnOriginTailCase is one leaf's expectation with the whole analysis in place.
// Diagnostics are exact. Pending is exact unless contains is set; absent lists
// obligations that must not exist, and a region admits no Pending inside it
// other than the listed ones.
type returnOriginTailCase struct {
	name, digest, text string
	allowOldEscape     bool
	summary            string
	escapes            []returnOriginTailEscape
	pending            []returnOriginTailPending
	contains           bool
	absent             []returnOriginTailPending
	region             *returnOriginTailSpan
	complete, unknown  bool
	slots              []uint32
}

const (
	returnOriginTailResultReason   = "function result contains an unproved source"
	returnOriginTailOutgoingReason = "outgoing reference has unresolved or captured provenance"
	returnOriginTailStoreReason    = "store through a place needs reference-content transfer"
)

// A compare or select arm's implicit tail is the value of its block, exactly as
// sema types it and HIR lowers it: code after the compare stays visible, and a
// written `return` inside an arm stays a function return.
func TestReturnOriginArmTailIsBlockResult(t *testing.T) {
	ownedLocal := returnOriginTailSpan{53, 81, `let owned: string = "local";`}
	for _, tc := range []returnOriginTailCase{
		{name: "tail_value_then_local_return", digest: "d3509cd969aa3786326476dbf2339d2b4b410679d77bd1f8bf50f529006656bc",
			allowOldEscape: true, complete: true, unknown: true, text: `pragma no_std;
fn probe(flag: bool) -> &string {
    let owned: string = "local";
    let n: int = compare flag { true => { 1; } false => { 2; } };
    return &owned;
}
`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{152, 166, "return &owned;"}, "owned", ownedLocal}}},
		{name: "bare_tail_then_local_return", digest: "89890e2efd1315fc5fe610f3f7911bc1a3650d0a6cfe9eb0068d8dd39ac077c7",
			allowOldEscape: true, complete: true, unknown: true, text: `pragma no_std;
fn probe(flag: bool) -> &string {
    let owned: string = "local";
    compare flag { true => { let m: int = 1; } false => { let m: int = 2; } };
    return &owned;
}
`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{165, 179, "return &owned;"}, "owned", ownedLocal}}},
		{name: "tail_value_then_block_exit", digest: "5bc46ebc01ddb20e424998d27a32ad566e23287b17a25bca13b97a03a462f652",
			complete: true, text: returnOriginTailBlockExitSource,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{185, 196, "ret &owned;"}, "owned",
				returnOriginTailSpan{148, 176, `let owned: string = "owned";`}}}},
		{name: "explicit_return_inside_arm", digest: "9605c87136641d792958cf15f831d9444b063f48707ff313e2945ff97c4cf0ce",
			allowOldEscape: true, complete: true, unknown: true, slots: []uint32{1}, text: `pragma no_std;
fn probe(flag: bool, outside: &string) -> &string {
    let owned: string = "owned";
    let n: int = compare flag { true => { return &owned; } false => { 2; } };
    return outside;
}
`,
			escapes: []returnOriginTailEscape{{returnOriginTailSpan{142, 156, "return &owned;"}, "owned",
				returnOriginTailSpan{71, 99, `let owned: string = "owned";`}}}},
		{name: "tail_value_into_returned_binding", digest: "45a30a13c7bbca6a17092d2eb602a91f46b07cbe42e7133edd9dcf0a3a2f6a6d",
			complete: true, slots: []uint32{1, 2}, text: `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    let chosen: &string = compare flag { true => { a; } false => { a; } };
    if flag { return chosen; }
    return b;
}
`},
		{name: "tail_arm_in_loop_backedge", digest: "39fdc9ea004e84fd5a9855a46ce57fb14d1f32d8b14bbfbafb1e1d13a8885a2e",
			complete: true, slots: []uint32{1, 2}, text: `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    let mut alias: &string = a;
    let mut i: int = 0;
    while i < 2 {
        let step: int = compare flag { true => { 1; } false => { 2; } };
        alias = b;
        i = i + step;
    }
    return alias;
}
`},
		{name: "ret_and_tail_join", digest: "1527078d293328d09f1cf549d510d427d1dad95d020d66487006f0b1d0f6dae9",
			complete: true, slots: []uint32{1, 2}, text: `pragma no_std;
fn probe(flag: bool, a: &string, b: &string) -> &string {
    let mut alias: &string = a;
    let n: int = compare flag { true => { if flag { ret 1; } alias = b; 2; } false => { 3; } };
    return alias;
}
`},
	} {
		t.Run(tc.name, func(t *testing.T) { checkReturnOriginTailCase(t, tc) })
	}
}

// Shared with the public driver leaf of the same name, byte for byte.
const returnOriginTailBlockExitSource = `pragma no_std;
fn probe(flag: bool) -> int {
    let n: int = compare flag { true => { 1; } false => { 2; } };
    let escaped: &string = {
        let owned: string = "owned";
        ret &owned;
    };
    return n;
}
`

func checkReturnOriginTailCase(t *testing.T, tc returnOriginTailCase) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.text))); got != tc.digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	var spans []returnOriginTailSpan
	for _, escape := range tc.escapes {
		spans = append(spans, escape.at, escape.decl)
	}
	for _, list := range [][]returnOriginTailPending{tc.pending, tc.absent} {
		for _, pending := range list {
			spans = append(spans, pending.at)
		}
	}
	if tc.region != nil {
		spans = append(spans, *tc.region)
	}
	for _, span := range spans {
		if span.start > span.end || int(span.end) > len(tc.text) || tc.text[span.start:span.end] != span.text {
			t.Fatalf("PRECONDITION: span [%d,%d) does not hold %q", span.start, span.end, span.text)
		}
	}
	result, unit := returnOriginPublicationFixture(t, tc.text, tc.allowOldEscape)
	analysis, err := AnalyzeReturnOrigins(t.Context(), result, []ReturnOriginUnit{unit})
	encoded, marshalErr := json.Marshal(analysis)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	t.Logf("RETURN_ORIGIN_TAIL_ANALYSIS case=%s sha256=%s error=%v analysis=%s", tc.name, tc.digest, err, encoded)
	if err != nil || analysis == nil {
		t.Fatalf("analysis did not run: %v", err)
	}
	file := unit.Builder.Files.Get(unit.FileID).Span.File
	at := func(span returnOriginTailSpan) source.Span {
		return source.Span{File: file, Start: span.start, End: span.end}
	}
	checkReturnOriginTailEscapes(t, tc, unit, analysis, at)
	want := make([]ReturnOriginPending, 0, len(tc.pending))
	for _, pending := range tc.pending {
		want = append(want, ReturnOriginPending{SourceKey: unit.SourceKey, Span: at(pending.at), Reason: pending.reason})
	}
	if tc.contains {
		for _, item := range want {
			if !slices.Contains(analysis.Pending, item) {
				t.Errorf("required Pending is missing: %+v", item)
			}
		}
	} else if !slices.Equal(analysis.Pending, want) {
		t.Errorf("Pending=%+v, want exactly %+v", analysis.Pending, want)
	}
	for _, pending := range tc.absent {
		item := ReturnOriginPending{SourceKey: unit.SourceKey, Span: at(pending.at), Reason: pending.reason}
		if slices.Contains(analysis.Pending, item) {
			t.Errorf("forbidden Pending is present: %+v", item)
		}
	}
	if tc.region != nil {
		for _, pending := range analysis.Pending {
			inside := pending.Span.File == file && pending.Span.Start >= tc.region.start && pending.Span.End <= tc.region.end
			if inside && !slices.Contains(want, pending) {
				t.Errorf("unexpected Pending inside [%d,%d): %+v", tc.region.start, tc.region.end, pending)
			}
		}
	}
	if analysis.Complete() != tc.complete {
		t.Errorf("Complete()=%t, want %t", analysis.Complete(), tc.complete)
	}
	name := tc.summary
	if name == "" {
		name = "probe"
	}
	var summary *ReturnOriginSummary
	for i := range analysis.Summaries {
		if analysis.Summaries[i].Name == name {
			if summary != nil {
				t.Fatalf("PRECONDITION: duplicate summary for %s", name)
			}
			summary = &analysis.Summaries[i]
		}
	}
	if summary == nil {
		t.Fatalf("PRECONDITION: no summary for %s", name)
	}
	if summary.NoNormalReturn || summary.Unknown != tc.unknown || !slices.Equal(summary.ParamSlots, tc.slots) {
		t.Errorf("summary %s = %+v, want slots=%v unknown=%t", name, *summary, tc.slots, tc.unknown)
	}
}

func checkReturnOriginTailEscapes(t *testing.T, tc returnOriginTailCase, unit ReturnOriginUnit, analysis *ReturnOriginAnalysis, at func(returnOriginTailSpan) source.Span) {
	t.Helper()
	if len(analysis.Diagnostics) != len(tc.escapes) {
		t.Fatalf("diagnostics=%+v, want exactly %d SEM3139", analysis.Diagnostics, len(tc.escapes))
	}
	for i, want := range tc.escapes {
		owner := returnOriginTailOwner(t, unit, want.owner)
		if owner.Span != at(want.decl) {
			t.Fatalf("PRECONDITION: owner %s declared at %v, want %v", want.owner, owner.Span, at(want.decl))
		}
		d := analysis.Diagnostics[i]
		notes := []diag.Note{{Span: at(want.decl), Msg: fmt.Sprintf("'%s' owns storage that ends in this scope", want.owner)}}
		help := []diag.Note{{Span: at(want.at), Msg: "keep the owner outside this scope, or return an owned value instead"}}
		if d.Code != diag.SemaBorrowEscapesReturn || d.Severity != diag.SevError || d.Primary != at(want.at) ||
			d.Message != fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", want.owner) ||
			!slices.Equal(d.Notes, notes) || !slices.Equal(d.Help, help) {
			t.Errorf("diagnostic %d = %+v, want SEM3139 at %v for %s", i, d, at(want.at), want.owner)
		}
	}
}

func returnOriginTailOwner(t *testing.T, unit ReturnOriginUnit, name string) *symbols.Symbol {
	t.Helper()
	var owner *symbols.Symbol
	for i := range unit.Symbols.Table.Symbols.Data() {
		sym := unit.Symbols.Table.Symbols.Get(symbols.SymbolID(i + 1))
		text, _ := unit.Builder.StringsInterner.Lookup(sym.Name)
		if text == name && (sym.Kind == symbols.SymbolLet || sym.Kind == symbols.SymbolParam) {
			if owner != nil {
				t.Fatalf("PRECONDITION: owner %s is ambiguous", name)
			}
			owner = sym
		}
	}
	if owner == nil {
		t.Fatalf("PRECONDITION: owner %s is missing", name)
	}
	return owner
}
