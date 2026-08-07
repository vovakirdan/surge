package vm

import (
	"fmt"
	"math"

	"fortio.org/safecast"

	"surge/internal/mir"
	"surge/internal/types"
)

// projectIndexLocation resolves one index projection against an array base.
func (vm *VM) projectIndexLocation(frame *Frame, base Location, proj mir.PlaceProj) (Location, *VMError) {
	v, vmErr := vm.loadLocationRaw(base)
	if vmErr != nil {
		return Location{}, vmErr
	}
	for i := 0; i < 8 && (v.Kind == VKRef || v.Kind == VKRefMut); i++ {
		v, vmErr = vm.loadLocationRaw(v.Loc)
		if vmErr != nil {
			return Location{}, vmErr
		}
	}
	// A FIXED array is a value composite, so indexing one is arithmetic on its
	// own extent — the same operation as a field projection, differing only in
	// how the member index is spelled. A dynamic array is still a heap object
	// and keeps the path below.
	if owner, isComposite := v.Storage(); isComposite {
		return vm.projectStorageIndex(frame, owner, proj, base.IsMut)
	}
	if v.Kind != VKHandleArray {
		return Location{}, vm.eb.invalidLocation(fmt.Sprintf("index projection on non-array value (got %s)", v.Kind))
	}

	obj, vmErr := vm.heapAliveForRef(v.H)
	if vmErr != nil {
		return Location{}, vmErr
	}

	idxLocal := proj.IndexLocal
	idxVal, vmErr := vm.readLocal(frame, idxLocal)
	if vmErr != nil {
		return Location{}, vmErr
	}
	if obj.Kind != OKArray && obj.Kind != OKArraySlice {
		return Location{}, vm.eb.invalidLocation(fmt.Sprintf("index projection on %v", obj.Kind))
	}
	view, vmErr := vm.arrayViewFromHandle(v.H)
	if vmErr != nil {
		return Location{}, vmErr
	}
	idx, vmErr := vm.arrayIndexFromValue(idxVal, view.length)
	if vmErr != nil {
		return Location{}, vmErr
	}

	baseIdx := view.start + idx
	idx32, err := safecast.Conv[int32](baseIdx)
	if err != nil {
		return Location{}, vm.eb.invalidLocation("index projection: index overflow")
	}

	byteOffset, vmErr := vm.arrayElemByteOffset(v.TypeID, baseIdx)
	if vmErr != nil {
		return Location{}, vmErr
	}
	return Location{
		Kind:       LKArrayElem,
		Handle:     view.baseHandle,
		Index:      idx32,
		ByteOffset: byteOffset,
		IsMut:      base.IsMut,
	}, nil
}

// arrayElemByteOffset returns the ABI byte offset of one element of an array type.
func (vm *VM) arrayElemByteOffset(arrayValueType types.TypeID, baseIdx int) (int32, *VMError) {
	if vm.Layouts == nil || vm.Types == nil {
		return 0, vm.eb.invalidLocation("index projection: missing finalized layout registry or type interner")
	}
	elemType := types.NoTypeID
	arrType := vm.valueType(arrayValueType)
	if t, ok := vm.Types.ArrayInfo(arrType); ok {
		elemType = t
	} else if t, _, ok := vm.Types.ArrayFixedInfo(arrType); ok {
		elemType = t
	} else if tt, ok := vm.Types.Lookup(arrType); ok && tt.Kind == types.KindArray {
		elemType = tt.Elem
	}
	if elemType == types.NoTypeID {
		return 0, vm.eb.invalidLocation("index projection: missing element type")
	}
	el, err := vm.Layouts.Require(elemType)
	if err != nil {
		return 0, vm.eb.invalidLocation(fmt.Sprintf("index projection: %v", err))
	}
	baseIndex, err := safecast.Conv[uint64](baseIdx)
	if err != nil {
		return 0, vm.eb.invalidLocation("index projection: negative byte offset")
	}
	if el.Stride != 0 && baseIndex > math.MaxUint64/el.Stride {
		return 0, vm.eb.invalidLocation("index projection: byte offset overflow")
	}
	off := el.Stride * baseIndex
	byteOffset, err := safecast.Conv[int32](off)
	if err != nil {
		return 0, vm.eb.invalidLocation("index projection: byte offset overflow")
	}
	return byteOffset, nil
}

// loadArrayElem reads one array element through a resolved location.
func (vm *VM) loadArrayElem(loc Location) (Value, *VMError) {
	obj, vmErr := vm.heapAliveForRef(loc.Handle)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if obj.Kind != OKArray && obj.Kind != OKArraySlice {
		return Value{}, vm.eb.invalidLocation(fmt.Sprintf("expected array handle, got %v", obj.Kind))
	}
	view, vmErr := vm.arrayViewFromHandle(loc.Handle)
	if vmErr != nil {
		return Value{}, vmErr
	}
	idx := int(loc.Index)
	if loc.Index < 0 || idx < 0 || idx >= view.length {
		return Value{}, vm.eb.arrayIndexOutOfRange(idx, view.length)
	}
	val := view.baseObj.Arr[view.start+idx]
	if vm.Types != nil && view.baseObj != nil {
		if elemType, ok := vm.Types.ArrayInfo(view.baseObj.TypeID); ok {
			if retagged, ok := vm.retagUnionValue(val, elemType); ok {
				val = retagged
			}
		}
	}
	return val, nil
}

