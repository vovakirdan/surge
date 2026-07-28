package sema

import (
	"testing"

	"surge/internal/diag"
)

// Taking a projection out of a live binding is a PARTIAL MOVE. Moves are
// tracked per binding rather than per place, so sema can neither invalidate
// just the place that left nor leave the container holding only what remains —
// the read produces a second NAME for the field instead of taking it.
//
// This gate is SCAFFOLDING for Epic 24 and step 8 removes it. It exists so the
// place-keyed moved-set (step 2) and the path-carrying drop obligations (step
// 6) can land in separate steps: in between, a place-aware moved-set with
// binding-granular drops would release a field that had already moved, and the
// rejection keeps that window unreachable from user code.
//
// The shapes below are not hypothetical. Measured on the native backend before
// the gate: the by-value receiver returning a heap field, and the struct
// literal built from a dying value, each produce invalid reads AND an invalid
// free while printing the right answer.
func TestPartialMoveIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// The canonical shape: bind a move-only field of a live value.
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
			// The explicit spelling is refused too, and deliberately: it is the
			// DESTINATION, and letting it through before the tracking exists
			// would ship exactly the unsound state this gate prevents.
			name: "explicit_own_projection",
			src: `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn take(i: own Inner) -> int { return 1; }
fn f(o: Outer) -> int {
	return take(own o.inner);
}
`,
		},
		{
			// A by-value receiver handing out a heap field. This is an ordinary
			// getter, and it is a use-after-free plus a double free today.
			name: "by_value_receiver_returns_heap_field",
			src: `
type Foo = { bar: string, n: int }
fn get_bar(self: Foo) -> string {
	return self.bar;
}
`,
		},
		{
			// Building a value from the fields of one that dies immediately —
			// the shape the HTTP request parser used on every request.
			name: "struct_literal_from_dying_value",
			src: `
type Head = { method: string, target: string }
type Req = { method: string, target: string }
fn to_req(head: Head) -> Req {
	return Req { method = head.method, target = head.target };
}
`,
		},
		{
			// A discarded field read moves just as much as a bound one.
			name: "discarded_field_read",
			src: `
type Foo = { bar: string }
fn f(x: Foo) -> nothing {
	let _ = x.bar;
	return nothing;
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
			if !hasCode(semaBag, diag.SemaPartialMoveUnsupported) {
				t.Fatalf("expected %v, got %s", diag.SemaPartialMoveUnsupported, diagnosticsSummary(semaBag))
			}
			// The snippet must fail for THIS reason and no other, or the row
			// could pass on an unrelated diagnostic and stop testing the gate.
			for _, item := range semaBag.Items() {
				if item.Severity == diag.SevError && item.Code != diag.SemaPartialMoveUnsupported {
					t.Fatalf("expected only %v, also got %s",
						diag.SemaPartialMoveUnsupported, diagnosticsSummary(semaBag))
				}
			}
		})
	}
}

// The must-still-be-accepted side, and it carries most of the weight here: an
// implementation that rejected EVERY field read would pass every row above for
// the wrong reason. Each of these is a field read that is not a move.
func TestPartialMoveGateKeepsNonMovesLegal(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// A borrow of a field reads it without taking it.
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
			// A Copy field DUPLICATES, so the container keeps its value and
			// nothing is taken out of it.
			name: "copy_field_read",
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
			// A scalar field is Copy for the same reason.
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
			// Moving the WHOLE binding is not a partial move and never was.
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
				t.Fatalf("a shape that is not a partial move was rejected: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}

// DEBT SENTINEL, not a contract: a field read whose BASE is a temporary is not
// gated, because `resolvePlace` does not resolve a call as a place. The epic
// wants that shape to stay legal — nobody else can be using the temporary — but
// it does not merely alias today, it CRASHES: `let e = mk().inner;` segfaults on
// the native backend, with invalid reads and invalid frees under valgrind,
// because the temporary's drop takes the field the binding now holds.
//
// So "stays accepted" currently means "accepts a use-after-free". Delete this
// test when RV2-DEBT-084 is fixed and assert the real behaviour instead.
func TestPartialMoveOutOfTemporaryIsNotGated(t *testing.T) {
	parseBag, semaBag := runSemaOnSnippet(t, `
type Inner = { tail: string }
type Outer = { inner: Inner, label: int }
fn mk() -> Outer { return Outer { inner = Inner { tail = "t" }, label = 7 }; }
fn f() -> Inner {
	let e = mk().inner;
	return e;
}
`)
	if parseBag.HasErrors() {
		t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
	}
	if hasCode(semaBag, diag.SemaPartialMoveUnsupported) {
		t.Fatalf("a temporary-based field read is now gated; RV2-DEBT-084 may be fixed, " +
			"in which case delete this sentinel rather than adjusting it")
	}
}
