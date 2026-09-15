package driver

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"

	"surge/internal/sema"
)

// A certified Range step copies the walked element out of its base and never borrows the cursor
// variable; an array or cursor element keeps a refusal. Outside core a template cannot type such a
// call (type_labels.go:490–503), so core's own Array<T>.from_range witnesses the template step.
const (
	rangeNextMutableEffect = "mutable argument may replace reference-bearing contents"
	rangeNextOpaqueEffect  = "opaque call may change reference-bearing or callable contents"
	rangeNextLoanElement   = "cursor element that can hold storage loans needs its backing loan transfer"
	rangeNextRefuted       = "opaque result type may carry borrowed state"
	rangeNextCore          = "core/array.sg"
)

type rangeNextSpan struct {
	start, end int
	snippet    string
}

// rangeNextPending is one obligation; an empty unit means the test's own source.
type rangeNextPending struct {
	unit   string
	at     rangeNextSpan
	reason string
}

type rangeNextLeaf struct {
	name, function  string
	body            rangeNextSpan // the test source's function item; none for a core-only leaf
	clean, summary  bool          // clean: no own Pending inside body; summary: normal, known, exactly slots
	slots           []uint32
	quiet, silent   bool // quiet: no diagnostic in the test source; silent: none in any unit
	present, absent []rangeNextPending
}

// root: a root program with full core, whose finalized closure carries instance use sites.
type rangeNextSource struct {
	name, text, digest string
	root               bool
	leaves             []rangeNextLeaf
}

// The template step inside core's Array<T>.from_range.
var rangeNextCoreStep = rangeNextSpan{2130, 2141, "iter.next()"}

func rangeNextSources() []rangeNextSource {
	return []rangeNextSource{
		{name: "template_uses", digest: "914d19911a3abf19d09ef6986edb18bdeb9a2a6db51d1f5e8f5890944246dc21", text: `pragma module::dep;
fn use_int(xs: &uint64[]) -> uint64[] {
    return Array::<uint64>::from_range(xs.__range());
}
fn use_ref(r: Range<&string>) -> nothing {
    let _ = Array::<&string>::from_range(r);
    return nothing;
}
`, leaves: []rangeNextLeaf{
			{name: "use_int", function: "use_int", body: rangeNextSpan{20, 115, ""}, clean: true, quiet: true, summary: true},
			// The template body's recorded condition refuses a borrowing element at its call.
			{name: "use_ref", function: "use_ref", body: rangeNextSpan{116, 225, ""},
				present: []rangeNextPending{{"", rangeNextSpan{171, 202, "Array::<&string>::from_range(r)"}, rangeNextRefuted}}},
		}},
		// The step's finalized use from from_range<uint64[]> is refused at the core step itself.
		{name: "root_view", root: true, digest: "47bb42883810bd0b23fdc5a88f1be87948130ec87c0905bf06710257c5410e78", text: `fn use_view(r: Range<uint64[]>) -> nothing {
    let _ = Array::<uint64[]>::from_range(r);
    return nothing;
}
`, leaves: []rangeNextLeaf{
			{name: "use_view", function: "use_view", body: rangeNextSpan{0, 112, ""},
				present: []rangeNextPending{{rangeNextCore, rangeNextCoreStep, rangeNextLoanElement}}},
		}},
		{name: "int_control", digest: "e002a744a086e75e77593fd890c9222226625637f1b8578d727bf9bd93967575", text: `pragma module::dep;
fn first_int(n: int) -> Option<int> {
    let mut c: Range<int> = 0..n;
    return c.next();
}
`, leaves: []rangeNextLeaf{
			{name: "first_int", function: "first_int", body: rangeNextSpan{20, 114, ""}, clean: true, quiet: true, summary: true},
		}},
		{name: "loan_element", digest: "3a898b719e1dc48d6c1347099461b9875183f972c9eb7449d189b8d90cde2730", text: `pragma module::dep;
fn wrap<T>(x: T) -> Array<T> {
    let mut out: Array<T> = [];
    out.push(x);
    return out;
}
fn first_view() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let views: Array<uint64[]> = wrap::<uint64[]>(xs[[1..3]]);
    let mut c: Range<uint64[]> = views.__range();
    return c.next();
}
`, leaves: []rangeNextLeaf{
			{name: "first_view", function: "first_view", body: rangeNextSpan{118, 357, ""},
				present: []rangeNextPending{{"", rangeNextSpan{346, 354, "c.next()"}, rangeNextLoanElement}}},
		}},
		// Core from_range must stay free of any diagnostic in any unit, also with arm tails as block results.
		{name: "core_from_range", digest: "2d4aebfcb28d71ceceb6318ba2630e2c8a7220e3f1ba1fe8e7c6a5ac73735177", text: `pragma module::dep;
fn collect(n: int) -> int[] {
    return Array::<int>::from_range(0..n);
}
`, leaves: []rangeNextLeaf{
			{name: "from_range", silent: true, absent: []rangeNextPending{{rangeNextCore, rangeNextCoreStep, rangeNextMutableEffect},
				{rangeNextCore, rangeNextCoreStep, rangeNextOpaqueEffect}, {rangeNextCore, rangeNextCoreStep, rangeNextLoanElement}}},
			{name: "collect", function: "collect", body: rangeNextSpan{20, 94, ""}, clean: true, quiet: true, summary: true},
		}},
	}
}

