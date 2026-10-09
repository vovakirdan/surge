package driver

import "testing"

const nestedStoreRefusal = "store through a place needs reference-content transfer"

// A nested store into a reference-free final element changes no origin fact.
// The chain still has to be a typed member/index path and its outer store has
// to be the certified canonical index-set operation. A final dynamic-array
// element keeps the refusal because it can carry a storage loan.
const nestedStoreSource = `pragma module::dep;
type Box = { values: int[] };
fn member_store(p: &mut Box, i: int, v: int) -> nothing {
    p.values[i] = v;
    return nothing;
}
fn nested_store() -> int {
    let row0: int[] = [1, 2];
    let row1: int[] = [3, 4];
    let mut grid: int[][] = [row0, row1];
    grid[1][0] = 8;
    return clone(grid[1][0]);
}
fn loan_store(dst: &mut uint64[][][], value: uint64[]) -> nothing {
    dst[0][0] = value;
    return nothing;
}
`

const nestedStoreDigest = "86e762111c2d6945f8b912d4e54f505f3a38b060b7dd60b3f3ecd312d81063b0"

func TestAnalyzeNestedReferenceFreeIndexStores(t *testing.T) {
	checkOriginSource(t, nestedStoreSource, nestedStoreDigest, originSpan{0, len(nestedStoreSource), nestedStoreSource})
	f, analysis := analyzeOriginRoot(t, "nested_store", nestedStoreSource, false, nil)
	for _, row := range []struct{ header, name string }{
		{"fn member_store(", "member_store"},
		{"fn nested_store(", "nested_store"},
	} {
		fn := patternFn(t, nestedStoreSource, row.header)
		originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
		originNoEscape(t, analysis, f.owner.File.ID, fn)
		requireOriginSummary(t, analysis, f.owner.File.ID, row.name, false, []uint32{})
	}
	loan := patternFn(t, nestedStoreSource, "fn loan_store(")
	at := patternIn(t, nestedStoreSource, loan, "dst[0][0] = value")
	originExactPending(t, analysis, f.unit.SourceKey, loan, []originRefusal{{at, nestedStoreRefusal}})
}