// storeArrayElem writes one array element through a resolved location.
func (vm *VM) storeArrayElem(loc Location, val Value) *VMError {
	obj, vmErr := vm.heapAliveForRef(loc.Handle)
	if vmErr != nil {
		return vmErr
	}
	if obj.Kind != OKArray && obj.Kind != OKArraySlice {
		return vm.eb.invalidLocation(fmt.Sprintf("expected array handle, got %v", obj.Kind))
	}
	view, vmErr := vm.arrayViewFromHandle(loc.Handle)
	if vmErr != nil {
		return vmErr
	}
	idx := int(loc.Index)
	if loc.Index < 0 || idx < 0 || idx >= view.length {
		return vm.eb.arrayIndexOutOfRange(idx, view.length)
	}
	if vm.Types != nil && view.baseObj != nil {
		if elemType, ok := vm.Types.ArrayInfo(view.baseObj.TypeID); ok {
			if retagged, ok := vm.retagUnionValue(val, elemType); ok {
				val = retagged
			}
		}
	}
	adopted, vmErr := vm.adoptIntoContainer(view.baseObj, val)
	if vmErr != nil {
		return vmErr
	}
	baseIdx := view.start + idx
	vm.dropValue(view.baseObj.Arr[baseIdx])
	view.baseObj.Arr[baseIdx] = adopted
	return nil
}

// loadMapElem reads one map entry value through a resolved location.
func (vm *VM) loadMapElem(loc Location) (Value, *VMError) {
	obj, vmErr := vm.heapAliveForRef(loc.Handle)
	if vmErr != nil {
		return Value{}, vmErr
	}
	if obj.Kind != OKMap {
		return Value{}, vm.eb.invalidLocation(fmt.Sprintf("expected map handle, got %v", obj.Kind))
	}
	idx := int(loc.Index)
	if loc.Index < 0 || idx < 0 || idx >= len(obj.MapEntries) {
		return Value{}, vm.eb.outOfBounds(idx, len(obj.MapEntries))
	}
	val := obj.MapEntries[idx].Value
	if vm.Types != nil {
		if _, valueType, ok := vm.Types.MapInfo(obj.TypeID); ok {
			if retagged, ok := vm.retagUnionValue(val, valueType); ok {
				val = retagged
			}
		}
	}
	return val, nil
}

// storeMapElem writes one map entry value through a resolved location.
func (vm *VM) storeMapElem(loc Location, val Value) *VMError {
	obj, vmErr := vm.heapAliveForRef(loc.Handle)
	if vmErr != nil {
		return vmErr
	}
	if obj.Kind != OKMap {
		return vm.eb.invalidLocation(fmt.Sprintf("expected map handle, got %v", obj.Kind))
	}
	idx := int(loc.Index)
	if loc.Index < 0 || idx < 0 || idx >= len(obj.MapEntries) {
		return vm.eb.outOfBounds(idx, len(obj.MapEntries))
	}
	if vm.Types != nil {
		if _, valueType, ok := vm.Types.MapInfo(obj.TypeID); ok {
			if retagged, ok := vm.retagUnionValue(val, valueType); ok {
				val = retagged
			}
		}
	}
	vm.dropValue(obj.MapEntries[idx].Value)
	obj.MapEntries[idx].Value = val
	return nil
}

// projectStorageIndex names one element of a fixed array held in storage.
//
// The bound comes from the LAYOUT rather than from a length word: a fixed array
// has as many members as its type says and no runtime length to disagree with,
// which is the whole reason it can live inline.
func (vm *VM) projectStorageIndex(frame *Frame, owner StorageRef, proj mir.PlaceProj, isMut bool) (Location, *VMError) {
	members, err := vm.compositeMembers(owner.TypeID)
	if err != nil {
		return Location{}, vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	idxVal, vmErr := vm.readLocal(frame, proj.IndexLocal)
	if vmErr != nil {
		return Location{}, vmErr
	}
	idx, vmErr := vm.arrayIndexFromValue(idxVal, len(members))
	if vmErr != nil {
		return Location{}, vmErr
	}
	member, vmErr := vm.memberStorage(owner, idx)
	if vmErr != nil {
		return Location{}, vmErr
	}
	idx32, convErr := safecast.Conv[int32](idx)
	if convErr != nil {
		return Location{}, vm.eb.invalidLocation("index projection: index overflow")
	}
	byteOffset, convErr := safecast.Conv[int32](member.Offset)
	if convErr != nil {
		return Location{}, vm.eb.invalidLocation("index projection: byte offset overflow")
	}
	return Location{
		Kind:       LKStorage,
		Storage:    member,
		Index:      idx32,
		ByteOffset: byteOffset,
		IsMut:      isMut,
	}, nil
}
