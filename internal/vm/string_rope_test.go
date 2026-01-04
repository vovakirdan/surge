package vm

import (
	"strings"
	"testing"
	"unicode/utf8"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

func callUnaryIntrinsic(vm *VM, name string, arg Value, resultType types.TypeID) (Value, *VMError) {
	fn := &mir.Func{
		Locals: []mir.Local{
			{Name: "arg", Type: arg.TypeID},
			{Name: "out", Type: resultType},
		},
		Blocks: []mir.Block{{}},
		Entry:  0,
	}
	frame := NewFrame(fn)

	if vmErr := vm.writeLocal(frame, 0, arg); vmErr != nil {
		return Value{}, vmErr
	}

	call := mir.CallInstr{
		HasDst: true,
		Dst:    mir.Place{Local: 1},
		Callee: mir.Callee{Name: name},
		Args: []mir.Operand{
			{Kind: mir.OperandCopy, Place: mir.Place{Local: 0}, Type: arg.TypeID},
		},
	}

	var writes []LocalWrite
	if vmErr := vm.callIntrinsic(frame, &call, &writes); vmErr != nil {
		return Value{}, vmErr
	}
	return vm.readLocal(frame, 1)
}

func callUnaryIntrinsicNoDst(vm *VM, name string, arg Value) *VMError {
	fn := &mir.Func{
		Locals: []mir.Local{
			{Name: "arg", Type: arg.TypeID},
		},
		Blocks: []mir.Block{{}},
		Entry:  0,
	}
	frame := NewFrame(fn)

	if vmErr := vm.writeLocal(frame, 0, arg); vmErr != nil {
		return vmErr
	}

	call := mir.CallInstr{
		HasDst: false,
		Callee: mir.Callee{Name: name},
		Args: []mir.Operand{
			{Kind: mir.OperandCopy, Place: mir.Place{Local: 0}, Type: arg.TypeID},
		},
	}

	var writes []LocalWrite
	return vm.callIntrinsic(frame, &call, &writes)
}

func makeRangeValue(vm *VM, start, end *int, inclusive bool, intType types.TypeID) Value {
	var startVal Value
	hasStart := false
	if start != nil {
		startVal = MakeInt(int64(*start), intType)
		hasStart = true
	}
	var endVal Value
	hasEnd := false
	if end != nil {
		endVal = MakeInt(int64(*end), intType)
		hasEnd = true
	}
	h := vm.Heap.AllocRange(types.NoTypeID, startVal, endVal, hasStart, hasEnd, inclusive)
	return MakeHandleRange(h, types.NoTypeID)
}

func runeAtIndex(s string, idx int) (rune, bool) {
	runes := []rune(s)
	if len(runes) == 0 {
		return 0, false
	}
	if idx < 0 {
		idx += len(runes)
	}
	if idx < 0 || idx >= len(runes) {
		return 0, false
	}
	return runes[idx], true
}

func assertRangeSliceMatches(t *testing.T, vm *VM, val Value, rangeVal Value, flat string, intType types.TypeID) {
	t.Helper()
	strObj := vm.Heap.Get(val.H)
	rObj := vm.Heap.Get(rangeVal.H)
	start, end, vmErr := vm.rangeBounds(&rObj.Range, vm.stringCPLen(strObj))
	if vmErr != nil {
		vm.dropValue(rangeVal)
		t.Fatalf("rangeBounds failed: %v", vmErr)
	}
	if start > end {
		start = end
	}
	runes := []rune(flat)
	if start > len(runes) {
		start = len(runes)
	}
	if end > len(runes) {
		end = len(runes)
	}
	want := string(runes[start:end])

	sliceVal, vmErr := vm.evalStringIndex(val, rangeVal)
	vm.dropValue(rangeVal)
	if vmErr != nil {
		if sliceVal.IsHeap() {
			vm.dropValue(sliceVal)
		}
		t.Fatalf("evalStringIndex failed: %v", vmErr)
	}
	assertStringState(t, vm, sliceVal, want, intType, false)
	vm.dropValue(sliceVal)
}

func assertStringState(t *testing.T, vm *VM, val Value, flat string, intType types.TypeID, checkRanges bool) {
	t.Helper()
	if val.Kind != VKHandleString {
		t.Fatalf("expected string handle, got %v", val.Kind)
	}
	obj := vm.Heap.Get(val.H)
	gotCP := vm.stringCPLen(obj)
	wantCP := utf8.RuneCountInString(flat)
	if gotCP != wantCP {
		t.Fatalf("cp len mismatch: want %d, got %d", wantCP, gotCP)
	}
	gotBytes := vm.stringByteLen(obj)
	if gotBytes != len(flat) {
		t.Fatalf("byte len mismatch: want %d, got %d", len(flat), gotBytes)
	}

	indices := []int{0, 1, gotCP / 2, gotCP - 1, -1}
	for _, idx := range indices {
		expected, ok := runeAtIndex(flat, idx)
		if !ok {
			continue
		}
		idxVal := MakeInt(int64(idx), intType)
		gotVal, vmErr := vm.evalStringIndex(val, idxVal)
		if vmErr != nil {
			t.Fatalf("index %d failed: %v", idx, vmErr)
		}
		if gotVal.Kind != VKInt || gotVal.Int != int64(expected) {
			t.Fatalf("index %d mismatch: want %d, got %v", idx, expected, gotVal)
		}
	}

	if checkRanges {
		if gotCP > 1 {
			start := 1
			end := gotCP - 1
			rangeVal := makeRangeValue(vm, &start, &end, false, intType)
			assertRangeSliceMatches(t, vm, val, rangeVal, flat, intType)
		}
		if gotCP > 0 {
			end := -1
			rangeVal := makeRangeValue(vm, nil, &end, false, intType)
			assertRangeSliceMatches(t, vm, val, rangeVal, flat, intType)
		}
		if gotCP > 2 {
			start := 1
			end := 1
			rangeVal := makeRangeValue(vm, &start, &end, true, intType)
			assertRangeSliceMatches(t, vm, val, rangeVal, flat, intType)
		}
	}

	flatBytes := vm.stringBytes(obj)
	if flatBytes != flat {
		t.Fatalf("flatten mismatch: want %q, got %q", flat, flatBytes)
	}
	copyVal := MakeHandleString(vm.Heap.AllocString(val.TypeID, flat), val.TypeID)
	eqVal, vmErr := vm.evalBinaryOp(ast.ExprBinaryEq, val, copyVal)
	vm.dropValue(copyVal)
	if vmErr != nil {
		t.Fatalf("eq failed: %v", vmErr)
	}
	if eqVal.Kind != VKBool || !eqVal.Bool {
		t.Fatalf("eq mismatch for %q", flat)
	}
}

func TestStringRopeReferenceModel(t *testing.T) {
	requireVMBackend(t)
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, types.NewInterner(), nil)
	builtins := vmInstance.Types.Builtins()
	intType := builtins.Int

	makeStr := func(s string) Value {
		h := vmInstance.Heap.AllocString(builtins.String, s)
		return MakeHandleString(h, builtins.String)
	}

	seg1 := strings.Repeat("a", 90)
	seg2 := strings.Repeat("b", 40) + "\U0001F642" + strings.Repeat("c", 40)
	seg3 := strings.Repeat("d", 80)

	flat := seg1
	val := makeStr(seg1)
	assertStringState(t, vmInstance, val, flat, intType, true)

	segVal := makeStr(seg2)
	next, vmErr := vmInstance.concatStringValues(val, segVal)
	vmInstance.dropValue(val)
	vmInstance.dropValue(segVal)
	if vmErr != nil {
		t.Fatalf("concat seg2 failed: %v", vmErr)
	}
	val = next
	flat += seg2
	assertStringState(t, vmInstance, val, flat, intType, true)

	segVal = makeStr(seg3)
	next, vmErr = vmInstance.concatStringValues(val, segVal)
	vmInstance.dropValue(val)
	vmInstance.dropValue(segVal)
	if vmErr != nil {
		t.Fatalf("concat seg3 failed: %v", vmErr)
	}
	val = next
	flat += seg3
	assertStringState(t, vmInstance, val, flat, intType, true)

	vmInstance.dropValue(val)
}

