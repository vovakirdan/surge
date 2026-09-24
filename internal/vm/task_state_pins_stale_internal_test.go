package vm

import (
	"strings"
	"testing"
)

// RV2-DEBT-372. A task state may HOLD a location into storage that is gone: a
// child parked while its parent was cancelled wakes to the same cancel after its
// parent's state was released, and reaches its own yield still holding the
// location its borrow made. The yield collects the state's pins before it looks
// at the cancellation, so the collector must pass that location over -- no pin,
// no retain, no error -- because holding it is not a use. Reading it is, and
// must still fail as stale (storage model section 7: "stale locations fail
// deterministically").
func TestTaskStatePinsPassOverAStaleLocation(t *testing.T) {
	f := newStorageFixture(t)
	members, err := f.vm.compositeMembers(f.node)
	if err != nil {
		t.Fatalf("Node must have describable members: %v", err)
	}
	size, err := f.vm.storageSizeOf(f.node)
	if err != nil {
		t.Fatalf("Node must have a size: %v", err)
	}
	gone := newArena(&StoragePlan{Size: size, Align: 8}, 1)
	node, err := f.vm.storageRefAt(gone, 0, f.node)
	if err != nil {
		t.Fatalf("naming the node must succeed: %v", err)
	}
	f.writeNode(t, node, 1, 2, 3, "resident")
	lent, err := node.memberRef(members[1])
	if err != nil {
		t.Fatalf("lending the label cell must succeed: %v", err)
	}
	if err := f.vm.storageDrop(node); err != nil {
		t.Fatalf("dropping the node must succeed: %v", err)
	}
	if !gone.retire() {
		t.Fatal("an arena nothing pins must retire")
	}

	held := MakeRef(Location{Kind: LKStorage, Storage: lent}, lent.TypeID)
	pins, vmErr := f.vm.collectTaskStatePins(held)
	if vmErr != nil {
		t.Fatalf("PVMR-UNIT the pin collector refused a stale location it only holds: %v", vmErr.Message)
	}
	if len(pins.arenas) != 0 || len(pins.handles) != 0 || gone.pins != 0 {
		t.Errorf("PVMR-UNIT the pin collector took %d arena pin(s) and %d handle(s) for a stale location (arena pins %d)",
			len(pins.arenas), len(pins.handles), gone.pins)
	}
	f.vm.releaseTaskStatePins(pins)
	got, readErr := f.vm.storageReadCell(lent, members[1])
	if readErr == nil || !strings.Contains(readErr.Error(), "stale reference") {
		t.Errorf("PVMR-UNIT reading a stale location must fail as stale, got %v (err %v)", got, readErr)
	}
}
