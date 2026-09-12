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
// TriviallyTransportableBits tracks `IsCopy` minus what the relinquishing
// walk cannot make private (CountedBlockStaysShared), and ONLY that: a
// composite was excluded too while no crossing route gave the far side an
// owner, and rejoined once they did; the reference-counted scalars were
// excluded while the crossing copy RETAINED such a field rather than
// deep-copying it, and rejoined once every crossing began to un-share its
// relinquishing operand. What CountedBlockStaysShared keeps out of this
// Copy-only axis is a Copy handle whose ring no walk reaches —
// `Channel<float>`. A map and a dynamic array never reach the axis at all:
// neither is Copy. The map's table is refused by the same predicate wherever
// a crossing asks it; the array's buffer is walked by the runtime where an
// owned MOVE relinquishes it — a capture, a channel element, a `blocking`
// body's `ret`, which crosses no shard — never on a crossing reply, which
// takes only plain-copy data.
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
		// A reference-counted scalar is Copy and ships a pointer to a block
		// with a non-atomic count — and it rides, because the producer of a
		// crossing result un-shares the value in its relinquishing operand
		// before the reply names it (`float` and `CopyCounted` are those rows).
		// What does not ride is what no walk can make private: a channel's
		// ring (`Channel<float>` is Copy, and this predicate is what keeps it
		// out), a map's table (the predicate refuses it first; a map is not
		// Copy either) — the same shapes the capture gate refuses. A dynamic
		// array (`arr`) is not Copy, so it never reaches this axis; its buffer
		// is walked by the runtime where an owned move relinquishes it.
		//
		// Any other value composite rides: each crossing route gives the far
		// side an owner — a capture is duplicated at its operand, a channel
		// element at the send, and a RESULT is a transfer with one owner at a
		// time and needs no copy. This axis only says whether the bits may
		// travel.
		wantBits := copyable && !res.CountedBlockStaysShared(id)
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

// Runtime handles and generic Copy instantiations must agree across all axis
// implementations. Counted int/uint payloads own references; fixed64 payloads
// do not. A composite's answer follows its members, not its Copy marker.
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

fn probe(t: Task<int>, ts: Task<string>, c: Channel<int>, oi: Opt<int>, of: Opt<float>, p: Pair<int>, oi64: Opt<int64>, ou: Opt<uint>, ou64: Opt<uint64>, p64: Pair<int64>, pu: Pair<uint>, pu64: Pair<uint64>) -> int {
    return 0;
}
`
	res := coreSnippetResult(t, src)
	in := res.TypeInterner

	rows := map[string]bool{
		"Task<int>":    true,
		"Task<string>": true,
		"Channel<int>": true,
		"Opt<int>":     true,
		"Opt<int64>":   false,
		"Opt<uint>":    true,
		"Opt<uint64>":  false,
		"Opt<float>":   true,
		"Pair<int>":    true,
		"Pair<int64>":  false,
		"Pair<uint>":   true,
		"Pair<uint64>": false,
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
@copy type Pair64 = { a: int64, b: int64 };
@copy type PairU = { a: uint, b: uint };
@copy type PairU64 = { a: uint64, b: uint64 };
type Plain64 = { a: int64, b: int64 };
@copy type Inner64 = { x: int64 };
@copy type Outer64 = { inner: Inner64, label: int64 };
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
tag Hold64(Pair64);
type HeldPair64 = Hold64(Pair64) | Nothing_;
tag HoldF(Pf);
tag HoldS(Tagged);
tag Nothing_();
type HeldPair = Hold(Pair) | Nothing_;
type HeldPf = HoldF(Pf) | Nothing_;
type HeldTagged = HoldS(Tagged) | Nothing_;

fn probe(p: Pair, pf: Pf, m: Mixed, pl: Plain, tg: Tagged, o: Outer, d: Deep, bx: Boxed, fx: Fixed, hp: HeldPair, hf: HeldPf, ht: HeldTagged, ai: int[3], af: float[2], strs: string[2], ti: (int, int), ts: (int, string), tf: (bool, float), p64: Pair64, pu: PairU, pu64: PairU64, pl64: Plain64, o64: Outer64, hp64: HeldPair64, ai64: int64[3], au: uint[3], au64: uint64[3], ti64: (int64, int64)) -> int {
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
		"Pair64":                         false,
		"PairU":                          true,
		"PairU64":                        false,
		"Plain64":                        false,
		"Outer64":                        false,
		"HeldPair64":                     false,
		"(int64, int64)":                 false,
		"ArrayFixed<int64, const 3, 3>":  false,
		"ArrayFixed<uint, const 3, 3>":   true,
		"ArrayFixed<uint64, const 3, 3>": false,
		"Pair":                           true,
		"Pf":                             true,
		"Mixed":                          true,
		"Plain":                          true,
		"Tagged":                         true,
		"Outer":                          true,
		"Deep":                           true,
		"Boxed":                          true,
		"Fixed":                          true,
		"HeldPair":                       true,
		"HeldPf":                         true,
		"HeldTagged":                     true,
		"(int, int)":                     true,
		"(int, string)":                  true,
		"(bool, float)":                  true,
		// A fixed array is the nominal `ArrayFixed<T, const N, N>` to the
		// interner, and this is how its label spells it.
		"ArrayFixed<int, const 3, 3>":    true,
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

// MayShare walks union payloads; ContainsRefCountedScalar deliberately does
// not. Counted blocks in arrays can be made private; blocks in maps/channels
// cannot. Fixed64 siblings preserve the independent storage-shape controls.
func TestMayShareCountedBlockWalksUnionPayloads(t *testing.T) {
	src := `
