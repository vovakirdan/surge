package sema

import (
	"context"
	"testing"

	"surge/internal/diag"
	"surge/internal/types"
)

// The ownership axes were introduced as NAMES for questions `IsCopy` used to
// answer alone, and OwnsHeap has since parted from it in two places, both
// pinned here. The reference-counted scalars are Copy at the surface and a
// counted block underneath. The value composites live inline and own exactly
// what their members own, so a `@copy` pair of ints is bits and a plain pair
// holding a string is not. This walks every type the snippet's interner holds
// and pins the invariants:
//
//	OwnsHeap(T) == true                       for a reference-counted scalar
//	OwnsHeap(T) == false                      for a borrow, which owns nothing
//	OwnsHeap(T) == ContainsRefCountedScalar   for a Copy struct, tuple or fixed
//	                                          array: its members are all Copy,
//	                                          so a counted scalar is the only
//	                                          thing it can own
//	OwnsHeap(T) == !IsCopy(T)                 everywhere but the composites
//
// The composite rows this cannot state without restating the walk — a
// move-only struct, a union, a nesting — are pinned by name in
// TestOwnsHeapFollowsTheMembers. And every type, composite or not, has to
// answer the same through the interner-only leg (`OwnsHeapIn`), which HIR
// normalization asks: one axis, one answer.
//
// `float` is the only reference-counted scalar today. When `int` and `uint`
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

@copy
type CopyCounted = { x: float, y: uint };

type Owning = { name: string };

type Wrapper = { inner: Owning, label: int };

