package vm

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/mir"
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
	if slot.IsDropped {
		return vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: local %q used after drop", slot.Name))
	}

	vm.dropValue(slot.V)
	slot.IsDropped = true
	return nil
}

func (vm *VM) dropFrameLocals(frame *Frame) {
	if frame == nil || frame.Func == nil {
		return
	}
	// Contract: implicit drops run in strictly reverse local order.
	for id := len(frame.Locals) - 1; id >= 0; id-- {
		slot := &frame.Locals[id]
		if !slot.IsInit || slot.IsMoved || slot.IsDropped {
			continue
		}
		vm.dropValue(slot.V)
		slot.V = Value{}
		slot.IsInit = false
		slot.IsMoved = false
		slot.IsDropped = false
	}
}

func (vm *VM) dropAllFrames() {
	for i := len(vm.Stack) - 1; i >= 0; i-- {
		vm.dropFrameLocals(&vm.Stack[i])
	}
}

func (vm *VM) dropValue(v Value) {
	if vm == nil || vm.Heap == nil || !v.IsHeap() || v.H == 0 {
		return
	}
	vm.Heap.Release(v.H)
}

func (vm *VM) checkLeaksOrPanic() {
	if vm.Heap == nil {
		return
	}
	leakCount := 0
	kindCounts := make(map[ObjectKind]int, 8)
	const maxList = 8
	list := make([]string, 0, maxList)
	for h := Handle(1); h < vm.Heap.next; h++ {
		obj, ok := vm.Heap.lookup(h)
		if !ok || obj == nil || obj.RefCount == 0 {
			continue
		}
		leakCount++
		kindCounts[obj.Kind]++
		if len(list) < maxList {
			list = append(list, fmt.Sprintf("%s#%d(rc=%d,type=type#%d)", vm.objectKindLabel(obj.Kind), h, obj.RefCount, obj.TypeID))
		}
	}
	if leakCount == 0 {
		return
	}
	msg := fmt.Sprintf("heap leak detected: %d objects still alive", leakCount)
	kindList := make([]string, 0, len(kindCounts))
	for kind := range kindCounts {
		kindList = append(kindList, fmt.Sprintf("%s=%d", vm.objectKindLabel(kind), kindCounts[kind]))
	}
	sort.Strings(kindList)
	if len(kindList) > 0 {
		msg += " (" + strings.Join(kindList, ", ") + ")"
	}
	if len(list) > 0 {
		msg += ": " + strings.Join(list, ", ")
	}
	vm.panic(PanicRCHeapLeakDetected, msg)
}

func (vm *VM) objectKindLabel(k ObjectKind) string {
	switch k {
	case OKString:
		return "string"
	case OKArray:
		return "array"
	case OKStruct:
		return "struct"
	case OKTag:
		return "tag"
	case OKRange:
		return "range"
	case OKBigInt:
		return "bigint"
	case OKBigUint:
		return "biguint"
	case OKBigFloat:
		return "bigfloat"
	default:
		return "object"
	}
}
