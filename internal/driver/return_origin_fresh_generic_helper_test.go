package driver

import "testing"

const freshGenericHelperSource = `fn mapping<T, U>(xs: &T[], f: fn(&T) -> U) -> U[] {
    let mut out: U[] = [];
    for x in xs { out.push(f(&x)); }
    return out;
}
fn filter<T>(xs: &T[], f: fn(&T) -> bool) -> T[] {
    let mut out: T[] = [];
    for x in xs { if f(&x) { out.push(clone(x)); } }
    return out;
}
fn rebound<T>(xs: &T[]) -> T[] {
    let mut out: T[] = [];
    out = xs[[0..1]];
    return out;
}
fn square(x: &int) -> int { return x * x; }
fn even(x: &int) -> bool { return x % 2 == 0; }
fn probe(xs: &int[]) -> int {
    let ys = mapping::<int, int>(xs, square);
    let zs = filter::<int>(xs, even);
    return (ys.__len() + zs.__len()) to int;
}
fn expose(xs: &int[]) -> int[] {
    return rebound::<int>(xs);
}
`

func TestAnalyzeFreshGenericArrayHelpers(t *testing.T) {
	f, analysis := analyzeOriginRoot(t, "fresh_generic_helpers", freshGenericHelperSource, false, nil)
	probe := patternFn(t, freshGenericHelperSource, "fn probe(")
	originExactPending(t, analysis, f.unit.SourceKey, probe, nil)
	originNoEscape(t, analysis, f.owner.File.ID, probe)
	requireOriginSummary(t, analysis, f.owner.File.ID, "probe", false, nil)

	expose := patternFn(t, freshGenericHelperSource, "fn expose(")
	originExactPending(t, analysis, f.unit.SourceKey, expose, nil)
	requireOriginSummary(t, analysis, f.owner.File.ID, "expose", false, []uint32{0})
}
