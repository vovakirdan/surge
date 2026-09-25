package vm

import (
	"surge/internal/types"
)

// The NULL runtime handle.
//
// A core runtime handle -- `Task<T>`, `Channel<T>`, `Range<T>`, the families
// `types.Interner.IsRuntimeHandleType` accepts -- is declared as a nominal
// struct with one private member, but it is not a language struct: its whole
// meaning is a word the runtime handed out. Its DEFAULT is therefore not a
// struct built member by member. The native backend gives it a null pointer,
// "their uninitialized sentinel" (internal/backend/llvm/emit_intrinsics_default.go:67-73),
// and the VM gives it the same thing: a handle-kinded value that names no
// object, H == 0.
//
// That is not a new convention in the VM, only a new producer of it. A heap
// value with H == 0 is already inert on every lifetime path: dropValue skips it
// (drop.go), duplicateValue and cloneForShare hand it back uncounted
// (clone_value.go, eval_data.go), releaseContainedValue passes it by (heap.go),
// and a handle cell stores it as zero (storage_cell.go's handleBits). The
// native runtime is equally inert there: `rt_task_handle_drop(NULL)` returns
// (runtime/native/rt_task_lifetime.c:296-302, pinned by
// TestRuntimeV2TaskHandleDropTreatsAnEmptySlotAsNothing), and so do
// `rt_channel_handle_retain(NULL)` and `rt_channel_handle_drop(NULL)`
// (runtime/native/rt_channel_refcount.c:51-67).
//
// A zeroed handle CELL -- a member of a composite that was defaulted, or never
// written -- decodes as `nothing` rather than as a typed null (storage_ops.go's
// handleValue), so the consumers below accept both spellings of "no object".
//
// What the runtime refuses, the VM refuses with the same words. A NULL task
// handle that is awaited, cancelled, cloned, spawned or selected on dies in the
// native runtime's handle lookup with "invalid task handle"
// (task_from_handle, runtime/native/rt_async_state.c:516-522); a NULL channel
// that is sent on, received from, closed or selected on dies with
// "async: null channel handle" (channel_from_handle,
// runtime/native/rt_channel_lane.h:499-505). Both go through panic_msg and exit
// 1 (runtime/native/rt_async_panic.c:6-11, rt_fatal.c:52). A null handle that
// is USED must fail on both backends, never do something.
//
// A null `Range` is the one family where the backends do not agree, and the VM
// keeps its own loud refusal (Heap.Get's "invalid handle 0") rather than copy
// either native behaviour: native slicing reads a NULL range as the whole range
// (range_bounds, runtime/native/rt_string.c:239 and rt_array.c:211), and native
// iteration loads through it unchecked.

const (
	// nullTaskHandleMessage is the native runtime's refusal of a NULL task
	// handle, word for word, so both backends name the same failure.
	nullTaskHandleMessage = "invalid task handle"
	// nullChannelHandleMessage is the native runtime's refusal of a NULL
	// channel handle, word for word.
	nullChannelHandleMessage = "async: null channel handle"
)

// nullRuntimeHandle is the default of a core runtime handle type: the value
// kind the VM carries that family in, naming no object.
func (vm *VM) nullRuntimeHandle(typeID types.TypeID) Value {
	if vm.isRangeHandleType(typeID) {
		return MakeHandleRange(0, typeID)
	}
	return MakeResource(0, typeID)
}

// isNullRuntimeHandle reports whether a value that a task or channel operation
// is about to read a word out of names no runtime object at all.
//
// A reference is looked through first, because operations take their handle by
// borrow as often as by value, and the null is what the reference points at.
func (vm *VM) isNullRuntimeHandle(val Value) (bool, *VMError) {
	if val.Kind == VKRef || val.Kind == VKRefMut {
		loaded, vmErr := vm.loadLocationRaw(val.Loc)
		if vmErr != nil {
			return false, vmErr
		}
		val = loaded
	}
	switch val.Kind {
	case VKResource, VKHandleRange:
		return val.H == 0, nil
	case VKNothing:
		return true, nil
	default:
		return false, nil
	}
}

// isRangeHandleType reports whether a runtime handle type is the Range family,
// which the VM carries as a range object rather than as a resource word.
func (vm *VM) isRangeHandleType(typeID types.TypeID) bool {
	if vm == nil || vm.Types == nil || vm.Types.Strings == nil {
		return false
	}
	info, ok := vm.Types.StructInfo(vm.valueType(typeID))
	if !ok || info == nil {
		return false
	}
	name, ok := vm.Types.Strings.Lookup(info.Name)
	return ok && name == "Range"
}
