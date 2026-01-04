package vm

import (
	"testing"

	"surge/internal/types"
)

func TestABIMemoryAllocAlignment(t *testing.T) {
	requireVMBackend(t)
	typesInterner := types.NewInterner()
	builtins := typesInterner.Builtins()
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, typesInterner, nil)

	ptrType := typesInterner.Intern(types.MakePointer(builtins.Uint8))
	size := MakeInt(32, builtins.Uint)
	align := MakeInt(16, builtins.Uint)

	ptrVal, vmErr := callIntrinsic(vmInstance, "rt_alloc", []Value{size, align}, ptrType)
	if vmErr != nil {
		t.Fatalf("rt_alloc failed: %v", vmErr)
	}
	alloc, vmErr := vmInstance.rawGet(ptrVal.Loc.Handle)
	if vmErr != nil {
		t.Fatalf("raw memory lookup failed: %v", vmErr)
	}
	if alloc.align != 16 {
		t.Fatalf("alloc alignment want 16, got %d", alloc.align)
	}
	if int(ptrVal.Loc.ByteOffset)%16 != 0 {
		t.Fatalf("pointer offset not aligned: %d", ptrVal.Loc.ByteOffset)
	}

	if _, vmErr := callIntrinsic(vmInstance, "rt_free", []Value{ptrVal, size, align}, types.NoTypeID); vmErr != nil {
		t.Fatalf("rt_free failed: %v", vmErr)
	}
}

func TestABIMemoryMemmoveOverlap(t *testing.T) {
	requireVMBackend(t)
	typesInterner := types.NewInterner()
	builtins := typesInterner.Builtins()
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, typesInterner, nil)

	ptrType := typesInterner.Intern(types.MakePointer(builtins.Uint8))
	size := MakeInt(5, builtins.Uint)
	align := MakeInt(1, builtins.Uint)

	ptrVal, vmErr := callIntrinsic(vmInstance, "rt_alloc", []Value{size, align}, ptrType)
	if vmErr != nil {
		t.Fatalf("rt_alloc failed: %v", vmErr)
	}

	h := vmInstance.Heap.AllocString(builtins.String, "abcde")
	vmInstance.stringBytes(vmInstance.Heap.Get(h))
	srcPtr := MakePtr(Location{Kind: LKStringBytes, Handle: h}, ptrType)
	if _, vmErr = callIntrinsic(vmInstance, "rt_memcpy", []Value{ptrVal, srcPtr, size}, types.NoTypeID); vmErr != nil {
		t.Fatalf("rt_memcpy failed: %v", vmErr)
	}

	srcOff := MakePtr(Location{Kind: LKRawBytes, Handle: ptrVal.Loc.Handle, ByteOffset: 0}, ptrType)
	dstOff := MakePtr(Location{Kind: LKRawBytes, Handle: ptrVal.Loc.Handle, ByteOffset: 1}, ptrType)
	moveLen := MakeInt(4, builtins.Uint)
	if _, vmErr = callIntrinsic(vmInstance, "rt_memmove", []Value{dstOff, srcOff, moveLen}, types.NoTypeID); vmErr != nil {
		t.Fatalf("rt_memmove failed: %v", vmErr)
	}

	alloc, vmErr := vmInstance.rawGet(ptrVal.Loc.Handle)
	if vmErr != nil {
		t.Fatalf("raw memory lookup failed: %v", vmErr)
	}
	if got := string(alloc.data); got != "aabcd" {
		t.Fatalf("memmove overlap mismatch: %q", got)
	}

	if _, vmErr := callIntrinsic(vmInstance, "rt_free", []Value{ptrVal, size, align}, types.NoTypeID); vmErr != nil {
		t.Fatalf("rt_free failed: %v", vmErr)
	}
}

func TestABIMemoryReallocPreservesPrefix(t *testing.T) {
	requireVMBackend(t)
	typesInterner := types.NewInterner()
	builtins := typesInterner.Builtins()
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, typesInterner, nil)

	ptrType := typesInterner.Intern(types.MakePointer(builtins.Uint8))
	align := MakeInt(1, builtins.Uint)
	oldSize := MakeInt(4, builtins.Uint)

	ptrVal, vmErr := callIntrinsic(vmInstance, "rt_alloc", []Value{oldSize, align}, ptrType)
	if vmErr != nil {
		t.Fatalf("rt_alloc failed: %v", vmErr)
	}

	h := vmInstance.Heap.AllocString(builtins.String, "abcd")
	vmInstance.stringBytes(vmInstance.Heap.Get(h))
	srcPtr := MakePtr(Location{Kind: LKStringBytes, Handle: h}, ptrType)
	if _, vmErr = callIntrinsic(vmInstance, "rt_memcpy", []Value{ptrVal, srcPtr, oldSize}, types.NoTypeID); vmErr != nil {
		t.Fatalf("rt_memcpy failed: %v", vmErr)
	}

	newSize := MakeInt(6, builtins.Uint)
	ptrVal2, vmErr := callIntrinsic(vmInstance, "rt_realloc", []Value{ptrVal, oldSize, newSize, align}, ptrType)
	if vmErr != nil {
		t.Fatalf("rt_realloc grow failed: %v", vmErr)
	}
	alloc, vmErr := vmInstance.rawGet(ptrVal2.Loc.Handle)
	if vmErr != nil {
		t.Fatalf("raw memory lookup failed: %v", vmErr)
	}
	if got := string(alloc.data[:4]); got != "abcd" {
		t.Fatalf("realloc grow prefix mismatch: %q", got)
	}

	shrinkSize := MakeInt(2, builtins.Uint)
	ptrVal3, vmErr := callIntrinsic(vmInstance, "rt_realloc", []Value{ptrVal2, newSize, shrinkSize, align}, ptrType)
	if vmErr != nil {
		t.Fatalf("rt_realloc shrink failed: %v", vmErr)
	}
	alloc, vmErr = vmInstance.rawGet(ptrVal3.Loc.Handle)
	if vmErr != nil {
		t.Fatalf("raw memory lookup failed: %v", vmErr)
	}
	if got := string(alloc.data[:2]); got != "ab" {
		t.Fatalf("realloc shrink prefix mismatch: %q", got)
	}

	if _, vmErr := callIntrinsic(vmInstance, "rt_free", []Value{ptrVal3, shrinkSize, align}, types.NoTypeID); vmErr != nil {
		t.Fatalf("rt_free failed: %v", vmErr)
	}
}
