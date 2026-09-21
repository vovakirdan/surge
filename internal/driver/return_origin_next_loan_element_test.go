package driver

import "testing"

// A manual cursor step is the same read a `for … in` makes: an element that stores an array or a
// cursor keeps the loans its container records, so the step is refused where the walk is.
const nextLoanElementSource = `fn step_opt(views: &Array<Option<uint64[]>>) -> nothing {
    let mut c = views.__range();
    let o = c.next();
    return nothing;
}
fn step_int(xs: &Array<int>) -> nothing {
    let mut c = xs.__range();
    let o = c.next();
    return nothing;
}
`

// 4 RUN: 1 parent, 1 source, 2 leaves.
func TestAnalyzeNextLoanElement(t *testing.T) {
	optStep, intStep := originSpan{103, 111, "c.next()"}, originSpan{219, 227, "c.next()"}
	checkOriginSource(t, nextLoanElementSource, "ec1b1b2263a8fa15c51ae3afcd6dbb705df0915a56181d31fa701d5955e19581", optStep, intStep)
	t.Run("next_loan_element", func(t *testing.T) {
		f, analysis := analyzeOriginRoot(t, "next_loan_element", nextLoanElementSource, false, nil)
		t.Run("step_opt", func(t *testing.T) {
			coreRangeRefusal("step_opt", optStep, []string{rangeNextLoanElement}, nil).check(t, f.unit.SourceKey, f, analysis)
		})
		t.Run("step_int", func(t *testing.T) {
			coreRangeClean(nextLoanElementSource, "step_int", 135, 250, nil).check(t, f.unit.SourceKey, f, analysis)
		})
	})
}
