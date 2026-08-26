package sema

import (
	"testing"

	"surge/internal/diag"
)

// Taking a field out of a live value is a PARTIAL MOVE: the place leaves, the
// container keeps the rest, and the container's drop is narrowed to what
// stayed. This file is the frozen contract for what that means in the type
// checker. What it CANNOT pin is reclamation — a residual drop that frees the
// wrong thing still prints the right answer — so the drop half lives with the
// allocation census and valgrind in the backend suites.

// The spelling is explicit. A plain read is accepted everywhere else in the
// language and leaves the container standing, so nothing in the text would say
// the value had been emptied; `own` is the marker that already means "I am
// taking this".
func TestTakingAFieldWithoutOwnIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "bind_field_of_live_binding",
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn f(o: Outer) -> Inner {
	let e = o.inner;
	return e;
}
`,
		},
		{
			// An ordinary getter. It was a use-after-free plus a double free
			// before this rule existed, and it is the shape stdlib used most.
			name: "by_value_receiver_returns_heap_field",
			src: `
type Foo = { bar: string, n: int }
fn get_bar(self: Foo) -> string {
	return self.bar;
}
`,
		},
		{
			// Building a value from the fields of one that dies immediately.
			name: "struct_literal_from_dying_value",
			src: `
type Head = { method: string, target: string }
type Req = { method: string, target: string }
fn to_req(head: Head) -> Req {
	return Req { method = head.method, target = head.target };
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if !hasCode(semaBag, diag.SemaPartialMoveNeedsOwn) {
				t.Fatalf("expected %v, got %s", diag.SemaPartialMoveNeedsOwn, diagnosticsSummary(semaBag))
			}
			// The snippet must fail for THIS reason and no other, or the row
			// could pass on an unrelated diagnostic and stop testing the rule.
			for _, item := range semaBag.Items() {
				if item.Severity == diag.SevError && item.Code != diag.SemaPartialMoveNeedsOwn {
					t.Fatalf("expected only %v, also got %s",
						diag.SemaPartialMoveNeedsOwn, diagnosticsSummary(semaBag))
				}
			}
		})
	}
}