func TestStringForceFlattenIntrinsic(t *testing.T) {
	requireVMBackend(t)
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, types.NewInterner(), nil)
	builtins := vmInstance.Types.Builtins()

	left := MakeHandleString(vmInstance.Heap.AllocString(builtins.String, strings.Repeat("a", 100)), builtins.String)
	right := MakeHandleString(vmInstance.Heap.AllocString(builtins.String, strings.Repeat("b", 100)), builtins.String)
	joined, vmErr := vmInstance.concatStringValues(left, right)
	vmInstance.dropValue(left)
	vmInstance.dropValue(right)
	if vmErr != nil {
		t.Fatalf("concat failed: %v", vmErr)
	}

	obj := vmInstance.Heap.Get(joined.H)
	if obj.StrFlatKnown {
		vmInstance.dropValue(joined)
		t.Fatalf("expected rope before flatten")
	}

	if vmErr := callUnaryIntrinsicNoDst(vmInstance, "rt_string_force_flatten", joined); vmErr != nil {
		vmInstance.dropValue(joined)
		t.Fatalf("force flatten failed: %v", vmErr)
	}
	if !obj.StrFlatKnown {
		vmInstance.dropValue(joined)
		t.Fatalf("expected flattened string")
	}
	if got := vmInstance.stringBytes(obj); got != strings.Repeat("a", 100)+strings.Repeat("b", 100) {
		vmInstance.dropValue(joined)
		t.Fatalf("flatten content mismatch: %q", got)
	}

	vmInstance.dropValue(joined)
}

