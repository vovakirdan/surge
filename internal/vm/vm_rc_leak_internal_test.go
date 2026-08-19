package vm

import (
	"strings"
	"testing"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

// The leak detector's positive control, and since 2026-08-11 the only one.
//
// It used to have a second: `testdata/golden/vm_rc/vm_rc_leak_panics.sg` built
// two arrays pointing at each other and expected `panic VM3302 ... (array=2)`.
// That program depended on `*a_ref` handing out a second owner of a heap array,
// which the language now refuses — and refusing it is what makes an
// unreclaimable cycle unconstructible from source at all, so there is no
// end-to-end program left to write. The fixture moved to
// `testdata/golden/sema/invalid/move_out_of_shared_borrow.sg`, where it records
// the refusal instead.
//
// What that fixture covered and this test did not is the ARRAY leg of the leak
// report, so an array is allocated here too. Reaching the detector directly is
// the honest way to keep the control: it asks whether the detector fires and
// what it says, which was always the question — the cycle was only a way to
// leave something alive.
func TestVMRCLeakDetectionPanics(t *testing.T) {
	requireVMBackend(t)
	typesInterner := types.NewInterner()
	builtins := typesInterner.Builtins()
	arrType := typesInterner.Intern(types.MakeArray(builtins.Int, types.ArrayDynamicLength))
	vmInstance := New(withElementLayouts(t, typesInterner, builtins.Int, arrType),
		NewTestRuntime(nil, ""), nil, typesInterner, nil)
	vmInstance.Heap.AllocString(types.NoTypeID, "leak")
	vmInstance.Heap.AllocBigInt(types.NoTypeID, bignum.IntFromInt64(1))
	vmInstance.Heap.AllocArray(arrType, nil)

	defer func() {
		if r := recover(); r != nil {
			err, ok := r.(*VMError)
			if !ok {
				t.Fatalf("unexpected panic type: %T", r)
			}
			if err.Code != PanicRCHeapLeakDetected {
				t.Fatalf("expected %v, got %v", PanicRCHeapLeakDetected, err.Code)
			}
			if !strings.Contains(err.Message, "heap leak detected") {
				t.Fatalf("expected leak message, got: %q", err.Message)
			}
			if !strings.Contains(err.Message, "bigint=1") {
				t.Fatalf("expected bigint leak in message, got: %q", err.Message)
			}
			if !strings.Contains(err.Message, "string=1") {
				t.Fatalf("expected string leak in message, got: %q", err.Message)
			}
			if !strings.Contains(err.Message, "array=1") {
				t.Fatalf("expected array leak in message, got: %q", err.Message)
			}
			return
		}
		t.Fatal("expected panic, got nil")
	}()

	vmInstance.checkLeaksOrPanic()
}
