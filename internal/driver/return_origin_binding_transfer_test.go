package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
)

const dropObligation = "explicit drop needs owner-incarnation invalidation"

type dropSpan struct {
	start, end uint32
	text       string
}

type dropLeaf struct {
	name, digest, text string
	// allowEscape tolerates an eager SEM3139 in the root bag (the leaf may draw one).
	allowEscape bool
	// drops are every `@drop` statement span in the source.
	drops []dropSpan
	// keepsObligation names the leaf whose target the rule must NOT admit.
	keepsObligation bool
	// escape is the span that carries the analysis SEM3139 the expiry produces.
	escape *dropSpan
	// quiet spans must carry no analysis diagnostic at all after.
	quiet []dropSpan
	// cleanRoot demands an empty root Pending set after.
	cleanRoot bool
	// eagerMustBeEmpty asserts the source bag carries no refusal at the escape
	// span, so the analysis is measurably the only refusal there.
	eagerMustBeEmpty bool
}

// `drop_owner_then_alias_read` and `drop_owner_alias_outer_dead` read an alias of
// a dropped heap owner. Stage A S-A5 measured that sema admits that read, that the
// analysis raises exactly one escape at the recorded span, and that the source bag
// holds nothing there — so the expiry is the only refusal of that use-after-free,
// and both leaves assert all three. They keep `allowEscape`, so the leaf still
// states its own fact if the eager checker ever starts refusing at the same span.
func dropLeaves() []dropLeaf {
	at := func(start, end uint32, text string) dropSpan { return dropSpan{start, end, text} }
	return []dropLeaf{
		{name: "drop_reference", digest: "a8fdfce4c840596eb5d5cebfe1b77d285410df7bdda62b8df8a3fa7b69713bef",
			text:      "fn t() -> nothing {\n    let mut x: int = 1;\n    let r: &int = &x;\n    @drop r;\n    x = 2;\n    return nothing;\n}\n",
			drops:     []dropSpan{at(70, 78, "@drop r;")},
			cleanRoot: true},
		{name: "drop_owner_string", digest: "4619a66ad7a88aa1d40544ad8d9460255b76501013baa13455f6ad4493fa5a08",
			text:      "fn t() -> nothing {\n    let s: string = \"a\";\n    @drop s;\n    return nothing;\n}\n",
			drops:     []dropSpan{at(49, 57, "@drop s;")},
			cleanRoot: true},
		{name: "drop_then_new_incarnation", digest: "02ca14c6851d886ccd472be403a7192adf9208689924a688569d81db3d1114e4",
			text: "fn t() -> nothing {\n    let mut s: string = \"a\";\n    @drop s;\n    s = \"b\";\n    let r: &string = &s;\n" +
				"    let q: &string = r;\n    return nothing;\n}\n",
			drops:     []dropSpan{at(53, 61, "@drop s;")},
			quiet:     []dropSpan{at(121, 122, "r")},
			cleanRoot: true},
		{name: "drop_in_else_branch", digest: "ac4cf2b0da11add46b7d9475a10cb0f0593d023ed5fa818cd6356e5fa2d58736",
			text: "fn t(flag: bool) -> nothing {\n    let key: string = \"k\";\n    if flag {\n        let kept: string = key;\n" +
				"    } else {\n        @drop key;\n    }\n    return nothing;\n}\n",
			drops:     []dropSpan{at(124, 134, "@drop key;")},
			cleanRoot: true},
		{name: "drop_copy_then_read", digest: "2af545f4745cb03ba8d389d70d09eedc86ece7b5ad0c626f7c28b9429d9e623f",
			text: "fn t() -> int32 {\n    let x: int32 = 5;\n    let r: &int32 = &x;\n    @drop x;\n    let y: int32 = *r;\n" +
				"    return y;\n}\n",
			drops:     []dropSpan{at(68, 76, "@drop x;")},
			quiet:     []dropSpan{at(97, 98, "r")},
			cleanRoot: true},
		// `tag` is a reserved word (internal/token/keywords.go), so the first
		// spelling of this fixture never parsed and never reached the analysis.
		{name: "drop_projection_stays_pending", digest: "736637b9018b0dc98688df1da8d6c5f0baeb2ed5bbfdaa773b4ffda3917fefbe",
			text: "type Outer = { inner: string, mark: int };\nfn t() -> nothing {\n    let o: Outer = Outer { inner = \"a\", mark = 1 };\n" +
				"    @drop o.inner;\n    return nothing;\n}\n",
			drops:           []dropSpan{at(119, 133, "@drop o.inner;")},
			keepsObligation: true},
		{name: "drop_does_not_hide_escape", digest: "b3abb3c9dcce04d0f676d5d8d287aa9bb2bd02fee477e0637f13696ca139b5c8",
			text:        "fn leak() -> &string {\n    let s: string = \"a\";\n    let r: &string = &s;\n    @drop r;\n    return &s;\n}\n",
			allowEscape: true,
			drops:       []dropSpan{at(77, 85, "@drop r;")},
			escape:      &dropSpan{90, 100, "return &s;"}},
		{name: "drop_owner_then_alias_read", digest: "e3b5dde51bf0342807a33611a0feb189b07dcef2edbef903886ef2c88ed5b240",
			text: "fn t() -> nothing {\n    let s: string = \"a\";\n    let r: &string = &s;\n    @drop s;\n    let q: &string = r;\n" +
				"    return nothing;\n}\n",
			allowEscape:      true,
			drops:            []dropSpan{at(74, 82, "@drop s;")},
			escape:           &dropSpan{104, 105, "r"},
			eagerMustBeEmpty: true},
		{name: "drop_owner_alias_outer_dead", digest: "0e8180bd976abe60022b08700d1e8197b624274eb8915f598c16b3b07f6c43a0",
			text: "fn t(flag: bool) -> nothing {\n    let s: string = \"a\";\n    let r: &string = &s;\n    if flag {\n        @drop s;\n    }\n" +
				"    return nothing;\n}\n",
			allowEscape:      true,
			drops:            []dropSpan{at(102, 110, "@drop s;")},
			escape:           &dropSpan{92, 116, "{\n        @drop s;\n    }"},
			eagerMustBeEmpty: true},
	}
}

