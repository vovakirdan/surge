package vm

import (
	"surge/internal/types"
)

// A map's entries are TWO runs, one of keys and one of values, indexed
// together: entry `i` is key slot `i` beside value slot `i`.
//
// They are two runs rather than one run of pairs because a pair would need a
// type of its own, and interning a struct type per key/value combination would
// make the type interner grow with the program's data. Two runs also keep each
// side answering from its own declared type, which is the property the whole
// flip is for: a key is addressed as the map's key type and a value as the
// map's value type, never as whatever the slot happens to hold.
//
// `MapIndex` is unchanged and owns nothing. It maps a DERIVED lookup key to a
// position, and a position is what both runs are addressed by.

// reserveMapRuns gives a map storage for `capacity` entries.
func (vm *VM) reserveMapRuns(obj *Object, keyType, valType types.TypeID, capacity int) *VMError {
	if obj.storage == nil {
		obj.storage = newScratch()
	}
	want := safeUint64FromInt(max(capacity, 0))
	keys, err := vm.reserveRun(obj.storage, keyType, want)
	if err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	vals, err := vm.reserveRun(obj.storage, valType, want)
	if err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	obj.MapKeys, obj.MapVals = keys, vals
	obj.MapKeyType, obj.MapValType = keyType, valType
	obj.MapLen = 0
	obj.MapCap = max(capacity, 0)
	return nil
}

// mapKeySlot and mapValSlot address the two halves of entry `index`.
//
// Neither takes a type from its caller. The map's own key and value types are
// the only answer, which is what stops one side of an entry from being written
// as one thing and read back as another.
func (vm *VM) mapKeySlot(obj *Object, index int) (StorageRef, *VMError) {
	return vm.mapSlot(obj, obj.MapKeys, obj.MapKeyType, index)
}

func (vm *VM) mapValSlot(obj *Object, index int) (StorageRef, *VMError) {
	return vm.mapSlot(obj, obj.MapVals, obj.MapValType, index)
}

func (vm *VM) mapSlot(obj *Object, base StorageRef, elem types.TypeID, index int) (StorageRef, *VMError) {
	if obj == nil || base.Arena == nil {
		return StorageRef{}, vm.eb.makeError(PanicOutOfBounds, "this map has no entry storage")
	}
	if index < 0 || index >= obj.MapCap {
		return StorageRef{}, vm.eb.outOfBounds(index, obj.MapCap)
	}
	ref, err := vm.runElemRef(base, elem, index)
	if err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return ref, nil
}

// setMapLen records the entry count in the object AND in both runs' entries, so
// a teardown owes exactly the live keys and the live values.
func (vm *VM) setMapLen(obj *Object, length int) *VMError {
	if obj == nil || obj.storage == nil {
		return nil
	}
	live := safeUint64FromInt(max(length, 0))
	if err := obj.storage.setRunLive(obj.MapKeys, live); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if err := obj.storage.setRunLive(obj.MapVals, live); err != nil {
		return vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	obj.MapLen = max(length, 0)
	return nil
}

// growMapRuns makes room for at least minCap entries, moving both runs.
//
// The two runs grow TOGETHER and in one step, because a position names a slot
// in each of them: growing one alone would leave entry `i` split across an old
// run and a new one. Each half follows the same order the array run does —
// retire the old extent before reserving, so a run that occupied no bytes
// cannot have its replacement mistaken for it.
func (vm *VM) growMapRuns(obj *Object, minCap int) *VMError {
	if obj == nil || minCap <= obj.MapCap {
		return nil
	}
	if obj.storage == nil {
		return vm.eb.makeError(PanicUnimplemented, "a map must own storage before it can grow")
	}
	newCap := growRunCapacity(obj.MapCap, minCap)
	oldLen := obj.MapLen

	keys, vmErr := vm.moveRun(obj.storage, obj.MapKeys, obj.MapKeyType, oldLen, newCap)
	if vmErr != nil {
		return vmErr
	}
	vals, vmErr := vm.moveRun(obj.storage, obj.MapVals, obj.MapValType, oldLen, newCap)
	if vmErr != nil {
		return vmErr
	}
	obj.MapKeys, obj.MapVals = keys, vals
	obj.MapCap = newCap
	return vm.setMapLen(obj, oldLen)
}

// moveRun retires one run and hands back a larger one holding the same values.
func (vm *VM) moveRun(store *scratch, old StorageRef, elem types.TypeID, length, newCap int) (StorageRef, *VMError) {
	if releaseErr := store.release(old); releaseErr != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, releaseErr.Error())
	}
	next, err := vm.reserveRun(store, elem, safeUint64FromInt(max(newCap, 0)))
	if err != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	if length <= 0 {
		return next, nil
	}
	stride, strideErr := vm.runStride(elem)
	if strideErr != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, strideErr.Error())
	}
	span, ok := mulChecked(stride, safeUint64FromInt(length))
	if !ok {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, "a run overflows while growing")
	}
	// Both extents are resolved only AFTER the reservation, because reserving
	// can replace the arena's byte slice.
	src, resolveErr := old.resolve(span)
	if resolveErr != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, resolveErr.Error())
	}
	dst, resolveErr := next.resolve(span)
	if resolveErr != nil {
		return StorageRef{}, vm.eb.makeError(PanicUnimplemented, resolveErr.Error())
	}
	copy(dst, src)
	return next, nil
}

// initMapEntry writes a key and a value into slots that hold NOTHING.
func (vm *VM) initMapEntry(frame *Frame, obj *Object, index int, key, val Value) *VMError {
	if vmErr := vm.initRunSlot(frame, obj.MapKeys, obj.MapKeyType, index, key); vmErr != nil {
		return vmErr
	}
	return vm.initRunSlot(frame, obj.MapVals, obj.MapValType, index, val)
}

// replaceMapValue writes over an initialised value slot, releasing what it held.
func (vm *VM) replaceMapValue(frame *Frame, obj *Object, index int, val Value) *VMError {
	ref, vmErr := vm.mapValSlot(obj, index)
	if vmErr != nil {
		return vmErr
	}
	return vm.storeStorage(frame, ref, val)
}

// takeMapEntry moves both halves of entry `index` out, leaving the slots zeroed.
func (vm *VM) takeMapEntry(frame *Frame, obj *Object, index int) (key, val Value, vmErr *VMError) {
	key, vmErr = vm.takeRunSlot(frame, obj.MapKeys, obj.MapKeyType, index)
	if vmErr != nil {
		return Value{}, Value{}, vmErr
	}
	val, vmErr = vm.takeRunSlot(frame, obj.MapVals, obj.MapValType, index)
	if vmErr != nil {
		vm.dropValue(key)
		return Value{}, Value{}, vmErr
	}
	return key, val, nil
}

// copyMapEntry moves entry `from` onto entry `to` as raw bytes.
//
// It is the swap-with-last a removal performs. The destination has already been
// emptied by the take that removed it, so this owes no release; the source
// slots are left holding stale bytes that the shortened length makes dead.
func (vm *VM) copyMapEntry(obj *Object, to, from int) *VMError {
	if vmErr := vm.copyRunSlot(obj.MapKeys, obj.MapKeyType, to, from); vmErr != nil {
		return vmErr
	}
	return vm.copyRunSlot(obj.MapVals, obj.MapValType, to, from)
}
