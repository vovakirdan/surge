package sema

import (
	"testing"

	"surge/internal/diag"
)

// A shared reference lends read access, so nothing reached through one may be
// written. This file is the frozen contract for that rule.
//
// It is a compile-time contract on purpose. The two backends cannot be made to
// agree on this at run time: the VM tags every location with a mutability bit
// and traps on the store, while a native reference is a bare pointer with
// nowhere to keep such a bit, so the identical store lands in the referent and
// the program carries on with the wrong answer. Both then exited 1 — one by
// trapping, one by computing a different result — which is why an exit-code
// comparison alone never noticed. Refusing in sema is what makes the two
// backends the same language, and it makes the runtime trap unreachable from
// well-typed source rather than redundant: it stays as defence in depth.

// Every way of reaching the referent is the same write.
func TestWritingThroughASharedReferenceIsRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// The plainest shape, and the one that needs no container at all:
			// a shared parameter written through by an implicit deref.
			name: "shared_parameter_implicit_deref",
			src: `
@copy type Leaf = { x: int, y: int }
fn poke(r: &Leaf) -> int {
	r.x = 99;
	return r.x;
}
`,
		},
		{
			// Spelling the deref out changes nothing.
			name: "shared_parameter_explicit_deref",
			src: `
@copy type Leaf = { x: int, y: int }
fn poke(r: &Leaf) -> int {
	(*r).x = 99;
	return r.x;
}
`,
		},
		{
			// Replacing the whole referent rather than one field.
			name: "whole_referent",
			src: `
@copy type Leaf = { x: int, y: int }
fn poke(r: &Leaf) -> int {
	*r = Leaf { x = 1, y = 2 };
	return r.x;
}
`,
		},
		{
			// Depth does not launder it. A rule that only inspected the
			// outermost projection would let this one through.
			name: "nested_field_through_shared_base",
			src: `
@copy type Inner = { x: int }
type Outer = { inner: Inner }
fn poke(r: &Outer) -> int {
	r.inner.x = 1;
	return r.inner.x;
}
`,
		},
		{
			// A reborrow of a shared reference is still shared, and reborrowing
			// is the usual way a check that only looked at parameters gets
			// bypassed.
			name: "shared_reborrow",
			src: `
@copy type Leaf = { x: int, y: int }
fn poke(r: &Leaf) -> int {
	let s = &*r;
	s.x = 99;
	return s.x;
}
`,
		},
		// The route that started this — an indexed read binding the element
		// carrier, so the binding is a shared reference with no `&` anywhere in
		// the source — is NOT a row here. It cannot be: this harness checks one
		// file with no stdlib, and without stdlib there is no `__index` to
		// select, so a fixed-array read yields the element VALUE and the shape
		// does not exist to be refused. It is proved end to end instead, where
		// a real `__index` is in scope.
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, tc.src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if !hasCode(semaBag, diag.SemaStoreThroughSharedRef) {
				t.Fatalf("expected %v, got %s", diag.SemaStoreThroughSharedRef, diagnosticsSummary(semaBag))
			}
		})
	}
}

// The control, and it carries most of the weight: an implementation that
// refused every write through every reference would pass every row above.
func TestWritingThroughAnExclusiveReferenceIsAccepted(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			// The whole point of an exclusive borrow.
			name: "exclusive_parameter",
			src: `
@copy type Leaf = { x: int, y: int }
fn poke(r: &mut Leaf) -> int {
	r.x = 99;
	return r.x;
}
`,
		},
		{
			name: "exclusive_parameter_explicit_deref",
			src: `
@copy type Leaf = { x: int, y: int }
fn poke(r: &mut Leaf) -> int {
	(*r).x = 99;
	return r.x;
}
`,
		},
		{
			name: "exclusive_nested_field",
			src: `
@copy type Inner = { x: int }
type Outer = { inner: Inner }
fn poke(r: &mut Outer) -> int {
	r.inner.x = 1;
	return r.inner.x;
}
`,
		},
		{
			// Assigning to the binding itself REPOINTS the reference; it does
			// not write through it. The rule keys on whether a projection
			// reaches the referent, which is what separates these two.
			name: "repointing_a_shared_reference",
			src: `
@copy type Leaf = { x: int, y: int }
fn pick(a: &Leaf, b: &Leaf, first: bool) -> int {
	let mut r = a;
	if !first { r = b; }
	return r.x;
}
`,
		},
		{
			// Reading through a shared reference is what it is for.
			name: "reading_through_a_shared_reference",
			src: `
@copy type Inner = { x: int }
type Outer = { inner: Inner }
fn peek(r: &Outer) -> int {
	return r.inner.x;
}
`,
		},
		{
			// An owned local is not a reference, however deep the write goes.
			name: "owned_local",
			src: `
@copy type Inner = { x: int }
type Outer = { inner: Inner }
fn build() -> int {
	let mut o = Outer { inner = Inner { x = 1 } };
	o.inner.x = 2;
	return o.inner.x;
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
			if hasCode(semaBag, diag.SemaStoreThroughSharedRef) {
				t.Fatalf("a legal write was refused: %s", diagnosticsSummary(semaBag))
			}
		})
	}
}
