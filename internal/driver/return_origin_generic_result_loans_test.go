package driver

import (
	"fmt"
	"slices"
	"testing"

	"surge/internal/diag"
	"surge/internal/sema"
	"surge/internal/source"
)

// originLoanTagUse is the row an implicit Some or Success injection leaves at its
// own operand, because the tag use has no typed call of its own. Leaves allow it.
const originLoanTagUse = "generic tag use disagrees with its original typed call"

// originImplicitTagTarget is the other row such an injection leaves: the wrapper
// it builds has no declaration target to read, because no tag call was written.
const originImplicitTagTarget = "tag constructor lacks its original declaration target"

// requireOriginEscapeAt finds one SEM3139 by span rather than by owner name, so
// a source may declare the same name in two bodies and still say which one
// escaped. The note covers the owner's whole declaration, so it is asked to
// CONTAIN the identifier: that survives a wider or narrower statement, and it
// still fails when the note names a different owner.
func requireOriginEscapeAt(t *testing.T, analysis *sema.ReturnOriginAnalysis, file source.FileID, primary, ownerIdent originSpan, owner string) {
	t.Helper()
	want := source.Span{File: file, Start: uint32(primary.start), End: uint32(primary.end)}
	for _, d := range analysis.Diagnostics {
		if d.Code == diag.SemaBorrowEscapesReturn && d.Severity == diag.SevError && d.Primary == want &&
			d.Message == fmt.Sprintf("borrow of '%s' outlives its owner when this scope exits", owner) &&
			slices.ContainsFunc(d.Notes, func(n diag.Note) bool {
				return n.Span.File == file && int(n.Span.Start) <= ownerIdent.start && ownerIdent.end <= int(n.Span.End) &&
					n.Msg == fmt.Sprintf("'%s' owns storage that ends in this scope", owner)
			}) {
			return
		}
	}
	t.Errorf("missing SEM3139 at %q whose note covers the owner at %d:%d: %+v", primary.snippet, ownerIdent.start, ownerIdent.end, analysis.Diagnostics)
}

// originExactPending requires the Pending rows inside one function range to be
// exactly the wanted ones, apart from the reasons a leaf allows.
func originExactPending(t *testing.T, analysis *sema.ReturnOriginAnalysis, key string, fn originSpan, want []originRefusal, allow ...string) {
	t.Helper()
	got := originPendingWithin(analysis, key, fn.start, fn.end)
	for _, refusal := range want {
		if !originPendingAt(analysis, key, refusal.span, refusal.reason) {
			t.Errorf("missing %q at %d:%d %q: %+v", refusal.reason, refusal.span.start, refusal.span.end, refusal.span.snippet, got)
		}
	}
	for _, row := range got {
		wanted := slices.ContainsFunc(want, func(refusal originRefusal) bool {
			return int(row.Span.Start) == refusal.span.start && int(row.Span.End) == refusal.span.end && row.Reason == refusal.reason
		})
		if wanted || slices.Contains(allow, row.Reason) {
			continue
		}
		t.Errorf("unexpected %q at %d:%d inside %q", row.Reason, row.Span.Start, row.Span.End, fn.snippet)
	}
}

// originNoEscape requires that no diagnostic points inside one function range.
func originNoEscape(t *testing.T, analysis *sema.ReturnOriginAnalysis, file source.FileID, fn originSpan) {
	t.Helper()
	for _, d := range analysis.Diagnostics {
		if d.Primary.File == file && int(d.Primary.Start) >= fn.start && int(d.Primary.End) <= fn.end {
			t.Errorf("unexpected diagnostic inside %q: %+v", fn.snippet, d)
		}
	}
}

// originG6 is one loan-discard refusal at a frozen span.
func originG6(start, end int, snippet string) originRefusal {
	return originRefusal{span: originSpan{start, end, snippet}, reason: backingLoanDiscard}
}

// originRootDiagnostics counts the diagnostics of one analyzed source file.
func originRootDiagnostics(analysis *sema.ReturnOriginAnalysis, file source.FileID) int {
	count := 0
	for _, d := range analysis.Diagnostics {
		if d.Primary.File == file {
			count++
		}
	}
	return count
}

