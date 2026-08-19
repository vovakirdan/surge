package vm

import (
	"testing"

	"surge/internal/types"
)

// The element run is the storage a dynamic array's elements live in, and these
// are the questions a universal element list never had to answer: does slot `i`
// keep what slot `i` was given, does a value read back as the thing that was
// written, and does a teardown owe exactly one drop per element it still holds.
//
// Every row here READS BACK what it wrote. That is not thoroughness for its own
// sake — the defect shape this project keeps hitting during a representation
// change is a cell that can be written and not read, and an asymmetric cell
// passes every test that does not round-trip.

// A run keeps its elements apart. Under a universal element list this was true
// by construction, because each element was its own value; in a run it is
// arithmetic, and arithmetic that is off by a stride silently returns the
// neighbour.
func TestElementRunKeepsEachElementDistinct(t *testing.T) {
	f := newStorageFixture(t)
	store := newScratch()
	const count = 5

	base, err := f.vm.reserveRun(store, f.i64, count)
	if err != nil {
		t.Fatalf("reserving a run of %d must succeed: %v", count, err)
	}
	member, err := f.vm.runElemMember(f.i64)
	if err != nil {
		t.Fatalf("an element must be describable: %v", err)
	}

	for i := range count {
		ref, err := f.vm.runElemRef(base, f.i64, i)
		if err != nil {
			t.Fatalf("addressing element %d must succeed: %v", i, err)
		}
		if err := f.vm.storageWriteCell(ref, member, MakeInt(int64(100+i), f.i64)); err != nil {
			t.Fatalf("writing element %d must succeed: %v", i, err)
		}
	}
	for i := range count {
		ref, err := f.vm.runElemRef(base, f.i64, i)
		if err != nil {
			t.Fatalf("addressing element %d must succeed: %v", i, err)
		}
		got, err := f.vm.storageReadCell(ref, member)
		if err != nil {
			t.Fatalf("reading element %d must succeed: %v", i, err)
		}
		if got.Int != int64(100+i) {
			t.Fatalf("element %d reads back %d, want %d — a run whose stride is wrong returns a neighbour",
				i, got.Int, 100+i)
		}
	}
}

// Every cell kind an element can have, written and read back through a run.
//
// A run gives every slot the SAME encoding, taken from the array's element type
// rather than from what the slot happens to hold, so a kind that encodes and
// does not decode shows up here rather than in the one program that both writes
// and reads it.
func TestElementRunRoundTripsEveryElementKind(t *testing.T) {
	f := newStorageFixture(t)

	handle := f.vm.Heap.AllocString(types.NoTypeID, "carried")
	defer f.vm.Heap.Release(handle)

	rows := []struct {
		name  string
		elem  types.TypeID
		write Value
		check func(t *testing.T, got Value)
	}{
		{
			name:  "sized int",
			elem:  f.i64,
			write: MakeInt(-7, f.i64),
			check: func(t *testing.T, got Value) {
				if got.Int != -7 {
					t.Fatalf("a sized int reads back %d, want -7", got.Int)
				}
			},
		},
		{
			name:  "unsized int",
			elem:  f.anyInt,
			write: MakeInt(42, f.anyInt),
			check: func(t *testing.T, got Value) {
				if got.Int != 42 {
					t.Fatalf("an unsized int reads back %d, want 42", got.Int)
				}
			},
		},
		{
			name:  "handle",
			elem:  f.text,
			write: MakeHandleString(handle, f.text),
			check: func(t *testing.T, got Value) {
				if got.H != handle {
					t.Fatalf("a handle reads back %d, want %d", got.H, handle)
				}
			},
		},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			store := newScratch()
			base, err := f.vm.reserveRun(store, row.elem, 3)
			if err != nil {
				t.Fatalf("reserving a run of %s must succeed: %v", row.name, err)
			}
			member, err := f.vm.runElemMember(row.elem)
			if err != nil {
				t.Fatalf("describing a %s element must succeed: %v", row.name, err)
			}
			// The middle slot, so a read that ignored the index would be caught
			// by reading a zeroed neighbour rather than the value under test.
			ref, err := f.vm.runElemRef(base, row.elem, 1)
			if err != nil {
				t.Fatalf("addressing the middle element must succeed: %v", err)
			}
			if writeErr := f.vm.storageWriteCell(ref, member, row.write); writeErr != nil {
				t.Fatalf("writing a %s element must succeed: %v", row.name, writeErr)
			}
			readBack, err := f.vm.runElemRef(base, row.elem, 1)
			if err != nil {
				t.Fatalf("re-addressing the middle element must succeed: %v", err)
			}
			got, err := f.vm.storageReadCell(readBack, member)
			if err != nil {
				t.Fatalf("reading a %s element must succeed: %v", row.name, err)
			}
			row.check(t, got)
		})
	}
}

