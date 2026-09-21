package driver

import (
	"strings"
	"testing"

	"surge/internal/sema"
	"surge/internal/source"
)

// A Range is a loan carrier: a template's by-value Range formal keeps its roots, the numeric
// bounds constructors return a fresh descriptor, and a `__range` cursor borrows its base. An
// element that can hold a borrow stays refused where the cursor is asked for.
type coreRangeLeaf struct {
	name  string
	check func(t *testing.T, key string, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis)
}

// Every source is a root program: only a root's finalized closure carries the use sites and
// instances that the per-use checks walk.
type coreRangeSource struct {
	name, text, digest string
	escape             bool
	spans              []originSpan
	leaves             []coreRangeLeaf
}

// coreRangeFn freezes one function item of a test source.
func coreRangeFn(t *testing.T, text, name string, start, end int) originSpan {
	t.Helper()
	if start < 0 || end > len(text) || start >= end || !strings.HasPrefix(text[start:end], "fn "+name) || text[end-1] != '}' {
		t.Fatalf("PRECONDITION: frozen body %d:%d is not fn %s", start, end, name)
	}
	return originSpan{start, end, text[start:end]}
}

// coreRangeEscapes counts the diagnostics whose primary span lies inside one function.
func coreRangeEscapes(analysis *sema.ReturnOriginAnalysis, f originalGenericFixture, fn originSpan) int {
	count := 0
	for _, d := range analysis.Diagnostics {
		if d.Primary.File == f.owner.File.ID && int(d.Primary.Start) >= fn.start && int(d.Primary.End) <= fn.end {
			count++
		}
	}
	return count
}

// coreRangeClean is a body with no obligation and no diagnostic, whose result names these formals.
func coreRangeClean(text, name string, start, end int, slots []uint32) coreRangeLeaf {
	return coreRangeLeaf{name: name, check: func(t *testing.T, key string, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
		fn := coreRangeFn(t, text, name, start, end)
		originExactPending(t, analysis, key, fn, nil)
		originNoEscape(t, analysis, f.owner.File.ID, fn)
		requireOriginSummary(t, analysis, f.owner.File.ID, name, false, slots)
	}}
}

// coreRangeRefusal requires some reasons at one call and forbids others at it; other rows are logged.
func coreRangeRefusal(name string, call originSpan, present, absent []string) coreRangeLeaf {
	return coreRangeLeaf{name: name, check: func(t *testing.T, key string, _ originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
		for _, reason := range present {
			if !originPendingAt(analysis, key, call, reason) {
				t.Errorf("%s: missing %q at %d:%d %q", name, reason, call.start, call.end, call.snippet)
			}
		}
		for _, reason := range absent {
			if originPendingAt(analysis, key, call, reason) {
				t.Errorf("%s: unexpected %q at %d:%d %q", name, reason, call.start, call.end, call.snippet)
			}
		}
	}}
}

const coreRangeBoundsSource = `fn span_of(n: int) -> Range<int> {
    return rt_range_int_new(0, n, false);
}
`

const coreRangeFloorSource = `type Tagged<T> = { n: uint64 };
fn keep<T>(r: Range<Tagged<T>>) -> Range<Tagged<T>> {
    return r;
}
fn through(xs: &Tagged<uint64>[4]) -> Range<Tagged<uint64>> {
    return keep::<uint64>(xs.__range());
}
fn pass<T>(r: Range<T>) -> Range<T> {
    return r;
}
`

const coreRangeCursorSource = `fn cursor<T>(xs: &Array<T>) -> Range<T> {
    return xs.__range();
}
fn leak_cursor() -> Range<uint64> {
    let fixed: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let v: uint64[] = fixed[[0..4]];
    return cursor::<uint64>(&v);
}
`

const coreRangeCursorRefusalSource = `fn cursor<T>(xs: &Array<T>) -> Range<T> {
    return xs.__range();
}
fn refs(xs: &Array<&string>) -> Range<&string> {
    return cursor::<&string>(xs);
}
fn nested(xs: &Array<uint64[]>) -> Range<uint64[]> {
    return cursor::<uint64[]>(xs);
}
`