// A generic body's by-value formal keeps its actual's loan in V(0), so a call
// whose own type is reference-free and no loan carrier hands that loan to a
// reader that drops it. The call refuses it and keeps the roots, so the direct
// return still names the owner. A parameter pass-through stays clean.
const genericResultLoanSource = `fn some<T>(x: T) -> Option<T> {
    return Some(x);
}
fn leak() -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let o = some::<uint64[]>(a[[0..2]]);
    return o;
}
fn leak_direct() -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    return some::<uint64[]>(a[[0..2]]);
}
fn keep(b: uint64[]) -> Option<uint64[]> {
    let o = some::<uint64[]>(b);
    return o;
}
`

const genericResultLoanDigest = "1980f6ecc568db351fc67d995d3d5eb782d7ab2795dd4b7cffacb99b13b1b14f"

func TestAnalyzeGenericCallResultLoans(t *testing.T) {
	checkOriginSource(t, genericResultLoanSource, genericResultLoanDigest)
	f, analysis := analyzeOriginRoot(t, "generic_result_loans", genericResultLoanSource, true, nil)
	key, file := f.unit.SourceKey, f.owner.File.ID
	whole := originSpan{0, len(genericResultLoanSource), genericResultLoanSource}
	leak := originSpan{54, 207, genericResultLoanSource[54:207]}
	leakDirect := originSpan{208, 353, genericResultLoanSource[208:353]}
	keep := originSpan{354, 445, genericResultLoanSource[354:445]}
	tagSite := originSpan{43, 50, "Some(x)"}
	leakCall := originG6(163, 190, "some::<uint64[]>(a[[0..2]])")
	directCall := originG6(323, 350, "some::<uint64[]>(a[[0..2]])")
	escape := originSpan{316, 351, "return some::<uint64[]>(a[[0..2]]);"}
	checkOriginSource(t, genericResultLoanSource, genericResultLoanDigest, tagSite, leakCall.span, directCall.span, escape)

	t.Run("leak", func(t *testing.T) {
		originExactPending(t, analysis, key, leak, []originRefusal{leakCall})
		originNoEscape(t, analysis, file, leak)
		requireOriginSummary(t, analysis, file, "leak", false, nil)
	})
	t.Run("leak_direct", func(t *testing.T) {
		requireOriginEscapeAt(t, analysis, file, escape, originSpan{255, 256, "a"}, "a")
		originExactPending(t, analysis, key, leakDirect, []originRefusal{directCall})
		requireOriginSummary(t, analysis, file, "leak_direct", true, nil)
	})
	t.Run("keep", func(t *testing.T) {
		originExactPending(t, analysis, key, keep, nil)
		requireOriginSummary(t, analysis, file, "keep", false, nil)
	})
	t.Run("tag_caller_pin", func(t *testing.T) {
		if !originPendingAt(analysis, key, tagSite, originTagCallerRefusal) {
			t.Errorf("the instantiated tag use lost its refusal at %q", tagSite.snippet)
		}
	})
	t.Run("whole_source", func(t *testing.T) {
		originExactPending(t, analysis, key, whole, []originRefusal{
			{span: tagSite, reason: originTagCallerRefusal},
			leakCall,
			directCall,
		})
		if got := originRootDiagnostics(analysis, file); got != 1 {
			t.Errorf("root diagnostics = %d, want exactly the one escape: %+v", got, analysis.Diagnostics)
		}
	})
}

