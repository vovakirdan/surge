package vm

import (
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// A drop of a PROJECTED place must release that place and nothing else.
//
// The VM used to ignore `Place.Proj` entirely and drop the whole local, so
// `@drop o.inner` took `o` with it and the next read of `o` panicked with
// use-after-free. That is worse than the native backend's version of the same
// gap, which merely emitted nothing.
//
// No source syntax produces this yet — Epic 24's gate and the step-5 refusal
// both block it — so the instruction is driven directly. The point is that the
// VM has to be right BEFORE anything is allowed to produce the shape.
func TestVMDropOfProjectedPlaceReleasesOnlyThatField(t *testing.T) {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	strTy := typesIn.Builtins().String
	objTy := typesIn.RegisterStruct(typesIn.Strings.Intern("Object"), source.Span{})
	typesIn.SetStructFields(objTy, []types.StructField{
		{Name: typesIn.Strings.Intern("note"), Type: strTy},
		{Name: typesIn.Strings.Intern("id"), Type: typesIn.Builtins().Int},
	})

	fn := &mir.Func{
		ID:     1,
		Sym:    symbols.NoSymbolID,
		Name:   "projected_drop",
		Result: types.NoTypeID,
		Entry:  0,
		Locals: []mir.Local{{Name: "o", Type: objTy, Flags: mir.LocalFlagOwnsHeap}},
		Blocks: []mir.Block{{
			ID: 0,
			Instrs: []mir.Instr{{
				Kind: mir.InstrDrop,
				Drop: mir.DropInstr{Place: mir.Place{
					Local: 0,
					Proj:  []mir.PlaceProj{{Kind: mir.PlaceProjField, FieldName: "note", FieldIdx: 0}},
				}},
			}},
			Term: mir.Terminator{Kind: mir.TermReturn},
		}},
		Span: source.Span{Start: 1, End: 1},
	}

	mod := &mir.Module{Funcs: map[mir.FuncID]*mir.Func{fn.ID: fn}, Meta: &mir.ModuleMeta{}}
	if err := mir.FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU(), nil); err != nil {
		t.Fatalf("finalize layouts: %v", err)
	}
	vmInstance := New(mod, NewTestRuntime(nil, ""), nil, typesIn, nil)

	// `o` is a struct { note: string, id: int } holding one heap string. The
	// activation gives it the storage it occupies: a composite IS its bytes, so
	// there is no container object to hand the slot instead.
	frame := vmInstance.activate(fn)
	vmInstance.Stack = []*Frame{frame}

	noteHandle := vmInstance.Heap.AllocString(strTy, "kept")
	built, vmErr := vmInstance.buildStruct(frame, objTy, []Value{
		MakeHandleString(noteHandle, strTy),
		MakeInt(7, typesIn.Builtins().Int),
	})
	if vmErr != nil {
		t.Fatalf("build the struct: %v", vmErr)
	}
	if storeErr := vmInstance.writeLocal(frame, 0, built); storeErr != nil {
		t.Fatalf("store the struct: %v", storeErr)
	}

	if dropErr := vmInstance.execInstrDrop(frame, &fn.Blocks[0].Instrs[0]); dropErr != nil {
		t.Fatalf("projected drop failed: %v", dropErr)
	}

	// The field is gone...
	if obj, ok := vmInstance.Heap.lookup(noteHandle); !ok || obj == nil || !obj.Freed {
		t.Fatalf("the projected drop did not release the field it named")
	}
	// ...and the rest of the container it lived in is untouched.
	owner, isComposite := frame.Locals[0].V.Storage()
	if !isComposite {
		t.Fatalf("the projected drop took the whole container with it")
	}
	id, vmErr := vmInstance.peekMember(owner, 1)
	if vmErr != nil {
		t.Fatalf("read the surviving field: %v", vmErr)
	}
	if id.Kind != VKInt || id.Int != 7 {
		t.Fatalf("the projected drop disturbed a field it did not name: %v", id)
	}
	// The member must be CLEARED, or dropping the container releases the field
	// a second time — a drop walks every member it still owns — and the
	// refcount turns that into a use-after-free panic.
	note, vmErr := vmInstance.peekMember(owner, 0)
	if vmErr != nil {
		t.Fatalf("read the dropped field: %v", vmErr)
	}
	if note.IsHeap() {
		t.Fatalf("the dropped field was left in the container and will be released again")
	}

	// Proof of that last point rather than assertion of it: dropping the
	// container now must not release the field twice.
	vmInstance.dropValue(frame.Locals[0].V)
	if obj, ok := vmInstance.Heap.lookup(noteHandle); !ok || obj == nil || obj.RefCount != 0 {
		t.Fatalf("dropping the container after a projected drop touched the field again")
	}
}

