package vm

import (
	"testing"

	"surge/internal/mir"
)

// The storage an activation owns. These rows are about lifetime rather than
// contents: who has bytes, when those bytes go away, and what happens to a
// reference that outlives them.

// composite locals of the same function are laid out once and given to every
// activation, but the BYTES are per activation. Two activations sharing bytes
// would be one variable wearing two names, which is exactly what recursion
// would turn into a wrong answer.
func TestActivationsOfOneFunctionOwnDisjointStorage(t *testing.T) {
	f := newStorageFixture(t)
	fn := f.compositeFunc()

	first := f.vm.activate(fn)
	second := f.vm.activate(fn)

	if first.storage == nil || second.storage == nil {
		t.Fatal("an activation of a function with composite locals must own bytes")
	}
	if first.storage == second.storage {
		t.Fatal("two activations share one arena, so they share one variable")
	}
	if len(first.storage.bytes) == 0 {
		t.Fatal("the arena of an activation with composite locals is empty")
	}
	if first.storage.Generation() == 0 {
		t.Fatal("a live arena must not read as the zero generation, which is what a retired one reads as")
	}

	// The plan behind them is one plan: it is a pure function of the slot types
	// and the layout registry, so recomputing it per activation would be work
	// with no answer of its own.
	planned := f.vm.storagePlanFor(fn)
	asked := f.vm.storagePlanFor(fn)
	if planned != asked {
		t.Fatal("the storage plan of a function was computed twice")
	}
}