const coreRangeDeclarationsSource = `fn walk(xs: &Option<&string>[2]) -> Range<Option<&string>> {
    return xs.__range();
}
fn walk_dyn(xs: &Array<&string>) -> Range<&string> {
    return xs.__range();
}
`

func coreRangeSources() []coreRangeSource {
	leakReturn := originSpan{215, 243, "return cursor::<uint64>(&v);"}
	refsCall := originSpan{129, 150, "cursor::<&string>(xs)"}
	nestedCall := originSpan{218, 240, "cursor::<uint64[]>(xs)"}
	walkCall, walkDynCall := originSpan{72, 84, "xs.__range()"}, originSpan{152, 164, "xs.__range()"}
	return []coreRangeSource{
		{name: "range_bounds_call", text: coreRangeBoundsSource, digest: "6b3dc3e073f6408a940b5eaad7e315bbcec642615cab8215905139a701afcb61",
			spans:  []originSpan{{46, 75, "rt_range_int_new(0, n, false)"}},
			leaves: []coreRangeLeaf{coreRangeClean(coreRangeBoundsSource, "span_of", 0, 78, nil)}},
		{name: "range_formal_floor", text: coreRangeFloorSource, digest: "445a9580273b06d8dc6ac11ead92a88e2599ff41603e57e60d3f29dd488ae4a7",
			spans: []originSpan{{42, 63, "(r: Range<Tagged<T>>)"}, {175, 203, "keep::<uint64>(xs.__range())"}, {217, 230, "(r: Range<T>)"}},
			leaves: []coreRangeLeaf{
				coreRangeClean(coreRangeFloorSource, "keep", 32, 101, []uint32{0}),
				coreRangeClean(coreRangeFloorSource, "through", 102, 206, []uint32{0}),
				coreRangeClean(coreRangeFloorSource, "pass", 207, 260, []uint32{0}),
			}},
		{name: "template_cursor", text: coreRangeCursorSource, digest: "24bd14907a071341a6e816353ad064d328da461e99dff7c0d9ce3b0dfeb417fc", escape: true,
			spans: []originSpan{{53, 65, "xs.__range()"}, leakReturn, {222, 242, "cursor::<uint64>(&v)"}, {113, 118, "fixed"}, {182, 183, "v"}},
			leaves: []coreRangeLeaf{
				coreRangeClean(coreRangeCursorSource, "cursor", 0, 68, []uint32{0}),
				// A dynamic cursor keeps the view's own value and the loans the view carries, so both owners escape.
				{name: "leak_cursor", check: func(t *testing.T, key string, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
					fn := coreRangeFn(t, coreRangeCursorSource, "leak_cursor", 69, 245)
					requireOriginEscapeAt(t, analysis, f.owner.File.ID, leakReturn, originSpan{182, 183, "v"}, "v")
					requireOriginEscapeAt(t, analysis, f.owner.File.ID, leakReturn, originSpan{113, 118, "fixed"}, "fixed")
					if got := coreRangeEscapes(analysis, f, fn); got != 2 {
						t.Errorf("leak_cursor has %d diagnostics, want exactly 2: %+v", got, analysis.Diagnostics)
					}
					originExactPending(t, analysis, key, fn, nil)
					requireOriginSummary(t, analysis, f.owner.File.ID, "leak_cursor", true, nil)
				}},
			}},
		// A separate analysis: an instance over a borrowing element also pends inside `cursor` itself.
		{name: "template_cursor_refusal", text: coreRangeCursorRefusalSource, digest: "b358d1dacb4c3926d3160f982a4a8d1e5970f825175f9569433398ff1b0837de",
			spans: []originSpan{{53, 65, "xs.__range()"}, refsCall, nestedCall},
			leaves: []coreRangeLeaf{
				coreRangeRefusal("refs", refsCall, []string{genericConditionRefuted}, nil),
				// Status control: an array element is a loan carrier, refused before and after.
				coreRangeRefusal("nested", nestedCall, []string{genericConditionUnsupported}, nil),
			}},
		{name: "cursor_declarations", text: coreRangeDeclarationsSource, digest: "d99e7edbcd3cbd7ec58cb5cd41a96c69864de4f3431f59d87c0d15f955fcc7e0",
			spans: []originSpan{walkCall, walkDynCall},
			leaves: []coreRangeLeaf{
				coreRangeRefusal("walk", walkCall, []string{genericConditionRefuted}, []string{genericConditionUnsupported, arrayPopLegacyTransfer}),
				coreRangeRefusal("walk_dyn", walkDynCall, []string{genericConditionRefuted, arrayPopLegacyTransfer}, []string{genericConditionUnsupported}),
			}},
	}
}

