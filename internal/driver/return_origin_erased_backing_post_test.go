package driver

import "testing"

// A generic mutator may keep an Unknown in its template post-state when it
// clones and rewrites T. Once T is a concrete type that can retain neither a
// borrow nor a storage loan, no value of T can carry that Unknown. The call's
// backing therefore remains source-free.
const erasedBackingPostSource = `pragma module::dep;
type Plain = { text: string, n: int };
extern<Plain> {
    pub fn __clone(self: &Plain) -> Plain {
        return Plain { text = clone(self.text), n = self.n };
    }
}
fn reverse_plain() -> nothing {
    let mut xs: Plain[] = [Plain { text = "a", n = 1 }, Plain { text = "b", n = 2 }];
    xs.reverse_in_place();
    return nothing;
}
`

const erasedBackingPostDigest = "4077b99b0cc9b38debeedb4dee60ffc9ed866519862451353b674a17f9d3f18b"

func TestAnalyzeErasedConcreteBackingPost(t *testing.T) {
	checkOriginSource(t, erasedBackingPostSource, erasedBackingPostDigest,
		originSpan{0, len(erasedBackingPostSource), erasedBackingPostSource})
	f, analysis := analyzeOriginRoot(t, "erased_backing_post", erasedBackingPostSource, false, nil)
	fn := patternFn(t, erasedBackingPostSource, "fn reverse_plain(")
	originExactPending(t, analysis, f.unit.SourceKey, fn, nil)
	originNoEscape(t, analysis, f.owner.File.ID, fn)
	requireOriginSummary(t, analysis, f.owner.File.ID, "reverse_plain", false, []uint32{})
}
