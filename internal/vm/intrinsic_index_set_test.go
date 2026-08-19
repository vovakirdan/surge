package vm

import (
	"testing"

	"surge/internal/types"
)

func TestVMIndexSetArrayAndSlice(t *testing.T) {
	requireVMBackend(t)
	typesInterner := types.NewInterner()
	builtins := typesInterner.Builtins()
	arrType := typesInterner.Intern(types.MakeArray(builtins.Int, types.ArrayDynamicLength))
	vmInstance := New(withElementLayouts(t, typesInterner, builtins.Int, arrType),
		NewTestRuntime(nil, ""), nil, typesInterner, nil)

	elems := []Value{
		MakeInt(1, builtins.Int),
		MakeInt(2, builtins.Int),
		MakeInt(3, builtins.Int),
	}
	hBase := vmInstance.Heap.AllocArray(arrType, elems)
	arrVal := MakeHandleArray(hBase, arrType)

	if _, vmErr := callIntrinsic(vmInstance, "__index_set", []Value{
		arrVal,
		MakeInt(1, builtins.Int),
		MakeInt(42, builtins.Int),
	}, types.NoTypeID); vmErr != nil {
		t.Fatalf("__index_set array failed: %v", vmErr)
	}

	obj := vmInstance.Heap.Get(hBase)
	if obj == nil || obj.Kind != OKArray {
		t.Fatalf("expected array object, got %v", obj)
	}
	if got := elemOf(t, vmInstance, obj, 1); got.Kind != VKInt || got.Int != 42 {
		t.Fatalf("array index 1 mismatch: %+v", got)
	}

	if _, vmErr := callIntrinsic(vmInstance, "__index_set", []Value{
		arrVal,
		MakeInt(-1, builtins.Int),
		MakeInt(7, builtins.Int),
	}, types.NoTypeID); vmErr != nil {
		t.Fatalf("__index_set negative index failed: %v", vmErr)
	}
	if got := elemOf(t, vmInstance, obj, 2); got.Kind != VKInt || got.Int != 7 {
		t.Fatalf("array index -1 mismatch: %+v", got)
	}

	hSlice := vmInstance.Heap.AllocArraySlice(arrType, hBase, 1, 2, 2)
	sliceVal := MakeHandleArray(hSlice, arrType)
	if _, vmErr := callIntrinsic(vmInstance, "__index_set", []Value{
		sliceVal,
		MakeInt(0, builtins.Int),
		MakeInt(99, builtins.Int),
	}, types.NoTypeID); vmErr != nil {
		t.Fatalf("__index_set slice failed: %v", vmErr)
	}
	if got := elemOf(t, vmInstance, obj, 1); got.Kind != VKInt || got.Int != 99 {
		t.Fatalf("slice index 0 mismatch: %+v", got)
	}
}

// elemOf reads one element of an array object's run, uncounted. The elements
// are no longer a list to index, so a test that wants to look at one asks the
// run the same way the interpreter does.
func elemOf(t *testing.T, machine *VM, obj *Object, index int) Value {
	t.Helper()
	ref, vmErr := machine.runElemSlot(obj, index)
	if vmErr != nil {
		t.Fatalf("addressing element %d must succeed: %v", index, vmErr)
	}
	value, vmErr := machine.peekStorage(ref)
	if vmErr != nil {
		t.Fatalf("reading element %d must succeed: %v", index, vmErr)
	}
	return value
}
