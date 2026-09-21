package driver

import (
	"testing"

	"surge/internal/sema"
)

// Array<T>.slice returns a view of its receiver: over a borrow-free element it keeps the base's
// loans, so a view of dying storage is refused; an element that can hold a borrow stays refused
// at the call. The harness and the row helpers are return_origin_core_range_test.go's.
const coreSlicePayloadFreeSource = `fn middle(xs: &uint64[]) -> uint64[] {
    return xs.slice(1..3);
}
`

const coreSliceEscapeSource = `fn leak_view() -> uint64[] {
    let fixed: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let v: uint64[] = fixed[[0..4]];
    return v.slice(1..3);
}
fn inner() -> nothing {
    let mut w: uint64[] = [];
    {
        let fixed: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
        let t: uint64[] = fixed[[0..4]];
        w = t.slice(0..4);
    }
    return nothing;
}
`

const coreSliceRefusalSource = `fn firsts(xs: &Array<&string>) -> Array<&string> {
    return xs.slice(0..1);
}
fn nested(xs: &Array<uint64[]>) -> Array<uint64[]> {
    return xs.slice(0..1);
}
`

// coreSliceEscape is a body whose one diagnostic names the fixed storage a returned or stored view reads.
func coreSliceEscape(name string, start, end int, primary, owner originSpan, unknown bool) coreRangeLeaf {
	return coreRangeLeaf{name: name, check: func(t *testing.T, key string, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
		fn := coreRangeFn(t, coreSliceEscapeSource, name, start, end)
		requireOriginEscapeAt(t, analysis, f.owner.File.ID, primary, owner, "fixed")
		if got := coreRangeEscapes(analysis, f, fn); got != 1 {
			t.Errorf("%s has %d diagnostics, want exactly 1: %+v", name, got, analysis.Diagnostics)
		}
		originExactPending(t, analysis, key, fn, nil)
		requireOriginSummary(t, analysis, f.owner.File.ID, name, unknown, nil)
	}}
}

func coreSliceSources() []coreRangeSource {
	leakReturn := originSpan{139, 160, "return v.slice(1..3);"}
	innerBlock := originSpan{221, 369, coreSliceEscapeSource[221:369]}
	firstsCall, nestedCall := originSpan{62, 76, "xs.slice(0..1)"}, originSpan{144, 158, "xs.slice(0..1)"}
	return []coreRangeSource{
		{name: "slice_payload_free", text: coreSlicePayloadFreeSource, digest: "d2b8f702913a9c1d6395041f721a8d142ec013f9e4e6cba3c9e4615c46bab0a3",
			spans:  []originSpan{{50, 64, "xs.slice(1..3)"}},
			leaves: []coreRangeLeaf{coreRangeClean(coreSlicePayloadFreeSource, "middle", 0, 67, []uint32{0})}},
		{name: "slice_view_escape", text: coreSliceEscapeSource, digest: "c2a5e8fee271d719ecdc6a49c9ec055468cb494762e5fc6681e4944c051f39ee", escape: true,
			spans: []originSpan{leakReturn, innerBlock, {146, 159, "v.slice(1..3)"}, {349, 362, "t.slice(0..4)"}, {37, 42, "fixed"}, {235, 240, "fixed"}},
			leaves: []coreRangeLeaf{
				coreSliceEscape("leak_view", 0, 162, leakReturn, originSpan{37, 42, "fixed"}, true),
				coreSliceEscape("inner", 163, 391, innerBlock, originSpan{235, 240, "fixed"}, false),
			}},
		{name: "slice_reference_refusal", text: coreSliceRefusalSource, digest: "94ff79651d33e65541153128c7f2444c2573441bc9b6d45275efb8f17e9f2dc4",
			spans: []originSpan{firstsCall, nestedCall},
			leaves: []coreRangeLeaf{
				coreRangeRefusal("firsts", firstsCall, []string{genericConditionRefuted}, nil),
				coreRangeRefusal("nested", nestedCall, []string{genericConditionUnsupported}, nil),
			}},
	}
}

// 9 RUN: 1 parent, 3 sources, 5 leaves.
func TestAnalyzeCoreSliceOrigins(t *testing.T) {
	sources, leaves := coreSliceSources(), 0
	for _, src := range sources {
		leaves += len(src.leaves)
	}
	if len(sources) != 3 || leaves != 5 || coreSliceEscapeSource[221] != '{' || coreSliceEscapeSource[368] != '}' {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaves=%d", len(sources), leaves)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			checkOriginSource(t, src.text, src.digest, src.spans...)
			f, analysis := analyzeOriginRoot(t, "core_slice_"+src.name, src.text, src.escape, nil)
			for _, leaf := range src.leaves {
				t.Run(leaf.name, func(t *testing.T) { leaf.check(t, f.unit.SourceKey, f, analysis) })
			}
		})
	}
}

var coreSliceConcatRows = []coreRangeRow{
	{"core/intrinsics.sg", 37316, 37321, "__add", genericConditionUnsupported},
	{"core/intrinsics.sg", 37771, 37776, "__add", genericConditionUnsupported},
}

var coreSliceIndexRows = []coreRangeRow{
	{"core/intrinsics.sg", 37475, 37482, "__index", genericConditionUnsupported},
	{"core/intrinsics.sg", 37962, 37969, "__index", genericConditionUnsupported},
}

var coreSliceBodyRows = []coreRangeRow{
	{"core/array.sg", 2893, 2904, "-> Array<T>", originResultRefusal},
	{"core/array.sg", 2915, 2930, "return self[r];", originOutgoingRefusal},
	{"core/array.sg", 2922, 2929, "self[r]", "index requires a non-scalar index transfer"},
}

// 4 RUN: the 7 census rows of array concatenation, the range-index declarations and Array<T>.slice.
func TestReturnOriginSliceCoreRows(t *testing.T) {
	if len(coreSliceConcatRows) != 2 || len(coreSliceIndexRows) != 2 || len(coreSliceBodyRows) != 3 {
		t.Fatalf("PRECONDITION: frozen rows changed: %d %d %d", len(coreSliceConcatRows), len(coreSliceIndexRows), len(coreSliceBodyRows))
	}
	f, analysis := coreRangeRowsFixture(t, coreSliceConcatRows, coreSliceIndexRows, coreSliceBodyRows)
	t.Run("concat_declarations", func(t *testing.T) { coreRangeRowsGone(t, analysis, coreSliceConcatRows) })
	t.Run("range_index_declarations", func(t *testing.T) { coreRangeRowsGone(t, analysis, coreSliceIndexRows) })
	t.Run("slice_body", func(t *testing.T) {
		coreRangeRowsGone(t, analysis, coreSliceBodyRows)
		requireOriginSummary(t, analysis, coreRangeArrayFile(t, f), "slice", false, []uint32{0})
	})
}
