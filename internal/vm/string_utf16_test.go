package vm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

func callStringIntrinsic(vm *VM, name string, ptr Value, length Value, resultType types.TypeID) (Value, *VMError) {
	fn := &mir.Func{
		Locals: []mir.Local{
			{Name: "ptr", Type: ptr.TypeID},
			{Name: "len", Type: length.TypeID},
			{Name: "out", Type: resultType},
		},
		Blocks: []mir.Block{{}},
		Entry:  0,
	}
	frame := NewFrame(fn)

	if vmErr := vm.writeLocal(frame, 0, ptr); vmErr != nil {
		return Value{}, vmErr
	}
	if vmErr := vm.writeLocal(frame, 1, length); vmErr != nil {
		return Value{}, vmErr
	}

	call := mir.CallInstr{
		HasDst: true,
		Dst:    mir.Place{Local: 2},
		Callee: mir.Callee{Name: name},
		Args: []mir.Operand{
			{Kind: mir.OperandCopy, Place: mir.Place{Local: 0}, Type: ptr.TypeID},
			{Kind: mir.OperandCopy, Place: mir.Place{Local: 1}, Type: length.TypeID},
		},
	}

	var writes []LocalWrite
	if vmErr := vm.callIntrinsic(frame, &call, &writes); vmErr != nil {
		return Value{}, vmErr
	}
	return vm.readLocal(frame, 2)
}

func TestStringUTF16ConstructorParity(t *testing.T) {
	typesInterner := types.NewInterner()
	builtins := typesInterner.Builtins()
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, typesInterner, nil)

	utf16Units := []uint16{0x0041, 0xD83D, 0xDE42}
	utf8Bytes := []byte("A🙂")

	elems16 := make([]Value, len(utf16Units))
	for i, u := range utf16Units {
		elems16[i] = MakeInt(int64(u), builtins.Uint16)
	}
	arrH16 := vmInstance.Heap.AllocArray(types.NoTypeID, elems16)
	ptr16Type := typesInterner.Intern(types.MakePointer(builtins.Uint16))
	ptr16 := MakePtr(Location{Kind: LKArrayElem, Handle: arrH16, Index: 0}, ptr16Type)
	len16 := MakeInt(int64(len(utf16Units)), builtins.Uint)

	elems8 := make([]Value, len(utf8Bytes))
	for i, b := range utf8Bytes {
		elems8[i] = MakeInt(int64(b), builtins.Uint8)
	}
	arrH8 := vmInstance.Heap.AllocArray(types.NoTypeID, elems8)
	ptr8Type := typesInterner.Intern(types.MakePointer(builtins.Uint8))
	ptr8 := MakePtr(Location{Kind: LKArrayElem, Handle: arrH8, Index: 0}, ptr8Type)
	len8 := MakeInt(int64(len(utf8Bytes)), builtins.Uint)

	utf16Val, vmErr := callStringIntrinsic(vmInstance, "rt_string_from_utf16", ptr16, len16, builtins.String)
	if vmErr != nil {
		t.Fatalf("rt_string_from_utf16 failed: %v", vmErr)
	}
	utf8Val, vmErr := callStringIntrinsic(vmInstance, "rt_string_from_bytes", ptr8, len8, builtins.String)
	if vmErr != nil {
		t.Fatalf("rt_string_from_bytes failed: %v", vmErr)
	}

	if utf16Val.Kind != VKHandleString || utf8Val.Kind != VKHandleString {
		t.Fatalf("expected string handles, got %v and %v", utf16Val.Kind, utf8Val.Kind)
	}
	s16 := vmInstance.Heap.Get(utf16Val.H).Str
	s8 := vmInstance.Heap.Get(utf8Val.H).Str
	if s16 != s8 {
		t.Fatalf("utf16/utf8 mismatch: %q vs %q", s16, s8)
	}
	if s16 != "A🙂" {
		t.Fatalf("unexpected utf16 result: %q", s16)
	}
}
