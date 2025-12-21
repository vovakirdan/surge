package vm

import "surge/internal/types"

type bytesViewLayoutInfo struct {
	layout   *StructLayout
	ownerIdx int
	ptrIdx   int
	lenIdx   int
	ok       bool
}

func (vm *VM) bytesViewLayout(typeID types.TypeID) (bytesViewLayoutInfo, *VMError) {
	layout, vmErr := vm.layouts.Struct(typeID)
	if vmErr != nil {
		return bytesViewLayoutInfo{}, vmErr
	}
	info := bytesViewLayoutInfo{layout: layout}
	if len(layout.FieldNames) != 3 {
		return info, nil
	}
	ownerIdx, okOwner := layout.IndexByName["owner"]
	ptrIdx, okPtr := layout.IndexByName["ptr"]
	lenIdx, okLen := layout.IndexByName["len"]
	if !okOwner || !okPtr || !okLen {
		return info, nil
	}
	info.ownerIdx = ownerIdx
	info.ptrIdx = ptrIdx
	info.lenIdx = lenIdx
	info.ok = true
	if vm.Types == nil {
		return info, nil
	}

	builtins := vm.Types.Builtins()
	if vm.valueType(layout.FieldTypes[ownerIdx]) != builtins.String {
		return bytesViewLayoutInfo{layout: layout}, nil
	}
	if vm.valueType(layout.FieldTypes[lenIdx]) != builtins.Uint {
		return bytesViewLayoutInfo{layout: layout}, nil
	}
	ptrType, ok := vm.Types.Lookup(layout.FieldTypes[ptrIdx])
	if !ok || ptrType.Kind != types.KindPointer {
		return bytesViewLayoutInfo{layout: layout}, nil
	}
	if vm.valueType(ptrType.Elem) != builtins.Uint8 {
		return bytesViewLayoutInfo{layout: layout}, nil
	}
	return info, nil
}
