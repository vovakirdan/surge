package driver

import (
	"testing"

	"surge/internal/sema"
)

// A deferred contract method that resolves to a generic implementation: the use of that
// implementation has no original typed request, so it is believed only through its pinned
// finalized outcome. Every leaf freezes its source and each span it reads.

const implementationUseRefusal = "generic original call disagrees with its selected canonical template"

// `len(x)` used to leave "generic original method call lacks its receiver expression"
// here, pinned as pre-existing by P1p-G. P1x-B certifies the free-form `self` call,
// so those spans are clean and the `cleared` lists below carry them instead.

type deferredImplementationLeaf struct {
	name, text, digest, edgeKey string
	edge                        originSpan
	others                      int
	cleared                     []originSpan
	rootRows                    []originRefusal
	prepare                     func(t *testing.T, f originalGenericFixture)
}

// deferredImplementationOutcome is the one resolved generic-implementation outcome at a site.
// One edge serves every instance of its caller, so a site may also carry non-generic outcomes
// for other instances; others says exactly how many, and the whole multiset is pinned.
func deferredImplementationOutcome(t *testing.T, f originalGenericFixture, key string, span originSpan, others int) sema.ResolvedDeferredCall {
	t.Helper()
	calls := deferredMethodOutcomes(t, f, key, span)
	var generic []sema.ResolvedDeferredCall
	concrete := 0
	for _, call := range calls {
		switch {
		case call.Outcome != sema.DeferredCallableResolved:
		case len(call.CalleeTemplateArgs) != 0:
			generic = append(generic, call)
		default:
			concrete++
		}
	}
	if len(generic) != 1 || concrete != others || len(calls) != others+1 {
		t.Fatalf("PRECONDITION: %s %d:%d wants one resolved generic-implementation outcome and %d non-generic: %+v",
			key, span.start, span.end, others, calls)
	}
	return generic[0]
}