func registerBytesViewType(t *testing.T, vm *VM) types.TypeID {
	t.Helper()
	if vm == nil || vm.Types == nil {
		t.Fatal("missing type interner")
	}
	if vm.Files == nil {
		vm.Files = source.NewFileSet()
	}
	content := []byte("type BytesView = { owner: string, ptr: *byte, len: uint, };")
	fileID := vm.Files.AddVirtual("bytes_view.sg", content)
	decl := source.Span{File: fileID, Start: 0, End: uint32(len(content))}

	nameInterner := source.NewInterner()
	bytesViewName := nameInterner.Intern("BytesView")
	ownerName := nameInterner.Intern("owner")
	ptrName := nameInterner.Intern("ptr")
	lenName := nameInterner.Intern("len")

	typeID := vm.Types.RegisterStruct(bytesViewName, decl)
	builtins := vm.Types.Builtins()
	ptrType := vm.Types.Intern(types.MakePointer(builtins.Uint8))
	vm.Types.SetStructFields(typeID, []types.StructField{
		{Name: ownerName, Type: builtins.String},
		{Name: ptrName, Type: ptrType},
		{Name: lenName, Type: builtins.Uint},
	})
	return typeID
}

func TestStringSliceAndBytesViewOwnership(t *testing.T) {
	requireVMBackend(t)
	vmInstance := New(nil, NewTestRuntime(nil, ""), source.NewFileSet(), types.NewInterner(), nil)
	builtins := vmInstance.Types.Builtins()
	bytesViewType := registerBytesViewType(t, vmInstance)

	base := MakeHandleString(vmInstance.Heap.AllocString(builtins.String, "abcdef"), builtins.String)
	baseObj := vmInstance.Heap.Get(base.H)
	if baseObj.RefCount != 1 {
		vmInstance.dropValue(base)
		t.Fatalf("expected base refcount 1, got %d", baseObj.RefCount)
	}

	start := 1
	end := 4
	rangeVal := makeRangeValue(vmInstance, &start, &end, false, builtins.Int)
	sliceVal, vmErr := vmInstance.evalStringIndex(base, rangeVal)
	vmInstance.dropValue(rangeVal)
	if vmErr != nil {
		vmInstance.dropValue(base)
		t.Fatalf("slice failed: %v", vmErr)
	}
	baseObj = vmInstance.Heap.Get(base.H)
	if baseObj.RefCount != 2 {
		vmInstance.dropValue(sliceVal)
		vmInstance.dropValue(base)
		t.Fatalf("expected base refcount 2 after slice, got %d", baseObj.RefCount)
	}
	vmInstance.dropValue(sliceVal)
	baseObj = vmInstance.Heap.Get(base.H)
	if baseObj.RefCount != 1 {
		vmInstance.dropValue(base)
		t.Fatalf("expected base refcount 1 after slice drop, got %d", baseObj.RefCount)
	}

	bvVal, vmErr := callUnaryIntrinsic(vmInstance, "rt_string_bytes_view", base, bytesViewType)
	if vmErr != nil {
		vmInstance.dropValue(base)
		t.Fatalf("bytes view failed: %v", vmErr)
	}
	baseObj = vmInstance.Heap.Get(base.H)
	if baseObj.RefCount != 2 {
		vmInstance.dropValue(bvVal)
		vmInstance.dropValue(base)
		t.Fatalf("expected base refcount 2 after bytes view, got %d", baseObj.RefCount)
	}
	info, vmErr := vmInstance.bytesViewLayout(bytesViewType)
	if vmErr != nil {
		vmInstance.dropValue(bvVal)
		vmInstance.dropValue(base)
		t.Fatalf("bytes view layout failed: %v", vmErr)
	}
	if !info.ok {
		vmInstance.dropValue(bvVal)
		vmInstance.dropValue(base)
		t.Fatal("bytes view layout invalid")
	}
	bvObj := vmInstance.Heap.Get(bvVal.H)
	ownerVal := bvObj.Fields[info.ownerIdx]
	if ownerVal.Kind != VKHandleString || ownerVal.H != base.H {
		vmInstance.dropValue(bvVal)
		vmInstance.dropValue(base)
		t.Fatalf("bytes view owner mismatch")
	}

	vmInstance.dropValue(bvVal)
	baseObj = vmInstance.Heap.Get(base.H)
	if baseObj.RefCount != 1 {
		vmInstance.dropValue(base)
		t.Fatalf("expected base refcount 1 after bytes view drop, got %d", baseObj.RefCount)
	}
	vmInstance.dropValue(base)
	obj, ok := vmInstance.Heap.lookup(base.H)
	if !ok || obj == nil || !obj.Freed {
		t.Fatalf("expected base string to be freed")
	}
}
