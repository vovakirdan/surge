package sema

import (
	"testing"

	"surge/internal/types"
)

// The two ownership axes are introduced as NAMES for questions `IsCopy` used
// to answer alone, so today they must reproduce its answers exactly. This
// walks every type the snippet's interner holds and pins both invariants:
//
//	TriviallyTransportableBits(T) == IsCopy(T)
//	OwnsHeap(T)                   == !IsCopy(T), except borrows, which own nothing
//
// When the arbitrary-precision scalars become reclaimable, `int`, `uint` and
// `float` are what breaks the OwnsHeap row — deliberately. Anything ELSE
// breaking means the widening reached a type shape it was not meant to.
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
		if got := res.TriviallyTransportableBits(id); got != copyable {
			t.Errorf("type %d (%v): TriviallyTransportableBits=%v, want %v",
				id, tt.Kind, got, copyable)
		}

		want := !copyable
		if tt.Kind == types.KindReference || tt.Kind == types.KindPointer {
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

	var sawCopyBorrow, sawNonCopyBorrow, sawOwningComposite bool
	for id := types.TypeID(1); ; id++ {
		tt, ok := res.TypeInterner.Lookup(id)
		if !ok {
			break
		}
		switch {
		case tt.Kind == types.KindReference && res.IsCopyType(id):
			sawCopyBorrow = true
		case tt.Kind == types.KindReference && !res.IsCopyType(id):
			sawNonCopyBorrow = true
		case tt.Kind == types.KindStruct && res.OwnsHeap(id):
			sawOwningComposite = true
		}
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