// Controls for the result and the actual: a Copy instance, a loan-carrier result
// that keeps its roots, a reference-bearing result, and a receiver whose storage
// must not be refused. A view of a reference parameter, an Erring twin and a
// compare-arm binding are refused at the call that erases them.
const genericResultLoanControlSource = `fn some<T>(x: T) -> Option<T> {
    return Some(x);
}
fn id<T>(x: T) -> T {
    return x;
}
fn success<T>(x: T) -> Erring<T, Error> {
    return Success(x);
}
tag Duo<A, B>(A, B);
type Both<A, B> = Duo(A, B) | nothing;
fn duo<A, B>(a: A, b: B) -> Both<A, B> {
    return Duo::<A, B>(a, b);
}
fn number(n: int64) -> Option<int64> {
    let o = some::<int64>(n);
    return o;
}
fn first_view(xs: &uint64[4]) -> uint64[] {
    let v = id::<uint64[]>(xs[[0..2]]);
    return v;
}
fn ref_hold(r: &int64) -> nothing {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let d = duo::<uint64[], &int64>(a[[0..2]], r);
    return nothing;
}
fn map_store(value: int64) -> nothing {
    let mut m: Map<string, int64> = Map::<string, int64>.new();
    let _ = m.insert("key", value);
    return nothing;
}
fn param_view(xs: &uint64[4]) -> Option<uint64[]> {
    let o = some::<uint64[]>(xs[[0..2]]);
    return o;
}
fn leak_erring() -> Erring<uint64[], Error> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let e = success::<uint64[]>(a[[0..2]]);
    return e;
}
fn leak_arm() -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    return compare some::<Option<uint64[]>>(some::<uint64[]>(a[[0..2]])) {
        Some(inner) => inner;
        _ => nothing;
    };
}
`

const genericResultLoanControlDigest = "9e1013683f6e3dce3b43d5b26058a1e9fc822f5370d7a1425423eae36bcb79ed"

func TestAnalyzeGenericCallResultLoanControls(t *testing.T) {
	text := genericResultLoanControlSource
	checkOriginSource(t, text, genericResultLoanControlDigest)
	f, analysis := analyzeOriginRoot(t, "generic_result_loan_controls", text, false, nil)
	key, file := f.unit.SourceKey, f.owner.File.ID
	number := originSpan{292, 376, text[292:376]}
	firstView := originSpan{377, 476, text[377:476]}
	refHold := originSpan{477, 650, text[477:650]}
	mapStore := originSpan{651, 812, text[651:812]}
	paramView := originSpan{813, 922, text[813:922]}
	leakErring := originSpan{923, 1093, text[923:1093]}
	leakArm := originSpan{1094, 1330, text[1094:1330]}
	successSite := originSpan{145, 155, "Success(x)"}
	insert := originSpan{767, 789, `m.insert("key", value)`}
	paramCall := originG6(877, 905, "some::<uint64[]>(xs[[0..2]])")
	erringCall := originG6(1046, 1076, "success::<uint64[]>(a[[0..2]])")
	armInner := originG6(1239, 1266, "some::<uint64[]>(a[[0..2]])")
	armOuter := originG6(1214, 1267, "some::<Option<uint64[]>>(some::<uint64[]>(a[[0..2]]))")
	checkOriginSource(t, text, genericResultLoanControlDigest, successSite, insert, paramCall.span, erringCall.span, armInner.span, armOuter.span)

	t.Run("number", func(t *testing.T) {
		originExactPending(t, analysis, key, number, nil)
		requireOriginSummary(t, analysis, file, "number", false, nil)
	})
	t.Run("first_view", func(t *testing.T) {
		originExactPending(t, analysis, key, firstView, nil)
		requireOriginSummary(t, analysis, file, "first_view", false, []uint32{0})
	})
	t.Run("ref_hold", func(t *testing.T) {
		originExactPending(t, analysis, key, refHold, nil)
		requireOriginSummary(t, analysis, file, "ref_hold", false, nil)
	})
	t.Run("map_store", func(t *testing.T) {
		pins := 0
		for _, summary := range analysis.Summaries {
			if summary.Name == "insert" && !summary.Unknown && slices.Contains(summary.ParamSlots, uint32(0)) {
				pins++
			}
		}
		if pins == 0 {
			t.Fatalf("PRECONDITION: no core insert summary names slot 0: %+v", analysis.Summaries)
		}
		if originPendingAt(analysis, key, insert, backingLoanDiscard) {
			t.Errorf("a receiver's storage raised the loan-discard refusal at %q", insert.snippet)
		}
		logReturnOriginCallEvidence(t, map[string]any{"leaf": "map_store",
			"pending": originPendingWithin(analysis, key, mapStore.start, mapStore.end)})
	})
	t.Run("param_view", func(t *testing.T) {
		originExactPending(t, analysis, key, paramView, []originRefusal{paramCall})
		requireOriginSummary(t, analysis, file, "param_view", false, nil)
	})
	t.Run("leak_erring", func(t *testing.T) {
		if !originPendingAt(analysis, key, successSite, originTagCallerRefusal) {
			t.Fatalf("PRECONDITION: the Erring template tag use lost its refusal at %q", successSite.snippet)
		}
		originExactPending(t, analysis, key, leakErring, []originRefusal{erringCall})
		originNoEscape(t, analysis, file, leakErring)
		requireOriginSummary(t, analysis, file, "leak_erring", false, nil)
	})
	t.Run("leak_arm", func(t *testing.T) {
		originExactPending(t, analysis, key, leakArm, []originRefusal{armInner, armOuter})
		originNoEscape(t, analysis, file, leakArm)
		requireOriginSummary(t, analysis, file, "leak_arm", false, nil)
	})
}