fn probe(r: &int, m: &mut int, s: string, p: Plain, c: CopyPair, cc: CopyCounted, o: Owning, w: Wrapper, f: float, arr: int[], fixed: float[2]) -> int {
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
	in := res.TypeInterner

	checked := 0
	for id := types.TypeID(1); ; id++ {
		tt, ok := in.Lookup(id)
		if !ok {
			break
		}
		checked++

		copyable := res.IsCopyType(id)
		// A reference-counted scalar is Copy but ships a pointer to a block
		// with a non-atomic count, so it is not raw-bits transportable until
		// the boundary installs a deep copy — and neither is a Copy composite
		// HOLDING one, because the crossing copy retains the field rather than
		// deep-copying it (`CopyCounted` is that row).
		//
		// Any other value composite rides: each crossing route gives the far
		// side an owner — a capture is duplicated at its operand, a channel
		// element at the send, and a RESULT is a transfer with one owner at a
		// time and needs no copy. This axis only says whether the bits may
		// travel.
		wantBits := copyable && !res.ContainsRefCountedScalar(id)
		if got := res.TriviallyTransportableBits(id); got != wantBits {
			t.Errorf("type %d (%v): TriviallyTransportableBits=%v, want %v",
				id, tt.Kind, got, wantBits)
		}

		got := res.OwnsHeap(id)
		if leg := OwnsHeapIn(in, id); leg != got {
			t.Errorf("type %d (%v): OwnsHeap=%v but the interner-only leg says %v — one axis, one answer",
				id, tt.Kind, got, leg)
		}

		want := !copyable
		switch {
		case in.IsRefCountedScalar(id):
			// Copy at the surface, heap-owning underneath: `let b = a` leaves
			// both usable, and the block still has to be reclaimed.
			want = true
			if !copyable {
				t.Errorf("type %d (%v): a reference-counted scalar must stay Copy", id, tt.Kind)
			}
		case tt.Kind == types.KindReference || tt.Kind == types.KindPointer:
			// A borrow names storage it does not own. `&mut T` is the shape
			// that makes this its own clause rather than a restatement of
			// IsCopy: it is NOT Copy, yet dropping it would free a value the
			// holder never owned.
			want = false
		case in.IsValueComposite(id):
			if !copyable || tt.Kind == types.KindUnion {
				// Pinned by name below: a move-only composite's answer IS the
				// walk, and ContainsRefCountedScalar does not enter unions.
				continue
			}
			// A Copy composite holds only Copy members, and among those only
			// a reference-counted scalar owns anything — so the crossing
			// question and the drop question coincide for it, one level down.
			want = res.ContainsRefCountedScalar(id)
		}
		if got != want {
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

// The two legs above were compared over a snippet whose interner held no
// runtime handle and no generic `@copy` instantiation — exactly the types
// where the two Copy authorities behind them are computed differently and
// could part: a handle's answer IS `!isCopy` (a composite's never consults
// its own Copy bit), and an instantiation is marked Copy by a separate step
// (`type_decl_instantiate.go` → `MarkCopyType`) from the declaration's
// attribute. So the agreement is asked again here over a `core` module
// snippet, which is what makes `@intrinsic type Task<T>` a runtime handle
// (`isRuntimeHandleTypeDecl`), with the answer each row must give:
//
//   - `Task<int>`, `Task<string>`: a handle, not Copy, owns the task — true;
//   - `Channel<int>`: a handle declared `@copy`, so the handle leg answers
//     `!isCopy` = false today. The storage model has the copy RETAIN and the
//     drop release (RUNTIME_V2 §7), which is D3b C1's flip: the row records
//     the seam, and C1 changes it to true in the same change that emits the
//     drop;
//   - `Opt<int>` / `Opt<float>`: a generic `@copy` union, instantiated, owns
//     what its payload owns — false, then true for the counted scalar;
//   - `Pair<int>`: a generic `@copy` tuple alias. The Copy authorities DO
//     part here — `in.IsCopy(Pair<int>)` is true (the instantiation marks
//     the alias id), `Result.IsCopyType(Pair<int>)` is false (it resolves the
//     alias to `(int, int)` first, and the tuple carries no mark) — and the
//     axis must still answer alike, because a composite's answer never comes
//     from its own Copy bit. The parting itself is not pinned: it is a defect
//     of the Copy leg, recorded under RV2-DEBT-260's residue, not a contract.
func TestOwnsHeapLegsAgreeOverHandlesAndInstantiations(t *testing.T) {
	src := `
@intrinsic
type Task<T> = { __opaque: int };
@copy
@intrinsic
type Channel<T> = { __opaque: int };
tag SomeC<T>(T);
@copy type Opt<T> = SomeC(T) | nothing;
@copy type Pair<T> = (T, T);

fn probe(t: Task<int>, ts: Task<string>, c: Channel<int>, oi: Opt<int>, of: Opt<float>, p: Pair<int>) -> int {
    return 0;
}
`
	res := coreSnippetResult(t, src)
	in := res.TypeInterner

	rows := map[string]bool{
		"Task<int>":    true,
		"Task<string>": true,
		"Channel<int>": false,
		"Opt<int>":     false,
		"Opt<float>":   true,
		"Pair<int>":    false,
	}
	seen := make(map[string]bool, len(rows))
	sawHandle := false
	for id := types.TypeID(1); ; id++ {
		tt, ok := in.Lookup(id)
		if !ok {
			break
		}
		got := res.OwnsHeap(id)
		if leg := OwnsHeapIn(in, id); leg != got {
			t.Errorf("type %d (%v, %s): OwnsHeap=%v but the interner-only leg says %v — one axis, one answer",
				id, tt.Kind, types.Label(in, id), got, leg)
		}
		if in.IsRuntimeHandleType(id) {
			sawHandle = true
		}
		label := types.Label(in, id)
		want, ok := rows[label]
		if !ok {
			continue
		}
		seen[label] = true
		if got != want {
			t.Errorf("%s: OwnsHeap=%v, want %v", label, got, want)
		}
	}
	for label := range rows {
		if !seen[label] {
			t.Errorf("%s: the snippet never produced this type, so its row pinned nothing", label)
		}
	}
	// The walk must have crossed a real handle, or the agreement above was
	// proven where it was already known: `@intrinsic` outside `core` is a
	// plain struct.
	if !sawHandle {
		t.Errorf("interner holds no runtime handle — the snippet did not reach the handle leg")
	}
}

// coreSnippetResult checks a snippet as module `core`, where `@intrinsic`
// declarations are the runtime's own and `Task`/`Channel` become handles.
func coreSnippetResult(t *testing.T, src string) *Result {
	t.Helper()
	builder, fileID, parseBag := parseSource(t, src)
	if parseBag.Len() != 0 {
		t.Fatalf("snippet does not parse: %s", diagnosticsSummary(parseBag))
	}
	symRes := resolveSymbols(t, builder, fileID)
	semaBag := diag.NewBag(64)
	res := Check(context.Background(), builder, fileID, Options{
		Reporter:   &diag.BagReporter{Bag: semaBag},
		Symbols:    symRes,
		ModulePath: builder.StringsInterner.Intern("core"),
	})
	if semaBag.HasErrors() {
		t.Fatalf("unexpected sema diagnostics: %s", diagnosticsSummary(semaBag))
	}
	if res.TypeInterner == nil {
		t.Fatalf("expected a sema result")
	}
	return &res
}

// The composite half of the axis, pinned as SOURCE SHAPES with the answer the
// storage model gives each: a value composite owns heap iff one of its members
// does — a struct field, a tuple element, a union's tag payload, a fixed
// array's element — recursively, through nesting. Copy says nothing
// here (`Plain` and `Pair` answer alike), and the reference-counted scalar is
// the one Copy member that owns (`Pf`, `Mixed`, `[float; 2]`, `(bool, float)`).
//
// Every row is asked of both legs, the Result's and the interner-only one.
func TestOwnsHeapFollowsTheMembers(t *testing.T) {
	src := `
@copy type Pair = { a: int, b: int };
@copy type Pf = { a: float, b: float };
@copy type Mixed = { flag: bool, f: float };
type Plain = { a: int, b: int };
type Tagged = { s: string, n: int };
@copy type Inner = { x: int };
@copy type Outer = { inner: Inner, label: int };
type Deep = { outer: Outer, text: string };
type Boxed = { items: int[] };
type Fixed = { cells: float[2] };

tag Hold(Pair);
tag HoldF(Pf);
tag HoldS(Tagged);
tag Nothing_();
type HeldPair = Hold(Pair) | Nothing_;
type HeldPf = HoldF(Pf) | Nothing_;
type HeldTagged = HoldS(Tagged) | Nothing_;

fn probe(p: Pair, pf: Pf, m: Mixed, pl: Plain, tg: Tagged, o: Outer, d: Deep, bx: Boxed, fx: Fixed, hp: HeldPair, hf: HeldPf, ht: HeldTagged, ai: int[3], af: float[2], strs: string[2], ti: (int, int), ts: (int, string), tf: (bool, float)) -> int {
    return 0;
}
`
	parseBag, semaBag, res := runSemaOnSnippetResult(t, src)
	requireNoSemaErrors(t, parseBag, semaBag)
	if res == nil || res.TypeInterner == nil {
		t.Fatalf("expected a sema result")
	}
	in := res.TypeInterner

	rows := map[string]bool{
		"Pair":          false,
		"Pf":            true,
		"Mixed":         true,
		"Plain":         false,
		"Tagged":        true,
		"Outer":         false,
		"Deep":          true,
		"Boxed":         true,
		"Fixed":         true,
		"HeldPair":      false,
		"HeldPf":        true,
		"HeldTagged":    true,
		"(int, int)":    false,
		"(int, string)": true,
		"(bool, float)": true,
		// A fixed array is the nominal `ArrayFixed<T, const N, N>` to the
		// interner, and this is how its label spells it.
		"ArrayFixed<int, const 3, 3>":    false,
		"ArrayFixed<float, const 2, 2>":  true,
		"ArrayFixed<string, const 2, 2>": true,
	}
	seen := make(map[string]bool, len(rows))
	for id := types.TypeID(1); ; id++ {
		if _, ok := in.Lookup(id); !ok {
			break
		}
		label := types.Label(in, id)
		want, ok := rows[label]
		if !ok {
			continue
		}
		seen[label] = true
		if got := res.OwnsHeap(id); got != want {
			t.Errorf("%s: OwnsHeap=%v, want %v", label, got, want)
		}
		if got := OwnsHeapIn(in, id); got != want {
			t.Errorf("%s: the interner-only leg says OwnsHeap=%v, want %v", label, got, want)
		}
	}
	for label := range rows {
		if !seen[label] {
			t.Errorf("%s: the snippet never produced this type, so its row pinned nothing", label)
		}
	}
}
