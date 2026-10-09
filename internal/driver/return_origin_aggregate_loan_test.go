package driver

import "testing"

const aggregateLoanSource = `type Bag = { values: int[] };
extern<Bag> {
    fn __range(self: &Bag) -> Range<int> {
        return self.values.__range();
    }
}
fn through_param(b: &Bag) -> Range<int> {
    return b.__range();
}
fn in_frame() -> int {
    let b: Bag = Bag { values = [1, 2, 3] };
    let mut total: int = 0;
    for v in b {
        total = total + v;
    }
    return total;
}
fn leak() -> Range<int> {
    let fixed: int[3] = [1, 2, 3];
    let holder: Bag = Bag { values = fixed[[0..3]] };
    return holder.__range();
}
`

func TestAnalyzeAggregateContainerLoans(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "aggregate_container_loans", aggregateLoanSource, false, nil)
	for _, name := range []string{"through_param", "in_frame"} {
		fn := patternFn(t, aggregateLoanSource, "fn "+name+"(")
		originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
		originNoEscape(t, analysis, f.owner.File.ID, fn)
	}
	requireOriginSummary(t, analysis, f.owner.File.ID, "through_param", false, []uint32{0})

	leak := patternFn(t, aggregateLoanSource, "fn leak(")
	ret := patternIn(t, aggregateLoanSource, leak, "return holder.__range();")
	originExactPending(t, analysis, f.unit.SourceKey, leak, nil)
	requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID, ret, "holder")
}
