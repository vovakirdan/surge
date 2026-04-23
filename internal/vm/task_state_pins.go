package vm

import "fmt"

type pinnedLocal struct {
	frame *Frame
	local int32
}

type taskStatePins struct {
	locals  []pinnedLocal
	handles []Handle
}

type taskStatePinCollector struct {
	vm              *VM
	pins            taskStatePins
	visitedLocals   map[pinnedLocal]struct{}
	visitedHandles  map[Handle]struct{}
	retainedHandles map[Handle]struct{}
}

func (vm *VM) collectTaskStatePins(state Value) (taskStatePins, *VMError) {
	if vm == nil || state.Kind == VKInvalid {
		return taskStatePins{}, nil
	}
	collector := taskStatePinCollector{
		vm:              vm,
		visitedLocals:   make(map[pinnedLocal]struct{}),
		visitedHandles:  make(map[Handle]struct{}),
		retainedHandles: make(map[Handle]struct{}),
	}
	if vmErr := collector.visitValue(state); vmErr != nil {
		vm.releaseTaskStatePins(collector.pins)
		return taskStatePins{}, vmErr
	}
	return collector.pins, nil
}

func (vm *VM) setUserTaskState(state *userTaskState, next Value) *VMError {
	if state == nil {
		return nil
	}
	pins, vmErr := vm.collectTaskStatePins(next)
	if vmErr != nil {
		return vmErr
	}
	vm.setUserTaskStateWithPins(state, next, pins)
	return nil
}

func (vm *VM) setUserTaskStateWithPins(state *userTaskState, next Value, pins taskStatePins) {
	if state == nil {
		return
	}
	prevState := state.state
	prevPins := state.pins
	state.state = next
	state.pins = pins
	if prevState.Kind != VKInvalid {
		vm.dropValue(prevState)
	}
	vm.releaseTaskStatePins(prevPins)
}

func (vm *VM) releaseTaskStatePins(pins taskStatePins) {
	if vm == nil {
		return
	}
	for i := len(pins.handles) - 1; i >= 0; i-- {
		handle := pins.handles[i]
		if handle != 0 {
			vm.Heap.Release(handle)
		}
	}
	for i := len(pins.locals) - 1; i >= 0; i-- {
		pin := pins.locals[i]
		if pin.frame == nil || pin.local < 0 || int(pin.local) >= len(pin.frame.Locals) {
			continue
		}
		slot := &pin.frame.Locals[pin.local]
		if slot.PinCount == 0 {
			continue
		}
		slot.PinCount--
		if slot.PinCount == 0 && !vm.frameOnStack(pin.frame) {
			vm.releaseDetachedLocal(pin.frame, pin.local)
		}
	}
}

func (vm *VM) frameOnStack(frame *Frame) bool {
	if vm == nil || frame == nil {
		return false
	}
	for _, candidate := range vm.Stack {
		if candidate == frame {
			return true
		}
	}
	return false
}

func (vm *VM) releaseDetachedLocal(frame *Frame, local int32) {
	if frame == nil || local < 0 || int(local) >= len(frame.Locals) {
		return
	}
	slot := &frame.Locals[local]
	if !slot.IsInit {
		return
	}
	if !slot.IsMoved && !slot.IsDropped {
		vm.dropValue(slot.V)
	}
	slot.V = Value{}
	slot.IsInit = false
	slot.IsMoved = false
	slot.IsDropped = false
}

func (c *taskStatePinCollector) visitValue(v Value) *VMError {
	switch v.Kind {
	case VKRef, VKRefMut, VKPtr:
		if vmErr := c.visitLocation(v.Loc); vmErr != nil {
			return vmErr
		}
	}
	if v.IsHeap() && v.H != 0 {
		return c.visitHandle(v.H)
	}
	return nil
}

func (c *taskStatePinCollector) visitLocation(loc Location) *VMError {
	switch loc.Kind {
	case LKLocal:
		return c.pinLocal(loc.FrameRef, loc.Local)
	case LKStructField, LKArrayElem, LKMapElem, LKStringBytes, LKRawBytes, LKTagField:
		if vmErr := c.retainHandle(loc.Handle); vmErr != nil {
			return vmErr
		}
		return c.visitHandle(loc.Handle)
	case LKGlobal:
		return nil
	default:
		return c.vm.eb.invalidLocation(fmt.Sprintf("unsupported task-state location kind %d", loc.Kind))
	}
}