// `@drop` of a binding whose storage the drop releases ends that owner's
// incarnation, so the analysis stops owing an obligation at the statement and
// starts refusing borrows of the freed storage.
func TestAnalyzeReturnOriginDropTransfer(t *testing.T) {
	for _, leaf := range dropLeaves() {
		t.Run(leaf.name, func(t *testing.T) { checkDropLeaf(t, leaf) })
	}
}

func checkDropLeaf(t *testing.T, leaf dropLeaf) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(leaf.text))); got != leaf.digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	spans := slices.Clone(leaf.drops)
	spans = append(spans, leaf.quiet...)
	if leaf.escape != nil {
		spans = append(spans, *leaf.escape)
	}
	for _, span := range spans {
		if int(span.end) > len(leaf.text) || leaf.text[span.start:span.end] != span.text {
			t.Fatalf("PRECONDITION: span [%d,%d) does not hold %q", span.start, span.end, span.text)
		}
	}
	res := returnOriginStdlibFixture(t, leaf.text, leaf.allowEscape)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil {
		t.Fatalf("PRECONDITION: owning units: %v", err)
	}
	rootKey, _ := checkReturnOriginCloneUnits(t, res, inputs.units)
	at := func(span dropSpan) source.Span {
		return source.Span{File: res.File.ID, Start: span.start, End: span.end}
	}
	// The eager checker may already refuse at the span this leaf watches. Record
	// that bit per span, so a leaf's escape is never read as the analysis's alone.
	eager := map[string]int{}
	if leaf.escape != nil {
		for _, d := range res.Bag.Items() {
			if d != nil && d.Code == diag.SemaBorrowEscapesReturn && d.Primary == at(*leaf.escape) {
				eager[leaf.escape.text]++
			}
		}
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), res.Sema, inputs.units)
	logReturnOriginCallEvidence(t, map[string]any{"case": leaf.name, "analysis": analysis,
		"error": errorReturnOriginCallText(err), "typed_diagnostics": res.Bag.Items(), "eager_escape_at_span": eager})
	if err != nil || analysis == nil {
		t.Fatalf("analysis did not run: %v", err)
	}
	var root []sema.ReturnOriginPending
	for _, pending := range analysis.Pending {
		if pending.SourceKey == rootKey {
			root = append(root, pending)
		}
	}
	for _, span := range leaf.drops {
		obligation := sema.ReturnOriginPending{SourceKey: rootKey, Span: at(span), Reason: dropObligation}
		held := slices.Contains(root, obligation)
		if leaf.keepsObligation && !held {
			t.Errorf("a drop target the rule must not admit lost its obligation at %v: root %+v", at(span), root)
		}
		if !leaf.keepsObligation && held {
			t.Errorf("the drop at %v kept its obligation: root %+v", at(span), root)
		}
	}
	escapes := 0
	if leaf.escape != nil {
		for _, d := range analysis.Diagnostics {
			if d.Code == diag.SemaBorrowEscapesReturn && d.Primary == at(*leaf.escape) {
				escapes++
			}
		}
	}
	for _, span := range leaf.quiet {
		for _, d := range analysis.Diagnostics {
			if d.Primary == at(span) {
				t.Errorf("a live borrow was refused at %v: %+v", at(span), d)
			}
		}
	}
	if leaf.escape != nil && escapes != 1 {
		t.Errorf("%d escape refusals at %v, want exactly one: %+v", escapes, at(*leaf.escape), analysis.Diagnostics)
	}
	if leaf.eagerMustBeEmpty && len(eager) != 0 {
		t.Errorf("the source bag already refused at %v, so the expiry is not the only refusal: %v", at(*leaf.escape), eager)
	}
	if leaf.cleanRoot && len(root) != 0 {
		t.Errorf("root unit kept obligations: %+v", root)
	}
}
