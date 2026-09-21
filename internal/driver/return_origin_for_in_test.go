package driver

import (
	"testing"

	"surge/internal/sema"
)

// A `for … in` binds one copied element per step. An element that holds no borrow and can keep
// no storage loan leaves the loop with no obligation; an element that can is refused by name at
// the statement; the binding's scope closes on the statement; and a selected `__range` is admitted
// only as a plain body over one shared receiver. Every source is a root program.
const (
	forInKindRow       = "statement kind 12 needs an origin transfer"
	forInBorrowElement = "for-in over elements that can hold a borrow needs the buffer-alias model"
	forInRangeCall     = "for-in iterator needs its selected __range call transfer"
)

const forInIntLoopsSource = `fn sum(xs: &int[], n: int) -> int {
    let mut total: int = 0;
    for x in xs {
        total = total + x;
    }
    let r: Range<int> = 0..n;
    for i in r {
        total = total + i;
    }
    for j: int in 0..3 {
        if j == 2 {
            break;
        }
        continue;
    }
    return total;
}
`

const forInKeepSource = `fn keep(xs: &string[], out: &mut &string) -> nothing {
    for s in xs {
        *out = &s;
    }
    return nothing;
}
`

const forInLoanElementsSource = `fn each_view(views: &Array<uint64[]>) -> nothing {
    for v in views {
        continue;
    }
    return nothing;
}
fn each_opt_view(views: &Array<Option<uint64[]>>) -> nothing {
    for v in views {
        continue;
    }
    return nothing;
}
`

const forInTemplateSource = `fn each<T>(xs: &Array<T>) -> nothing {
    for x in xs {
        continue;
    }
    return nothing;
}
fn use_int(xs: &Array<int>) -> nothing {
    each::<int>(xs);
    return nothing;
}
`

const forInTemplateRefusalSource = `type Cur = Range<uint64>;
fn each<T>(xs: &Array<T>) -> nothing {
    for x in xs {
        continue;
    }
    return nothing;
}
fn use_ref(xs: &Array<&string>) -> nothing {
    each::<&string>(xs);
    return nothing;
}
fn use_view(xs: &Array<uint64[]>) -> nothing {
    each::<uint64[]>(xs);
    return nothing;
}
fn use_cursor(xs: &Array<Cur>) -> nothing {
    each::<Cur>(xs);
    return nothing;
}
`

const forInRefElementsSource = `fn refs(xs: &Array<&string>) -> nothing {
    for s in xs {
        continue;
    }
    return nothing;
}
`

const forInBagSource = `type Bag = { values: int[] };
extern<Bag> {
    fn __range(self: &Bag) -> Range<int> {
        return self.values.__range();
    }
}
fn total(b: &Bag) -> int {
    let mut s: int = 0;
    for v in b {
        s = s + v;
    }
    return s;
}
`

const forInOwnBagSource = `type Bag = { values: int[] };
extern<Bag> {
    fn __range(self: Bag) -> Range<int> {
        return 0..3;
    }
}
fn owned_total(b: Bag) -> int {
    let mut s: int = 0;
    for v in b {
        s = s + v;
    }
    return s;
}
`

// forInSpan freezes one `for` statement, from its keyword to its closing brace.
func forInSpan(t *testing.T, text string, start, end int) originSpan {
	t.Helper()
	if start < 0 || end > len(text) || start+4 >= end || text[start:start+4] != "for " || text[end-1] != '}' {
		t.Fatalf("PRECONDITION: frozen statement %d:%d is not a for-in", start, end)
	}
	return originSpan{start, end, text[start:end]}
}

// forInRefused requires one reason at the statement and forbids the unhandled-statement row there.
func forInRefused(name, text string, start, end int, reason string, absent ...string) coreRangeLeaf {
	return coreRangeLeaf{name: name, check: func(t *testing.T, key string, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
		loop := forInSpan(t, text, start, end)
		coreRangeRefusal(name, loop, []string{reason}, append([]string{forInKindRow}, absent...)).check(t, key, f, analysis)
	}}
}

