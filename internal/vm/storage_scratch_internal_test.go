package vm

import (
	"strings"
	"testing"
)

// The scratch discipline. Every row here is one of the four rules that keep a
// temporary from being freed twice or not at all, and each is checked as a
// property rather than as a call sequence, because the rules exist to survive
// edits that reorder the calls.

// Reserving is exact-layout allocation, not a bump of convenience: each extent
// starts where its own alignment allows and shares no byte with its neighbour.
// Two temporaries that overlapped would be one value wearing two names.
func TestScratchHandsOutDisjointAlignedExtents(t *testing.T) {
	f := newStorageFixture(t)
	s := newScratch()

	first, err := f.vm.reserveScratch(s, f.node)
	if err != nil {
		t.Fatalf("reserving the first temporary must succeed: %v", err)
	}
	second, err := f.vm.reserveScratch(s, f.node)
	if err != nil {
		t.Fatalf("reserving the second temporary must succeed: %v", err)
	}

	if first.Align == 0 || first.Offset%first.Align != 0 || second.Offset%second.Align != 0 {
		t.Fatalf("a temporary must start at its own alignment: %d and %d at align %d",
			first.Offset, second.Offset, first.Align)
	}
	size, err := f.vm.storageSizeOf(f.node)
	if err != nil {
		t.Fatalf("Node must have a size: %v", err)
	}
	if second.Offset < first.Offset+size {
		t.Fatalf("two temporaries overlap: %d and %d for %d bytes", first.Offset, second.Offset, size)
	}

	// Both are real storage: writing one must not be visible through the other.
	f.writeNode(t, first, 1, 2, 3, "first")
	f.writeNode(t, second, 4, 5, 6, "second")
	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	if got := f.readCell(t, first, members[2]); got.Int != 3 {
		t.Fatalf("the first temporary reads %d after the second was written, want 3", got.Int)
	}

	if err := f.vm.rewindScratch(s, scratchMark{}); err != nil {
		t.Fatalf("rewinding must release every temporary: %v", err)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("the rewind left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// A rewind drops what is still live. This is the panic-partway-through shape:
// an instruction that built two temporaries and then failed must release both,
// and it must release them through the mark rather than through a path that
// remembers which ones it got to.
func TestScratchRewindDropsEveryTemporaryStillLive(t *testing.T) {
	f := newStorageFixture(t)
	s := newScratch()

	mark := s.mark()
	first, err := f.vm.reserveScratch(s, f.node)
	if err != nil {
		t.Fatalf("reserving must succeed: %v", err)
	}
	second, err := f.vm.reserveScratch(s, f.node)
	if err != nil {
		t.Fatalf("reserving must succeed: %v", err)
	}
	held := []Handle{
		f.writeNode(t, first, 1, 2, 3, "abandoned"),
		f.writeNode(t, second, 4, 5, 6, "also abandoned"),
	}

	if err := f.vm.rewindScratch(s, mark); err != nil {
		t.Fatalf("rewinding must release every temporary: %v", err)
	}

	for i, handle := range held {
		if obj, ok := f.vm.Heap.lookup(handle); !ok || !obj.Freed {
			t.Fatalf("temporary %d was still live at the rewind and was not released", i)
		}
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("the rewind left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
	if s.used != mark.used || len(s.entries) != mark.entries {
		t.Fatalf("the rewind kept %d bytes and %d entries, want %d and %d",
			s.used, len(s.entries), mark.used, mark.entries)
	}
}

// A temporary that was MOVED into a slot is no longer the rewind's to drop.
// This is the rule that keeps a store from being a double free: the store takes
// the value and marks the extent dead in the same step, so the rewind that
// follows finds nothing owed.
func TestScratchRewindLeavesAMovedTemporaryAlone(t *testing.T) {
	f := newStorageFixture(t)
	s := newScratch()

	mark := s.mark()
	ref, err := f.vm.reserveScratch(s, f.node)
	if err != nil {
		t.Fatalf("reserving must succeed: %v", err)
	}
	moved := f.writeNode(t, ref, 1, 2, 3, "moved into a slot")

	if err := s.release(ref); err != nil {
		t.Fatalf("handing a temporary over must succeed: %v", err)
	}
	if err := f.vm.rewindScratch(s, mark); err != nil {
		t.Fatalf("rewinding after a handover must succeed: %v", err)
	}

	if obj, ok := f.vm.Heap.lookup(moved); !ok || obj.Freed {
		t.Fatal("the rewind released a temporary that had already been moved into a slot")
	}
	if got := f.vm.Heap.Get(moved).RefCount; got != 1 {
		t.Fatalf("the moved value is held %d times, want 1", got)
	}

	// Whoever the value moved to owes the release now.
	f.vm.Heap.Release(moved)
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("a moved temporary left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// Handing over an extent that is not a live temporary is refused rather than
// ignored. A mover that thought it was clearing scratch and was not would leave
// the rewind dropping a value that now has a second owner, and the two owners
// would each release it.
func TestScratchRefusesAHandoverItCannotAccount(t *testing.T) {
	f := newStorageFixture(t)
	s := newScratch()

	ref, reserveErr := f.vm.reserveScratch(s, f.node)
	if reserveErr != nil {
		t.Fatalf("reserving must succeed: %v", reserveErr)
	}
	if err := s.release(ref); err != nil {
		t.Fatalf("the first handover must succeed: %v", err)
	}
	err := s.release(ref)
	if err == nil {
		t.Fatal("handing the same temporary over twice must be refused")
	}
	if !strings.Contains(err.Error(), "no live scratch extent") {
		t.Fatalf("the refusal must say what it could not account for; got %q", err)
	}

	if err := f.vm.rewindScratch(s, scratchMark{}); err != nil {
		t.Fatalf("rewinding empty scratch must succeed: %v", err)
	}
}

// Scratch is REUSED, which is the case a frame arena only reaches when
// activations pool their storage — here it happens at every instruction
// boundary. A reference formed before a rewind names bytes that the next
// temporary now occupies, and those bytes read perfectly well, so the
// generation is the only thing that can refuse it.
func TestScratchReferenceIsRefusedAfterItsMarkIsRewound(t *testing.T) {
	f := newStorageFixture(t)
	s := newScratch()

	mark := s.mark()
	stale, staleErr := f.vm.reserveScratch(s, f.node)
	if staleErr != nil {
		t.Fatalf("reserving must succeed: %v", staleErr)
	}
	f.writeNode(t, stale, 1, 2, 3, "first instruction")
	if err := f.vm.rewindScratch(s, mark); err != nil {
		t.Fatalf("rewinding must release the first instruction's temporary: %v", err)
	}

	fresh, freshErr := f.vm.reserveScratch(s, f.node)
	if freshErr != nil {
		t.Fatalf("reserving after a rewind must succeed: %v", freshErr)
	}
	if fresh.Offset != stale.Offset {
		t.Fatalf("the rewind did not reclaim the space: %d then %d", stale.Offset, fresh.Offset)
	}
	if fresh.Gen == stale.Gen {
		t.Fatal("reused scratch handed out the generation it had before, so nothing distinguishes the two temporaries")
	}
	occupant := f.writeNode(t, fresh, 4, 5, 6, "second instruction")

	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	staleCount, projectErr := stale.memberRef(members[2])
	if projectErr != nil {
		t.Fatalf("projecting through a stale reference must still be arithmetic: %v", projectErr)
	}
	if got, readErr := f.vm.storageReadCell(staleCount, members[2]); readErr == nil {
		t.Fatalf("a reference into rewound scratch read %d out of the temporary that replaced it", got.Int)
	}
	if err := f.vm.storageDrop(stale); err == nil {
		t.Fatal("dropping through a reference into rewound scratch must be refused")
	}
	if obj, ok := f.vm.Heap.lookup(occupant); !ok || obj.Freed {
		t.Fatal("a refused stale drop released what the current temporary holds")
	}

	if err := f.vm.rewindScratch(s, mark); err != nil {
		t.Fatalf("the closing rewind must succeed: %v", err)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("scratch reuse left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}
