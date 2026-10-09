package driver

import "testing"

const nestedLoanConstructorSource = `pragma module::dep;
type Holder = { data: uint64[] };
fn read(base: &uint64[2]) -> uint64 {
    let h: Holder = Holder { data = base[[0..2]] };
    return clone(h.data[0]);
}
fn leak() -> Holder {
    let fixed: uint64[2] = [1:uint64, 2:uint64];
    return Holder { data = fixed[[0..2]] };
}
`

const nestedLoanConstructorDigest = "cfdaee7059053c457c29ace6e2d19d299d94d29e741921369f55bd5e2e31a8de"

func TestAnalyzeNestedLoanConstructor(t *testing.T) {
	checkOriginSource(t, nestedLoanConstructorSource, nestedLoanConstructorDigest,
		originSpan{0, len(nestedLoanConstructorSource), nestedLoanConstructorSource})
	f, analysis := analyzeOriginRoot(t, "nested_loan_constructor", nestedLoanConstructorSource, true, nil)
	read := patternFn(t, nestedLoanConstructorSource, "fn read(")
	originExactPending(t, analysis, f.unit.SourceKey, read, nil)
	originNoEscape(t, analysis, f.owner.File.ID, read)
	requireOriginSummary(t, analysis, f.owner.File.ID, "read", false, []uint32{})

	leak := patternFn(t, nestedLoanConstructorSource, "fn leak(")
	ret := patternIn(t, nestedLoanConstructorSource, leak, "return Holder { data = fixed[[0..2]] };")
	originExactPending(t, analysis, f.unit.SourceKey, leak, nil)
	requireOriginEscape(t, analysis, f.owner.Symbols, f.owner.File.ID, ret, "fixed")
}
