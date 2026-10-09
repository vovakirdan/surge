package driver

import "testing"

const bodyOperationSources = `type Arr = { items: uint64[3] };
extern<Arr> {
    fn __add(self: &Arr, other: &Arr) -> uint64[] {
        return self.items[[0..2]];
    }
    fn __mul(self: &Arr, other: &Arr) -> uint64[] {
        let mut out: uint64[] = [];
        out.push(self.items[0] + other.items[0]);
        return out;
    }
}
fn through(a: &Arr, b: &Arr) -> uint64[] {
    return a + b;
}
fn via_binding(p: &Arr) -> uint64[] {
    let r: &Arr = p;
    return r + r;
}
fn fresh() -> uint64[] {
    let a: Arr = Arr { items = [1:uint64, 2:uint64, 3:uint64] };
    let b: Arr = Arr { items = [4:uint64, 5:uint64, 6:uint64] };
    return a * b;
}
`

func TestAnalyzeSourceBodyOperationResults(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "body_operation_sources", bodyOperationSources, false, nil)
	for _, row := range []struct {
		name  string
		slots []uint32
	}{
		{"through", []uint32{0}},
		{"via_binding", []uint32{0}},
		{"fresh", nil},
	} {
		fn := patternFn(t, bodyOperationSources, "fn "+row.name+"(")
		originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
		originNoEscape(t, analysis, f.owner.File.ID, fn)
		requireOriginSummary(t, analysis, f.owner.File.ID, row.name, false, row.slots)
	}

}