// A run of composites owes one drop per element it still holds, and a teardown
// makes each of them exactly once.
//
// The universal element list could answer this by walking the values; a run has
// no list to walk, so the entry that reserved it has to remember how many
// values are in it. A rewind that treated the run as ONE value would release
// the first element and leak the rest.
func TestElementRunTeardownDropsEveryElementExactlyOnce(t *testing.T) {
	f := newStorageFixture(t)
	store := newScratch()
	const count = 4

	base, err := f.vm.reserveRun(store, f.node, count)
	if err != nil {
		t.Fatalf("reserving a run of composites must succeed: %v", err)
	}
	if err := store.setRunLive(base, count); err != nil {
		t.Fatalf("recording the run's length must succeed: %v", err)
	}

	carried := make([]Handle, 0, count)
	for i := range count {
		ref, err := f.vm.runElemRef(base, f.node, i)
		if err != nil {
			t.Fatalf("addressing element %d must succeed: %v", i, err)
		}
		if err := f.vm.storageZero(ref); err != nil {
			t.Fatalf("zeroing element %d must succeed: %v", i, err)
		}
		carried = append(carried, f.writeNode(t, ref, int64(i), int64(i), int64(i), "held"))
	}

	for i, h := range carried {
		obj, ok := f.vm.Heap.lookup(h)
		if !ok || obj.Freed {
			t.Fatalf("element %d's string is already gone before the teardown", i)
		}
	}

	if err := f.vm.rewindScratch(store, scratchMark{}); err != nil {
		t.Fatalf("tearing the run down must succeed: %v", err)
	}

	for i, h := range carried {
		obj, ok := f.vm.Heap.lookup(h)
		if ok && !obj.Freed {
			t.Fatalf("element %d's string survived the teardown, so its slot was never dropped", i)
		}
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("the heap is unbalanced after tearing down a run: %d allocations, %d frees — "+
			"a run dropped twice or not at all",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// A teardown drops the run's LENGTH and not its capacity, and the slots past
// the length are DEAD even when their bytes still look like values.
//
// A zeroed tail cannot show this: dropping a zeroed composite releases nothing,
// so a teardown that walked the capacity would pass anyway. The shape that can
// show it is the one the language actually produces — dropping a prefix shifts
// the remainder DOWN by raw bytes, which leaves the tail holding stale
// duplicates of values that now live at lower indices. Dropping those is a
// double free of everything the shift moved.
func TestElementRunTeardownLeavesTheDeadTailAlone(t *testing.T) {
	f := newStorageFixture(t)
	store := newScratch()
	const capacity = 4
	const removed = 2

	base, reserveErr := f.vm.reserveRun(store, f.node, capacity)
	if reserveErr != nil {
		t.Fatalf("reserving a run must succeed: %v", reserveErr)
	}
	if liveErr := store.setRunLive(base, capacity); liveErr != nil {
		t.Fatalf("recording the run's length must succeed: %v", liveErr)
	}

	carried := make([]Handle, 0, capacity)
	for i := range capacity {
		ref, addrErr := f.vm.runElemRef(base, f.node, i)
		if addrErr != nil {
			t.Fatalf("addressing element %d must succeed: %v", i, addrErr)
		}
		if zeroErr := f.vm.storageZero(ref); zeroErr != nil {
			t.Fatalf("zeroing element %d must succeed: %v", i, zeroErr)
		}
		carried = append(carried, f.writeNode(t, ref, int64(i), int64(i), int64(i), "held"))
	}

	// Drop the prefix the way the language does: release what is removed, then
	// move the survivors down as bytes and shorten the run.
	for i := range removed {
		ref, refErr := f.vm.runElemRef(base, f.node, i)
		if refErr != nil {
			t.Fatalf("addressing removed element %d must succeed: %v", i, refErr)
		}
		if dropErr := f.vm.storageDrop(ref); dropErr != nil {
			t.Fatalf("dropping removed element %d must succeed: %v", i, dropErr)
		}
	}
	stride, strideErr := f.vm.runStride(f.node)
	if strideErr != nil {
		t.Fatalf("an element stride must be known: %v", strideErr)
	}
	for i := removed; i < capacity; i++ {
		src, srcErr := f.vm.runElemRef(base, f.node, i)
		if srcErr != nil {
			t.Fatalf("addressing survivor %d must succeed: %v", i, srcErr)
		}
		dst, dstErr := f.vm.runElemRef(base, f.node, i-removed)
		if dstErr != nil {
			t.Fatalf("addressing the destination of survivor %d must succeed: %v", i, dstErr)
		}
		from, fromErr := src.resolve(stride)
		if fromErr != nil {
			t.Fatalf("resolving survivor %d must succeed: %v", i, fromErr)
		}
		moved := make([]byte, len(from))
		copy(moved, from)
		to, toErr := dst.resolve(stride)
		if toErr != nil {
			t.Fatalf("resolving the destination of survivor %d must succeed: %v", i, toErr)
		}
		copy(to, moved)
	}
	if shortenErr := store.setRunLive(base, capacity-removed); shortenErr != nil {
		t.Fatalf("shortening the run must succeed: %v", shortenErr)
	}

	// The survivors are still held exactly once each, now at the low indices,
	// and the stale copies of them sitting in the tail are not owners.
	for i := removed; i < capacity; i++ {
		obj, ok := f.vm.Heap.lookup(carried[i])
		if !ok || obj.Freed {
			t.Fatalf("survivor %d was released by the shift itself", i)
		}
	}

	if rewindErr := f.vm.rewindScratch(store, scratchMark{}); rewindErr != nil {
		t.Fatalf("tearing the shortened run down must succeed: %v", rewindErr)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("the heap is unbalanced: %d allocations, %d frees — a teardown that walks the "+
			"capacity drops the stale tail and frees every moved value twice",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// An element's alignment folds the run's, so a run inside a container that is
// itself under-aligned does not claim an alignment its address cannot honour.
//
// This is the same rule a struct field already obeys, reaching the element path
// through the run rather than through a member list.
func TestElementRunAlignmentFoldsTheRunsOwn(t *testing.T) {
	f := newStorageFixture(t)
	store := newScratch()

	base, err := f.vm.reserveRun(store, f.i64, 3)
	if err != nil {
		t.Fatalf("reserving a run must succeed: %v", err)
	}
	// A base the container could only place on an odd byte: every element of it
	// is reachable, and none of them is eight-byte aligned.
	base.Align = 1

	for i := range 3 {
		ref, err := f.vm.runElemRef(base, f.i64, i)
		if err != nil {
			t.Fatalf("addressing element %d must succeed: %v", i, err)
		}
		if ref.Align != 1 {
			t.Fatalf("element %d of an unaligned run claims alignment %d, want 1 — "+
				"an element may not require more than its run's address can honour", i, ref.Align)
		}
	}
}

// The growth policy is a bound on storage, not a convenience.
//
// A container's arena has no per-extent free, so a run that grew away leaves
// its bytes behind until the object dies. Doubling is what keeps that residue
// under twice the live run; growing by a constant would make it unbounded in
// the number of pushes, which is why the policy is pinned rather than left to
// whoever next edits it.
func TestElementRunGrowthStaysWithinDoubling(t *testing.T) {
	if got := growRunCapacity(0, 1); got != 4 {
		t.Fatalf("an empty run grows to %d, want 4 — a first push should not reserve one slot at a time", got)
	}
	if got := growRunCapacity(4, 4); got != 4 {
		t.Fatalf("a run that already fits grew to %d, want 4", got)
	}
	if got := growRunCapacity(4, 5); got != 8 {
		t.Fatalf("a run of 4 asked for 5 grew to %d, want 8", got)
	}
	// A reserve far past the current capacity still lands on a doubling, so the
	// residue stays bounded by the live run rather than by the request.
	if got := growRunCapacity(4, 100); got != 128 {
		t.Fatalf("a run of 4 asked for 100 grew to %d, want 128", got)
	}
	for current := 1; current <= 64; current *= 2 {
		for want := current + 1; want <= current*4; want++ {
			got := growRunCapacity(current, want)
			if got < want {
				t.Fatalf("growing %d to hold %d gave %d, which does not hold it", current, want, got)
			}
			if got >= 2*want {
				t.Fatalf("growing %d to hold %d gave %d, which is more than twice what was asked",
					current, want, got)
			}
		}
	}
}