func TestAnalyzeRangeNextOrigins(t *testing.T) {
	sources, leaves := rangeNextSources(), 0
	for _, src := range sources {
		leaves += len(src.leaves)
	}
	if len(sources) != 5 || leaves != 7 {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaves=%d", len(sources), leaves)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			f, analysis := analyzeRangeNextSource(t, src)
			for _, leaf := range src.leaves {
				t.Run(leaf.name, func(t *testing.T) { checkRangeNextLeaf(t, leaf, f, analysis) })
			}
		})
	}
}

// rangeNextRootFixture is the root-program, full-core harness of P1o's analyzeOriginRoot.
func rangeNextRootFixture(t *testing.T, text string) originalGenericFixture {
	t.Helper()
	res := returnOriginStdlibFixture(t, text, false)
	if err := FinalizeInstantiationClosure(t.Context(), res, 64); err != nil {
		t.Fatalf("PRECONDITION: source closure failed: %v", err)
	}
	inputs, err := collectReturnOriginUnits(res)
	if err != nil || len(inputs.units) != 11 {
		t.Fatalf("PRECONDITION: full eleven-unit input missing: units=%d error=%v", len(inputs.units), err)
	}
	checkReturnOriginStdlibBags(t, res, false)
	f, owners := originalGenericFixture{owner: res, authority: res.Sema, inputs: inputs}, 0
	for _, unit := range inputs.units {
		if unit.Builder.Files.Get(unit.FileID).Span.File == res.File.ID {
			f.unit, owners = unit, owners+1
		}
	}
	if owners != 1 {
		t.Fatalf("PRECONDITION: the test source has %d owning units", owners)
	}
	return f
}

