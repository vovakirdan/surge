package driver

import "testing"

const structEffectsSource = `type Cursor = { data: BytesView, index: int64 };
fn offset(p: &mut Cursor) -> int64 { return p.index; }
fn step(p: &mut Cursor) -> nothing {
    p.index = p.index + 1:int64;
    let _ = offset(p);
    return nothing;
}
fn consume(p: &mut Cursor, text: &string) -> bool {
    step(p);
    return text.__len() > 0:uint;
}
fn replace(p: &mut Cursor, data: BytesView) -> nothing {
    p.data = data;
    return nothing;
}
fn safe(s: &string) -> bool {
    let mut p: Cursor = Cursor { data = s.bytes(), index = 0:int64 };
    return consume(&mut p, "x");
}
fn unsafe_replace(s: &string, other: BytesView) -> nothing {
    let mut p: Cursor = Cursor { data = s.bytes(), index = 0:int64 };
    replace(&mut p, other);
    return nothing;
}
`

func TestAnalyzeBorrowedStructEffects(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "borrowed_struct_effects", structEffectsSource, false, nil)
	safe := patternFn(t, structEffectsSource, "fn safe(")
	originExactPending(t, analysis, f.unit.SourceKey, safe, nil)
	originNoEscape(t, analysis, f.owner.File.ID, safe)

	unsafe := patternFn(t, structEffectsSource, "fn unsafe_replace(")
	call := patternIn(t, structEffectsSource, unsafe, "replace(&mut p, other)")
	if !originPendingAt(analysis, f.unit.SourceKey, call, "mutable argument may replace reference-bearing contents") {
		t.Errorf("borrowed-field overwrite lost its mutable-effect refusal: %+v", originPendingWithin(analysis, f.unit.SourceKey, unsafe.start, unsafe.end))
	}
}
