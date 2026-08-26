package vm

import (
	"fmt"
	"sort"
	"strings"

	"surge/internal/asyncrt"
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
		// The value's ownership already went elsewhere (the VM flags
		// non-copy call-argument reads as moves, coarser than sema's
		// borrow-aware tracking), so a synthesized drop of this slot is
		// a no-op — mirroring the LLVM backend, where the emitter nulls
		// consumed slots and the free helpers ignore null. Double drops
		// still panic below.
		return nil
	}
	if slot.IsDropped {
		return vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: local %q used after drop", slot.Name))
	}

	vm.dropValue(slot.V)
	slot.IsDropped = true
	return nil
}

func (vm *VM) execDropGlobal(globalID mir.GlobalID) *VMError {
	if int(globalID) < 0 || int(globalID) >= len(vm.Globals) {
		return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", globalID))
	}
	slot := &vm.Globals[globalID]
	if !slot.IsInit {
		return vm.eb.useBeforeInit(slot.Name)
	}
	if slot.IsMoved {
		return vm.eb.useAfterMove(slot.Name)
	}
	if slot.IsDropped {
		return vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: global %q used after drop", slot.Name))
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
		if !slot.IsInit || slot.PinCount != 0 {
			continue
		}
		if !slot.IsMoved && !slot.IsDropped {
			vm.dropValue(slot.V)
		}
		slot.V = Value{}
		slot.IsInit = false
		slot.IsMoved = false
		slot.IsDropped = false
	}
}

func (vm *VM) dropAllFrames() {
	for i := len(vm.Stack) - 1; i >= 0; i-- {
		vm.dropFrameLocals(vm.Stack[i])
		// Abandoning the stack abandons every activation on it, including the
		// one that was partway through an instruction, so its temporaries are
		// released here rather than at a boundary it will never reach. A
		// temporary that cannot be released is not reported from here: this path
		// is already unwinding, and the leak check that follows a shutdown sees
		// the same fact with the whole heap in front of it.
		_ = vm.retireActivation(vm.Stack[i]) //nolint:errcheck // see above
	}
}

func (vm *VM) dropGlobals() {
	for i := len(vm.Globals) - 1; i >= 0; i-- {
		slot := &vm.Globals[i]
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

func (vm *VM) dropAsyncTasks() {
	if vm == nil || vm.Async == nil {
		return
	}
	drained := vm.Async.DrainTasks()
	for _, task := range drained.Tasks {
		if task == nil {
			continue
		}
		if state, ok := task.State.(*userTaskState); ok && state != nil {
			if state.state.Kind != VKInvalid {
				vm.dropValue(state.state)
			}
			vm.releaseTaskStatePins(state.pins)
			state.state = Value{}
			state.pins = taskStatePins{}
			task.State = nil
		} else if v, ok := task.State.(Value); ok {
			vm.dropValue(v)
		}
		// A completed result whose cohort is empty should already be gone: the
		// task owns it and releases it the moment nothing can claim it. Finding
		// one here is a defect, and it is counted rather than quietly swept —
		// the sweep is exactly what used to hide this whole class, so that an
		// end-to-end program could never assert it had left nothing behind.
		if task.Status == asyncrt.TaskDone && valueHoldsStorage(task.ResultValue) && vm.taskCohortEmpty(task.ID) {
			vm.unclaimedTaskResults++
		}
		vm.dropValue(task.ResultValue)
		// A resume value still on a task at shutdown is a payload that was
		// delivered and never read.
		vm.transportRelease(task.ResumeValue)
		task.State = nil
		task.ResultValue = Value{}
		task.ResumeValue = Value{}
	}
	// Drop-without-receive: values still in a channel buffer, or in a parked
	// sender's queue entry, when the program ends. The receive that would have
	// consumed them never comes, so this is the release they get — and their
	// only one, because the drain takes them out of the runtime's hold in the
	// same step it hands them here.
	for _, payload := range drained.ChannelPayloads {
		vm.transportRelease(payload)
	}
}

func (vm *VM) dropValue(v Value) {
	if vm == nil || vm.Heap == nil {
		return
	}
	// A composite owns storage rather than a counted object, so releasing one
	// walks its members instead of decrementing anything.
	if v.Kind == VKComposite {
		vm.dropComposite(v)
		return
	}
	if !v.IsHeap() || v.H == 0 {
		return
	}
	vm.Heap.Release(v.H)
}

func (vm *VM) checkLeaksOrPanic() {
	if vm.Heap == nil {
		return
	}
	// Asked before the heap walk, because by now the drain has already freed
	// these and the heap can no longer see them. That is the point: a sweep
	// that reclaims what an owner should have released reports a clean heap
	// while the ownership rule is broken.
	if vm.unclaimedTaskResults > 0 {
		vm.panic(PanicRCHeapLeakDetected, fmt.Sprintf(
			"heap leak detected: %d completed task result(s) reached shutdown with an empty entitlement cohort; a task's canonical result is the task's to release, not the drain's",
			vm.unclaimedTaskResults))
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
			list = append(list, vm.objectSummary(obj))
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
	case OKArraySlice:
		return "array_slice"
	case OKMap:
		return "map"
	case OKResource:
		return "resource"
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
