package vm

import (
	"fmt"
	"math"

	"surge/internal/types"
)

// asyncOwnerRegion is one homogeneous logical owner. Backing may grow, but it
// never becomes the owner: identity, type, slots, and generations stay here.
type asyncOwnerRegion struct {
	id         asyncOwnerID
	generation uint32
	typeID     types.TypeID
	cell       storageMember
	stride     uint64
	arena      Arena
	slots      []asyncPayloadSlot
	retiring   bool
	destroying bool
	retired    bool
}

func (vm *VM) newAsyncOwnerRegion(
	id asyncOwnerID,
	typeID types.TypeID,
	capacity int,
) (*asyncOwnerRegion, *VMError) {
	if vm == nil || vm.Layouts == nil || typeID == types.NoTypeID {
		return nil, vm.eb.invalidLocation("async owner has no exact payload layout")
	}
	cell, err := vm.memberAt(0, typeID)
	if err != nil {
		return nil, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	stride, ok := alignUpChecked(cell.Size, cell.Align)
	if !ok {
		return nil, vm.eb.invalidLocation("async owner slot stride overflows")
	}
	if capacity < 1 {
		capacity = 1
	}
	if uint64(capacity) > math.MaxUint32 {
		return nil, vm.eb.invalidLocation("async owner capacity exceeds claim index")
	}
	byteLen, ok := mulAsyncExtent(stride, uint64(capacity))
	byteCapacity, fitsInt := checkedAsyncInt(byteLen)
	if !ok || !fitsInt {
		return nil, vm.eb.invalidLocation("async owner backing size overflows")
	}
	owner := &asyncOwnerRegion{
		id: id, typeID: typeID, cell: cell, stride: stride,
		arena: Arena{bytes: make([]byte, byteCapacity), gen: 1},
		slots: make([]asyncPayloadSlot, capacity),
	}
	for i := range owner.slots {
		owner.slots[i].generation = 1
	}
	return owner, nil
}

func (vm *VM) reserveAsyncPayload(
	owner *asyncOwnerRegion,
	role asyncSlotRole,
) (asyncPayload, *asyncPayloadSlot, StorageRef, *VMError) {
	if owner == nil || owner.retiring || owner.retired {
		return asyncPayload{}, nil, StorageRef{}, vm.eb.invalidLocation("async payload has no concrete owner")
	}
	if !asyncSlotRoleAllowed(owner.id.kind, role) {
		return asyncPayload{}, nil, StorageRef{}, vm.eb.invalidLocation("async slot role does not match its owner")
	}
	index := -1
	for i := range owner.slots {
		if owner.slots[i].state == asyncPayloadEmpty {
			index = i
			break
		}
	}
	if index < 0 {
		var vmErr *VMError
		index, vmErr = vm.growAsyncOwner(owner)
		if vmErr != nil {
			return asyncPayload{}, nil, StorageRef{}, vmErr
		}
	}
	slot := &owner.slots[index]
	slot.state = asyncPayloadReserved
	slot.role = role
	ref, err := vm.asyncSlotRef(owner, index)
	if err != nil {
		vm.abortAsyncReservation(owner, slot)
		return asyncPayload{}, nil, StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	payload := asyncPayload{
		ownerKind: owner.id.kind, ownerID: owner.id.id,
		ownerGeneration: owner.generation, region: owner.id.arm,
		index: uint32(index), slotGeneration: slot.generation,
	}
	return payload, slot, ref, nil
}

func (vm *VM) growAsyncOwner(owner *asyncOwnerRegion) (int, *VMError) {
	old := len(owner.slots)
	if old == 0 || old > math.MaxUint32/2 {
		return -1, vm.eb.invalidLocation("async owner slot count overflows")
	}
	next := old * 2
	byteLen, ok := mulAsyncExtent(owner.stride, uint64(next))
	byteCapacity, fitsInt := checkedAsyncInt(byteLen)
	if !ok || !fitsInt {
		return -1, vm.eb.invalidLocation("async owner backing size overflows")
	}
	owner.arena.bytes = append(owner.arena.bytes, make([]byte, byteCapacity-len(owner.arena.bytes))...)
	owner.slots = append(owner.slots, make([]asyncPayloadSlot, old)...)
	for i := old; i < next; i++ {
		owner.slots[i].generation = 1
	}
	return old, nil
}

func (vm *VM) asyncSlotRef(owner *asyncOwnerRegion, index int) (StorageRef, error) {
	if owner == nil || index < 0 || index >= len(owner.slots) {
		return StorageRef{}, fmt.Errorf("async owner slot %d is out of range", index)
	}
	offset, ok := mulAsyncExtent(owner.stride, uint64(index))
	if !ok {
		return StorageRef{}, fmt.Errorf("async owner slot %d offset overflows", index)
	}
	return vm.storageRefAt(&owner.arena, offset, owner.typeID)
}

func mulAsyncExtent(left, right uint64) (uint64, bool) {
	if left != 0 && right > math.MaxUint64/left {
		return 0, false
	}
	return left * right, true
}

func maxIntValue() int { return int(^uint(0) >> 1) }

func checkedAsyncInt(value uint64) (int, bool) {
	if value > uint64(maxIntValue()) { //nolint:gosec // the positive int bound widens without loss
		return 0, false
	}
	return int(value), true //nolint:gosec // the comparison above proves this conversion
}