// 16 RUN: 1 parent, 5 sources, 10 leaves.
func TestAnalyzeCoreRangeOrigins(t *testing.T) {
	sources, leaves := coreRangeSources(), 0
	for _, src := range sources {
		leaves += len(src.leaves)
	}
	if len(sources) != 5 || leaves != 10 {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaves=%d", len(sources), leaves)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			checkOriginSource(t, src.text, src.digest, src.spans...)
			f, analysis := analyzeOriginRoot(t, "core_range_"+src.name, src.text, src.escape, nil)
			for _, leaf := range src.leaves {
				t.Run(leaf.name, func(t *testing.T) { leaf.check(t, f.unit.SourceKey, f, analysis) })
			}
		})
	}
}

// coreRangeRow is one census row of the core units: a frozen span, its text and its reason.
type coreRangeRow struct {
	key        string
	start, end int
	text       string
	reason     string
}

const (
	coreArrayUnitDigest = "532a6cd1bc46d2d665d71f13f988358e42fe30dd39810117278afd46b967ffbc"
	coreRangeAnchorText = "fn anchor(n: int) -> int {\n    return n;\n}\n"
	coreRangeAnchorSum  = "7c4afa47134ddd276b96a31df0f1e4d6a588aadd6a3ec86102a6da695f26d83f"
	coreRangeParameter  = "parameter requires concrete type or callable provenance"
)

var coreRangeBoundsRows = []coreRangeRow{
	{"core/intrinsics.sg", 6744, 6760, "rt_range_int_new", genericConditionUnsupported},
	{"core/intrinsics.sg", 6833, 6856, "rt_range_int_from_start", genericConditionUnsupported},
	{"core/intrinsics.sg", 6919, 6938, "rt_range_int_to_end", genericConditionUnsupported},
	{"core/intrinsics.sg", 6999, 7016, "rt_range_int_full", genericConditionUnsupported},
}

var coreRangeCursorRows = []coreRangeRow{
	{"core/intrinsics.sg", 37635, 37642, "__range", genericConditionUnsupported},
	{"core/intrinsics.sg", 38138, 38145, "__range", genericConditionUnsupported},
}

var coreRangeFromRangeRows = []coreRangeRow{
	{"core/array.sg", 1971, 1984, "(r: Range<T>)", coreRangeParameter},
	{"core/array.sg", 2068, 2069, "r", originOutgoingRefusal},
	{"core/array.sg", 2090, 2360, "{\n            let next_opt: Option<T> = iter.next();\n            compare next_opt {\n                Some(v) => {\n                    let _ = out.push(v);\n                }\n                nothing => {\n                    break;\n                }\n            };\n        }", originOutgoingRefusal},
	{"core/array.sg", 2130, 2134, "iter", originOutgoingRefusal},
	{"core/array.sg", 2201, 2261, "{\n                    let _ = out.push(v);\n                }", originOutgoingRefusal},
	{"core/array.sg", 2261, 2261, "}", originOutgoingRefusal},
	{"core/array.sg", 2311, 2317, "break;", originOutgoingRefusal},
	{"core/array.sg", 2369, 2380, "return out;", originOutgoingRefusal},
}