@shard_movable
type P = { v: float };

@shard_movable
type Plain = { a: int };
@shard_movable type Plain64 = { a: int64 };
tag Bare64(Plain64);
@shard_movable type V64 = Bare64(Plain64) | Empty();

tag Held(P);
tag Bare(Plain);
tag Empty();

@shard_movable
type U = Held(P) | Empty();

@shard_movable
type V = Bare(Plain) | Empty();

@copy
@intrinsic
type Channel<T> = { __opaque: int };

fn probe(p: own P, u: own U, v: own V, w: own Plain, f: float, arr: float[], s: string, ch: Channel<float>, ci: Channel<int>, fixed: float[4], fixedi: int[4], xss: float[][], chs: Channel<float>[], m: Map<int, float>, mi: Map<int, int>, p64: own Plain64, v64: own V64, ci64: Channel<int64>, mi64: Map<int64, int64>, fixed64: int64[4], unsigned: uint, u64: uint64) -> int {
    return 0;
}
`
	res := coreSnippetResult(t, src)
	in := res.TypeInterner

	rows := map[string]struct{ share, contains, private bool }{
		"Plain64":                       {false, false, true},
		"own Plain64":                   {false, false, true},
		"V64":                           {false, false, true},
		"own V64":                       {false, false, true},
		"Channel<int64>":                {false, false, true},
		"Map<int64, int64>":             {false, false, true},
		"ArrayFixed<int64, const 4, 4>": {false, false, true},
		"uint":                          {true, true, true},
		"uint64":                        {false, false, true},
		"float":                         {true, true, true},
		"P":                             {true, true, true},
		"own P":                         {true, true, true},
		"U":                             {true, false, true},
		"own U":                         {true, false, true},
		"V":                             {true, false, true},
		"Plain":                         {true, true, true},
		"own Plain":                     {true, true, true},
		"string":                        {false, false, true},
		// A dynamic array's elements are counted blocks in a buffer the handle
		// names; the runtime walks that buffer element by element in the
		// relinquishing operand, so the array is made private and crosses,
		// through nesting. An array answers for its ELEMENT: an array of
		// channels stays refused because no walk reaches a ring.
		"Array<float>":          {true, false, true},
		"Array<Array<float>>":   {true, false, true},
		"Array<Channel<float>>": {true, false, false},
		// A map keyed or valued by a counted scalar shares like an array does
		// and, unlike one, has no per-element walk: its table stays refused.
		"Map<int, float>": {true, false, false},
		"Map<int, int>":   {true, false, false},
		// A runtime handle shares whenever its payload does: the handle's own
		// count is atomic so a copy may live on another shard, and a send from
		// there retains a block into a ring the creator's shard owns. No walk
		// over the handle's bytes reaches that ring, so it cannot be made
		// private either.
		"Channel<float>": {true, false, false},
		"Channel<int>":   {true, false, false},
		// A fixed array is a nominal struct with no declared fields; its
		// element type lives only in ArrayFixedInfo. BOTH questions must see
		// through it: a Copy `float[4]` copied as bits duplicates four
		// references. Its elements are inline, so the walk reaches them.
		"ArrayFixed<float, const 4, 4>": {true, true, true},
		"ArrayFixed<int, const 4, 4>":   {true, true, true},
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
		if got := res.MayShareCountedBlock(id); got != want.share {
			t.Errorf("%s: MayShareCountedBlock=%v, want %v", label, got, want.share)
		}
		if got := res.ContainsRefCountedScalar(id); got != want.contains {
			t.Errorf("%s: ContainsRefCountedScalar=%v, want %v", label, got, want.contains)
		}
		if got := res.CountedBlockCanBeMadePrivate(id); got != want.private {
			t.Errorf("%s: CountedBlockCanBeMadePrivate=%v, want %v", label, got, want.private)
		}
		if got := res.CountedBlockStaysShared(id); got != (want.share && !want.private) {
			t.Errorf("%s: CountedBlockStaysShared=%v, want %v", label, got, want.share && !want.private)
		}
	}
	for label := range rows {
		if !seen[label] {
			t.Errorf("%s: the snippet never produced this type, so its row pinned nothing", label)
		}
	}
}

// Relinquishment must prepare counted leaves and inspect dynamic array views.
// Fixed64 arrays need the view walk even without counts. Arrays behind a
// handle remain refused; its table/ring/result storage cannot be traversed.
func TestNeedsRelinquishWalkArmsEveryArrayCrossing(t *testing.T) {
	src := `
