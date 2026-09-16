package driver

import "testing"

// The rows a core `Map::<K, V>.new()` leaves at its own call: an opaque generic
// result whose borrowed state core cannot classify. They belong to the
// constructor, not to the loan guard, so leaves that assert an exact set allow them.
const (
	originCalleeSourceRefusal  = "callee returned an unproved source"
	originGenericResultRefusal = "generic result contains an unproved source"
	originOpaqueStateRefusal   = "opaque result borrowed-state classification is unsupported"
)

// A result that is not erased itself can still store an erased part that keeps a
// loan, and an actual that carries references can still hold one. Both are
// refused at the call. A scalar instance of the same shapes stays clean.
const resultLoanPartsSource = `fn some<T>(x: T) -> Option<T> {
    return Some(x);
}
tag Duo<A, B>(A, B);
type Both<A, B> = Duo(A, B) | nothing;
fn duo<A, B>(a: A, b: B) -> Both<A, B> {
    return Duo::<A, B>(a, b);
}
fn pack<T>(x: T, r: &int64) -> Both<Option<T>, &int64> {
    return duo::<Option<T>, &int64>(some::<T>(x), r);
}
fn first<A, B>(d: Both<A, B>) -> Option<A> {
    return compare d {
        Duo(x, _) => some::<A>(x);
        _ => nothing;
    };
}
fn leak_pack(n: &int64) -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let d = pack::<uint64[]>(a[[0..2]], n);
    return compare d {
        Duo(o, _) => o;
        _ => nothing;
    };
}
fn leak_first(n: &int64) -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let d = duo::<uint64[], &int64>(a[[0..2]], n);
    let o = first::<uint64[], &int64>(d);
    return o;
}
fn first_scalar(n: &int64) -> Option<int64> {
    let d = duo::<int64, &int64>(5:int64, n);
    let o = first::<int64, &int64>(d);
    return o;
}
fn map_view_insert() -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut m: Map<string, uint64[]> = Map::<string, uint64[]>.new();
    let _ = m.insert("k", a[[0..2]]);
    let o = m.insert("k", a[[1..3]]);
    return o;
}
fn insert_direct(value: int64) -> Option<int64> {
    let mut m: Map<string, int64> = Map::<string, int64>.new();
    return m.insert("key", value);
}
`

const resultLoanPartsDigest = "c7a5ac4a598dc6952d15e7b5d81a42b7b782bbbf281cff9c2ae8d509feacb229"

func TestAnalyzeGenericResultLoanParts(t *testing.T) {
	text := resultLoanPartsSource
	checkOriginSource(t, text, resultLoanPartsDigest)
	f, analysis := analyzeOriginRoot(t, "result_loan_parts", text, false, nil)
	key, file := f.unit.SourceKey, f.owner.File.ID
	leakPack := originSpan{434, 666, text[434:666]}
	leakFirst := originSpan{667, 887, text[667:887]}
	firstScalar := originSpan{888, 1034, text[888:1034]}
	mapViewInsert := originSpan{1035, 1304, text[1035:1304]}
	insertDirect := originSpan{1305, 1455, text[1305:1455]}
	packCall := originG6(557, 587, "pack::<uint64[]>(a[[0..2]], n)")
	firstCall := originG6(842, 870, "first::<uint64[], &int64>(d)")
	duoCall := originSpan{791, 828, "duo::<uint64[], &int64>(a[[0..2]], n)"}
	scalarDuo := originSpan{946, 978, "duo::<int64, &int64>(5:int64, n)"}
	scalarFirst := originSpan{992, 1017, "first::<int64, &int64>(d)"}
	firstInsert := originG6(1225, 1249, `m.insert("k", a[[0..2]])`)
	secondInsert := originG6(1263, 1287, `m.insert("k", a[[1..3]])`)
	checkOriginSource(t, text, resultLoanPartsDigest, packCall.span, firstCall.span, duoCall, scalarDuo, scalarFirst, firstInsert.span, secondInsert.span)

	t.Run("leak_pack", func(t *testing.T) {
		originExactPending(t, analysis, key, leakPack, []originRefusal{packCall})
		originNoEscape(t, analysis, file, leakPack)
		requireOriginSummary(t, analysis, file, "leak_pack", false, nil)
	})
	t.Run("leak_first", func(t *testing.T) {
		if originPendingAt(analysis, key, duoCall, backingLoanDiscard) {
			t.Errorf("a result whose carrier part is not erased was refused at %q", duoCall.snippet)
		}
		originExactPending(t, analysis, key, leakFirst, []originRefusal{firstCall})
		originNoEscape(t, analysis, file, leakFirst)
		requireOriginSummary(t, analysis, file, "leak_first", false, nil)
	})
	t.Run("first_scalar", func(t *testing.T) {
		for _, span := range []originSpan{scalarDuo, scalarFirst} {
			if originPendingAt(analysis, key, span, backingLoanDiscard) {
				t.Errorf("a scalar instance was refused at %q", span.snippet)
			}
		}
		originExactPending(t, analysis, key, firstScalar, nil)
		requireOriginSummary(t, analysis, file, "first_scalar", false, nil)
	})
	t.Run("map_view_insert", func(t *testing.T) {
		// `Map::<string, uint64[]>.new()` at 1182:1211 is an opaque generic result
		// whose borrowed state core cannot classify. Those three rows are the
		// constructor's own and are not what the guard refuses at the two inserts.
		originExactPending(t, analysis, key, mapViewInsert, []originRefusal{firstInsert, secondInsert},
			originCalleeSourceRefusal, originGenericResultRefusal, originOpaqueStateRefusal)
		originNoEscape(t, analysis, file, mapViewInsert)
		requireOriginSummary(t, analysis, file, "map_view_insert", false, nil)
	})
	t.Run("insert_direct", func(t *testing.T) {
		logReturnOriginCallEvidence(t, map[string]any{"leaf": "insert_direct",
			"pending":     originPendingWithin(analysis, key, insertDirect.start, insertDirect.end),
			"diagnostics": analysis.Diagnostics})
	})
}

