package driver

import "testing"

// A source body receives a loan-carrier formal as the value it is and can
// consume it without erasing its owner. An opaque declaration has no body proof
// and retains the ordinary G6 loan-discard refusal.
const loanFormalSource = `pragma module::dep;
fn consume(r: Range<int>) -> nothing {
    let _ = r;
    return nothing;
}
@intrinsic fn stash(r: Range<int>) -> nothing;
fn safe(a: &int[]) -> nothing {
    consume(a.__range());
    return nothing;
}
fn unsafe(a: &int[]) -> nothing {
    stash(a.__range());
    return nothing;
}
`

const loanFormalDigest = "a22c20147a7367a308c458f48c18cab27ac3be436d6aa7856b5141397eb0b004"

func TestAnalyzeLoanCarrierFormal(t *testing.T) {
	checkOriginSource(t, loanFormalSource, loanFormalDigest,
		originSpan{0, len(loanFormalSource), loanFormalSource})
	f, analysis := analyzeOriginRoot(t, "loan_formal", loanFormalSource, false, nil)
	safe := patternFn(t, loanFormalSource, "fn safe(")
	originExactPending(t, analysis, f.unit.SourceKey, safe, nil)
	requireOriginSummary(t, analysis, f.owner.File.ID, "safe", false, []uint32{})

	unsafe := patternFn(t, loanFormalSource, "fn unsafe(")
	originExactPending(t, analysis, f.unit.SourceKey, unsafe, []originRefusal{
		{originSpan{261, 279, "stash(a.__range())"}, backingLoanDiscard},
	})
}
