package vm

import (
	"fmt"

	"surge/internal/mir"
)

// execInstr executes a single instruction.
func (vm *VM) execInstr(frame *Frame, instr *mir.Instr) (advanceIP bool, pushFrame *Frame, vmErr *VMError) {
	var writes []LocalWrite
	var (
		storeLoc Location
		storeVal Value
		hasStore bool
	)
	var (
		doJump bool
		jumpBB mir.BlockID
	)

	switch instr.Kind {
	case mir.InstrAssign:
		hasStore, storeLoc, storeVal, writes, vmErr = vm.execInstrAssign(frame, instr, writes)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrCall:
		var newFrame *Frame
		newFrame, vmErr = vm.execCall(frame, &instr.Call, &writes)
		if vmErr != nil {
			return false, nil, vmErr
		}
		if newFrame != nil {
			pushFrame = newFrame
		}

	case mir.InstrDrop:
		vmErr = vm.execInstrDrop(frame, instr)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrEndBorrow:
		vmErr = vm.execInstrEndBorrow(frame, instr)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrAwait:
		hasStore, storeLoc, storeVal, writes, vmErr = vm.execInstrAwait(frame, instr, writes)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrSpawn:
		hasStore, storeLoc, storeVal, writes, vmErr = vm.execInstrSpawn(frame, instr, writes)
		if vmErr != nil {
			return false, nil, vmErr
		}

	case mir.InstrPoll:
		pollRes, pollErr := vm.execInstrPoll(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB

	case mir.InstrJoinAll:
		pollRes, pollErr := vm.execInstrJoinAll(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB
	case mir.InstrChanSend:
		pollRes, pollErr := vm.execInstrChanSend(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB
	case mir.InstrChanRecv:
		pollRes, pollErr := vm.execInstrChanRecv(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB
	case mir.InstrNetWait:
		pollRes, pollErr := vm.execInstrNetWait(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB
	case mir.InstrTimeout:
		pollRes, pollErr := vm.execInstrTimeout(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB
	case mir.InstrSelect:
		pollRes, pollErr := vm.execInstrSelect(frame, instr, writes)
		vmErr = pollErr
		if vmErr != nil {
			return false, nil, vmErr
		}
		hasStore = pollRes.hasStore
		storeLoc = pollRes.storeLoc
		storeVal = pollRes.storeVal
		writes = pollRes.writes
		doJump = pollRes.doJump
		jumpBB = pollRes.jumpBB

	case mir.InstrNop:
		// Nothing to do

	case mir.InstrEnvelopeRelease:
		// The VM's iterator step/cursor values and boxed unions are
		// ordinary heap-managed objects (see eval_iter.go / the VM's GC)
		// with their own refcount/GC lifecycle — there is no separate raw
		// envelope box to free here, so this is a genuine no-op on this
		// backend (the LLVM-only leak it patches, for both for-loops and
		// `compare`, does not exist in the VM's representation).

	default:
		return false, nil, vm.eb.unimplemented(fmt.Sprintf("instruction kind %d", instr.Kind))
	}

	// Trace the instruction
	if vm.Trace != nil {
		vm.Trace.TraceInstr(len(vm.Stack), frame.Func, frame.BB, frame.IP, instr, frame.Span, writes)
		if hasStore {
			vm.Trace.TraceStore(storeLoc, storeVal)
		}
	}

	if doJump {
		frame.BB = jumpBB
		frame.IP = 0
		return false, nil, nil
	}
	if pushFrame != nil {
		return false, pushFrame, nil
	}
	return true, nil, nil
}

func (vm *VM) execInstrAssign(frame *Frame, instr *mir.Instr, writes []LocalWrite) (hasStore bool, storeLoc Location, storeVal Value, writesOut []LocalWrite, vmErr *VMError) {
	val, vmErr := vm.evalRValue(frame, &instr.Assign.Src)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	dst := instr.Assign.Dst
	if len(dst.Proj) == 0 {
		switch dst.Kind {
		case mir.PlaceGlobal:
			vmErr = vm.writeGlobal(dst.Global, val)
			if vmErr != nil {
				return false, Location{}, Value{}, writes, vmErr
			}
			return true, Location{Kind: LKGlobal, Global: int32(dst.Global), IsMut: true}, val, writes, nil
		default:
			localID := dst.Local
			vmErr = vm.writeLocal(frame, localID, val)
			if vmErr != nil {
				return false, Location{}, Value{}, writes, vmErr
			}
			stored := frame.Locals[localID].V
			writes = append(writes, LocalWrite{
				LocalID: localID,
				Name:    frame.Locals[localID].Name,
				Value:   stored,
			})
			return false, Location{}, Value{}, writes, nil
		}
	}
	loc, vmErr := vm.EvalPlace(frame, dst)
	if vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	if vmErr := vm.storeLocation(loc, val); vmErr != nil {
		return false, Location{}, Value{}, writes, vmErr
	}
	return true, loc, val, writes, nil
}

func (vm *VM) execInstrDrop(frame *Frame, instr *mir.Instr) *VMError {
	if instr.Drop.Shallow {
		return vm.execDropShallow(frame, instr.Drop.Place)
	}
	// A PROJECTED drop releases one place, not the whole binding. Falling
	// through to execDrop here dropped the entire local, so `@drop o.inner`
	// took `o` with it and the next read of `o` panicked use-after-free.
	if len(instr.Drop.Place.Proj) != 0 {
		return vm.execDropProjected(frame, instr.Drop.Place)
	}
	switch instr.Drop.Place.Kind {
	case mir.PlaceGlobal:
		return vm.execDropGlobal(instr.Drop.Place.Global)
	default:
		return vm.execDrop(frame, instr.Drop.Place.Local)
	}
}

// execDropProjected releases the value at a projection and CLEARS the slot.
//
// Clearing is not tidiness. Freeing a struct releases every field it still
// holds (Heap.Free walks obj.Fields), so a field released here and left in
// place would be released a second time when its container goes — which the
// refcount catches as a use-after-free panic rather than as silent corruption,
// but a panic all the same. This mirrors the native backend, which stores null
// into the slot after freeing for the same reason.
func (vm *VM) execDropProjected(frame *Frame, place mir.Place) *VMError {
	loc, vmErr := vm.EvalPlace(frame, place)
	if vmErr != nil {
		return vmErr
	}
	val, vmErr := vm.loadLocationRaw(loc)
	if vmErr != nil {
		return vmErr
	}
	if !val.IsHeap() || val.H == 0 {
		// Nothing owned here: a Copy field, or a place already released.
		return nil
	}
	// Storing the empty value IS the release: a store releases whatever it
	// overwrites. Releasing first and storing after releases twice, which the
	// refcount catches as a use-after-free — measured while writing this.
	return vm.storeLocation(loc, Value{})
}

func (vm *VM) execInstrEndBorrow(frame *Frame, instr *mir.Instr) *VMError {
	switch instr.EndBorrow.Place.Kind {
	case mir.PlaceGlobal:
		globalID := instr.EndBorrow.Place.Global
		if int(globalID) < 0 || int(globalID) >= len(vm.Globals) {
			return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid global id %d", globalID))
		}
		slot := &vm.Globals[globalID]
		slot.V = Value{}
		slot.IsInit = false
		slot.IsMoved = false
		slot.IsDropped = false
	default:
		localID := instr.EndBorrow.Place.Local
		if int(localID) < 0 || int(localID) >= len(frame.Locals) {
			return vm.eb.makeError(PanicOutOfBounds, fmt.Sprintf("invalid local id %d", localID))
		}
		slot := &frame.Locals[localID]
		slot.V = Value{}
		slot.IsInit = false
		slot.IsMoved = false
		slot.IsDropped = false
	}
	return nil
}

// execDropShallow releases a container's own storage and leaves its contents
// alone — the closing move of a residual drop, after the live fields have been
// dropped one at a time.
//
// "Leaves them alone" needs doing rather than saying here: releasing a struct
// walks its fields and releases each (Heap.Free), so the fields still present —
// the ones that moved away — would be released as well. They are cleared first,
// which makes the release touch nothing but the container itself.
func (vm *VM) execDropShallow(frame *Frame, place mir.Place) *VMError {
	loc, vmErr := vm.EvalPlace(frame, place)
	if vmErr != nil {
		return vmErr
	}
	val, vmErr := vm.loadLocationRaw(loc)
	if vmErr != nil {
		return vmErr
	}
	if !val.IsHeap() || val.H == 0 {
		return nil
	}
	if obj, ok := vm.Heap.lookup(val.H); ok && obj != nil {
		for i := range obj.Fields {
			obj.Fields[i] = Value{}
		}
	}
	vm.dropValue(val)
	// The place itself is cleared for the same reason a projected drop clears
	// its slot: whatever holds this container must not release it again.
	if len(place.Proj) != 0 {
		return vm.storeLocation(loc, Value{})
	}
	if place.Kind != mir.PlaceGlobal {
		frame.Locals[place.Local].IsDropped = true
	}
	return nil
}