func (c *taskStatePinCollector) pinLocal(frame *Frame, local int32) *VMError {
	if frame == nil {
		return c.vm.eb.invalidLocation("invalid local frame <nil>")
	}
	if local < 0 || int(local) >= len(frame.Locals) {
		return c.vm.eb.invalidLocation(fmt.Sprintf("invalid local id %d", local))
	}
	key := pinnedLocal{frame: frame, local: local}
	if _, ok := c.visitedLocals[key]; ok {
		return nil
	}
	c.visitedLocals[key] = struct{}{}
	slot := &frame.Locals[local]
	if !slot.IsInit {
		return c.vm.eb.useBeforeInit(slot.Name)
	}
	if slot.IsDropped {
		return c.vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: local %q used after drop", slot.Name))
	}
	slot.PinCount++
	c.pins.locals = append(c.pins.locals, key)
	return c.visitValue(slot.V)
}

func (c *taskStatePinCollector) retainHandle(handle Handle) *VMError {
	if handle == 0 {
		return c.vm.eb.makeError(PanicInvalidHandle, "invalid handle 0")
	}
	if _, ok := c.retainedHandles[handle]; ok {
		return nil
	}
	obj, ok := c.vm.Heap.lookup(handle)
	if !ok || obj == nil {
		return c.vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid handle %d", handle))
	}
	if obj.Freed || obj.RefCount == 0 {
		return c.vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: handle %d (alloc=%d)", handle, obj.AllocID))
	}
	c.vm.Heap.Retain(handle)
	c.retainedHandles[handle] = struct{}{}
	c.pins.handles = append(c.pins.handles, handle)
	return nil
}

func (c *taskStatePinCollector) visitHandle(handle Handle) *VMError {
	if handle == 0 {
		return nil
	}
	if _, ok := c.visitedHandles[handle]; ok {
		return nil
	}
	obj, ok := c.vm.Heap.lookup(handle)
	if !ok || obj == nil {
		return c.vm.eb.makeError(PanicInvalidHandle, fmt.Sprintf("invalid handle %d", handle))
	}
	if obj.Freed || obj.RefCount == 0 {
		return c.vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: handle %d (alloc=%d)", handle, obj.AllocID))
	}
	c.visitedHandles[handle] = struct{}{}
	switch obj.Kind {
	case OKString:
		if vmErr := c.visitHandle(obj.StrLeft); vmErr != nil {
			return vmErr
		}
		if vmErr := c.visitHandle(obj.StrRight); vmErr != nil {
			return vmErr
		}
		return c.visitHandle(obj.StrSliceBase)
	case OKArray:
		for _, elem := range obj.Arr {
			if vmErr := c.visitValue(elem); vmErr != nil {
				return vmErr
			}
		}
	case OKArraySlice:
		return c.visitHandle(obj.ArrSliceBase)
	case OKMap:
		for i := range obj.MapEntries {
			if vmErr := c.visitValue(obj.MapEntries[i].Key); vmErr != nil {
				return vmErr
			}
			if vmErr := c.visitValue(obj.MapEntries[i].Value); vmErr != nil {
				return vmErr
			}
		}
	case OKStruct:
		for _, field := range obj.Fields {
			if vmErr := c.visitValue(field); vmErr != nil {
				return vmErr
			}
		}
	case OKTag:
		for _, field := range obj.Tag.Fields {
			if vmErr := c.visitValue(field); vmErr != nil {
				return vmErr
			}
		}
	case OKRange:
		if obj.Range.Kind == RangeArrayIter {
			return c.visitHandle(obj.Range.ArrayBase)
		}
		if obj.Range.HasStart {
			if vmErr := c.visitValue(obj.Range.Start); vmErr != nil {
				return vmErr
			}
		}
		if obj.Range.HasEnd {
			if vmErr := c.visitValue(obj.Range.End); vmErr != nil {
				return vmErr
			}
		}
	}
	return nil
}
