package vm

import (
	"testing"

	"surge/internal/mir"
)

// The runtime guard on a store through a non-mutable location, exercised
// directly.
//
// Sema now refuses every source that could reach it, so nothing the compiler
// produces trips this any more. It stays because unreachable-by-construction
// and unreachable-in-fact are different claims: the VM also executes MIR that
// arrives from a test harness, a fixture, or a future lowering, and this is the
// one place that still carries a mutability bit at all. The native backend has
// no equivalent — a reference there is a bare pointer — which is exactly why
// the rule cannot be left to run time on either side.
//
// Driving storeLocation rather than a program is deliberate: a test that went
// through the compiler would be asserting that sema still lets the bad store
// past, which is the thing that was just fixed.
func TestStoreThroughANonMutableLocationStillTraps(t *testing.T) {
	fn := &mir.Func{
		ID:     mir.FuncID(1),
		Name:   "holder",
		Locals: []mir.Local{{Name: "x"}},
		Blocks: []mir.Block{{ID: 0}},
	}
	vmInstance := New(&mir.Module{}, NewTestRuntime(nil, ""), nil, nil, nil)
	frame := NewFrame(fn)
	frame.Locals[0] = LocalSlot{Name: "x", V: Value{Kind: VKInt, Int: 1}, IsInit: true}
	vmInstance.Stack = []*Frame{frame}

	shared := Location{Kind: LKLocal, FrameRef: frame, Local: 0, IsMut: false}
	vmErr := vmInstance.storeLocation(shared, Value{Kind: VKInt, Int: 2})
	if vmErr == nil {
		t.Fatal("a store through a non-mutable location was allowed")
	}
	if vmErr.Code != PanicStoreThroughNonMutRef {
		t.Fatalf("expected %v, got %v", PanicStoreThroughNonMutRef, vmErr.Code)
	}
	if frame.Locals[0].V.Int != 1 {
		t.Fatalf("the refused store still landed: local is %d, want 1", frame.Locals[0].V.Int)
	}

	// The positive control: the same store through a mutable location is the
	// operation this guard must not be blocking.
	exclusive := Location{Kind: LKLocal, FrameRef: frame, Local: 0, IsMut: true}
	if vmErr := vmInstance.storeLocation(exclusive, Value{Kind: VKInt, Int: 2}); vmErr != nil {
		t.Fatalf("a store through a mutable location was refused: %v", vmErr)
	}
	if frame.Locals[0].V.Int != 2 {
		t.Fatalf("the allowed store did not land: local is %d, want 2", frame.Locals[0].V.Int)
	}
}