// An implicit Some injection keeps the payload's loans, and an erased wrapper
// hands them to a reader that drops them. The injection refuses the loan and
// keeps the roots; a reference-bearing wrapper keeps its payload as before.
const injectedResultLoanSource = `fn view_of(xs: &uint64[4]) -> uint64[] {
    return xs[[0..2]];
}
fn inject_let() -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    let o: Option<uint64[]> = view_of(&a);
    return o;
}
fn inject_return() -> Option<uint64[]> {
    let a: uint64[4] = [1:uint64, 2:uint64, 3:uint64, 4:uint64];
    return view_of(&a);
}
fn inject_param(xs: &uint64[4]) -> Option<uint64[]> {
    let o: Option<uint64[]> = view_of(xs);
    return o;
}
fn inject_ref(r: &string) -> Option<&string> {
    let o: Option<&string> = r;
    return o;
}
`

const injectedResultLoanDigest = "9619afd5db3f1a2952419ddf7a1be4f370d59e521aa24ba2be9e8569fbb555f9"

func TestAnalyzeInjectedResultLoans(t *testing.T) {
	text := injectedResultLoanSource
	checkOriginSource(t, text, injectedResultLoanDigest)
	f, analysis := analyzeOriginRoot(t, "injected_result_loans", text, true, nil)
	key, file := f.unit.SourceKey, f.owner.File.ID
	injectLet := originSpan{66, 227, text[66:227]}
	injectReturn := originSpan{228, 359, text[228:359]}
	injectParam := originSpan{360, 472, text[360:472]}
	injectRef := originSpan{473, 567, text[473:567]}
	letCall := originG6(199, 210, "view_of(&a)")
	returnCall := originG6(345, 356, "view_of(&a)")
	paramCall := originG6(444, 455, "view_of(xs)")
	refOperand := originSpan{549, 550, "r"}
	escape := originSpan{338, 357, "return view_of(&a);"}
	checkOriginSource(t, text, injectedResultLoanDigest, letCall.span, returnCall.span, paramCall.span, refOperand, escape)

	t.Run("inject_let", func(t *testing.T) {
		originExactPending(t, analysis, key, injectLet, []originRefusal{letCall}, originLoanTagUse, originImplicitTagTarget)
		originNoEscape(t, analysis, file, injectLet)
		requireOriginSummary(t, analysis, file, "inject_let", false, nil)
	})
	t.Run("inject_return", func(t *testing.T) {
		requireOriginEscapeAt(t, analysis, file, escape, originSpan{277, 278, "a"}, "a")
		originExactPending(t, analysis, key, injectReturn, []originRefusal{returnCall}, originLoanTagUse, originImplicitTagTarget)
	})
	t.Run("inject_param", func(t *testing.T) {
		originExactPending(t, analysis, key, injectParam, []originRefusal{paramCall}, originLoanTagUse, originImplicitTagTarget)
		requireOriginSummary(t, analysis, file, "inject_param", false, nil)
	})
	t.Run("inject_ref", func(t *testing.T) {
		if originPendingAt(analysis, key, refOperand, backingLoanDiscard) {
			t.Errorf("a reference-bearing wrapper raised the loan-discard refusal at %q", refOperand.snippet)
		}
		originExactPending(t, analysis, key, injectRef, nil, originLoanTagUse, originImplicitTagTarget)
		requireOriginSummary(t, analysis, file, "inject_ref", false, []uint32{0})
	})
}