func analyzeRangeNextSource(t *testing.T, src rangeNextSource) (originalGenericFixture, *sema.ReturnOriginAnalysis) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(src.text))); got != src.digest {
		t.Fatalf("PRECONDITION: frozen source %s changed: %s", src.name, got)
	}
	var f originalGenericFixture
	if src.root {
		f = rangeNextRootFixture(t, src.text)
	} else {
		f = originalGenericSignatureFixture(t, src.text, false, true)
	}
	for _, leaf := range src.leaves {
		if b := leaf.body; leaf.function != "" && (b.start < 0 || b.end > len(src.text) || b.start >= b.end ||
			!strings.HasPrefix(src.text[b.start:b.end], "fn "+leaf.function) || src.text[b.end-1] != '}') {
			t.Fatalf("PRECONDITION: frozen body %d:%d is not fn %s", b.start, b.end, leaf.function)
		}
		for _, want := range append(slices.Clone(leaf.present), leaf.absent...) {
			text, s := rangeNextUnitText(t, f, want.unit, src.text), want.at
			if s.start < 0 || s.end > len(text) || s.start >= s.end || text[s.start:s.end] != s.snippet {
				t.Fatalf("PRECONDITION: frozen span %s %d:%d is not %q", want.unit, s.start, s.end, s.snippet)
			}
		}
	}
	analysis, err := sema.AnalyzeReturnOrigins(t.Context(), f.authority, f.inputs.units)
	if err != nil || analysis == nil {
		t.Fatalf("return-origin analysis did not run: %v", err)
	}
	var local []sema.ReturnOriginPending
	for _, pending := range analysis.Pending {
		if pending.SourceKey == f.unit.SourceKey || pending.SourceKey == rangeNextCore {
			local = append(local, pending)
		}
	}
	logReturnOriginCallEvidence(t, map[string]any{"stage": "range_next", "source": src.name, "source_key": f.unit.SourceKey,
		"pending": local, "diagnostics": analysis.Diagnostics, "summaries": analysis.Summaries})
	return f, analysis
}

func rangeNextUnitText(t *testing.T, f originalGenericFixture, unit, own string) string {
	t.Helper()
	if unit == "" {
		return own
	}
	for _, candidate := range f.inputs.units {
		if candidate.SourceKey == unit {
			return string(f.owner.FileSet.Get(candidate.Builder.Files.Get(candidate.FileID).Span.File).Content)
		}
	}
	t.Fatalf("PRECONDITION: unit %s is not an owning unit", unit)
	return ""
}

func checkRangeNextLeaf(t *testing.T, leaf rangeNextLeaf, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
	t.Helper()
	at := func(pending sema.ReturnOriginPending, want rangeNextPending) bool {
		unit := want.unit
		if unit == "" {
			unit = f.unit.SourceKey
		}
		return pending.SourceKey == unit && int(pending.Span.Start) == want.at.start && int(pending.Span.End) == want.at.end && pending.Reason == want.reason
	}
	for _, want := range leaf.present {
		if !slices.ContainsFunc(analysis.Pending, func(p sema.ReturnOriginPending) bool { return at(p, want) }) {
			t.Errorf("%s: lost %q at %s %d:%d %q", leaf.name, want.reason, want.unit, want.at.start, want.at.end, want.at.snippet)
		}
	}
	for _, pending := range analysis.Pending {
		for _, gone := range leaf.absent {
			if at(pending, gone) {
				t.Errorf("%s: step at %s %d:%d still has: %s", leaf.name, gone.unit, gone.at.start, gone.at.end, gone.reason)
			}
		}
		if leaf.clean && pending.SourceKey == f.unit.SourceKey &&
			int(pending.Span.Start) >= leaf.body.start && int(pending.Span.End) <= leaf.body.end {
			t.Errorf("%s: obligation left unfinished: %s at %d:%d", leaf.name, pending.Reason, pending.Span.Start, pending.Span.End)
		}
	}
	for _, d := range analysis.Diagnostics {
		if leaf.silent || (leaf.quiet && d.Primary.File == f.owner.File.ID) {
			t.Errorf("%s: unexpected diagnostic: %+v", leaf.name, d)
		}
	}
	if !leaf.summary {
		return
	}
	var found []sema.ReturnOriginSummary
	for _, summary := range analysis.Summaries {
		if summary.Name == leaf.function && summary.Source.File == f.owner.File.ID {
			found = append(found, summary)
		}
	}
	if len(found) != 1 {
		t.Fatalf("PRECONDITION: %s has %d dependency summaries", leaf.function, len(found))
	}
	if s := found[0]; s.NoNormalReturn || s.Unknown || !slices.Equal(s.ParamSlots, leaf.slots) {
		t.Errorf("%s summary = %+v, want a normal known result with slots %v", leaf.function, s, leaf.slots)
	}
}