// coreRangeRowsFixture analyzes a root with no generic call, so core is read exactly as the census reads it.
func coreRangeRowsFixture(t *testing.T, rows ...[]coreRangeRow) (originalGenericFixture, *sema.ReturnOriginAnalysis) {
	t.Helper()
	checkOriginSource(t, coreRangeAnchorText, coreRangeAnchorSum)
	f, analysis := analyzeOriginRoot(t, "core_rows", coreRangeAnchorText, false, nil)
	content := map[string]string{"core/array.sg": rangeNextUnitText(t, f, "core/array.sg", ""), "core/intrinsics.sg": rangeNextUnitText(t, f, "core/intrinsics.sg", "")}
	checkOriginSource(t, content["core/array.sg"], coreArrayUnitDigest)
	checkOriginSource(t, content["core/intrinsics.sg"], coreIntrinsicsUnitDigest)
	for _, group := range rows {
		for _, row := range group {
			text := content[row.key]
			switch {
			case row.start < 1 || row.end > len(text) || row.start > row.end:
				t.Fatalf("PRECONDITION: %s %d:%d is outside its unit", row.key, row.start, row.end)
			case row.start == row.end && text[row.start-1:row.start] != row.text: // a zero-width row is pinned by the byte it follows
				t.Fatalf("PRECONDITION: %s %d:%d does not follow %q", row.key, row.start, row.end, row.text)
			case row.start != row.end && text[row.start:row.end] != row.text:
				t.Fatalf("PRECONDITION: %s %d:%d is %q, want %q", row.key, row.start, row.end, text[row.start:row.end], row.text)
			}
		}
	}
	for _, p := range analysis.Pending {
		if strings.HasPrefix(p.SourceKey, "core/") {
			t.Logf("CORE_ROWS_PENDING %s %d:%d %q", p.SourceKey, p.Span.Start, p.Span.End, p.Reason)
		}
	}
	return f, analysis
}

// coreRangeRowsGone requires that none of these census rows is left.
func coreRangeRowsGone(t *testing.T, analysis *sema.ReturnOriginAnalysis, rows []coreRangeRow) {
	t.Helper()
	for _, row := range rows {
		if originPendingAt(analysis, row.key, originSpan{row.start, row.end, ""}, row.reason) {
			t.Errorf("census row still Pending: %s %d:%d %q", row.key, row.start, row.end, row.reason)
		}
	}
}

// coreRangeArrayFile is the file of core/array.sg, where a core body's summary is recorded.
func coreRangeArrayFile(t *testing.T, f originalGenericFixture) source.FileID {
	t.Helper()
	for _, unit := range f.inputs.units {
		if unit.SourceKey == "core/array.sg" {
			return unit.Builder.Files.Get(unit.FileID).Span.File
		}
	}
	t.Fatal("PRECONDITION: core/array.sg is not an owning unit")
	return 0
}

// 4 RUN: the 14 census rows of the Range constructors, the `__range` declarations and Array<T>.from_range.
func TestReturnOriginRangeCoreRows(t *testing.T) {
	if len(coreRangeBoundsRows) != 4 || len(coreRangeCursorRows) != 2 || len(coreRangeFromRangeRows) != 8 {
		t.Fatalf("PRECONDITION: frozen rows changed: %d %d %d", len(coreRangeBoundsRows), len(coreRangeCursorRows), len(coreRangeFromRangeRows))
	}
	f, analysis := coreRangeRowsFixture(t, coreRangeBoundsRows, coreRangeCursorRows, coreRangeFromRangeRows)
	t.Run("bounds_constructors", func(t *testing.T) { coreRangeRowsGone(t, analysis, coreRangeBoundsRows) })
	t.Run("cursor_declarations", func(t *testing.T) { coreRangeRowsGone(t, analysis, coreRangeCursorRows) })
	t.Run("from_range_body", func(t *testing.T) {
		coreRangeRowsGone(t, analysis, coreRangeFromRangeRows)
		requireOriginSummary(t, analysis, coreRangeArrayFile(t, f), "from_range", false, nil) // control: fresh storage, before and after
	})
}
