package vm

import (
	"testing"

	"surge/internal/types"
)

func TestVMIndexSetArrayAndSlice(t *testing.T) {
	requireVMBackend(t)
	typesInterner := types.NewInterner()
	builtins := typesInterner.Builtins()
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, typesInterner, nil)

	arrType := typesInterner.Intern(types.MakeArray(builtins.Int, types.ArrayDynamicLength))
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
	if got := obj.Arr[1]; got.Kind != VKInt || got.Int != 42 {
		t.Fatalf("array index 1 mismatch: %+v", got)
	}

	if _, vmErr := callIntrinsic(vmInstance, "__index_set", []Value{
		arrVal,
		MakeInt(-1, builtins.Int),
		MakeInt(7, builtins.Int),
	}, types.NoTypeID); vmErr != nil {
		t.Fatalf("__index_set negative index failed: %v", vmErr)
	}
	if got := obj.Arr[2]; got.Kind != VKInt || got.Int != 7 {
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
	if got := obj.Arr[1]; got.Kind != VKInt || got.Int != 99 {
		t.Fatalf("slice index 0 mismatch: %+v", got)
	}
}
