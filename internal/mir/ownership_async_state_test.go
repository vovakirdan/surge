package mir_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
)

const ownershipAsyncStateSource = ownershipTaskPrelude + `
@intrinsic fn timeout<T>(task: Task<T>, ms: uint) -> TaskResult<T>;

@copy
type Cell = { value: int };

async fn await_task(task: Task<int>) -> int {
    let _ = task.await();
    return 0;
}

async fn timeout_task(task: Task<int>) -> int {
    let _ = timeout(task, 1:uint);
    return 0;
}

async fn copy_cell_task(cell: Cell, task: Task<int>) -> int {
    let _ = task.await();
    return cell.value;
}

async fn float_state_task(value: float, task: Task<int>) -> int {
    let _ = task.await();
    let _ = value + 1.0;
    return 0;
}
`

// frameStateWord returns the lifecycle word an instruction stores into a frame,
// and whether it is such a store at all.
func frameStateWord(ins *mir.Instr) (int64, bool) {
	if ins.Kind != mir.InstrAssign {
		return 0, false
	}
	dst := ins.Assign.Dst
	if dst.Kind != mir.PlaceLocal || len(dst.Proj) != 1 ||
		dst.Proj[0].Kind != mir.PlaceProjField || dst.Proj[0].FieldName != mir.FrameStateField {
		return 0, false
	}
	src := ins.Assign.Src
	if src.Kind != mir.RValueUse || src.Use.Kind != mir.OperandConst || src.Use.Const.Kind != mir.ConstInt {
		return 0, false
	}
	return src.Use.Const.IntValue, true
}

func wordName(word int64) string {
	switch word {
	case mir.FrameStatePacked:
		return "PACKED"
	case mir.FrameStateSpent:
		return "SPENT"
	default:
		return "not-a-lifecycle-word"
	}
}

// The suspension frame says what it is holding, and every point that changes the
// answer writes it.
//
// One frame type is reclaimed by more than one path, and none of those paths can
// see the block it is reclaiming a frame from. What separates a frame that still
// holds a packed suspension from one already emptied into resumed locals used to
// be a sentence in a comment beside each call site — which no reclamation can
// read. So this asserts the word at each of the four points, and asserts the one
// property the argument for a plain store rests on: the SPENT write at entry is
// ADJACENT to the read that empties the frame. Nothing runs between them, so no
// schedule exists in which the frame is empty while the word still says PACKED.
//
// It also asserts what is no longer there. A frame release call in compiled code
// was the third reclamation, and leaving it beside the word would be two answers
// to one question.
func TestAsyncFrameCarriesItsLifecycleWord(t *testing.T) {
	compiled := compileCrossingMIR(t, ownershipAsyncStateSource, nil)
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}

	polls, spentAtEntry, packedAtYield, spentAtReturn := 0, 0, 0, 0
	for _, id := range compiled.mod.SortedFuncIDs() {
		f := compiled.mod.Funcs[id]
		if f == nil || !strings.HasSuffix(baseName(f.Name), "$poll") {
			continue
		}
		polls++
		for bi := range f.Blocks {
			bb := &f.Blocks[bi]
			for ii := range bb.Instrs {
				ins := &bb.Instrs[ii]
				if word, ok := frameStateWord(ins); ok && word != mir.FrameStatePacked && word != mir.FrameStateSpent {
					t.Errorf("%s bb%d#%d writes %d into %s, which is neither lifecycle word",
						f.Name, bi, ii, word, mir.FrameStateField)
				}
				if ins.Kind == mir.InstrCall && ins.Call.Callee.Name == "__async_state_free" {
					t.Errorf("%s bb%d#%d still calls a frame release builtin; the lifecycle word replaced it",
						f.Name, bi, ii)
				}
				if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueField ||
					ins.Assign.Src.Field.FieldName != "__payload" {
					continue
				}
				if !ins.Assign.Src.Field.MoveOut {
					t.Fatalf("%s resume payload read aliases the frame instead of emptying it: %+v",
						f.Name, ins.Assign.Src.Field)
				}
				if ii+1 >= len(bb.Instrs) {
					t.Fatalf("%s bb%d empties the frame in its last instruction; the SPENT write has nowhere "+
						"adjacent to go, and a window opens where the frame lies", f.Name, bi)
				}
				word, ok := frameStateWord(&bb.Instrs[ii+1])
				if !ok || word != mir.FrameStateSpent {
					t.Fatalf("%s bb%d#%d empties the frame and the next instruction is not the SPENT write "+
						"(got %+v); anything between them is a window in which the frame lies",
						f.Name, bi, ii, bb.Instrs[ii+1])
				}
				spentAtEntry++
			}

			last, ok := lastFrameStateWord(bb)
			switch bb.Term.Kind {
			case mir.TermAsyncYield:
				// The payload was just written back into the frame, so the
				// frame is PACKED before the runtime is handed it: reclaiming
				// it from here walks what it holds.
				if !ok || last != mir.FrameStatePacked {
					t.Errorf("%s bb%d yields with the frame's last word %q; a suspension packs the frame",
						f.Name, bi, wordName(last))
					continue
				}
				packedAtYield++
			case mir.TermAsyncReturn, mir.TermAsyncReturnCancelled:
				// Both outcomes were reached from a resumed body, so both leave
				// the frame empty. They differ in who reclaims it, not in what
				// is left in it.
				if !ok || last != mir.FrameStateSpent {
					t.Errorf("%s bb%d returns with the frame's last word %q; a completed activation "+
						"leaves the frame empty", f.Name, bi, wordName(last))
					continue
				}
				spentAtReturn++
			}
		}
	}

	// A count of zero anywhere means the shape stopped being compiled, not that
	// the rule holds.
	if polls == 0 || spentAtEntry == 0 || packedAtYield == 0 || spentAtReturn == 0 {
		t.Fatalf("evidence missing: polls=%d spent_at_entry=%d packed_at_yield=%d spent_at_return=%d",
			polls, spentAtEntry, packedAtYield, spentAtReturn)
	}

	findings := mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema)
	for _, fn := range []string{
		"await_task", "await_task$poll",
		"timeout_task", "timeout_task$poll",
		"copy_cell_task", "copy_cell_task$poll",
		"float_state_task", "float_state_task$poll",
	} {
		if got := findingsIn(findings, fn); len(got) != 0 {
			t.Errorf("real-lowered %s should be clean, got:\n%s", fn, joinLines(got))
		}
	}
}