func forInSources() []coreRangeSource {
	keepLoop, keepReturn, keepOwner := originSpan{59, 97, forInKeepSource[59:97]}, originSpan{102, 117, "return nothing;"}, originSpan{63, 64, "s"}
	refCall, viewCall, cursorCall := originSpan{178, 197, "each::<&string>(xs)"}, originSpan{272, 292, "each::<uint64[]>(xs)"}, originSpan{364, 379, "each::<Cur>(xs)"}
	return []coreRangeSource{
		{name: "int_loops", text: forInIntLoopsSource, digest: "584da0c84a916502cfd0d680848abc37a0cb72335b101f7d216e931ad883e6f3",
			spans:  []originSpan{{68, 81, "for x in xs {"}, {149, 161, "for i in r {"}, {199, 219, "for j: int in 0..3 {"}},
			leaves: []coreRangeLeaf{coreRangeClean(forInIntLoopsSource, "sum", 0, 312, nil)}},
		{name: "root_keep", text: forInKeepSource, digest: "78c0525c4cdb474c945fbe56165512b1c07d84835edb0159d06c0a0c6d4d1ab3", escape: true,
			spans: []originSpan{keepLoop, keepReturn, keepOwner},
			leaves: []coreRangeLeaf{{name: "keep", check: func(t *testing.T, key string, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
				fn := coreRangeFn(t, forInKeepSource, "keep", 0, 119)
				// The binding's scope closes on the statement; the expired root is reported again where the function ends.
				requireOriginEscapeAt(t, analysis, f.owner.File.ID, keepLoop, keepOwner, "s")
				requireOriginEscapeAt(t, analysis, f.owner.File.ID, keepReturn, keepOwner, "s")
				if got := coreRangeEscapes(analysis, f, fn); got != 2 {
					t.Errorf("keep has %d diagnostics, want exactly 2: %+v", got, analysis.Diagnostics)
				}
				originExactPending(t, analysis, key, fn, nil)
			}}}},
		{name: "loan_elements", text: forInLoanElementsSource, digest: "4c3273bd9501b05f19676ebca6a09cb0d88fb48a51248ed4a1805b95dc9a1064",
			spans: []originSpan{{55, 71, "for v in views {"}, {185, 201, "for v in views {"}},
			leaves: []coreRangeLeaf{
				forInRefused("each_view", forInLoanElementsSource, 55, 95, rangeNextLoanElement, forInBorrowElement),
				// An Option over a view is no loan carrier itself, and NoBorrowedState still cannot clear it.
				forInRefused("each_opt_view", forInLoanElementsSource, 185, 225, rangeNextLoanElement, forInBorrowElement),
			}},
		{name: "template_each", text: forInTemplateSource, digest: "b570da7e47c1b1cf2241674ccc549db2ca72bef4d929e0ab3a58b90400995b22",
			spans: []originSpan{{43, 56, "for x in xs {"}, {148, 163, "each::<int>(xs)"}},
			leaves: []coreRangeLeaf{
				coreRangeClean(forInTemplateSource, "each", 0, 102, nil),
				coreRangeClean(forInTemplateSource, "use_int", 103, 186, nil),
			}},
		// A separate analysis: an instance over a borrowing element also pends inside `each` itself.
		{name: "template_each_refusal", text: forInTemplateRefusalSource, digest: "308c9fe7a8629bd988edc4fabed48e7ea375f21ba044fee4efae63d68818fb2e",
			spans: []originSpan{{69, 82, "for x in xs {"}, refCall, viewCall, cursorCall},
			leaves: []coreRangeLeaf{
				coreRangeRefusal("use_ref", refCall, []string{genericConditionRefuted}, []string{genericConditionUnsupported}),
				coreRangeRefusal("use_view", viewCall, []string{genericConditionUnsupported}, []string{genericConditionRefuted}),
				coreRangeRefusal("use_cursor", cursorCall, []string{genericConditionUnsupported}, []string{genericConditionRefuted}),
			}},
		{name: "ref_elements", text: forInRefElementsSource, digest: "1d8b4723249adb0fdc4acc5a8d2dd048b6a88c48818d9435972cb201132882b9",
			spans:  []originSpan{{46, 59, "for s in xs {"}},
			leaves: []coreRangeLeaf{forInRefused("refs", forInRefElementsSource, 46, 83, forInBorrowElement, rangeNextLoanElement)}},
		{name: "bag_range", text: forInBagSource, digest: "c22b1e3e153166eec52e475cfaa09272407e45879811f72db707a79c8c048839",
			spans: []originSpan{{188, 200, "for v in b {"}},
			leaves: []coreRangeLeaf{{name: "total", check: func(t *testing.T, key string, f originalGenericFixture, analysis *sema.ReturnOriginAnalysis) {
				fn := coreRangeFn(t, forInBagSource, "total", 133, 241)
				originExactPending(t, analysis, key, fn, nil)
				originNoEscape(t, analysis, f.owner.File.ID, fn)
				requireOriginSummary(t, analysis, f.owner.File.ID, "total", false, nil)
			}}}},
		// A `__range` that takes its receiver by value is not a read of it.
		{name: "own_bag_range", text: forInOwnBagSource, digest: "050a5832725bd1fe1285128cd86c2b18c2839a5be7fe05356c1d433293a3c337",
			spans:  []originSpan{{175, 187, "for v in b {"}},
			leaves: []coreRangeLeaf{forInRefused("owned_total", forInOwnBagSource, 175, 212, forInRangeCall)}},
	}
}

// 21 RUN: 1 parent, 8 sources, 12 leaves.
func TestAnalyzeForInOrigins(t *testing.T) {
	sources, leaves := forInSources(), 0
	for _, src := range sources {
		leaves += len(src.leaves)
	}
	if len(sources) != 8 || leaves != 12 {
		t.Fatalf("PRECONDITION: frozen roster changed: sources=%d leaves=%d", len(sources), leaves)
	}
	for _, src := range sources {
		t.Run(src.name, func(t *testing.T) {
			checkOriginSource(t, src.text, src.digest, src.spans...)
			f, analysis := analyzeOriginRoot(t, "for_in_"+src.name, src.text, src.escape, nil)
			for _, leaf := range src.leaves {
				t.Run(leaf.name, func(t *testing.T) { leaf.check(t, f.unit.SourceKey, f, analysis) })
			}
		})
	}
}
