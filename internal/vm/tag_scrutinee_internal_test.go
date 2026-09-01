package vm

import (
	"reflect"
	"testing"

	"surge/internal/mir"
)

// Rule 13: a tag temporary may name exact storage, but it may not become a
// universal Value owner again. This assertion is deliberately about the type
// graph rather than the current field name, so renaming the old sidecar cannot
// make the negative control pass.
func TestTagScrutineeHasNoUniversalValueOwner(t *testing.T) {
	forbidden := reflect.TypeOf(Value{})
	typeOfScrutinee := reflect.TypeOf(tagScrutinee{})
	for i := range typeOfScrutinee.NumField() {
		field := typeOfScrutinee.Field(i)
		if field.Type == forbidden {
			t.Fatalf("tag scrutinee field %s is a universal Value owner", field.Name)
		}
	}
}

func TestTagScrutineeDropsItsExactCompositeStorage(t *testing.T) {
	fixture := newStorageFixture(t)
	storage := fixture.ref(t, 0, fixture.node)
	handle := fixture.writeNode(t, storage, 1, 2, 3, "tag temporary")

	scrutinee := tagScrutinee{kind: VKComposite}
	scrutinee.captureOwnership(MakeComposite(storage))
	if scrutinee.ownership != tagScrutineeOwnsStorage {
		t.Fatalf("tag scrutinee ownership = %d, want exact storage", scrutinee.ownership)
	}
	if scrutinee.storage.Arena != storage.Arena || scrutinee.storage.Offset != storage.Offset ||
		scrutinee.storage.TypeID != storage.TypeID {
		t.Fatal("tag scrutinee did not retain the exact composite extent")
	}

	fixture.vm.releaseTagScrutinee(scrutinee)
	object, ok := fixture.vm.Heap.lookup(handle)
	if !ok || object == nil || !object.Freed {
		t.Fatal("releasing the tag scrutinee did not drop its exact storage")
	}
}

func TestTagScrutineeDropsItsConcreteHeapHandle(t *testing.T) {
	fixture := newStorageFixture(t)
	handle := fixture.vm.Heap.AllocString(fixture.text, "scalar temporary")

	scrutinee := tagScrutinee{kind: VKHandleString}
	scrutinee.captureOwnership(MakeHandleString(handle, fixture.text))
	if scrutinee.ownership != tagScrutineeOwnsHeap || scrutinee.heap != handle {
		t.Fatalf("tag scrutinee heap owner = (%d, %d), want (%d, %d)",
			scrutinee.ownership, scrutinee.heap, tagScrutineeOwnsHeap, handle)
	}

	fixture.vm.releaseTagScrutinee(scrutinee)
	object, ok := fixture.vm.Heap.lookup(handle)
	if !ok || object == nil || !object.Freed {
		t.Fatal("releasing the tag scrutinee did not drop its concrete heap handle")
	}
}

func TestTagScrutineeRetainsHeapLoadedThroughReference(t *testing.T) {
	fixture := newStorageFixture(t)
	handle := fixture.vm.Heap.AllocString(fixture.text, "borrowed heap")
	frame := &Frame{
		Locals:  []LocalSlot{{V: MakeHandleString(handle, fixture.text), IsInit: true, TypeID: fixture.text}},
		scratch: newScratch(),
	}
	fixture.vm.Stack = []*Frame{frame}
	op := mir.Operand{
		Kind:  mir.OperandAddrOf,
		Place: mir.Place{Kind: mir.PlaceLocal, Local: 0},
	}

	scrutinee, vmErr := fixture.vm.evalTagScrutinee(frame, &op)
	if vmErr != nil {
		t.Fatalf("evaluate heap through reference: %v", vmErr)
	}
	if scrutinee.ownership != tagScrutineeOwnsHeap || !scrutinee.viaRef || scrutinee.heap != handle {
		t.Fatalf("heap scrutinee = %#v, want retained reference load", scrutinee)
	}
	if got := fixture.vm.Heap.Get(handle).RefCount; got != 2 {
		t.Fatalf("heap refcount after reference load = %d, want 2", got)
	}

	fixture.vm.releaseTagScrutinee(scrutinee)
	if got := fixture.vm.Heap.Get(handle).RefCount; got != 1 {
		t.Fatalf("heap refcount after scrutinee release = %d, want 1", got)
	}
	fixture.vm.Heap.Release(handle)
}

func TestTagPayloadMismatchReleasesOwnedScrutinee(t *testing.T) {
	fixture := newStorageFixture(t)
	storage := fixture.ref(t, 2, fixture.choice)
	shape, err := fixture.vm.unionMembers(fixture.choice)
	if err != nil {
		t.Fatalf("describe fixture union: %v", err)
	}
	wrapped, err := fixture.vm.storageCaseIndexOf(fixture.choice, shape, fixture.wrap)
	if err != nil {
		t.Fatalf("find Wrapped arm: %v", err)
	}
	if err := fixture.vm.storageSetActiveCase(storage, shape, wrapped); err != nil {
		t.Fatalf("activate Wrapped arm: %v", err)
	}
	defer func() {
		if err := fixture.vm.storageDrop(storage); err != nil {
			t.Errorf("drop original union: %v", err)
		}
	}()

	frame := &Frame{
		Locals:  []LocalSlot{{V: MakeComposite(storage), IsInit: true, TypeID: fixture.choice}},
		scratch: newScratch(),
	}
	fixture.vm.Stack = []*Frame{frame}
	tp := &mir.TagPayload{
		Value: mir.Operand{
			Kind:  mir.OperandCopy,
			Place: mir.Place{Kind: mir.PlaceLocal, Local: 0},
		},
		TagName: "Bare",
		Index:   0,
	}

	if _, vmErr := fixture.vm.evalTagPayload(frame, tp); vmErr == nil {
		t.Fatal("mismatched payload unexpectedly succeeded")
	} else if vmErr.Code != PanicTagPayloadTagMismatch {
		t.Fatalf("payload mismatch code = %v, want %v", vmErr.Code, PanicTagPayloadTagMismatch)
	}
	if len(frame.scratch.entries) != 1 || frame.scratch.entries[0].live {
		t.Fatalf("owned scratch scrutinee survived payload error: %#v", frame.scratch.entries)
	}
	if arm, vmErr := fixture.vm.tagArmName(storage); vmErr != nil {
		t.Fatalf("read original union after payload error: %v", vmErr)
	} else if arm != "Wrapped" {
		t.Fatalf("original union arm after payload error = %q, want Wrapped", arm)
	}
}
