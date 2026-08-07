package vm

import "surge/internal/mir"

// activate creates one activation of a function together with the storage that
// activation owns: the byte extents its composite slots are laid out in, and
// the scratch its instructions build temporaries in.
//
// Storage is attached HERE and not in NewFrame because only the VM knows the
// layout registry, and an extent whose size did not come from that registry is
// a size the VM invented. A frame built without a VM therefore has no storage
// at all, which is the honest state: it can hold what fits in a slot and
// refuses what needs bytes, rather than silently getting an extent of some
// plausible size.
func (vm *VM) activate(fn *mir.Func) *Frame {
	frame := NewFrame(fn)
	if vm == nil || fn == nil {
		return frame
	}
	frame.storage = newArena(vm.storagePlanFor(fn), 1)
	return frame
}

// storagePlanFor returns the arena plan of a function, computing it once.
//
// The plan is a pure function of the slot types and the layout registry, so
// every activation of a function shares one. The cache is keyed by the function
// itself and only ever looked up, never walked, so it cannot introduce an
// order that differs between two compiles of the same program.
func (vm *VM) storagePlanFor(fn *mir.Func) *StoragePlan {
	if plan, ok := vm.storagePlans[fn]; ok {
		return plan
	}
	plan := vm.FunctionStorage(fn)
	if vm.storagePlans == nil {
		vm.storagePlans = make(map[*mir.Func]*StoragePlan, 16)
	}
	vm.storagePlans[fn] = &plan
	return &plan
}

// beginStep opens an instruction boundary on an activation.
//
// The reclamation of temporaries happens HERE, at the start of the next
// instruction, rather than at the end of the previous one. The two differ for
// exactly one instruction kind and it is the important one: a call does not
// finish when it is dispatched, it finishes when the callee returns, and a
// rewind placed after dispatch would reclaim the arguments the callee is
// standing on. Reclaiming at the top of the next boundary needs no special case
// for calls, because whatever the previous instruction built is still above the
// mark when this frame is next scheduled.
//
// A temporary that could not be released is reported rather than swallowed. It
// means a value was built and then became unreachable without being stored,
// which is a leak whether or not the program that caused it goes on to succeed.
func (vm *VM) beginStep(frame *Frame) *VMError {
	if frame == nil || frame.scratch == nil {
		return nil
	}
	if err := vm.rewindScratch(frame.scratch, frame.scratchMark); err != nil {
		return vm.eb.makeError(PanicMemoryLeakDetected, err.Error())
	}
	frame.scratchMark = frame.scratch.mark()
	return nil
}

// retireActivation gives up the storage of an activation that has left the
// stack, releasing whatever it still owns.
//
// The scratch is rewound to nothing first. In a program that ran to the end of
// its last instruction there is nothing left there, but an activation can also
// leave the stack from the middle of one — a panic, or a terminator that
// evaluates an operand and then pops — and that is exactly the case where a
// temporary was built and never stored.
//
// Retirement is refused while a task state pins one of this activation's slots.
// That refusal is what makes the generation a staleness signal rather than a
// race: a pinned frame keeps its bytes, so a reference into them is stale only
// when the storage really is gone.
func (vm *VM) retireActivation(frame *Frame) *VMError {
	if vm == nil || frame == nil {
		return nil
	}
	var leaked *VMError
	if frame.scratch != nil {
		if err := vm.rewindScratch(frame.scratch, scratchMark{}); err != nil {
			leaked = vm.eb.makeError(PanicMemoryLeakDetected, err.Error())
		}
	}
	frame.scratchMark = scratchMark{}
	if frame.storage != nil {
		frame.storage.retire()
	}
	return leaked
}

// pinFrameStorage records that a task state is borrowing one of an
// activation's slots, so the arena behind that slot outlives the stack.
func pinFrameStorage(frame *Frame) {
	if frame != nil && frame.storage != nil {
		frame.storage.pin()
	}
}

// unpinFrameStorage gives back one such borrow.
func unpinFrameStorage(frame *Frame) {
	if frame != nil && frame.storage != nil && frame.storage.pins != 0 {
		frame.storage.unpin()
	}
}
