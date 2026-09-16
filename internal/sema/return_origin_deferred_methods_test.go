package sema

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"
)

const deferredMethodRequirementsSource = "contract Top<T> {\n    fn __top() -> T;\n}\ncontract Absorb<T> {\n    fn __absorb(self: T) -> uint;\n}\ncontract Sized<T> {\n    fn __size(self: &T) -> uint;\n}\n@intrinsic fn sink<X>(x: X) -> nothing;\nfn top<T: Top<T>>() -> T {\n    return T.__top();\n}\nfn absorb<T: Absorb<T>>(x: T) -> uint {\n    return x.__absorb();\n}\nfn size<T: Sized<T>>(x: &T) -> uint {\n    return x.__size();\n}\nfn stop<E>(e: E) -> nothing {\n    sink(e);\n}\nfn root() -> nothing {\n    return nothing;\n}\n"

const deferredMethodMixedSource = "@intrinsic fn put<X>(dst: &mut X, x: X) -> nothing;\nfn fill<T>(dst: &mut T, v: T) -> nothing {\n    put(dst, v);\n}\nfn root() -> nothing {\n    return nothing;\n}\n"

const deferredMethodRosterSource = "type Plain = { a: int, b: string };\ntag Hit(int);\ntype Maybe = Hit(int) | nothing;\nfn probe(a: int, b: string, c: &string, d: Plain, e: Maybe, f: fn() -> int, h: bool, k: uint64[]) -> nothing {\n    return nothing;\n}\nfn root() -> nothing {\n    return nothing;\n}\n"

// checkDeferredMethodSource freezes a source and the one span a leaf reads.
func checkDeferredMethodSource(t *testing.T, text, digest string, start, end int, snippet string) {
	t.Helper()
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(text))); got != digest {
		t.Fatalf("PRECONDITION: frozen source changed: %s", got)
	}
	if snippet != "" && (start < 0 || end > len(text) || start >= end || text[start:end] != snippet) {
		t.Fatalf("PRECONDITION: frozen span %d:%d is not %q", start, end, snippet)
	}
}

func deferredMethodFact(t *testing.T, a *returnOriginAnalyzer, name string) (*returnOriginFunction, returnOriginSummaryFact) {
	t.Helper()
	var found *returnOriginFunction
	for _, fn := range a.functions {
		if fn.name == name {
			if found != nil {
				t.Fatalf("PRECONDITION: body %s is not unique", name)
			}
			found = fn
		}
	}
	if found == nil {
		t.Fatalf("PRECONDITION: body %s is missing", name)
	}
	fact := a.summaries[found.key]
	t.Logf("DEFERRED_METHOD_FACT body=%s required=%+v conditions=%d pending=%+v", found.key, fact.required, len(fact.conditions), a.report.Pending)
	return found, fact
}

// A deferred method's admitted result and a template's by-value actual record
// NoBorrowedState on their own slot; a scalar result records nothing.
func TestReturnOriginDeferredMethodRequirements(t *testing.T) {
	const digest = "1192c09a585e3183c430e2941ef3693a8f5ac716274ec7194dccdc6c233784cf"
	slot := []returnOriginAtom{{kind: returnOriginNoBorrowedState, slot: 0}}
	for _, tc := range []struct {
		name, body, snippet string
		start, end          int
		atoms               []returnOriginAtom
	}{
		{"ref_free_result", "size", "x.__size()", 360, 370, nil},
		{"template_result", "top", "T.__top()", 231, 240, slot},
		{"value_receiver", "absorb", "x.__absorb()", 295, 307, slot},
		{"template_opaque_actual", "stop", "sink(e)", 408, 415, slot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkDeferredMethodSource(t, deferredMethodRequirementsSource, digest, tc.start, tc.end, tc.snippet)
			a := returnOriginConditionFixture(t, deferredMethodRequirementsSource)
			_, fact := deferredMethodFact(t, a, tc.body)
			if fact.required.failed() || !slices.Equal(fact.required.atoms, tc.atoms) {
				t.Fatalf("%s required = %+v, want atoms %v and no failure", tc.body, fact.required, tc.atoms)
			}
		})
	}
	// A kept unproved effect keeps the body-level test and records nothing.
	t.Run("mixed_opaque_actual", func(t *testing.T) {
		checkDeferredMethodSource(t, deferredMethodMixedSource, "b3a4592f58cdb8b06cbab5620140b98f8b18fc8db5b3c7952e18007fac271d09", 99, 110, "put(dst, v)")
		a := returnOriginConditionAnalyzer(t, deferredMethodMixedSource)
		_, fact := deferredMethodFact(t, a, "fill")
		refused := slices.ContainsFunc(a.report.Pending, func(p ReturnOriginPending) bool {
			return p.SourceKey == "origin.sg" && p.Span.Start == 99 && p.Span.End == 110 && p.Reason == "opaque call may change reference-bearing or callable contents"
		})
		if len(fact.required.atoms) != 0 || !refused {
			t.Fatalf("mixed actual moved its template entry or lost the body refusal: required=%+v pending=%+v", fact.required, a.report.Pending)
		}
	})
}

