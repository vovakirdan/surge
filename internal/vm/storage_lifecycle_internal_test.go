package vm

import (
	"testing"

	"surge/internal/types"
)

// The lifecycle matrix for inline storage: what an extent owes when it is
// written over, when part of it has been taken away, and when only part of it
// was ever written.
//
// Every row here is about a value whose storage OUTLIVES one of its own
// contents. A box could answer these questions by handing out a new box and
// letting the old one be released by whoever still held it. Exact-layout
// storage cannot: the bytes stay, so the operation that overwrites them is the
// last thing that can release what they held, and an operation that walks
// members has to know which members are really there.

// storageCopy does NOT release what it overwrites, and that is the contract
// rather than an omission — so it is pinned here, because the alternative
// reading is that it is a leak somebody should quietly "fix".
//
// The reason it belongs to the caller is that the two callers owe different
// things. A copy that INITIALIZES writes into storage owning nothing, and a
// release there would be a drop nobody owes. A copy that REPLACES owes exactly
// one release, and it has to be ordered against reading the source. Only the
// caller knows which it is; storageReplace is the one that owes it.
func TestStorageCopyLeavesTheReleaseOfWhatItOverwritesToItsCaller(t *testing.T) {
	f := newStorageFixture(t)
	dst := f.ref(t, 0, f.node)
	src := f.ref(t, 1, f.node)

	overwritten := f.writeNode(t, dst, 1, 2, 3, "overwritten")
	f.writeNode(t, src, 4, 5, 6, "kept")

	if err := f.vm.storageCopy(dst, src); err != nil {
		t.Fatalf("copying over an initialized destination must succeed: %v", err)
	}
	obj, ok := f.vm.Heap.lookup(overwritten)
	if !ok || obj.Freed {
		t.Fatal("storageCopy released what it overwrote; that release is its caller's to make")
	}
	if obj.RefCount != 1 {
		t.Fatalf("the overwritten value is held %d times, want 1 — still owed by the caller", obj.RefCount)
	}

	// The caller settling the debt is what makes the heap balance.
	f.vm.Heap.Release(overwritten)
	if err := f.vm.storageDrop(dst); err != nil {
		t.Fatalf("dropping the copy must succeed: %v", err)
	}
	if err := f.vm.storageDrop(src); err != nil {
		t.Fatalf("dropping the original must succeed: %v", err)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("the heap is unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// The replacement path is the one that owes the release, and it must make it
// exactly once. Inline storage is the only reference to what it holds, so the
// bytes being overwritten are the last thing that could ever name it.
func TestStorageReplaceReleasesTheValueItOverwrites(t *testing.T) {
	f := newStorageFixture(t)
	dst := f.ref(t, 0, f.node)
	src := f.ref(t, 1, f.node)

	replaced := f.writeNode(t, dst, 1, 2, 3, "replaced")
	f.writeNode(t, src, 4, 5, 6, "kept")

	if err := f.vm.storageReplace(dst, src); err != nil {
		t.Fatalf("replacing an initialized destination must succeed: %v", err)
	}
	if obj, ok := f.vm.Heap.lookup(replaced); !ok || !obj.Freed {
		t.Fatal("the value the destination held was never released")
	}

	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	if got := f.readCell(t, dst, members[2]); got.Int != 6 {
		t.Fatalf("the destination holds %d after replacement, want 6", got.Int)
	}

	if err := f.vm.storageDrop(dst); err != nil {
		t.Fatalf("dropping the replacement must succeed: %v", err)
	}
	if err := f.vm.storageDrop(src); err != nil {
		t.Fatalf("dropping the original must succeed: %v", err)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("replacement left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// Self-assignment is where the two sides are the same bytes. Copying would zero
// the source before reading it and replacing would RELEASE the source before
// reading it, so both must recognize the case and do nothing. Getting this
// wrong destroys the value rather than garbling it, which is why both paths are
// checked and why the identity test comes before either the zero or the drop.
func TestStorageCopyAndReplaceOfAValueByItselfKeepIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*VM, StorageRef) error
	}{
		{"copied onto itself", func(machine *VM, ref StorageRef) error { return machine.storageCopy(ref, ref) }},
		{"replaced by itself", func(machine *VM, ref StorageRef) error { return machine.storageReplace(ref, ref) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStorageFixture(t)
			ref := f.ref(t, 0, f.node)
			handle := f.writeNode(t, ref, 7, 8, 9, "itself")

			if err := tc.run(f.vm, ref); err != nil {
				t.Fatalf("a value assigned to itself must succeed: %v", err)
			}
			obj, ok := f.vm.Heap.lookup(handle)
			if !ok || obj.Freed {
				t.Fatal("assigning a value to itself released what it held")
			}
			if obj.RefCount != 1 {
				t.Fatalf("assigning a value to itself left it held %d times, want 1", obj.RefCount)
			}

			members, err := f.vm.compositeMembers(f.node)
			if err != nil {
				t.Fatalf("Node must have describable members: %v", err)
			}
			if got := f.readCell(t, ref, members[2]); got.Int != 9 {
				t.Fatalf("the value reads %d after being assigned to itself, want 9", got.Int)
			}

			if err := f.vm.storageDrop(ref); err != nil {
				t.Fatalf("dropping must succeed: %v", err)
			}
			if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
				t.Fatalf("self-assignment left the heap unbalanced: %d allocations, %d frees",
					f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
			}
		})
	}
}

// A member moved out of a value leaves a residual: the members that were not
// moved. Dropping the residual must release those and must not release the
// moved member a second time, because the value it named now belongs to
// whoever it was moved to.
func TestStoragePartialMoveLeavesAResidualThatDropsOnlyWhatItStillOwns(t *testing.T) {
	f := newStorageFixture(t)
	ref := f.ref(t, 0, f.node)
	handle := f.writeNode(t, ref, 1, 2, 3, "moved-away")

	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	label := members[1]

	// The move: the member is read out and its extent stops naming it, which
	// is what makes the residual droppable without releasing it twice.
	taken := f.readCell(t, ref, label)
	if taken.H != handle {
		t.Fatalf("the moved member names handle %d, want %d", taken.H, handle)
	}
	labelRef, err := ref.memberRef(label)
	if err != nil {
		t.Fatalf("projecting the moved member must succeed: %v", err)
	}
	if err := f.vm.storageZero(labelRef); err != nil {
		t.Fatalf("clearing the moved member must succeed: %v", err)
	}

	if err := f.vm.storageDrop(ref); err != nil {
		t.Fatalf("dropping the residual must succeed: %v", err)
	}
	if obj, ok := f.vm.Heap.lookup(handle); !ok || obj.Freed {
		t.Fatal("dropping the residual released a member that had been moved out")
	}
	if got := f.vm.Heap.Get(handle).RefCount; got != 1 {
		t.Fatalf("the moved member is held %d times, want 1", got)
	}

	// The move's destination is what owes the release now.
	f.vm.dropValue(taken)
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("a partial move left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// rearm gives a retired arena the storage its next occupant would get: fresh
// bytes at the same offsets, one generation on.
//
// It reaches past the arena's own API because there is no reuse entry point
// yet — retirement is the only thing that bumps a generation today, and it
// releases the bytes as it goes. That makes retirement alone a weak test: a
// reference into released bytes is refused for having no storage, which is not
// the same fact as being refused for being stale. Arming the storage again is
// what separates the two, and it is the state a frame arena reaches the moment
// activations reuse their storage.
func (f *storageFixture) rearm() {
	f.arena.bytes = make([]byte, f.plan.Size)
}

// A reference into a reused extent must be refused rather than resolved
// against whoever holds those bytes now.
//
// This is the row a box never had to answer. A box that outlives its reference
// is still the same box, and the reference is still right about what it names;
// an extent that outlives its reference is a different value at the same
// address, and the bytes there are perfectly readable. Nothing but the
// generation can tell those two apart, so the check has to be what refuses —
// and the refusal has to happen before the read, not be inferred from a garbled
// result afterwards.
func TestStorageRefIntoAReusedExtentDoesNotReadItsNewOccupant(t *testing.T) {
	f := newStorageFixture(t)
	stale := f.ref(t, 0, f.node)
	f.writeNode(t, stale, 1, 2, 3, "first activation")

	members, describeErr := f.vm.compositeMembers(f.node)
	if describeErr != nil {
		t.Fatalf("Node must have describable members: %v", describeErr)
	}

	// The activation ends: what it owned is released and its storage retires.
	if err := f.vm.storageDrop(stale); err != nil {
		t.Fatalf("dropping the first activation's value must succeed: %v", err)
	}
	if !f.arena.retire() {
		t.Fatal("an unpinned arena must retire when its activation ends")
	}

	// The next activation takes the same storage and writes a different value
	// at the same offset.
	f.rearm()
	fresh := f.ref(t, 0, f.node)
	if fresh.Gen == stale.Gen {
		t.Fatal("reused storage handed out the generation it had before, so nothing distinguishes the two occupants")
	}
	occupant := f.writeNode(t, fresh, 4, 5, 6, "second activation")

	// The reference the first activation left behind now names bytes that read
	// perfectly well and belong to somebody else.
	staleCount, projectErr := stale.memberRef(members[2])
	if projectErr != nil {
		t.Fatalf("projecting through a stale reference must still be arithmetic: %v", projectErr)
	}
	if got, readErr := f.vm.storageReadCell(staleCount, members[2]); readErr == nil {
		t.Fatalf("a stale reference read %d out of the value that replaced it", got.Int)
	}

	// The whole-value operations refuse for the same reason, and refuse BEFORE
	// touching anything: a drop through a stale reference that got as far as a
	// member would release what the new occupant owns.
	if err := f.vm.storageDrop(stale); err == nil {
		t.Fatal("dropping through a stale reference must be refused")
	}
	if obj, ok := f.vm.Heap.lookup(occupant); !ok || obj.Freed {
		t.Fatal("a refused stale drop released what the new occupant holds")
	}

	// The refusal is the generation and not a blanket one: a reference formed
	// after the reuse reads the new occupant exactly.
	if got := f.readCell(t, fresh, members[2]); got.Int != 6 {
		t.Fatalf("the current reference reads %d, want 6", got.Int)
	}

	if err := f.vm.storageDrop(fresh); err != nil {
		t.Fatalf("dropping the second activation's value must succeed: %v", err)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("reused storage left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// An operation that fails partway must leave a BOUNDED value: the members it
// wrote are owned and droppable, the members it never reached are not, and
// nothing is released twice.
//
// This is the shape a panic mid-construction leaves, and the shape a refusal to
// obtain storage leaves. It is modelled here by a copy whose source cannot be
// read all the way through, because that fails the walk at a member boundary
// with earlier members already written — which is precisely what an interrupted
// construction looks like from the destination's side.
//
// The distinguishing risk is the count. The walk retained a member before it
// failed, so a destination treated as uninitialized leaks that retain, and a
// destination treated as fully built releases a member that was never written.
func TestStorageCopyThatFailsPartwayLeavesABoundedValue(t *testing.T) {
	f := newStorageFixture(t)
	dst := f.ref(t, 0, f.node)
	src := f.ref(t, 1, f.node)

	label := f.writeNode(t, src, 1, 2, 3, "kept")
	members, describeErr := f.vm.compositeMembers(f.node)
	if describeErr != nil {
		t.Fatalf("Node must have describable members: %v", describeErr)
	}

	// The source's LAST member is made unreadable, so the walk fails after it
	// has already written the two before it.
	f.writeCell(t, src, members[2], MakeBigInt(Handle(4242), types.NoTypeID))

	if err := f.vm.storageCopy(dst, src); err == nil {
		t.Fatal("a copy whose source cannot be read to the end must fail")
	}

	// What the walk got to is owned by the destination: two holders of one
	// string, the source's and the copy's.
	if got := f.vm.Heap.Get(label).RefCount; got != 2 {
		t.Fatalf("the member the walk wrote is held %d times, want 2", got)
	}
	if got := f.readCell(t, dst, members[2]); got.Kind != VKNothing {
		t.Fatalf("the member the walk never reached reads back as %s, want nothing", got.Kind)
	}

	// Dropping the bounded value releases exactly what was written.
	if err := f.vm.storageDrop(dst); err != nil {
		t.Fatalf("dropping a value the copy left half-built must succeed: %v", err)
	}
	if got := f.vm.Heap.Get(label).RefCount; got != 1 {
		t.Fatalf("dropping the half-built value left the member held %d times, want 1", got)
	}

	// And dropping it again releases nothing, because the first drop zeroed
	// what it released.
	if err := f.vm.storageDrop(dst); err != nil {
		t.Fatalf("dropping an already dropped value must succeed: %v", err)
	}
	if got := f.vm.Heap.Get(label).RefCount; got != 1 {
		t.Fatalf("a second drop released a member again: the member is held %d times, want 1", got)
	}

	// The source is the fixture's own debris: its last member still names a
	// handle nothing issued, so it is cleared before the source is released.
	srcCount, projectErr := src.memberRef(members[2])
	if projectErr != nil {
		t.Fatalf("projecting the source's last member must succeed: %v", projectErr)
	}
	if err := f.vm.storageZero(srcCount); err != nil {
		t.Fatalf("clearing the source's unreadable member must succeed: %v", err)
	}
	if err := f.vm.storageDrop(src); err != nil {
		t.Fatalf("dropping the source must succeed: %v", err)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("a failed copy left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// Partial initialization is the shape a user panic leaves behind when it
// interrupts construction: some members written, the rest still zeroed. The
// value must be droppable, and the drop must release exactly the members that
// were written — a zeroed handle cell names nothing, so walking it must be a
// no-op rather than a release of whatever handle 0 might be taken for.
func TestStorageDropOfAPartiallyInitializedValueReleasesOnlyWhatWasWritten(t *testing.T) {
	f := newStorageFixture(t)
	ref := f.ref(t, 0, f.node)

	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	leafRef, err := ref.memberRef(members[0])
	if err != nil {
		t.Fatalf("projecting the nested value must succeed: %v", err)
	}
	leafMembers, err := f.vm.compositeMembers(f.leaf)
	if err != nil {
		t.Fatalf("Leaf must have describable members: %v", err)
	}

	// Construction gets as far as the first scalar and then stops.
	f.writeCell(t, leafRef, leafMembers[0], MakeInt(11, types.NoTypeID))

	if err := f.vm.storageDrop(ref); err != nil {
		t.Fatalf("dropping a partially initialized value must succeed: %v", err)
	}
	if f.vm.heapCounters.freeCount != 0 {
		t.Fatalf("dropping storage that owns nothing released %d objects", f.vm.heapCounters.freeCount)
	}

	// Now the same value with the handle member written but the rest not.
	handle := f.vm.Heap.AllocString(types.NoTypeID, "half-built")
	f.writeCell(t, ref, members[1], MakeHandleString(handle, types.NoTypeID))
	if err := f.vm.storageDrop(ref); err != nil {
		t.Fatalf("dropping a half-built value must succeed: %v", err)
	}
	if obj, ok := f.vm.Heap.lookup(handle); !ok || !obj.Freed {
		t.Fatal("the one member that was written was not released")
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("partial initialization left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}
