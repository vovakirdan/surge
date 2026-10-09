package driver

import "testing"

// A scalar index already records the exact container backing. Dereferencing a
// reference element loads that backing content instead of mistaking the array
// for the element's referent. The compiler-generated variadic reference pack is
// the source-level witness; ordinary reference arrays remain SEM3138.
//
// BytesView is the other bounded load: its exact core marker says the value is
// borrowed, so reading it through a reference retains that reference's source.
// Direct extraction is still rejected by SEM3197; the two accepted reads feed
// the certified scalar BytesView index operation.
const projectionLoadSource = `pragma module::dep;
type Holder = { view: BytesView };
fn read_view(v: &BytesView) -> uint8 {
    return (*v)[0];
}
fn field_ref(h: &Holder) -> &BytesView {
    return h.view;
}
fn read_field(h: &Holder) -> uint8 {
    return (*h.view)[0];
}
fn first(...xs: &string) -> &string {
    return *xs[0];
}
fn leak() -> &string {
    let s: string = "local";
    return first(&s);
}
`

const projectionLoadDigest = "57f6cf504132a3a146d656b50423d6fdd72555e88e7990bd09f7f6f4501b21d0"

func TestAnalyzeProjectionLoads(t *testing.T) {
	checkOriginSource(t, projectionLoadSource, projectionLoadDigest, originSpan{0, len(projectionLoadSource), projectionLoadSource})
	f, analysis := analyzeOriginRoot(t, "projection_load", projectionLoadSource, true, nil)
	for _, row := range []struct {
		header string
		name   string
		slots  []uint32
	}{
		{"fn read_view(", "read_view", []uint32{}},
		{"fn field_ref(", "field_ref", []uint32{0}},
		{"fn read_field(", "read_field", []uint32{}},
		{"fn first(", "first", []uint32{0}},
	} {
		fn := patternFn(t, projectionLoadSource, row.header)
		originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
		originNoEscape(t, analysis, f.owner.File.ID, fn)
		requireOriginSummary(t, analysis, f.owner.File.ID, row.name, false, row.slots)
	}
	leak := patternFn(t, projectionLoadSource, "fn leak(")
	originExactPending(t, analysis, f.unit.SourceKey, leak, nil)
	requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID,
		patternIn(t, projectionLoadSource, leak, "return first(&s);"), "s")
}
