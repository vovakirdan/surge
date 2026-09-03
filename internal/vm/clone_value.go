package vm

import (
	"surge/internal/types"
)

// duplicateValue produces an INDEPENDENT value of a value composite:
// mutating the result must not be visible through the original, and dropping
// both must free exactly twice.
//
// It is the VM's half of the type-directed copy the language always described
// and the implementation never performed. A struct, tuple, tagged union or
// fixed array is a VALUE, so duplicating one has to duplicate its storage; the
// implementation stores it in a heap object, so today that means a fresh
// object per level.
//
// The walk is driven by the TYPE, not by the object kind, because the kinds do
// not separate the cases that matter: a dynamic `Array<T>` and a fixed array
// are both OKArray, and only the second is a value. `IsValueComposite` is the
// one predicate that draws that line, and it draws it in one place.
//
// Members are handled by their own category, which is why this is not a blanket
// deep copy:
//
//   - a nested value composite RECURSES, giving each level its own storage;
//   - a reference-counted scalar RETAINS. It is immutable, so sharing the block
//     is sound and a deep copy would be waste. (Same-shard only — a crossing
//     has to deep-copy it, and Epic 23's step 3 refuses that route for now.)
//   - a handle-backed member (string, dynamic array, map, range, task, channel)
//     RETAINS: it governs its own lifetime, and inventing a deep copy for it
//     here would change what those types mean.
//   - plain bits copy.
//
// Cycles are impossible: only the composite cases recurse, and a composite
// cannot contain itself by value — the layout would be infinite. Everything
// that could close a loop is handle-backed and is retained rather than walked.
func (vm *VM) duplicateValue(v Value) (Value, *VMError) {
	if vm == nil || vm.Heap == nil {
		return v, nil
	}
	// A composite has storage rather than a counted object, so duplicating one
	// duplicates bytes. There is no count to raise and nothing to share: the
	// storage IS the value, and two names for one extent would be one value
	// where the language says there are two.
	if v.Kind == VKComposite {
		return vm.duplicateComposite(vm.currentFrame(), v)
	}
	if !v.IsHeap() || v.H == 0 {
		return v, nil
	}
	if !vm.isValueCompositeType(v.TypeID) {
		// Not a value composite: keep the established behavior, which is to
		// share the storage and count the reference.
		return vm.cloneForShare(v)
	}

	obj, ok := vm.Heap.lookup(v.H)
	if !ok || obj == nil {
		return Value{}, vm.eb.makeError(PanicInvalidHandle, "clone of an invalid handle")
	}
	if obj.Freed || obj.RefCount == 0 {
		return Value{}, vm.eb.makeError(PanicRCUseAfterFree, "clone of a freed value")
	}

	// A struct and a tagged union no longer reach here at all: both are storage
	// and were answered above. What remains is the one value composite the heap
	// still owns.
	switch obj.Kind {
	case OKArray:
		// Read every LIVE element out of the run and clone it. The elements are
		// taken through the descriptor rather than from a list, so the clone
		// sees exactly the array's own length and not the stale tail a prefix
		// shift leaves behind.
		frame := vm.currentFrame()
		elems := make([]Value, 0, obj.ArrLen)
		for i := range obj.ArrLen {
			ref, vmErr := vm.runElemSlot(obj, i)
			if vmErr != nil {
				dropEach(vm, elems)
				return Value{}, vmErr
			}
			elem, vmErr := vm.readStorageValue(frame, ref)
			if vmErr != nil {
				dropEach(vm, elems)
				return Value{}, vmErr
			}
			elems = append(elems, elem)
		}
		cloned, vmErr := vm.cloneMembers(elems)
		dropEach(vm, elems)
		if vmErr != nil {
			return Value{}, vmErr
		}
		return MakeHandleArray(vm.Heap.AllocArray(obj.TypeID, cloned), v.TypeID), nil

	default:
		// The type says value composite and the object says otherwise. Rather
		// than guess a representation, share it as before: a wrong clone is a
		// silent aliasing or double-free bug, a shared one is the behavior
		// that was already there.
		return vm.cloneForShare(v)
	}
}

// cloneMembers clones each member in place, releasing what it already built if
// a later member fails — a half-built clone owns real references, and abandoning
// it would leak them.
func (vm *VM) cloneMembers(in []Value) ([]Value, *VMError) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]Value, len(in))
	for i := range in {
		cloned, vmErr := vm.duplicateValue(in[i])
		if vmErr != nil {
			for j := range i {
				vm.dropValue(out[j])
			}
			return nil, vmErr
		}
		out[i] = cloned
	}
	return out, nil
}

// isValueCompositeType asks the one shared predicate, so the VM, the native
// backend and sema cannot drift on what counts as a value.
func (vm *VM) isValueCompositeType(ty types.TypeID) bool {
	if vm == nil || vm.Types == nil || ty == types.NoTypeID {
		return false
	}
	return vm.Types.IsValueComposite(ty)
}

// dropEach releases a partial list of values a failed walk has already built.
// A half-read run owns real references, and abandoning it would leak them.
func dropEach(vm *VM, vals []Value) {
	for i := range vals {
		vm.dropValue(vals[i])
	}
}
