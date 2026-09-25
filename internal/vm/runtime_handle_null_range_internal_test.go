package vm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// A NULL `Range` that is USED is a runtime error, on both backends, in one
// sentence: `null range handle` under VM1203 (owner ruling 2026-09-25,
// docs/RUNTIME_V2.md "The Default Of A Runtime Handle"). The native runtime
// raises it from rt_range_require -- in both slice bounds readers, and before
// the emitted kind-byte load of a `for` or a `.next()` -- and these rows pin
// that the VM refuses it with the same words at each of its readers, instead
// of the heap's "invalid handle 0" or a type mismatch.
//
// The D2 return-origin gate refuses every program that reaches a Range default
// at this base, so no end-to-end row can pin it; the native half is pinned by
// the C stand (runtime_v2_null_range_stand_test.go) and the emitter row
// (internal/backend/llvm/emit_range_require_test.go).

// nullRangeWords is spelled out rather than read from the VM's constant, so a
// drift of either side's words is a red row here and in the C stand.
const nullRangeWords = "null range handle"

// refusalOf runs one reader and hands back what it refused with, whether the
// reader returned the error or the heap raised it (Heap.Get panics with a
// *VMError on handle 0, which is what an unguarded reader meets).
func refusalOf(read func() *VMError) (vmErr *VMError) {
	defer func() {
		if r := recover(); r != nil {
			raised, ok := r.(*VMError)
			if !ok {
				panic(r)
			}
			vmErr = raised
		}
	}()
	return read()
}

func wantNullRangeRefusal(t *testing.T, what string, vmErr *VMError) {
	t.Helper()
	if vmErr == nil || vmErr.Code != PanicInvalidHandle || vmErr.Message != nullRangeWords {
		t.Fatalf("%s: a null range must be refused as %q under %s, got %v",
			what, nullRangeWords, PanicInvalidHandle, vmErr)
	}
}

// rangeLocalFrame activates a function with one Range local holding val, so
// the iteration readers meet the null the way a program hands it to them.
func rangeLocalFrame(t *testing.T, fx nullHandleFixture, val Value) (*Frame, mir.Operand) {
	t.Helper()
	fn := &mir.Func{Locals: []mir.Local{{Type: fx.rangeHandle, Name: "r"}}, Blocks: []mir.Block{{}}}
	frame := fx.machine.activate(fn)
	fx.machine.Stack = append(fx.machine.Stack, frame)
	if vmErr := fx.machine.writeLocal(frame, 0, val); vmErr != nil {
		t.Fatalf("storing the range in a local must succeed: %v", vmErr)
	}
	return frame, mir.Operand{Kind: mir.OperandCopy, Type: fx.rangeHandle, Place: mir.Place{Kind: mir.PlaceLocal, Local: 0}}
}

// nullRangeArrayMachine is a VM holding one `int[]`, to slice by a null range:
// an array needs its element layout, which the handle fixture does not freeze.
func nullRangeArrayMachine(t *testing.T) (*VM, Value) {
	t.Helper()
	interner := types.NewInterner()
	builtins := interner.Builtins()
	arrType := interner.Intern(types.MakeArray(builtins.Int, types.ArrayDynamicLength))
	machine := New(withElementLayouts(t, interner, builtins.Int, arrType), NewTestRuntime(nil, ""), nil, interner, nil)
	h := machine.Heap.AllocArray(arrType, []Value{MakeInt(1, builtins.Int), MakeInt(2, builtins.Int)})
	return machine, MakeHandleArray(h, arrType)
}

func TestNullRangeIsRefusedByEveryVMReader(t *testing.T) {
	fx := newNullHandleFixture(t)
	nullRange, vmErr := fx.machine.defaultValue(fx.rangeHandle)
	if vmErr != nil {
		t.Fatalf("the default of Range must be the null handle: %v", vmErr)
	}
	str := MakeHandleString(fx.machine.Heap.AllocStringWithCPLen(types.NoTypeID, "abcdef", 6), types.NoTypeID)
	defer fx.machine.dropValue(str)
	arrMachine, arr := nullRangeArrayMachine(t)
	defer arrMachine.dropValue(arr)

	// Both spellings of the null: the typed null a default builds, and the
	// `nothing` a zeroed Range cell decodes to.
	for _, spelling := range []Value{nullRange, MakeNothing()} {
		name := spelling.Kind.String()
		wantNullRangeRefusal(t, "rangeFromValue("+name+")", refusalOf(func() *VMError {
			_, vmErr := fx.machine.rangeFromValue(spelling)
			return vmErr
		}))
		wantNullRangeRefusal(t, "string slice by "+name, refusalOf(func() *VMError {
			_, vmErr := fx.machine.evalStringIndex(str, spelling)
			return vmErr
		}))
		wantNullRangeRefusal(t, "array slice by "+name, refusalOf(func() *VMError {
			_, vmErr := arrMachine.evalArrayIndex(arr, spelling)
			return vmErr
		}))
	}

	// Iteration: a `for` over the range (iter_init), a step of a range that is
	// already a cursor (iter_next), and an explicit `.next()`.
	frame, operand := rangeLocalFrame(t, fx, nullRange)
	wantNullRangeRefusal(t, "for over a null range", refusalOf(func() *VMError {
		_, vmErr := fx.machine.evalIterInit(frame, &mir.IterInit{Iterable: operand})
		return vmErr
	}))
	wantNullRangeRefusal(t, "iter_next of a null range", refusalOf(func() *VMError {
		_, vmErr := fx.machine.evalIterNext(frame, &mir.IterNext{Iter: operand})
		return vmErr
	}))
	wantNullRangeRefusal(t, ".next() of a null range", refusalOf(func() *VMError {
		return fx.machine.handleRangeNext(frame, &mir.CallInstr{Args: []mir.Operand{operand}}, nil)
	}))
}

// The control: the null check refuses only the null, and a live range is
// still read and sliced past it.
func TestALiveRangeIsReadPastTheNullCheck(t *testing.T) {
	fx := newNullHandleFixture(t)
	i64 := fx.machine.Types.Intern(types.Type{Kind: types.KindInt, Width: 64})
	live := MakeHandleRange(fx.machine.Heap.AllocRange(fx.rangeHandle, MakeInt(1, i64), MakeInt(3, i64), true, true, false), fx.rangeHandle)
	defer fx.machine.dropValue(live)
	if _, vmErr := fx.machine.rangeFromValue(live); vmErr != nil {
		t.Fatalf("a live range must be read, got %v", vmErr)
	}
	str := MakeHandleString(fx.machine.Heap.AllocStringWithCPLen(types.NoTypeID, "abcdef", 6), types.NoTypeID)
	defer fx.machine.dropValue(str)
	sliced, vmErr := fx.machine.evalStringIndex(str, live)
	if vmErr != nil {
		t.Fatalf("slicing by a live range must succeed, got %v", vmErr)
	}
	defer fx.machine.dropValue(sliced)
	if got := fx.machine.stringBytes(fx.machine.Heap.Get(sliced.H)); got != "bc" {
		t.Fatalf("\"abcdef\"[1..3] is %q, want \"bc\"", got)
	}
}