// Lemma L1 over a closed roster: TRUE implies a reference-free shape, and a
// P1b loan carrier or borrowed view is never TRUE.
func TestReturnOriginNoBorrowedStateImpliesRefFree(t *testing.T) {
	checkDeferredMethodSource(t, deferredMethodRosterSource, "40107af5be2301b00b50c586d29b519f4a39746cb347ed5750443158550e98af", 0, 0, "")
	a := returnOriginConditionFixture(t, deferredMethodRosterSource)
	fn, _ := deferredMethodFact(t, a, "probe")
	in := fn.unit.Sema.TypeInterner
	want := []struct {
		name, requirement string
		shape             returnOriginShape
	}{
		{"a", "true", returnOriginRefFree},
		{"b", "true", returnOriginRefFree},
		{"c", "refuted", returnOriginCarriesRef},
		{"d", "true", returnOriginRefFree},
		{"e", "true", returnOriginRefFree},
		{"f", "unsupported", returnOriginShapeUnknown},
		{"h", "true", returnOriginRefFree},
		{"k", "unsupported", returnOriginRefFree},
	}
	if len(fn.info.Params) != len(want) {
		t.Fatalf("PRECONDITION: probe has %d formals, want %d", len(fn.info.Params), len(want))
	}
	var carriers []int
	for i, typ := range fn.info.Params {
		if a.loanCarrier(typ) || in.IsBorrowedView(typ) {
			carriers = append(carriers, i)
		}
	}
	if !slices.Equal(carriers, []int{7}) {
		t.Fatalf("PRECONDITION: the carrier predicate selects formals %v, want exactly k", carriers)
	}
	view := returnOriginView(fn)
	for i, typ := range fn.info.Params {
		required := view.requirement(returnOriginNoBorrowedState, typ)
		shape := returnOriginTypeShape(in, typ, nil)
		got := fmt.Sprintf("%+v", required)
		switch {
		case !required.failed() && len(required.atoms) == 0:
			got = "true"
		case required.refuted && !required.unsupported && len(required.atoms) == 0:
			got = "refuted"
		case required.unsupported && !required.refuted && len(required.atoms) == 0:
			got = "unsupported"
		}
		t.Logf("NO_BORROWED_STATE_ROSTER formal=%s type=%d requirement=%s shape=%d", want[i].name, typ, got, shape)
		if got != want[i].requirement || shape != want[i].shape {
			t.Errorf("%s: requirement %s shape %d, want %s shape %d", want[i].name, got, shape, want[i].requirement, want[i].shape)
		}
		if got == "true" && shape != returnOriginRefFree {
			t.Errorf("%s: NoBorrowedState TRUE with a shape that is not reference-free", want[i].name)
		}
		if slices.Contains(carriers, i) && got == "true" {
			t.Errorf("%s: a loan carrier is NoBorrowedState TRUE", want[i].name)
		}
	}
}
