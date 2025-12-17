package vm

import (
	"fmt"
	"strings"

	"surge/internal/mir"
	"surge/internal/types"
)

func (vm *VM) execDrop(frame *Frame, localID mir.LocalID) *VMError {
	if int(localID) < 0 || int(localID) >= len(frame.Locals) {
		return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", localID))
	}
	slot := &frame.Locals[localID]
	if !slot.IsInit {
		return vm.eb.useBeforeInit(slot.Name)
	}
	if slot.IsMoved {
		return vm.eb.useAfterMove(slot.Name)
	}

	if vm.localOwnsHeap(frame.Func.Locals[localID]) {
		vm.dropValue(slot.V)
	}

	slot.V = Value{}
	slot.IsInit = false
	slot.IsMoved = false
	return nil
}

func (vm *VM) dropFrameLocals(frame *Frame) {
	if frame == nil || frame.Func == nil {
		return
	}
	for id := range frame.Locals {
		slot := &frame.Locals[id]
		if !slot.IsInit || slot.IsMoved {
			continue
		}
		if !vm.localOwnsHeap(frame.Func.Locals[id]) {
			continue
		}
		vm.dropValue(slot.V)
		slot.V = Value{}
		slot.IsInit = false
		slot.IsMoved = false
	}
}

func (vm *VM) dropAllFrames() {
	for i := len(vm.Stack) - 1; i >= 0; i-- {
		vm.dropFrameLocals(&vm.Stack[i])
	}
}

func (vm *VM) dropValue(v Value) {
	switch v.Kind {
	case VKHandleString, VKHandleArray, VKHandleStruct:
		if v.H != 0 {
			vm.Heap.Free(v.H)
		}
	}
}

func (vm *VM) localOwnsHeap(local mir.Local) bool {
	if local.Flags&(mir.LocalFlagRef|mir.LocalFlagRefMut|mir.LocalFlagPtr) != 0 {
		return false
	}
	if local.Flags&mir.LocalFlagCopy != 0 {
		return false
	}
	switch localTy := vm.valueType(local.Type); {
	case localTy == types.NoTypeID:
		return false
	default:
		if vm.Types == nil {
			return true
		}
		tt, ok := vm.Types.Lookup(localTy)
		if !ok {
			return true
		}
		switch tt.Kind {
		case types.KindString, types.KindStruct:
			return true
		case types.KindArray:
			return tt.Count == types.ArrayDynamicLength
		default:
			return false
		}
	}
}

func (vm *VM) checkLeaksOrPanic() {
	if vm.Heap == nil {
		return
	}
	aliveCount := 0
	const maxList = 8
	list := make([]string, 0, maxList)
	for h := Handle(1); h < vm.Heap.next; h++ {
		obj, ok := vm.Heap.lookup(h)
		if !ok || obj == nil || !obj.Alive {
			continue
		}
		aliveCount++
		if len(list) < maxList {
			list = append(list, fmt.Sprintf("%s#%d(type=type#%d)", vm.objectKindLabel(obj.Kind), h, obj.TypeID))
		}
	}
	if aliveCount == 0 {
		return
	}
	msg := fmt.Sprintf("memory leak detected: %d objects still alive", aliveCount)
	if len(list) > 0 {
		msg += ": " + strings.Join(list, ", ")
	}
	vm.panic(PanicMemoryLeakDetected, msg)
}

func (vm *VM) objectKindLabel(k ObjectKind) string {
	switch k {
	case OKString:
		return "string"
	case OKArray:
		return "array"
	case OKStruct:
		return "struct"
	default:
		return "object"
	}
}
