package vm

import (
	"testing"

	"surge/internal/types"
)

func TestStorageMoveTransfersOnlyTheActiveUnionArm(t *testing.T) {
	f := newStorageFixture(t)
	src := f.ref(t, 2, f.choice)
	dst := f.ref(t, 3, f.choice)
	shape, err := f.vm.unionMembers(f.choice)
	if err != nil {
		t.Fatalf("describe Choice: %v", err)
	}
	wrapped, err := f.vm.storageCaseIndexOf(f.choice, shape, f.wrap)
	if err != nil {
		t.Fatalf("resolve Wrapped arm: %v", err)
	}
	if activateErr := f.vm.storageSetActiveCase(src, shape, wrapped); activateErr != nil {
		t.Fatalf("select source arm: %v", activateErr)
	}
	payload := shape.Cases[wrapped].Payload
	if len(payload) != 1 {
		t.Fatalf("Wrapped payload has %d members, want 1", len(payload))
	}
	srcLeaf, err := src.memberRef(payload[0])
	if err != nil {
		t.Fatalf("project source payload: %v", err)
	}
	leafMembers, err := f.vm.compositeMembers(f.leaf)
	if err != nil {
		t.Fatalf("describe Leaf: %v", err)
	}
	f.writeCell(t, srcLeaf, leafMembers[0], MakeInt(17, types.NoTypeID))
	f.writeCell(t, srcLeaf, leafMembers[1], MakeInt(23, types.NoTypeID))

	if moveErr := f.vm.storageMoveInit(dst, src); moveErr != nil {
		t.Fatalf("move exact union storage: %v", moveErr)
	}
	if got, activeErr := f.vm.storageActiveCase(dst, shape); activeErr != nil || got != wrapped {
		t.Fatalf("destination active arm = %d (%v), want %d", got, activeErr, wrapped)
	}
	dstLeaf, err := dst.memberRef(payload[0])
	if err != nil {
		t.Fatalf("project destination payload: %v", err)
	}
	if got := f.readCell(t, dstLeaf, leafMembers[0]); got.Int != 17 {
		t.Fatalf("destination first field = %d, want 17", got.Int)
	}
	if got := f.readCell(t, dstLeaf, leafMembers[1]); got.Int != 23 {
		t.Fatalf("destination second field = %d, want 23", got.Int)
	}
	if got := f.readCell(t, srcLeaf, leafMembers[0]); got.Int != 0 {
		t.Fatalf("source first field remained %d after move", got.Int)
	}
	if got := f.readCell(t, srcLeaf, leafMembers[1]); got.Int != 0 {
		t.Fatalf("source second field remained %d after move", got.Int)
	}
}