// A whole-local drop must keep working exactly as before: the projected path is
// an addition, not a replacement, and a gate that sent every drop through the
// new code would pass the row above while breaking everything else.
func TestVMWholeLocalDropStillReleasesTheBinding(t *testing.T) {
	typesIn := types.NewInterner()
	strTy := typesIn.Builtins().String

	fn := &mir.Func{
		ID:     1,
		Sym:    symbols.NoSymbolID,
		Name:   "whole_drop",
		Result: types.NoTypeID,
		Entry:  0,
		Locals: []mir.Local{{Name: "s", Type: types.NoTypeID, Flags: mir.LocalFlagOwnsHeap}},
		Blocks: []mir.Block{{
			ID: 0,
			Instrs: []mir.Instr{{
				Kind: mir.InstrDrop,
				Drop: mir.DropInstr{Place: mir.Place{Local: 0}},
			}},
			Term: mir.Terminator{Kind: mir.TermReturn},
		}},
		Span: source.Span{Start: 1, End: 1},
	}

	vmInstance := New(&mir.Module{}, NewTestRuntime(nil, ""), nil, typesIn, nil)
	handle := vmInstance.Heap.AllocString(strTy, "whole")

	frame := NewFrame(fn)
	frame.Locals[0] = LocalSlot{Name: "s", V: Value{Kind: VKHandleString, H: handle}, IsInit: true}
	vmInstance.Stack = []*Frame{frame}

	if vmErr := vmInstance.execInstrDrop(frame, &fn.Blocks[0].Instrs[0]); vmErr != nil {
		t.Fatalf("whole-local drop failed: %v", vmErr)
	}
	if obj, ok := vmInstance.Heap.lookup(handle); !ok || obj == nil || !obj.Freed {
		t.Fatalf("a whole-local drop stopped releasing its binding")
	}
	if !frame.Locals[0].IsDropped {
		t.Fatalf("a whole-local drop no longer marks its slot dropped")
	}
}

// The closing move of a residual drop: after the live fields have been dropped
// one at a time, the container's own storage goes and what is still sitting in
// it — the fields that moved away — is left alone.
//
// This is the operation that makes a residual drop expressible without a
// sentinel value in the moved field. Nulling the slot and deep-dropping as
// usual would work today and stop working the moment a field is stored inline,
// which is the representation this epic is clearing the way for.
func TestVMShallowDropReleasesTheContainerAndNotItsContents(t *testing.T) {
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	strTy := typesIn.Builtins().String
	objTy := typesIn.RegisterStruct(typesIn.Strings.Intern("Holder"), source.Span{})
	typesIn.SetStructFields(objTy, []types.StructField{
		{Name: typesIn.Strings.Intern("moved"), Type: strTy},
	})

	fn := &mir.Func{
		ID:     1,
		Sym:    symbols.NoSymbolID,
		Name:   "shallow_drop",
		Result: types.NoTypeID,
		Entry:  0,
		Locals: []mir.Local{{Name: "o", Type: objTy, Flags: mir.LocalFlagOwnsHeap}},
		Blocks: []mir.Block{{
			ID: 0,
			Instrs: []mir.Instr{{
				Kind: mir.InstrDrop,
				Drop: mir.DropInstr{Place: mir.Place{Local: 0}, Shallow: true},
			}},
			Term: mir.Terminator{Kind: mir.TermReturn},
		}},
		Span: source.Span{Start: 1, End: 1},
	}

	mod := &mir.Module{Funcs: map[mir.FuncID]*mir.Func{fn.ID: fn}, Meta: &mir.ModuleMeta{}}
	if err := mir.FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU(), nil); err != nil {
		t.Fatalf("finalize layouts: %v", err)
	}
	vmInstance := New(mod, NewTestRuntime(nil, ""), nil, typesIn, nil)
	frame := vmInstance.activate(fn)
	vmInstance.Stack = []*Frame{frame}

	// `moved` stands for a field that was given away: its bytes are still
	// sitting in the container, and the count they name belongs to whoever took
	// it. A shallow drop is what lets the container go without disturbing that.
	movedHandle := vmInstance.Heap.AllocString(strTy, "given away")
	built, vmErr := vmInstance.buildStruct(frame, objTy, []Value{
		MakeHandleString(movedHandle, strTy),
	})
	if vmErr != nil {
		t.Fatalf("build the struct: %v", vmErr)
	}
	if storeErr := vmInstance.writeLocal(frame, 0, built); storeErr != nil {
		t.Fatalf("store the struct: %v", storeErr)
	}

	if dropErr := vmInstance.execInstrDrop(frame, &fn.Blocks[0].Instrs[0]); dropErr != nil {
		t.Fatalf("shallow drop failed: %v", dropErr)
	}

	// A composite has no container allocation to free, so "the container is
	// gone" is that its extent no longer holds anything.
	owner, isComposite := frame.Locals[0].V.Storage()
	if !isComposite {
		t.Fatalf("the slot stopped naming its storage")
	}
	moved, vmErr := vmInstance.peekMember(owner, 0)
	if vmErr != nil {
		t.Fatalf("read the member after the shallow drop: %v", vmErr)
	}
	if moved.IsHeap() {
		t.Fatalf("a shallow drop left the container still naming what moved away")
	}
	// The whole point: what moved away survives, because it is no longer this
	// value's to release.
	obj, ok := vmInstance.Heap.lookup(movedHandle)
	if !ok || obj == nil {
		t.Fatalf("the moved-away field vanished from the heap")
	}
	if obj.Freed {
		t.Fatalf("a shallow drop released a field that had moved away")
	}

	// Whoever took it can still release it, exactly once.
	vmInstance.Heap.Release(movedHandle)
	if obj, ok := vmInstance.Heap.lookup(movedHandle); !ok || obj == nil || !obj.Freed {
		t.Fatalf("the new owner could not release what it had taken")
	}
}
