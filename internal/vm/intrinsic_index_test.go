package vm

import (
	"testing"

	"surge/internal/types"
)

func TestVMIndexReturnsReferenceForArrayElement(t *testing.T) {
	requireVMBackend(t)
	typesInterner := types.NewInterner()
	builtins := typesInterner.Builtins()
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, typesInterner, nil)

	arrType := typesInterner.Intern(types.MakeArray(builtins.Int, types.ArrayDynamicLength))
	refType := typesInterner.Intern(types.MakeReference(builtins.Int, false))
	hBase := vmInstance.Heap.AllocArray(arrType, []Value{
		MakeInt(10, builtins.Int),
		MakeInt(20, builtins.Int),
		MakeInt(30, builtins.Int),
	})

	ref, vmErr := callIntrinsic(vmInstance, "__index", []Value{
		MakeHandleArray(hBase, arrType),
		MakeInt(-1, builtins.Int),
	}, refType)
	if vmErr != nil {
		t.Fatalf("__index array failed: %v", vmErr)
	}
	if ref.Kind != VKRef || ref.TypeID != refType {
		t.Fatalf("expected shared element reference type#%d, got %+v", refType, ref)
	}
	if ref.Loc.Kind != LKArrayElem || ref.Loc.Handle != hBase || ref.Loc.Index != 2 {
		t.Fatalf("unexpected base array element location: %+v", ref.Loc)
	}
	value, vmErr := vmInstance.loadLocationRaw(ref.Loc)
	if vmErr != nil {
		t.Fatalf("load returned array reference: %v", vmErr)
	}
	if value.Kind != VKInt || value.Int != 30 {
		t.Fatalf("returned reference points to %+v, want int 30", value)
	}

	hSlice := vmInstance.Heap.AllocArraySlice(arrType, hBase, 1, 2, 2)
	ref, vmErr = callIntrinsic(vmInstance, "__index", []Value{
		MakeHandleArray(hSlice, arrType),
		MakeInt(0, builtins.Int),
	}, refType)
	if vmErr != nil {
		t.Fatalf("__index array slice failed: %v", vmErr)
	}
	// An element of a slice is named through the BASE array at an absolute index,
	// not through the slice header at a view-relative one. Both denote the same
	// element and load the same value, and the two spellings used to coexist —
	// index projection already produced this one while __index produced the other.
	// They are one spelling now, because a fixed array's slice has no base object
	// to name at all, so the header-relative form could not describe it and the
	// choice had to be made rather than left to which path a caller took.
	if ref.Loc.Kind != LKArrayElem || ref.Loc.Handle != hBase || ref.Loc.Index != 1 {
		t.Fatalf("unexpected slice element location: %+v", ref.Loc)
	}
	value, vmErr = vmInstance.loadLocationRaw(ref.Loc)
	if vmErr != nil {
		t.Fatalf("load returned slice reference: %v", vmErr)
	}
	if value.Kind != VKInt || value.Int != 20 {
		t.Fatalf("returned slice reference points to %+v, want int 20", value)
	}
}
