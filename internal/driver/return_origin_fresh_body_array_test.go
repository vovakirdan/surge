package driver

import "testing"

// A checked non-generic conversion body may return a fresh dynamic array when
// its element can retain no borrow or storage loan and its summary names no
// source. A body returning a fixed-field window keeps its source and therefore
// stays on the ordinary conversion refusal.
const freshBodyArraySource = `type Win = { cells: uint64[4] };
extern<Win> {
    fn __to(self: &Win, _: uint64[]) -> uint64[] {
        return self.cells[[0..2]];
    }
}
fn fresh(s: &string) -> byte[] {
    return (*s) to byte[];
}
fn borrowed(w: Win) -> uint {
    let v: uint64[] = w to uint64[];
    return v.__len();
}
`

const freshBodyArrayDigest = "0028319c67cae14f91616dd16269ea1241913320253d44667432fa33e3dff600"

func TestAnalyzeFreshBodyArrayResult(t *testing.T) {
	checkOriginSource(t, freshBodyArraySource, freshBodyArrayDigest,
		originSpan{0, len(freshBodyArraySource), freshBodyArraySource})
	f, analysis := analyzeOriginRoot(t, "fresh_body_array", freshBodyArraySource, false, nil)
	fresh := patternFn(t, freshBodyArraySource, "fn fresh(")
	originExactPending(t, analysis, f.unit.SourceKey, fresh, nil)
	requireOriginSummary(t, analysis, f.owner.File.ID, "fresh", false, []uint32{})

	borrowed := patternFn(t, freshBodyArraySource, "fn borrowed(")
	originExactPending(t, analysis, f.unit.SourceKey, borrowed, []originRefusal{
		{originSpan{255, 268, "w to uint64[]"}, originCastRetainRefusal},
		{originSpan{281, 282, "v"}, originOutgoingRefusal},
	})
}