// Inside a template body a concrete result, or a concrete part of one, is erased
// just as it is at an instance caller, and the lost root is the template's own
// local. The parts walk refuses both; a scalar instance of the shape stays clean.
const templateCallerLoanSource = `fn some<T>(x: T) -> Option<T> {
    return Some(x);
}
tag Duo<A, B>(A, B);
type Both<A, B> = Duo(A, B) | nothing;
fn duo<A, B>(a: A, b: B) -> Both<A, B> {
    return Duo::<A, B>(a, b);
}
fn first<A, B>(d: Both<A, B>) -> Option<A> {
    return compare d {
        Duo(x, _) => some::<A>(x);
        _ => nothing;
    };
}
fn pack_u<T, U>(x: T, u: U) -> Both<Option<T>, U> {
    return duo::<Option<T>, U>(some::<T>(x), u);
}
fn tpl_first<U>(u: U) -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let d = duo::<uint64[], U>(a[[0..2]], u);
    let o = first::<uint64[], U>(d);
    return o;
}
fn tpl_pack<U>(u: U) -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let d = pack_u::<uint64[], U>(a[[0..2]], u);
    return compare d {
        Duo(o, _) => o;
        _ => nothing;
    };
}
fn tpl_scalar<U>(u: U) -> Option<int64> {
    let d = duo::<int64, U>(5:int64, u);
    let o = first::<int64, U>(d);
    return o;
}
`

const templateCallerLoanDigest = "2dc305eeadb7fbf5b863364cc419581788e78ef5b2775808d64682b48f49e2d4"

func TestAnalyzeTemplateCallerResultLoans(t *testing.T) {
	text := templateCallerLoanSource
	checkOriginSource(t, text, templateCallerLoanDigest)
	f, analysis := analyzeOriginRoot(t, "template_caller_result_loans", text, false, nil)
	key, file := f.unit.SourceKey, f.owner.File.ID
	tplFirst := originSpan{424, 631, text[424:631]}
	tplPack := originSpan{632, 866, text[632:866]}
	tplScalar := originSpan{867, 999, text[867:999]}
	firstCall := originG6(591, 614, "first::<uint64[], U>(d)")
	packCall := originG6(752, 787, "pack_u::<uint64[], U>(a[[0..2]], u)")
	duoCall := originSpan{545, 577, "duo::<uint64[], U>(a[[0..2]], u)"}
	scalarDuo := originSpan{921, 948, "duo::<int64, U>(5:int64, u)"}
	scalarFirst := originSpan{962, 982, "first::<int64, U>(d)"}
	checkOriginSource(t, text, templateCallerLoanDigest, firstCall.span, packCall.span, duoCall, scalarDuo, scalarFirst)

	t.Run("tpl_first", func(t *testing.T) {
		if originPendingAt(analysis, key, duoCall, backingLoanDiscard) {
			t.Errorf("a result whose carrier part is not erased was refused at %q", duoCall.snippet)
		}
		originExactPending(t, analysis, key, tplFirst, []originRefusal{firstCall})
		originNoEscape(t, analysis, file, tplFirst)
		requireOriginSummary(t, analysis, file, "tpl_first", false, nil)
	})
	t.Run("tpl_pack", func(t *testing.T) {
		originExactPending(t, analysis, key, tplPack, []originRefusal{packCall})
		originNoEscape(t, analysis, file, tplPack)
		requireOriginSummary(t, analysis, file, "tpl_pack", false, nil)
	})
	t.Run("tpl_scalar", func(t *testing.T) {
		for _, span := range []originSpan{scalarDuo, scalarFirst} {
			if originPendingAt(analysis, key, span, backingLoanDiscard) {
				t.Errorf("a scalar instance was refused at %q", span.snippet)
			}
		}
		originExactPending(t, analysis, key, tplScalar, nil)
		requireOriginSummary(t, analysis, file, "tpl_scalar", false, nil)
	})
}

