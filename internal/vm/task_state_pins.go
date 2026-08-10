package vm

import "fmt"

type pinnedLocal struct {
	frame *Frame
	local int32
}

type taskStatePins struct {
	locals  []pinnedLocal
	handles []Handle
	// arenas are the storages a task state borrows bytes from. A composite has
	// no handle to retain, so what keeps it alive is a pin on the arena that
	// holds it: retirement is refused while a pin is outstanding, which is what
	// makes a captured reference into an activation's storage stay valid
	// exactly as long as the task state that captured it.
	arenas []*Arena
}

type taskStatePinCollector struct {
	vm              *VM
	pins            taskStatePins
	visitedLocals   map[pinnedLocal]struct{}
	visitedHandles  map[Handle]struct{}
	retainedHandles map[Handle]struct{}
	visitedArenas   map[*Arena]struct{}
	visitedExtents  map[storageExtent]struct{}
}

// storageExtent identifies one extent for the purpose of not walking it twice.
// A composite can be reached by more than one path — a reference to it and the
// slot that owns it — and walking it once per path would retain its members
// once per path.
type storageExtent struct {
	arena  *Arena
	offset uint64
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
		visitedArenas:   make(map[*Arena]struct{}),
		visitedExtents:  make(map[storageExtent]struct{}),
	}
	if vmErr := collector.visitValue(state); vmErr != nil {
		collector.rollbackPins()
		return taskStatePins{}, vmErr
	}
	return collector.pins, nil
}

func (c *taskStatePinCollector) rollbackPins() {
	if c == nil || c.vm == nil {
		return
	}
	for i := len(c.pins.arenas) - 1; i >= 0; i-- {
		if arena := c.pins.arenas[i]; arena != nil {
			arena.unpin()
		}
	}
	for i := len(c.pins.handles) - 1; i >= 0; i-- {
		handle := c.pins.handles[i]
		if handle != 0 {
			c.vm.Heap.Release(handle)
		}
	}
	for i := len(c.pins.locals) - 1; i >= 0; i-- {
		pin := c.pins.locals[i]
		if pin.frame == nil || pin.local < 0 || int(pin.local) >= len(pin.frame.Locals) {
			continue
		}
		slot := &pin.frame.Locals[pin.local]
		if slot.PinCount == 0 {
			continue
		}
		slot.PinCount--
		unpinFrameStorage(pin.frame)
	}
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
	for i := len(pins.arenas) - 1; i >= 0; i-- {
		if arena := pins.arenas[i]; arena != nil {
			arena.unpin()
		}
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
		unpinFrameStorage(pin.frame)
		if slot.PinCount == 0 && !vm.frameOnStack(pin.frame) {
			vm.releaseDetachedLocal(pin.frame, pin.local)
			// The activation is off the stack and this slot no longer holds it
			// up. Retiring is refused while any OTHER slot of the same frame is
			// still pinned, so the last release is the one that takes effect.
			// A temporary that could not be released is not reported from
			// here: releasing a pin is not an operation the program asked for,
			// so it has no result to fail. The shutdown leak check sees the
			// same fact with the whole heap in front of it.
			_ = vm.retireActivation(pin.frame) //nolint:errcheck // see above
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
	case VKComposite:
		if ref, ok := v.Storage(); ok {
			return c.visitStorage(ref)
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
	case LKStorage:
		return c.visitStorage(loc.Storage)
	case LKArrayElem, LKMapElem, LKStringBytes, LKRawBytes:
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
	// Pinned task state may extend backing storage lifetime, but it must not
	// resurrect locals whose ownership has already moved elsewhere.
	if slot.IsMoved {
		return c.vm.eb.useAfterMove(slot.Name)
	}
	if slot.IsDropped {
		return c.vm.eb.makeError(PanicRCUseAfterFree, fmt.Sprintf("use-after-free: local %q used after drop", slot.Name))
	}
	slot.PinCount++
	pinFrameStorage(frame)
	c.pins.locals = append(c.pins.locals, key)
	return c.visitValue(slot.V)
}

// visitStorage pins the arena a composite lives in and retains what it holds.
//
// Two things happen and neither substitutes for the other. The arena PIN keeps
// the bytes: without it the activation could retire and the captured reference
// would name storage that is gone. The member walk retains what those bytes
// point at: a handle sitting inside a composite is reachable only through the
// composite, so a task state outliving the expression that built it would
// otherwise hold counts nobody took.
func (c *taskStatePinCollector) visitStorage(ref StorageRef) *VMError {
	if ref.Arena == nil {
		return nil
	}
	if _, ok := c.visitedArenas[ref.Arena]; !ok {
		c.visitedArenas[ref.Arena] = struct{}{}
		ref.Arena.pin()
		c.pins.arenas = append(c.pins.arenas, ref.Arena)
	}
	key := storageExtent{arena: ref.Arena, offset: ref.Offset}
	if _, ok := c.visitedExtents[key]; ok {
		return nil
	}
	c.visitedExtents[key] = struct{}{}
	return c.visitStorageMembers(ref)
}

func (c *taskStatePinCollector) visitStorageMembers(ref StorageRef) *VMError {
	members, vmErr := c.storageMembersOf(ref)
	if vmErr != nil {
		return vmErr
	}
	for _, member := range members {
		memberRef, err := ref.memberRef(member)
		if err != nil {
			return c.vm.eb.makeError(PanicUnimplemented, err.Error())
		}
		if member.Kind == cellComposite {
			if vmErr := c.visitStorageMembers(memberRef); vmErr != nil {
				return vmErr
			}
			continue
		}
		value, readErr := c.vm.storageReadCell(memberRef, member)
		if readErr != nil {
			return c.vm.eb.makeError(PanicRCUseAfterFree, readErr.Error())
		}
		if value.IsHeap() && value.H != 0 {
			if vmErr := c.retainHandle(value.H); vmErr != nil {
				return vmErr
			}
		}
		if vmErr := c.visitValue(value); vmErr != nil {
			return vmErr
		}
	}
	return nil
}

// storageMembersOf returns the members a walk of one extent may touch. For a
// union that is the LIVE arm only, which is the same rule its drop follows.
func (c *taskStatePinCollector) storageMembersOf(ref StorageRef) ([]storageMember, *VMError) {
	if shape, err := c.vm.unionMembers(ref.TypeID); err == nil {
		index, activeErr := c.vm.storageActiveCase(ref, shape)
		if activeErr != nil {
			return nil, c.vm.eb.makeError(PanicRCUseAfterFree, activeErr.Error())
		}
		return shape.Cases[index].Payload, nil
	}
	members, err := c.vm.compositeMembers(ref.TypeID)
	if err != nil {
		return nil, c.vm.eb.makeError(PanicUnimplemented, err.Error())
	}
	return members, nil
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
		// An arena-backed slice holds no handle, so visiting one would pin
		// nothing and let the frame arena retire under a suspended task that is
		// still holding the slice. Pin the extent itself instead.
		if obj.ArrSliceBase == 0 && obj.ArrSliceStorage.Arena != nil {
			return c.visitStorage(obj.ArrSliceStorage)
		}
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