type Holder = { n: int, xs: int[] };
type Holder64 = { n: int64, xs: int64[] };

@copy
@intrinsic
type Channel<T> = { __opaque: int };

@intrinsic
type Task<T> = { __opaque: int };

fn probe(h: own Holder, xs: int[], fs: float[], xss: int[][], t: (int[], int),
         s: string, n: int, ch: Channel<int>, cf: Channel<float>, m: Map<int, int[]>,
         ca: Channel<int[]>, ta: Task<int[]>, ms: Map<int, string>, h64: own Holder64, xs64: int64[], xss64: int64[][], t64: (int64[], int64), ch64: Channel<int64>, m64: Map<int64, int64[]>, ca64: Channel<int64[]>, ta64: Task<int64[]>, ms64: Map<int64, string>, n64: int64, u: uint, u64: uint64) -> int {
    return 0;
}
`
	res := coreSnippetResult(t, src)
	in := res.TypeInterner

	rows := map[string]struct{ share, array, refused bool }{
		// Both representations preserve the independent array-view check.
		"Array<int64>":             {false, true, false},
		"Array<Array<int64>>":      {false, true, false},
		"Holder64":                 {false, true, false},
		"own Holder64":             {false, true, false},
		"(Array<int64>, int64)":    {false, true, false},
		"int64":                    {false, false, false},
		"uint":                     {true, false, false},
		"uint64":                   {false, false, false},
		"Channel<int64>":           {false, false, false},
		"Map<int64, string>":       {false, false, false},
		"Map<int64, Array<int64>>": {false, false, true},
		"Channel<Array<int64>>":    {false, false, true},
		"Task<Array<int64>>":       {false, false, true},
		"Array<int>":               {true, true, false},
		"Array<Array<int>>":        {true, true, false},
		"Holder":                   {true, true, false},
		"own Holder":               {true, true, false},
		"(Array<int>, int)":        {true, true, false},
		"Array<float>":             {true, true, false},
		"Channel<float>":           {true, false, false},
		"int":                      {true, false, false},
		"string":                   {false, false, false},
		"Channel<int>":             {true, false, false},
		"Map<int, string>":         {true, false, false},
		// The three storages no per-element walk steps. An array inside one is
		// not armed -- there is nothing to hand the runtime -- so the crossing
		// gate refuses the whole shape and the third column is where that is
		// pinned.
		"Map<int, Array<int>>": {true, false, true},
		"Channel<Array<int>>":  {true, false, true},
		"Task<Array<int>>":     {true, false, true},
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
		if got := res.MayShareCountedBlock(id); got != want.share {
			t.Errorf("%s: MayShareCountedBlock=%v, want %v", label, got, want.share)
		}
		if got := in.ContainsDynamicArray(id); got != want.array {
			t.Errorf("%s: ContainsDynamicArray=%v, want %v", label, got, want.array)
		}
		if got := res.NeedsRelinquishWalk(id); got != (want.share || want.array) {
			t.Errorf("%s: NeedsRelinquishWalk=%v, want %v", label, got, want.share || want.array)
		}
		if got := res.DynamicArrayStaysUnchecked(id); got != want.refused {
			t.Errorf("%s: DynamicArrayStaysUnchecked=%v, want %v", label, got, want.refused)
		}
		if got := res.CountedBlockStaysShared(id); got != (want.share && !res.CountedBlockCanBeMadePrivate(id)) {
			t.Errorf("%s: CountedBlockStaysShared=%v; the widening moved sema's admission set", label, got)
		}
		// Walked or refused, never neither: a row carrying an array anywhere
		// its bytes reach has to meet one of the two, and this is the check a
		// future container type must not slip past.
		if (want.array || want.refused) != (in.ContainsDynamicArray(id) || res.DynamicArrayStaysUnchecked(id)) {
			t.Errorf("%s: the row's two array columns do not match the predicates", label)
		}
	}
	for label := range rows {
		if !seen[label] {
			t.Errorf("%s: the snippet never produced this type, so its row pinned nothing", label)
		}
	}
}
