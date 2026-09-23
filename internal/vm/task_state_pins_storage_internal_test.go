package vm

import (
	"testing"

	"surge/internal/types"
)

// A task that borrows one cell of inline storage -- a string field, or a local
// of an async function the split moved into its state -- holds a reference whose
// target is that cell, not a composite. The walk must read the cell, retain the
// handle it holds exactly once, and give it back on release (RV2-DEBT-369).
func TestCollectTaskStatePinsWalksALeafStorageReferent(t *testing.T) {
	f := newStorageFixture(t)
	node := f.ref(t, 0, f.node)
	label := f.writeNode(t, node, 1, 2, 3, "label")
	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	before := f.vm.Heap.Get(label).RefCount

	for _, tc := range []struct {
		name    string
		member  storageMember
		handles int
	}{
		{name: "string", member: members[1], handles: 1},
		{name: "int", member: members[2], handles: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cell, err := node.memberRef(tc.member)
			if err != nil {
				t.Fatalf("projecting the member must succeed: %v", err)
			}
			pins, vmErr := f.vm.collectTaskStatePins(MakeRef(Location{Kind: LKStorage, Storage: cell}, types.NoTypeID))
			if vmErr != nil {
				t.Fatalf("a borrow of one %s cell must be walkable: %v", tc.name, vmErr)
			}
			if len(pins.arenas) != 1 || len(pins.handles) != tc.handles {
				t.Fatalf("want 1 arena pin and %d handle, got %d and %d", tc.handles, len(pins.arenas), len(pins.handles))
			}
			if got, want := f.vm.Heap.Get(label).RefCount, before+uint32(tc.handles); got != want {
				t.Fatalf("label refcount while pinned: want %d, got %d", want, got)
			}
			f.vm.releaseTaskStatePins(pins)
			if got := f.vm.Heap.Get(label).RefCount; got != before {
				t.Fatalf("label refcount after release: want %d, got %d", before, got)
			}
		})
	}
}

// A composite and its first member start at the same offset. A state that
// borrows the member before the composite must still walk the composite, or the
// composite's other members go unretained.
func TestCollectTaskStatePinsKeysExtentsByType(t *testing.T) {
	f := newStorageFixture(t)
	node := f.ref(t, 0, f.node)
	label := f.writeNode(t, node, 1, 2, 3, "label")
	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	leaf, err := node.memberRef(members[0])
	if err != nil {
		t.Fatalf("projecting the nested value must succeed: %v", err)
	}
	if leaf.Offset != node.Offset {
		t.Fatalf("the premise needs the first member at the composite's offset: %d != %d", leaf.Offset, node.Offset)
	}
	before := f.vm.Heap.Get(label).RefCount

	c := taskStatePinCollector{
		vm:              f.vm,
		visitedLocals:   make(map[pinnedLocal]struct{}),
		visitedHandles:  make(map[Handle]struct{}),
		retainedHandles: make(map[Handle]struct{}),
		visitedArenas:   make(map[*Arena]struct{}),
		visitedExtents:  make(map[storageExtent]struct{}),
	}
	for _, target := range []StorageRef{leaf, node} {
		if vmErr := c.visitValue(MakeRef(Location{Kind: LKStorage, Storage: target}, types.NoTypeID)); vmErr != nil {
			t.Fatalf("walking the state must succeed: %v", vmErr)
		}
	}
	if got := f.vm.Heap.Get(label).RefCount; got != before+1 {
		t.Fatalf("the composite behind its first member was not walked: label refcount want %d, got %d", before+1, got)
	}
	f.vm.releaseTaskStatePins(c.pins)
	if got := f.vm.Heap.Get(label).RefCount; got != before {
		t.Fatalf("label refcount after release: want %d, got %d", before, got)
	}
}
