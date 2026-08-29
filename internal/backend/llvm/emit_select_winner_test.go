package llvm

import (
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

// These stands are LLVM-only on purpose. The word bridge they pin the removal
// of was an emitter helper: the VM lane never reinterpreted a select's answer,
// it dispatched on the index the interpreter already held, so there is nothing
// on that lane for this shape to regress into. What both lanes share -- that a
// select's winner is the arm the program then runs -- is behaviour, and the
// select e2e fixtures assert it on both.

func selectWinnerEmitter(t *testing.T) (*Emitter, types.Builtins) {
	t.Helper()
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	registry, err := layout.FinalizeRegistry(layout.New(layout.X86_64LinuxGNU(), typesIn), nil)
	if err != nil {
		t.Fatalf("FinalizeRegistry: %v", err)
	}
	emitter := &Emitter{
		mod:   &mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}},
		types: typesIn,
	}
	return emitter, typesIn.Builtins()
}

func selectWinnerDestination(emitter *Emitter, dstType types.TypeID) *funcEmitter {
	return &funcEmitter{
		emitter:     emitter,
		f:           &mir.Func{Locals: []mir.Local{{Type: dstType, Name: "select_index"}}},
		localAlloca: map[mir.LocalID]string{mir.LocalID(0): "select_index"},
	}
}

// TestSelectWinnerIndexArrivesAsTheIndexType pins the accepted shape: the word
// is narrowed to the winner index's own type and stored, with no
// reinterpretation anywhere on the path.
func TestSelectWinnerIndexArrivesAsTheIndexType(t *testing.T) {
	emitter, builtins := selectWinnerEmitter(t)
	fe := selectWinnerDestination(emitter, builtins.Int32)

	if err := fe.emitSelectWinnerIndex("%bits", mir.Place{Local: mir.LocalID(0)}); err != nil {
		t.Fatalf("typed winner index refused: %v", err)
	}
	ir := emitter.buf.String()
	if !strings.Contains(ir, "trunc i64 %bits to i32") {
		t.Fatalf("winner index did not narrow to the index type:\n%s", ir)
	}
	if !strings.Contains(ir, "store i32 ") {
		t.Fatalf("winner index was not stored as the index type:\n%s", ir)
	}
	for _, reinterpretation := range []string{"inttoptr", "ptrtoint", "bitcast"} {
		if strings.Contains(ir, reinterpretation) {
			t.Fatalf("winner index still travels through %s:\n%s", reinterpretation, ir)
		}
	}
}

// TestSelectWinnerIndexRefusesAWordDestination is the negative control, and it
// is the whole proof: a winner index that arrives TYPED and one that arrives
// through a WORD are told apart here and nowhere else.
//
// Every row below is a destination type the deleted emitI64ToValue accepted by
// rebuilding a value out of the same 64 bits -- the third row emitting the very
// `inttoptr` this change removes. None of them is what a select lowers to, so
// each one reaching the emitter means the index and the destination have
// disagreed; the answer is a refusal, not reinterpreted bits.
//
// Against a tree with the bridge restored at the two call sites, each row
// returns a nil error and leaves IR in the buffer.
func TestSelectWinnerIndexRefusesAWordDestination(t *testing.T) {
	emitter, builtins := selectWinnerEmitter(t)
	for _, row := range []struct {
		name      string
		dstType   types.TypeID
		llvmType  string
		wordShape string
	}{
		{"machine word", builtins.Int64, "i64", "the raw i64, passed through unchanged"},
		{"double", builtins.Float64, "double", "bitcast i64 to double"},
		{"tagged pointer", builtins.Int, "ptr", "inttoptr i64 to ptr"},
	} {
		t.Run(row.name, func(t *testing.T) {
			// The row's claim about what the bridge used to emit rests on the
			// destination's LLVM type, so the stand states it rather than
			// trusting the name.
			llvmType, err := emitter.llvmValueType(row.dstType)
			if err != nil {
				t.Fatalf("llvmValueType: %v", err)
			}
			if llvmType != row.llvmType {
				t.Fatalf("destination lowers as %s, want %s -- the row's %q claim no longer applies",
					llvmType, row.llvmType, row.wordShape)
			}

			emitter.buf.Reset()
			fe := selectWinnerDestination(emitter, row.dstType)
			if err := fe.emitSelectWinnerIndex("%bits", mir.Place{Local: mir.LocalID(0)}); err == nil {
				t.Fatalf("a %s destination was accepted, emitting %s:\n%s",
					row.name, row.wordShape, emitter.buf.String())
			}
			if emitter.buf.Len() != 0 {
				t.Fatalf("a refused %s destination still emitted %d bytes of IR:\n%s",
					row.name, emitter.buf.Len(), emitter.buf.String())
			}
		})
	}
}
