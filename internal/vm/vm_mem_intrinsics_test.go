package vm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

func callIntrinsic(vm *VM, name string, args []Value, dstType types.TypeID) (Value, *VMError) {
	locals := make([]mir.Local, 0, len(args)+1)
	for _, arg := range args {
		locals = append(locals, mir.Local{Type: arg.TypeID})
	}
	outLocal := mir.LocalID(-1)
	if dstType != types.NoTypeID {
		outLocal = mir.LocalID(len(locals))
		locals = append(locals, mir.Local{Name: "out", Type: dstType})
	}
	fn := &mir.Func{
		Locals: locals,
		Blocks: []mir.Block{{}},
		Entry:  0,
	}
	frame := NewFrame(fn)
	for i, arg := range args {
		if vmErr := vm.writeLocal(frame, mir.LocalID(i), arg); vmErr != nil {
			return Value{}, vmErr
		}
	}
	call := mir.CallInstr{
		HasDst: dstType != types.NoTypeID,
		Callee: mir.Callee{Name: name},
	}
	if dstType != types.NoTypeID {
		call.Dst = mir.Place{Local: outLocal}
	}
	for i, arg := range args {
		call.Args = append(call.Args, mir.Operand{
			Kind:  mir.OperandCopy,
			Place: mir.Place{Local: mir.LocalID(i)},
			Type:  arg.TypeID,
		})
	}
	if vmErr := vm.callIntrinsic(frame, &call, nil); vmErr != nil {
		return Value{}, vmErr
	}
	if dstType == types.NoTypeID {
		return Value{}, nil
	}
	return vm.readLocal(frame, outLocal)
}

func TestVMMemIntrinsicsMemcpyMemmove(t *testing.T) {
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
	if ptrVal.Kind != VKPtr || ptrVal.Loc.Kind != LKRawBytes {
		t.Fatalf("expected raw pointer, got %v", ptrVal.Kind)
	}

	h := vmInstance.Heap.AllocString(builtins.String, "abcde")
	vmInstance.stringBytes(vmInstance.Heap.Get(h))
	srcPtr := MakePtr(Location{Kind: LKStringBytes, Handle: h}, ptrType)
	nVal := MakeInt(5, builtins.Uint)
	if _, vmErr = callIntrinsic(vmInstance, "rt_memcpy", []Value{ptrVal, srcPtr, nVal}, types.NoTypeID); vmErr != nil {
		t.Fatalf("rt_memcpy failed: %v", vmErr)
	}
	alloc, vmErr := vmInstance.rawGet(ptrVal.Loc.Handle)
	if vmErr != nil {
		t.Fatalf("raw memory lookup failed: %v", vmErr)
	}
	if got := string(alloc.data); got != "abcde" {
		t.Fatalf("memcpy data mismatch: %q", got)
	}

	srcOff := MakePtr(Location{Kind: LKRawBytes, Handle: ptrVal.Loc.Handle, ByteOffset: 0}, ptrType)
	dstOff := MakePtr(Location{Kind: LKRawBytes, Handle: ptrVal.Loc.Handle, ByteOffset: 1}, ptrType)
	nMove := MakeInt(4, builtins.Uint)
	if _, vmErr = callIntrinsic(vmInstance, "rt_memmove", []Value{dstOff, srcOff, nMove}, types.NoTypeID); vmErr != nil {
		t.Fatalf("rt_memmove failed: %v", vmErr)
	}
	alloc, vmErr = vmInstance.rawGet(ptrVal.Loc.Handle)
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

func TestVMMemIntrinsicsRealloc(t *testing.T) {
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