// A Map value and an erased element with its own __clone can hold a loan only
// after a producer refused it, so the caller-side container guard has nothing
// left to refuse. Each twin pins the producer row it depends on.
const containerLoanTwinSource = `type Box = { v: uint64[] };
extern<Box> {
    pub fn __clone(self: &Box) -> Box {
        return Box { v = [] };
    }
}
fn take_map(m: &mut Map<string, uint64[]>) -> Option<uint64[]> {
    let v = m.remove(&"k");
    return v;
}
fn leak_take_map() -> Option<uint64[]> {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let mut mm: Map<string, uint64[]> = Map::<string, uint64[]>.new();
    let _ = mm.insert("k", xs[[1..3]]);
    let o = take_map(&mut mm);
    return o;
}
fn clone_first(boxes: &Box[]) -> Box {
    let v = clone(boxes[0]);
    return v;
}
fn leak_clone_first() -> Box {
    let xs: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let boxes: Box[] = [Box { v = xs[[1..3]] }];
    let o = clone_first(&boxes);
    return o;
}
`

const containerLoanTwinDigest = "cfbd1ff4c5daa9e222fd95f37b2c2d6862055963a237896d5cc947864f60d555"

func TestAnalyzeContainerLoanTwins(t *testing.T) {
	text := containerLoanTwinSource
	checkOriginSource(t, text, containerLoanTwinDigest)
	f, analysis := analyzeOriginRoot(t, "container_loan_twins", text, false, nil)
	key, file := f.unit.SourceKey, f.owner.File.ID
	takeMap := originSpan{121, 229, text[121:229]}
	leakTakeMap := originSpan{230, 494, text[230:494]}
	cloneFirst := originSpan{495, 578, text[495:578]}
	leakCloneFirst := originSpan{579, 773, text[579:773]}
	insert := originG6(420, 446, `mm.insert("k", xs[[1..3]])`)
	takeCall := originSpan{460, 477, "take_map(&mut mm)"}
	literal := originG6(700, 722, "Box { v = xs[[1..3]] }")
	arrayLiteral := originSpan{699, 723, "[Box { v = xs[[1..3]] }]"}
	cloneCall := originSpan{546, 561, "clone(boxes[0])"}
	cloneFirstCall := originSpan{737, 756, "clone_first(&boxes)"}
	checkOriginSource(t, text, containerLoanTwinDigest, insert.span, takeCall, literal.span, arrayLiteral, cloneCall, cloneFirstCall)

	t.Run("map_remove_twin", func(t *testing.T) {
		if !originPendingAt(analysis, key, insert.span, backingLoanDiscard) {
			t.Errorf("the producer lost its refusal at %q", insert.span.snippet)
		}
		originNoEscape(t, analysis, file, leakTakeMap)
		logReturnOriginCallEvidence(t, map[string]any{"leaf": "map_remove_twin",
			"callee": originPendingWithin(analysis, key, takeMap.start, takeMap.end),
			"caller": originPendingWithin(analysis, key, leakTakeMap.start, leakTakeMap.end)})
	})
	t.Run("clone_twin", func(t *testing.T) {
		if !originPendingAt(analysis, key, literal.span, backingLoanDiscard) {
			t.Errorf("the producer lost its refusal at %q", literal.span.snippet)
		}
		if originPendingAt(analysis, key, arrayLiteral, backingLoanDiscard) {
			t.Errorf("the array literal refused an already emptied child at %q", arrayLiteral.snippet)
		}
		logReturnOriginCallEvidence(t, map[string]any{"leaf": "clone_twin",
			"callee": originPendingWithin(analysis, key, cloneFirst.start, cloneFirst.end),
			"caller": originPendingWithin(analysis, key, leakCloneFirst.start, leakCloneFirst.end)})
	})
}