// Returning gives the storage back. The bytes are reused by whatever activation
// comes next, so a reference that survived the return names a different value at
// the same address — and it reads perfectly well. The generation is the only
// thing that can tell the two apart.
func TestActivationStorageIsRefusedAfterItRetires(t *testing.T) {
	f := newStorageFixture(t)
	frame := f.vm.activate(f.compositeFunc())

	held, err := f.vm.storageRefAt(frame.storage, f.vm.storagePlanFor(frame.Func).OffsetOf(0), f.node)
	if err != nil {
		t.Fatalf("naming a composite local must succeed: %v", err)
	}
	f.writeNode(t, held, 1, 2, 3, "belongs to the activation")
	if err := f.vm.storageDrop(held); err != nil {
		t.Fatalf("releasing what the local held must succeed: %v", err)
	}

	if vmErr := f.vm.retireActivation(frame); vmErr != nil {
		t.Fatalf("retiring an activation that owes nothing must succeed: %v", vmErr.Message)
	}

	if _, err := held.resolve(1); err == nil {
		t.Fatal("a reference into a retired activation still resolved")
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("retiring left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// A task state that borrows a slot as its backing storage keeps that storage
// standing after the activation leaves the stack. Retiring anyway would hand the
// suspended task bytes that the next activation is free to overwrite, and the
// generation would not refuse them, because the reference was formed while the
// storage was current.
func TestPinnedActivationKeepsItsStorageUntilTheLastPinGoes(t *testing.T) {
	f := newStorageFixture(t)
	frame := f.vm.activate(f.compositeFunc())

	borrowed, err := f.vm.storageRefAt(frame.storage, f.vm.storagePlanFor(frame.Func).OffsetOf(0), f.node)
	if err != nil {
		t.Fatalf("naming a composite local must succeed: %v", err)
	}
	pinFrameStorage(frame)
	pinFrameStorage(frame)

	if vmErr := f.vm.retireActivation(frame); vmErr != nil {
		t.Fatalf("retiring a pinned activation must not report a leak: %v", vmErr.Message)
	}
	if _, err := borrowed.resolve(1); err != nil {
		t.Fatalf("a pinned activation gave up its storage: %v", err)
	}

	unpinFrameStorage(frame)
	if vmErr := f.vm.retireActivation(frame); vmErr != nil {
		t.Fatalf("retiring with one pin left must not report a leak: %v", vmErr.Message)
	}
	if _, err := borrowed.resolve(1); err != nil {
		t.Fatalf("an activation with one pin still outstanding gave up its storage: %v", err)
	}

	unpinFrameStorage(frame)
	if vmErr := f.vm.retireActivation(frame); vmErr != nil {
		t.Fatalf("retiring an unpinned activation must succeed: %v", vmErr.Message)
	}
	if _, err := borrowed.resolve(1); err == nil {
		t.Fatal("the last pin was released and the storage stayed")
	}
}

// The instruction boundary is where temporaries go. A value built by one
// instruction and stored nowhere is released before the next one runs, which is
// what bounds the lifetime of everything an expression builds on its way to an
// answer.
func TestInstructionBoundaryReclaimsWhatThePreviousOneBuilt(t *testing.T) {
	f := newStorageFixture(t)
	frame := f.vm.activate(f.compositeFunc())

	if vmErr := f.vm.beginStep(frame); vmErr != nil {
		t.Fatalf("opening the first instruction must succeed: %v", vmErr.Message)
	}
	temporary, err := f.vm.reserveScratch(frame.scratch, f.node)
	if err != nil {
		t.Fatalf("building a temporary must succeed: %v", err)
	}
	abandoned := f.writeNode(t, temporary, 1, 2, 3, "never stored")

	if vmErr := f.vm.beginStep(frame); vmErr != nil {
		t.Fatalf("opening the next instruction must succeed: %v", vmErr.Message)
	}

	if obj, ok := f.vm.Heap.lookup(abandoned); !ok || !obj.Freed {
		t.Fatal("a temporary the previous instruction abandoned survived into the next one")
	}
	if frame.scratch.used != 0 {
		t.Fatalf("the boundary kept %d bytes of the previous instruction", frame.scratch.used)
	}
	if vmErr := f.vm.retireActivation(frame); vmErr != nil {
		t.Fatalf("retiring must succeed: %v", vmErr.Message)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("the boundary left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// A call is the one instruction that does not finish where it is dispatched.
// The callee runs its own boundaries while the caller's call instruction is
// still in progress, so those boundaries must not reach the caller's
// temporaries — which is what the caller's arguments are while the call is
// being set up.
func TestACalleeBoundaryLeavesTheCallersTemporariesAlone(t *testing.T) {
	f := newStorageFixture(t)
	fn := f.compositeFunc()
	caller := f.vm.activate(fn)

	if vmErr := f.vm.beginStep(caller); vmErr != nil {
		t.Fatalf("opening the call instruction must succeed: %v", vmErr.Message)
	}
	argument, err := f.vm.reserveScratch(caller.scratch, f.node)
	if err != nil {
		t.Fatalf("building an argument must succeed: %v", err)
	}
	held := f.writeNode(t, argument, 1, 2, 3, "an argument in flight")

	callee := f.vm.activate(fn)
	for range 3 {
		if vmErr := f.vm.beginStep(callee); vmErr != nil {
			t.Fatalf("the callee's boundaries must succeed: %v", vmErr.Message)
		}
	}
	if vmErr := f.vm.retireActivation(callee); vmErr != nil {
		t.Fatalf("retiring the callee must succeed: %v", vmErr.Message)
	}

	if obj, ok := f.vm.Heap.lookup(held); !ok || obj.Freed {
		t.Fatal("the callee's instruction boundary reclaimed an argument the caller was still holding")
	}
	if _, err := argument.resolve(1); err != nil {
		t.Fatalf("the caller's temporary stopped resolving across a call: %v", err)
	}

	if vmErr := f.vm.retireActivation(caller); vmErr != nil {
		t.Fatalf("retiring the caller must release what it still held: %v", vmErr.Message)
	}
	if f.vm.heapCounters.allocCount != f.vm.heapCounters.freeCount {
		t.Fatalf("a call left the heap unbalanced: %d allocations, %d frees",
			f.vm.heapCounters.allocCount, f.vm.heapCounters.freeCount)
	}
}

// compositeFunc is a function whose locals are the fixture's composites, which
// is what makes its activations own bytes at all.
func (f *storageFixture) compositeFunc() *mir.Func {
	return &mir.Func{
		Name:   "activation-under-test",
		Locals: []mir.Local{{Name: "first", Type: f.node}, {Name: "second", Type: f.node}},
		Blocks: []mir.Block{{}},
	}
}
