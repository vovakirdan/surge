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
	if err := mir.FinalizeModuleMeta(mod, typesIn, layout.X86_64LinuxGNU()); err != nil {
		t.Fatalf("finalize layouts: %v", err)
	}
	vmInstance := New(mod, NewTestRuntime(nil, ""), nil, typesIn, nil)

	// `o` is a struct { note: string, id: int } holding one heap string.
	noteHandle := vmInstance.Heap.AllocString(strTy, "kept")
	structHandle := vmInstance.Heap.AllocStruct(objTy, []Value{
		{Kind: VKHandleString, H: noteHandle},
		{Kind: VKInt, Int: 7},
	})

	frame := NewFrame(fn)
	frame.Locals[0] = LocalSlot{
		Name:   "o",
		V:      Value{Kind: VKHandleStruct, H: structHandle, TypeID: objTy},
		IsInit: true,
	}
	vmInstance.Stack = []*Frame{frame}

	if vmErr := vmInstance.execInstrDrop(frame, &fn.Blocks[0].Instrs[0]); vmErr != nil {
		t.Fatalf("projected drop failed: %v", vmErr)
	}

	// The field is gone...
	if obj, ok := vmInstance.Heap.lookup(noteHandle); !ok || obj == nil || !obj.Freed {
		t.Fatalf("the projected drop did not release the field it named")
	}
	// ...and the container it lived in is untouched.
	obj, ok := vmInstance.Heap.lookup(structHandle)
	if !ok || obj == nil || obj.Freed {
		t.Fatalf("the projected drop released the whole container")
	}
	// The slot must be CLEARED, or freeing the container releases the field a
	// second time — Heap.Free walks obj.Fields — and the refcount turns that
	// into a use-after-free panic.
	if obj.Fields[0].IsHeap() {
		t.Fatalf("the dropped field was left in the container and will be released again")
	}

	// Proof of that last point rather than assertion of it: dropping the
	// container now must not panic.
	vmInstance.Heap.Release(structHandle)
	if obj, ok := vmInstance.Heap.lookup(structHandle); !ok || obj == nil || !obj.Freed {
		t.Fatalf("releasing the container after a projected drop did not free it")
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
	strTy := typesIn.Builtins().String

	fn := &mir.Func{
		ID:     1,
		Sym:    symbols.NoSymbolID,
		Name:   "shallow_drop",
		Result: types.NoTypeID,
		Entry:  0,
		Locals: []mir.Local{{Name: "o", Type: types.NoTypeID, Flags: mir.LocalFlagOwnsHeap}},
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

	vmInstance := New(&mir.Module{}, NewTestRuntime(nil, ""), nil, typesIn, nil)

	// `moved` stands for a field that was given away: it is still sitting in
	// the container, and it belongs to whoever took it.
	movedHandle := vmInstance.Heap.AllocString(strTy, "given away")
	structHandle := vmInstance.Heap.AllocStruct(types.NoTypeID, []Value{
		{Kind: VKHandleString, H: movedHandle},
	})

	frame := NewFrame(fn)
	frame.Locals[0] = LocalSlot{
		Name:   "o",
		V:      Value{Kind: VKHandleStruct, H: structHandle},
		IsInit: true,
	}
	vmInstance.Stack = []*Frame{frame}

	if vmErr := vmInstance.execInstrDrop(frame, &fn.Blocks[0].Instrs[0]); vmErr != nil {
		t.Fatalf("shallow drop failed: %v", vmErr)
	}

	if obj, ok := vmInstance.Heap.lookup(structHandle); !ok || obj == nil || !obj.Freed {
		t.Fatalf("a shallow drop did not release the container")
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