// The same shapes with the marker written. This is the row that says the rule
// above is a spelling requirement and not a refusal — without it, an
// implementation that rejected every field read would pass every row above.
func TestTakingAFieldWithOwnIsAccepted(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "bind_field_of_live_binding",
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn f(o: Outer) -> Inner {
	let e = own o.inner;
	return e;
}
`,
		},
		{
			name: "by_value_receiver_returns_heap_field",
			src: `
type Foo = { bar: string, n: int }
fn get_bar(self: Foo) -> string {
	return own self.bar;
}
`,
		},
		{
			name: "struct_literal_from_dying_value",
			src: `
type Head = { method: string, target: string }
type Req = { method: string, target: string }
fn to_req(head: Head) -> Req {
	return Req { method = own head.method, target = own head.target };
}
`,
		},
		{
			// To any depth: the path is what moves, not just a top-level field.
			name: "nested_path",
			src: `
type Deep = { a: string, b: string }
type Mid = { deep: Deep, note: string }
type Top = { mid: Mid, label: string }
fn take(s: own string) -> int { return 1; }
fn f(t: Top) -> int {
	return take(own t.mid.deep.a);
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if semaBag.HasErrors() {
				t.Fatalf("a written-out partial move was rejected: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// Rows 2 and 3: what left is gone, and a container missing a piece is no longer
// whole. Row 4 is their positive control and carries most of the weight — an
// implementation that marks the whole binding moved on any field move passes
// rows 2 and 3 for entirely the wrong reason.
func TestReadingAfterAPartialMove(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		rejected bool
	}{
		{
			// Row 2: the place that left.
			name:     "moved_field_itself",
			rejected: true,
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, other: Inner }
fn take(i: own Inner) -> int { return 1; }
fn f(o: Outer) -> int {
	let a = take(own o.inner);
	return take(own o.inner);
}
`,
		},
		{
			// Row 3: the container is no longer whole.
			name:     "whole_container",
			rejected: true,
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, other: Inner }
fn take(i: own Inner) -> int { return 1; }
fn whole(o: own Outer) -> int { return 1; }
fn f(o: Outer) -> int {
	let a = take(own o.inner);
	return whole(own o);
}
`,
		},
		{
			// Row 4: the sibling is untouched. MOVE-ONLY on purpose — a scalar
			// sibling would prove only that primitives survive.
			name:     "move_only_sibling",
			rejected: false,
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, other: Inner }
fn take(i: own Inner) -> int { return 1; }
fn f(o: Outer) -> int {
	let a = take(own o.inner);
	return take(own o.other);
}
`,
		},
		{
			// Row 9: depth does not widen the hole. `t.mid.deep.a` leaving
			// leaves its sibling and every place beside its ancestors readable.
			name:     "nested_sibling_and_uncle",
			rejected: false,
			src: `
type Deep = { a: string, b: string }
type Mid = { deep: Deep, note: string }
type Top = { mid: Mid, label: string }
fn take(s: own string) -> int { return 1; }
fn f(t: Top) -> int {
	let x = take(own t.mid.deep.a);
	let y = take(own t.mid.deep.b);
	let z = take(own t.mid.note);
	return take(own t.label);
}
`,
		},
		{
			// The ancestor of a moved place is not whole either.
			name:     "ancestor_of_moved_place",
			rejected: true,
			src: `
type Deep = { a: string, b: string }
type Mid = { deep: Deep, note: string }
type Top = { mid: Mid, label: string }
fn take(s: own string) -> int { return 1; }
fn takeMid(m: own Mid) -> int { return 1; }
fn f(t: Top) -> int {
	let x = take(own t.mid.deep.a);
	return takeMid(own t.mid);
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			switch {
			case tc.rejected && !semaBag.HasErrors():
				t.Fatalf("reading a place that had gone was accepted")
			case !tc.rejected && semaBag.HasErrors():
				t.Fatalf("reading a place that had NOT gone was rejected: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// A move out of an ARRAY element stays refused, and the reason is the
// remainder: releasing what survives means listing it, and an array's index is
// chosen at runtime over a container whose length is not part of the question.
// The alternative is a runtime drop flag per element, which this language does
// not have.
//
// A TUPLE is deliberately NOT in this group — see the row below it. Sharing the
// refusal with arrays was a mistake that made `let s: string = t.1;` unwritable.
func TestTakingAnArrayElementIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "array_element",
			src: `
type Inner = { tail: string }
fn take(i: own Inner) -> int { return 1; }
fn f(a: Inner[3]) -> int {
	return take(own a[0]);
}
`,
		},
		{
			// A field UNDER an element is refused for the same reason: the
			// container that would have to list its survivors is still the array.
			name: "field_of_array_element",
			src: `
type Inner = { tail: string }
fn take(s: own string) -> int { return 1; }
fn f(a: Inner[3]) -> int {
	return take(own a[0].tail);
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if !hasCode(semaBag, diag.SemaPartialMoveNotEnumerable) {
				t.Fatalf("expected %v, got %s", diag.SemaPartialMoveNotEnumerable, diagnosticsSummary(semaBag))
			}
		})
	}
}

// A TUPLE element can be taken, because a tuple's parts CAN be listed: its
// arity is fixed and it is only ever indexed by a literal, so the survivors of
// one element leaving are as statically known as a struct's fields. Only the
// syntax makes it look like an array.
//
// This row exists because the two were once refused together, which left an
// ordinary `let s: string = t.1;` with no spelling at all — the move was refused
// and the plain read was refused, so a tuple holding anything move-only became
// unreadable.
func TestTakingATupleElementIsAccepted(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "move_only_element",
			src: `
type Inner = { tail: string }
fn take(i: own Inner) -> int { return 1; }
fn f(p: (Inner, Inner)) -> int {
	return take(own p.0);
}
`,
		},
		{
			// The sibling survives, which is the whole point of enumerating.
			name: "sibling_survives",
			src: `
type Inner = { tail: string }
fn take(i: own Inner) -> int { return 1; }
fn f(p: (Inner, Inner)) -> int {
	let a = take(own p.0);
	return take(own p.1);
}
`,
		},
		{
			// A field UNDER a tuple element, and an element under a field: the
			// path mixes the two kinds freely because both are enumerable.
			name: "mixed_path",
			src: `
type Inner = { tail: string }
type Holder = { pair: (Inner, Inner) }
fn take(s: own string) -> int { return 1; }
fn f(h: Holder) -> int {
	return take(own h.pair.0.tail);
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if semaBag.HasErrors() {
				t.Fatalf("taking a tuple element was rejected: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// The negative control beside it: what left a tuple is gone.
func TestReadingATakenTupleElementIsRejected(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Inner = { tail: string }
fn take(i: own Inner) -> int { return 1; }
fn f(p: (Inner, Inner)) -> int {
	let a = take(own p.0);
	return take(own p.0);
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaUseAfterMove) {
		t.Fatalf("a tuple element was still readable after being taken: %s", diagnosticsSummary(semaBag))
	}
}

// Reading an element is untouched, and this is the row that says so. Refusing
// the MOVE must not cost the read: an element read is a Copy read that
// duplicates, and a move-only element read is a borrow and always was.
func TestReadingAnElementStaysLegal(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "copy_element_read",
			src: `
fn f(a: int[3]) -> int {
	return a[1];
}
`,
		},
		{
			name: "borrowed_element_read",
			src: `
type Inner = { tail: string }
fn peek(i: &Inner) -> int { return 1; }
fn f(a: Inner[3]) -> int {
	return peek(&a[0]);
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if semaBag.HasErrors() {
				t.Fatalf("an ordinary element read was rejected: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// Rows 10 and 12, plus the whole-binding case: reads that take nothing out of
// the container are unaffected. Each of these would be broken by a rule keyed
// on "is this a projection" rather than "does this take ownership".
func TestReadsThatTakeNothingStayLegal(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// Row 12: a borrow reads a field without taking it.
			name: "borrowed_field_read",
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn peek(i: &Inner) -> int { return 1; }
fn f(o: Outer) -> int {
	return peek(&o.inner);
}
`,
		},
		{
			// Row 10: a Copy composite duplicates, so the container stays whole
			// and is still readable beside the copy.
			name: "copy_composite_field_read",
			src: `
@copy
type CInner = { x: int }
type COuter = { c: CInner, label: int }
fn f(co: COuter) -> int {
	let e = co.c;
	return e.x + co.c.x;
}
`,
		},
		{
			name: "scalar_field_read",
			src: `
type Foo = { bar: string, n: int }
fn f(x: Foo) -> int {
	let n = x.n;
	return n;
}
`,
		},
		{
			name: "whole_binding_move",
			src: `
type Inner = { tail: string }
fn take(i: Inner) -> int { return 1; }
fn f(i: Inner) -> int {
	return take(i);
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if semaBag.HasErrors() {
				t.Fatalf("a read that takes nothing was rejected: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// Taking a field out of a TEMPORARY is accepted, and no marker is required.
//
// `own` exists so a reader can see that a live container was emptied and that
// reading it afterwards will fail. A temporary has no name, so there is nothing
// left to read and no later line that could be surprised — the marker would be
// telling the reader about a value they can never mention again.
//
// What makes this safe is that the temporary's statement-end release is
// narrowed to its remainder, exactly as a binding's exit drop is. Reclamation
// is pinned by the census, not here: this row would pass just as well for an
// implementation that accepted the program and freed the field twice.
func TestTakingAFieldOutOfATemporaryIsAccepted(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "bare",
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn mk() -> Outer { return Outer { inner = Inner { tail = "t" }, label = 7 }; }
fn f() -> Inner {
	let e = mk().inner;
	return e;
}
`,
		},
		{
			// Written out is accepted too: the marker is not wrong here, only
			// unnecessary.
			name: "with_own",
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn mk() -> Outer { return Outer { inner = Inner { tail = "t" }, label = 7 }; }
fn f() -> Inner {
	let e = own mk().inner;
	return e;
}
`,
		},
		{
			// Depth is no different for a temporary than for a binding.
			name: "nested_path",
			src: `
type Deep = { a: string }
type Mid = { deep: Deep, note: string }
type Top = { mid: Mid, label: string }
fn mk() -> Top { return Top { mid = Mid { deep = Deep { a = "x" }, note = "n" }, label = "l" }; }
fn f() -> string {
	let e = mk().mid.deep.a;
	return e;
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if semaBag.HasErrors() {
				t.Fatalf("taking a field out of a temporary was rejected: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// An ELEMENT of a temporary is refused for the reason every element move is:
// the remainder cannot be listed. The base being nameless changes nothing about
// that.
func TestTakingAnElementOutOfATemporaryIsRefused(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Inner = { tail: string }
fn mk() -> Inner[3] { return [Inner { tail = "a" }, Inner { tail = "b" }, Inner { tail = "c" }]; }
fn take(i: own Inner) -> int { return 1; }
fn f() -> int {
	return take(own mk()[0]);
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if !hasCode(semaBag, diag.SemaPartialMoveNotEnumerable) {
		t.Fatalf("expected %v, got %s", diag.SemaPartialMoveNotEnumerable, diagnosticsSummary(semaBag))
	}
}

// The controls, and they carry the weight: refusing every read off a temporary
// would pass the row above for the wrong reason and break ordinary code.
func TestReadsOffATemporaryThatTakeNothingStayLegal(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "copy_field_of_temporary",
			src: `
type Outer = { inner: int, label: int }
fn mk() -> Outer { return Outer { inner = 1, label = 7 }; }
fn f() -> int {
	let e = mk().label;
	return e;
}
`,
		},
		{
			// A Copy COMPOSITE off a temporary duplicates, so the statement-end
			// release takes nothing the reader is holding. A rule keyed on "is
			// this a composite" rather than "does this take ownership" would
			// reject it.
			//
			// Borrowing a field off a temporary — `peek(&mk().inner)` — is NOT a
			// control here: it is rejected as non-addressable, which predates
			// this rule and is a separate question.
			name: "copy_composite_field_of_temporary",
			src: `
@copy
type CInner = { x: int }
type COuter = { c: CInner, label: int }
fn mk() -> COuter { return COuter { c = CInner { x = 1 }, label = 7 }; }
fn f() -> int {
	let e = mk().c;
	return e.x;
}
`,
		},
		{
			name: "whole_temporary_moved",
			src: `
type Inner = { tail: string }
fn mk() -> Inner { return Inner { tail = "t" }; }
fn take(i: Inner) -> int { return 1; }
fn f() -> int {
	return take(mk());
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if semaBag.HasErrors() {
				t.Fatalf("a legal read off a temporary was rejected: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// The refusal carries a FIX, and this is the one diagnostic in the family where a
// compiler can offer one without guessing. It is reached only after the path is
// known enumerable, the place is known to be still present, and the read is known
// to be a projection of a named binding at a resolved move-only type — so `own` in
// front of the expression is exactly the form that would have been accepted, which
// is why it is marked always-safe and swept by `surge fix --all`.
//
// A newcomer from a language without this rule meets an error whose headline says
// what to write and whose fix writes it; the notes explain the consequence and the
// alternative, and an editor surfaces those through the language server.
func TestTakingAFieldWithoutOwnOffersTheOwnMarkerAsAFix(t *testing.T) {
	_, semaBag := runSemaOnSnippet(t, `
type Head = { method: string, target: string }
type Req = { method: string, target: string }
fn to_req(head: Head) -> Req {
	return Req { method = head.method, target = head.target };
}
`)
	var offered int
	for _, item := range semaBag.Items() {
		if item.Code != diag.SemaPartialMoveNeedsOwn {
			continue
		}
		for _, f := range item.Fixes {
			if len(f.Edits) != 1 || f.Edits[0].NewText != "own " {
				t.Fatalf("fix does not insert the marker: %+v", f.Edits)
			}
			// Always-safe is what lets `--all` sweep a file clean in one pass; a
			// heuristic applicability would leave every site to be applied by
			// hand, which for a rule this common is the difference between a
			// friendly surface and a chore.
			if f.Applicability != diag.FixApplicabilityAlwaysSafe {
				t.Fatalf("fix applicability is %v, want always-safe", f.Applicability)
			}
			if !f.IsPreferred {
				t.Fatal("fix is not marked preferred, so an editor has no reason to lead with it")
			}
			offered++
		}
	}
	// BOTH reads, not just the first: a file with several of these should come
	// out clean in one `surge fix --all`, and a fix attached to only the first
	// diagnostic would still pass a weaker assertion.
	if offered != 2 {
		t.Fatalf("expected a fix on each of the two reads, got %d", offered)
	}
}

// A discarded read takes nothing. `let _ = x.bar` names nobody to receive
// the field, so the field stays with `x` -- the same rule that releases a
// discarded PRODUCED value at its statement's end leaves a discarded PLACE
// where it is -- and there is no take for `own` to spell. The row used to
// sit among the refused ones, from when `_` bound (and then leaked) whatever
// it was handed.
func TestDiscardedFieldReadTakesNothing(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Foo = { bar: string }
fn f(x: Foo) -> nothing {
	let _ = x.bar;
	return nothing;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if hasCode(semaBag, diag.SemaPartialMoveNeedsOwn) {
		t.Fatalf("a discarded field read must not be a take: got %s", diagnosticsSummary(semaBag))
	}
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
}