// lastFrameStateWord returns the last lifecycle word a block writes.
func lastFrameStateWord(bb *mir.Block) (int64, bool) {
	for i := len(bb.Instrs) - 1; i >= 0; i-- {
		if word, ok := frameStateWord(&bb.Instrs[i]); ok {
			return word, true
		}
	}
	return 0, false
}

// The frame the async CONSTRUCTOR hands to the task is born packed. A task the
// scheduler drops before its first poll never reaches the poll function at all,
// so a word written only there would leave that frame describing itself with
// whatever its storage happened to contain.
func TestAsyncConstructorBuildsThePackedFrame(t *testing.T) {
	compiled := compileCrossingMIR(t, ownershipAsyncStateSource, nil)
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}

	built := 0
	for _, id := range compiled.mod.SortedFuncIDs() {
		f := compiled.mod.Funcs[id]
		if f == nil || strings.HasSuffix(baseName(f.Name), "$poll") {
			continue
		}
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueStructLit {
					continue
				}
				lit := &ins.Assign.Src.StructLit
				var word int64
				found := false
				for fi := range lit.Fields {
					if lit.Fields[fi].Name != mir.FrameStateField {
						continue
					}
					found = true
					word = lit.Fields[fi].Value.Const.IntValue
				}
				if !hasField(lit, "__pc") {
					continue
				}
				if !found {
					t.Fatalf("%s bb%d#%d builds a frame with a resume point and no lifecycle word",
						f.Name, bi, ii)
				}
				if word != mir.FrameStatePacked {
					t.Fatalf("%s bb%d#%d builds a frame whose word is %q, want PACKED",
						f.Name, bi, ii, wordName(word))
				}
				built++
			}
		}
	}
	if built == 0 {
		t.Fatal("no async constructor built a frame: the probe stopped measuring what it claims to")
	}
}

func hasField(lit *mir.StructLit, name string) bool {
	for i := range lit.Fields {
		if lit.Fields[i].Name == name {
			return true
		}
	}
	return false
}
