package sema

import (
	"testing"

	"surge/internal/types"
)

// The ownership axes were introduced as NAMES for questions `IsCopy` used to
// answer alone. They still track it everywhere EXCEPT the reference-counted
// scalars, which are the whole reason the axes are separate: those are Copy at
// the surface and heap-owning underneath. This walks every type the snippet's
// interner holds and pins the invariants:
//
//	OwnsHeap(T) == true                 for a reference-counted scalar
//	OwnsHeap(T) == true                 for a value composite, Copy or not
//	OwnsHeap(T) == false                for a borrow, which owns nothing
//	OwnsHeap(T) == !IsCopy(T)           everywhere else
//
// `float` is the only type in the first row today. When `int` and `uint`
// follow, they join it — and NOTHING ELSE may move. Another shape breaking
// this means the widening reached a type it was not meant to.
//
// TriviallyTransportableBits tracks `IsCopy` minus the reference-counted
// scalars, and ONLY those: a composite was excluded too while no crossing route
// gave the far side an owner, and rejoined once they did. What keeps the
// scalars out is that the crossing copy RETAINS such a field rather than
// deep-copying it, and the count is not atomic.
func TestOwnershipAxesAgreeWithCopyToday(t *testing.T) {
	src := `
type Plain = { a: int, b: int };

@copy
type CopyPair = { x: uint, y: uint };

type Owning = { name: string };

type Wrapper = { inner: Owning, label: int };

fn probe(r: &int, m: &mut int, s: string, p: Plain, c: CopyPair, o: Owning, w: Wrapper, f: float, arr: int[]) -> int {
    let local: string = s;
    let n: uint = 1;
    return 0;
}
`
	parseBag, semaBag, res := runSemaOnSnippetResult(t, src)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil || res.TypeInterner == nil {
		t.Fatalf("expected a sema result")
	}

	checked := 0
	for id := types.TypeID(1); ; id++ {
		tt, ok := res.TypeInterner.Lookup(id)
		if !ok {
			break
		}
		checked++

		copyable := res.IsCopyType(id)
		// A reference-counted scalar is Copy but ships a pointer to a block
		// with a non-atomic count, so it is not raw-bits transportable until
		// the boundary installs a deep copy.
		//
		// A value composite rides again: its bits are still a box pointer, but
		// each crossing route now gives the far side an owner — a capture is
		// duplicated at its operand, a channel element at the send, and a
		// RESULT is a transfer with one owner at a time and needs no copy.
		// This axis only says whether the bits may travel.
		wantBits := copyable && !res.TypeInterner.IsRefCountedScalar(id)
		if got := res.TriviallyTransportableBits(id); got != wantBits {
			t.Errorf("type %d (%v): TriviallyTransportableBits=%v, want %v",
				id, tt.Kind, got, wantBits)
		}

		want := !copyable
		switch {
		case res.TypeInterner.IsRefCountedScalar(id):
			// Copy at the surface, heap-owning underneath: `let b = a` leaves
			// both usable, and the block still has to be reclaimed.
			want = true
			if !copyable {
				t.Errorf("type %d (%v): a reference-counted scalar must stay Copy", id, tt.Kind)
			}
		case res.TypeInterner.IsValueComposite(id):
			// The second family in that row, and the one that made the axes
			// worth splitting twice: a struct, tuple, union or fixed array is a
			// value the language stores in a heap box. Whether it is Copy says
			// nothing about who frees the box — every holder does, which is
			// why a Copy composite is droppable and a Copy scalar is not.
			want = true
		case tt.Kind == types.KindReference || tt.Kind == types.KindPointer:
			// A borrow names storage it does not own. `&mut T` is the shape
			// that makes this its own clause rather than a restatement of
			// IsCopy: it is NOT Copy, yet dropping it would free a value the
			// holder never owned.
			want = false
		}
		if got := res.OwnsHeap(id); got != want {
			t.Errorf("type %d (%v): OwnsHeap=%v, want %v (IsCopy=%v)",
				id, tt.Kind, got, want, copyable)
		}
	}

	// Guard against the walk silently covering nothing if the snippet stops
	// type-checking: the builtins alone are more than a handful.
	if checked < 10 {
		t.Fatalf("expected the interner to hold the snippet's types, walked only %d", checked)
	}
}

// The interner must actually contain the shapes the invariant above cares
// about, or the walk proves nothing. This asserts the snippet produced at
// least one non-Copy borrow, one Copy borrow, and one heap-owning composite.
func TestOwnershipAxesSnippetCoversTheInterestingShapes(t *testing.T) {
	src := `
type Owning = { name: string };

fn probe(r: &int, m: &mut int, o: Owning) -> int {
    return 0;
}
`
	parseBag, semaBag, res := runSemaOnSnippetResult(t, src)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil || res.TypeInterner == nil {
		t.Fatalf("expected a sema result")
	}

	var sawCopyBorrow, sawNonCopyBorrow, sawOwningComposite, sawRefCountedScalar bool
	for id := types.TypeID(1); ; id++ {
		tt, ok := res.TypeInterner.Lookup(id)
		if !ok {
			break
		}
		switch {
		case res.TypeInterner.IsRefCountedScalar(id):
			sawRefCountedScalar = true
		case tt.Kind == types.KindReference && res.IsCopyType(id):
			sawCopyBorrow = true
		case tt.Kind == types.KindReference && !res.IsCopyType(id):
			sawNonCopyBorrow = true
		case tt.Kind == types.KindStruct && res.OwnsHeap(id):
			sawOwningComposite = true
		}
	}
	if !sawRefCountedScalar {
		t.Errorf("interner holds no reference-counted scalar — the shape the axes exist for")
	}
	if !sawCopyBorrow {
		t.Errorf("snippet produced no Copy borrow (&T)")
	}
	if !sawNonCopyBorrow {
		t.Errorf("snippet produced no non-Copy borrow (&mut T) — the shape that makes OwnsHeap more than !IsCopy")
	}
	if !sawOwningComposite {
		t.Errorf("snippet produced no heap-owning struct")
	}
}