func TestAnalyzeDeferredGenericImplementationUses(t *testing.T) {
	coreLen := originSpan{2225, 2237, "self.__len()"}
	for _, leaf := range []deferredImplementationLeaf{
		{name: "g1_array_len", digest: "f92dec8b718f1c19b154beb7b8a97514ba907712338ebf470d2d583322439a47",
			text:    "fn probe() -> uint {\n    let arr: int[] = [1, 2, 3];\n    let len_arr = len(arr);\n    return len_arr;\n}\n",
			edgeKey: "core/base.sg", edge: coreLen, cleared: []originSpan{{71, 79, "len(arr)"}}},
		{name: "g2_fixed_len", digest: "4e355ef13236ec11eae2c9ba34b6da90da577b9fc4bfedfb8c6c2202c988b47e",
			text:    "fn probe() -> uint {\n    let arr_fixed: ArrayFixed<int, 3> = [1, 2, 3];\n    return len(arr_fixed);\n}\n",
			edgeKey: "core/base.sg", edge: coreLen, cleared: []originSpan{{83, 97, "len(arr_fixed)"}}},
		{name: "g3_view_len", digest: "96b0857936b21d082147843264d4386fdd3dab5fa287c481b28a3bfb6ac1cc60",
			text:    "fn probe() -> uint {\n    let mut base: int[] = [10, 20, 30, 40, 50];\n    let mut view = base[[1..4]];\n    let vlen: uint = len(&view);\n    return vlen;\n}\n",
			edgeKey: "core/base.sg", edge: coreLen, cleared: []originSpan{{123, 133, "len(&view)"}}},
		{name: "g4_string_and_array", digest: "0397d255d296fffb05b04e1e1ec844c93e64ac37752484efab76792d5c899f9e",
			text: "fn probe() -> uint {\n    let mut a: int[] = [];\n    a.push(1);\n    let n: uint = len(a);\n    let s: string = \"hi\";\n    let m: uint = len(s);\n    return n + m;\n}\n",
			// Two instances share the one edge: len<Array<int>> is generic, len<string> is not.
			edgeKey: "core/base.sg", edge: coreLen, others: 1,
			cleared: []originSpan{{81, 87, "len(a)"}, {133, 139, "len(s)"}}},
		// A user generic implementation, measured admitted.
		{name: "g5_user_generic_impl", digest: "8b644bab4d4e496a14c5e91ff7329a83a5e713c2cbf0d5aa0c99b8070784799e",
			text:    "contract Countable<T> {\n    fn count(self: &T) -> uint;\n}\ntype Bag<T> = { items: T[] };\nextern<Bag<T>> {\n    fn count(self: &Bag<T>) -> uint {\n        return 0:uint;\n    }\n}\nfn total<T: Countable<T>>(x: &T) -> uint {\n    return x.count();\n}\nfn probe(b: &Bag<int>) -> uint {\n    return total(b);\n}\n",
			cleared: []originSpan{{228, 237, "x.count()"}, {285, 293, "total(b)"}},
			prepare: func(t *testing.T, f originalGenericFixture) {
				deferredImplementationOutcome(t, f, f.unit.SourceKey, originSpan{228, 237, "x.count()"}, 0)
			}},
		// A generic implementation with a body and one by-value argument: checkGenericPromise's body
		// branch, and the argument pin positively. Core offers no such implementation: its only
		// contract-reachable generic implementations are Array/ArrayFixed __len, which are
		// body-less intrinsics, and __to is the conversion operator, not a callable contract method.
		{name: "g6_user_generic_arg", digest: "484726f99fcb0d31ad980503f99292a6b467411c575aaa728a6920f4c5d7cf55",
			text:    "contract Countable<T> {\n    fn count(self: &T, extra: uint) -> uint;\n}\ntype Bag<T> = { items: T[] };\nextern<Bag<T>> {\n    fn count(self: &Bag<T>, extra: uint) -> uint {\n        return extra;\n    }\n}\nfn total<T: Countable<T>>(x: &T) -> uint {\n    return x.count(2:uint);\n}\nfn probe(b: &Bag<int>) -> uint {\n    return total(b);\n}\n",
			cleared: []originSpan{{253, 268, "x.count(2:uint)"}, {316, 324, "total(b)"}},
			prepare: func(t *testing.T, f originalGenericFixture) {
				found := deferredImplementationOutcome(t, f, f.unit.SourceKey, originSpan{253, 268, "x.count(2:uint)"}, 0)
				body := false
				for _, candidate := range f.authority.CallableCandidates {
					if candidate.Symbol == found.Callee {
						body = candidate.HasBody && !candidate.Intrinsic
						logReturnOriginCallEvidence(t, map[string]any{"selected_implementation": candidate, "arguments": found.Args})
					}
				}
				if !body || len(found.Args) != 1 {
					t.Fatal("PRECONDITION: Bag<T>.count is not the selected implementation with a body and one argument")
				}
			}},
	} {
		t.Run(leaf.name, func(t *testing.T) {
			spans := append([]originSpan{}, leaf.cleared...)
			for _, row := range leaf.rootRows {
				spans = append(spans, row.span)
			}
			checkOriginSource(t, leaf.text, leaf.digest, spans...)
			f, analysis := analyzeOriginRoot(t, "deferred_implementation_"+leaf.name, leaf.text, false, func(f originalGenericFixture) {
				if leaf.edgeKey != "" {
					deferredImplementationOutcome(t, f, leaf.edgeKey, deferredCoreSpan(t, f, leaf.edgeKey, leaf.edge.start, leaf.edge.end, leaf.edge.snippet), leaf.others)
				}
				if leaf.prepare != nil {
					leaf.prepare(t, f)
				}
			})
			if leaf.edgeKey != "" {
				deferredCleared(t, analysis, leaf.edgeKey, deferredCoreSpan(t, f, leaf.edgeKey, leaf.edge.start, leaf.edge.end, leaf.edge.snippet))
			}
			deferredCleared(t, analysis, f.unit.SourceKey, leaf.cleared...)
			root := originPendingWithin(analysis, f.unit.SourceKey, 0, 1<<30)
			for _, want := range leaf.rootRows {
				if !originPendingAt(analysis, f.unit.SourceKey, want.span, want.reason) {
					t.Errorf("lost %q at %d:%d %q: %+v", want.reason, want.span.start, want.span.end, want.span.snippet, root)
				}
			}
			if len(root) != len(leaf.rootRows) {
				t.Errorf("root Pending is not exactly %d pre-existing row(s): %+v", len(leaf.rootRows), root)
			}
		})
	}
}
