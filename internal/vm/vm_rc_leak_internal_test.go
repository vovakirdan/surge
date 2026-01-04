package vm

import (
	"strings"
	"testing"

	"surge/internal/types"
	"surge/internal/vm/bignum"
)

func TestVMRCLeakDetectionPanics(t *testing.T) {
	requireVMBackend(t)
	vmInstance := New(nil, NewTestRuntime(nil, ""), nil, types.NewInterner(), nil)
	vmInstance.Heap.AllocString(types.NoTypeID, "leak")
	vmInstance.Heap.AllocBigInt(types.NoTypeID, bignum.IntFromInt64(1))

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
			return
		}
		t.Fatal("expected panic, got nil")
	}()

	vmInstance.checkLeaksOrPanic()
}
