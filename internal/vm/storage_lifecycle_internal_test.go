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
