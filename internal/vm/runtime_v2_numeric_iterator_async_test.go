package vm_test

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/mir"
)

// The ready rendezvous proves the child has entered the loop and initialized
// x. It cannot complete while gate has no sender. Cancellation may arrive
// during the ready continuation or the gate wait; both suspend states must own
// the live x/cursor/source. No delay or scheduler-speed guess is the witness.
func numericIteratorAsyncSource(cancel bool) string {
	return numericIteratorAsyncProgram(cancel, false)
}

func numericIteratorAsyncProgram(cancel, countFromArgv bool) string {
	return numericIteratorAsyncTypedProgram("float", "array", cancel, countFromArgv)
}

// Heap variants keep a caller seed alive across child cleanup. The shared
// allocation baseline still separates process bootstrap from workload owners;
// neither a scheduler census nor an unchanged byte total alone is the oracle.
func numericIteratorAsyncTypedProgram(kind, form string, cancel, countFromArgv bool) string {
	values := "fn numeric_async_values() -> float[] { return [1.5, 2.5, 3.5]; }"
	loop := "for x in numeric_async_values()"
	failedAnswer, tokenError, emptyError := "-1.0", "-2.0", "-3.0"
	answerCheck, markerBase := "answer != 1.5", "numeric-iterator"
	seedParameter, seedArgument, seedBefore, seedAfter := "", "", "", ""
	if kind != "float" {
		if kind != "int" && kind != "uint" || form != "array" && form != "fast" {
			panic("invalid heap iterator async type or form")
		}
		values = fmt.Sprintf("fn numeric_async_values(seed: %s) -> %s[] { return [seed, (9223372036854775812:uint64):%s, (9223372036854775813:uint64):%s]; }", kind, kind, kind, kind)
		loop = "for x in numeric_async_values(seed)"
		if form == "fast" {
			values = ""
			loop = fmt.Sprintf("for x: %s in seed..((9223372036854775814:uint64):%s)", kind, kind)
		}
		failedAnswer, tokenError, emptyError = "0:"+kind, "0:"+kind, "0:"+kind
		answerCheck = "(answer:uint64) != 9223372036854775811:uint64"
		markerBase += "-heap-" + kind + "-" + form
		seedParameter, seedArgument = ", seed: "+kind, ", seed"
		seedBefore = fmt.Sprintf("let seed: %s = (9223372036854775811:uint64):%s;\n    ", kind, kind)
		seedAfter = "if (seed:uint64) != 9223372036854775811:uint64 { return 4; }\n    "
	}
	action := "gate.send(9);"
	outcome := fmt.Sprintf("let answer: %s = compare child.await() { Success(value) => value; Cancelled() => %s; };\n    if %s { return 2; }", kind, failedAnswer, answerCheck)
	marker := markerBase + "-resumed-witness"
	if cancel {
		action = "child.cancel();"
		outcome = "let cancelled: bool = compare child.await() { Success(_) => false; Cancelled() => true; };\n    if !cancelled { return 3; }"
		marker = markerBase + "-cancelled-witness"
	}
	parameter, argument, loopStart, loopEnd := "", "", "", ""
	entrypoint := "@entrypoint\nfn main() -> int"
	witness := fmt.Sprintf("print(%q);", marker)
	if countFromArgv {
		parameter, argument = "rounds: uint", "rounds"
		entrypoint = "@entrypoint(\"argv\")\nfn main(rounds: uint) -> int"
		loopStart = "let mut round: uint = 0:uint;\n    while round < rounds {"
		loopEnd = "round = round + 1:uint;\n    }"
		witness = fmt.Sprintf("print(%q + (round to string));", marker+" rounds=")
	}
	return fmt.Sprintf(`
%s

async fn numeric_suspended_loop(ready: Channel<int>, gate: Channel<int>%s) -> %s {
    %s {
        ready.send(1);
        let token: int = compare gate.recv() { Some(value) => value; nothing => -1; };
        if token != 9 { return %s; }
        return x;
    }
    return %s;
}

async fn numeric_async_driver(%s) -> int {
    %s
    %slet ready = Channel::<int>::new(0:uint);
    let gate = Channel::<int>::new(0:uint);
    let child: Task<%s> = spawn numeric_suspended_loop(ready, gate%s);
    let entered: int = compare ready.recv() { Some(value) => value; nothing => -1; };
    if entered != 1 { return 1; }
    %s
    %s
    %sready.close();
    gate.close();
    %s
    %s
    return 0;
}

%s {
    let task = spawn numeric_async_driver(%s);
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`, values, seedParameter, kind, loop, tokenError, emptyError, parameter, loopStart, seedBefore, kind, seedArgument, action, outcome, seedAfter, loopEnd, witness, entrypoint, argument)
}

func TestVMNumericIteratorSuspendLifecycle(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		name, marker := "resume", "numeric-iterator-resumed-witness\n"
		if cancel {
			name, marker = "cancel", "numeric-iterator-cancelled-witness\n"
		}
		t.Run(name, func(t *testing.T) {
			result := runProgramFromSource(t, numericIteratorAsyncSource(cancel), runOptions{captureStdout: true})
			if result.exitCode != 0 || result.stdout != marker || result.stderr != "" {
				t.Fatalf("iterator %s: exit=%d stdout=%q stderr=%q artifacts=%s", name, result.exitCode, result.stdout, result.stderr, result.artifactsDir)
			}
		})
	}
}

func TestRuntimeV2NumericIteratorSuspendValgrindBaseline(t *testing.T) {
	for _, row := range []struct {
		name, workers, marker string
		cancel                bool
	}{
		{"workers_1_resume", "1", "numeric-iterator-resumed-witness", false},
		{"workers_1_cancel", "1", "numeric-iterator-cancelled-witness", true},
		{"workers_8_resume", "8", "numeric-iterator-resumed-witness", false},
		{"workers_8_cancel", "8", "numeric-iterator-cancelled-witness", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			output := buildAsyncAllocationProgram(t, numericIteratorAsyncProgram(row.cancel, true))
			runAsyncAllocationBaseline(t, output, row.marker, asyncAllocationEnvironment(t, row.workers))
		})
	}
}

func TestNumericIteratorSuspendFrameOwnsLiveBindings(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	mod, _, _ := compileToMIRFromSource(t, numericIteratorAsyncSource(false))
	var poll *mir.Func
	for _, fn := range mod.Funcs {
		if fn != nil && strings.HasSuffix(fn.Name, "numeric_suspended_loop$poll") {
			poll = fn
		}
	}
	if poll == nil {
		t.Fatal("missing numeric loop poll function")
	}
	want := make(map[mir.LocalID]string)
	families := make(map[string]int)
	for id, local := range poll.Locals {
		family := ""
		switch {
		case local.Name == "x":
			family = "x"
		case strings.HasPrefix(local.Name, "__iter"):
			family = "cursor"
		case strings.HasPrefix(local.Name, "__src"):
			family = "source"
		}
		if family != "" {
			if local.Flags&mir.LocalFlagOwnsHeap == 0 {
				t.Fatalf("%s has no owning flag", local.Name)
			}
			want[mir.LocalID(id)] = family
			families[family]++
		}
	}
	for _, family := range []string{"x", "cursor", "source"} {
		if families[family] != 1 {
			t.Fatalf("expected one live %s local, got %v", family, families)
		}
	}
	packed, resumed, finished := 0, 0, 0
	for bi := range poll.Blocks {
		bb := &poll.Blocks[bi]
		lastWord := int64(-1)
		for ii := range bb.Instrs {
			ins := &bb.Instrs[ii]
			if word, ok := numericIteratorFrameWord(ins); ok {
				lastWord = word
			}
			if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueField && ins.Assign.Src.Field.FieldName == "__payload" {
				if !ins.Assign.Src.Field.MoveOut || ii+1 == len(bb.Instrs) {
					t.Fatal("resume did not move the frame payload")
				}
				if word, ok := numericIteratorFrameWord(&bb.Instrs[ii+1]); !ok || word != mir.FrameStateSpent {
					t.Fatal("resume did not mark the emptied frame SPENT immediately")
				}
				resumed++
			}
			if ins.Kind != mir.InstrCall || !strings.HasPrefix(ins.Call.Callee.Name, "Pc") {
				continue
			}
			seen := make(map[string]bool)
			for _, arg := range ins.Call.Args {
				switch arg.Kind {
				case mir.OperandCopy, mir.OperandCopyValue, mir.OperandRetain, mir.OperandMove, mir.OperandAddrOf, mir.OperandAddrOfMut:
				default:
					continue
				}
				if arg.Place.Kind != mir.PlaceLocal {
					continue
				}
				if family, ok := want[arg.Place.Local]; ok {
					if arg.Kind != mir.OperandMove {
						t.Fatalf("suspension did not move %s into its payload: %s", family, arg.Kind)
					}
					seen[family] = true
				}
			}
			if len(seen) == 3 {
				packed++
			}
		}
		if bb.Term.Kind == mir.TermAsyncYield && lastWord != mir.FrameStatePacked {
			t.Fatal("yield did not leave the frame PACKED")
		}
		if bb.Term.Kind == mir.TermAsyncReturn || bb.Term.Kind == mir.TermAsyncReturnCancelled {
			if lastWord != mir.FrameStateSpent {
				t.Fatal("completion did not leave the frame SPENT")
			}
			finished++
		}
	}
	if packed == 0 || resumed == 0 || finished == 0 {
		t.Fatalf("missing loop frame evidence: packed=%d resumed=%d finished=%d", packed, resumed, finished)
	}
}

func numericIteratorFrameWord(ins *mir.Instr) (int64, bool) {
	if ins.Kind != mir.InstrAssign || ins.Assign.Dst.Kind != mir.PlaceLocal || len(ins.Assign.Dst.Proj) != 1 ||
		ins.Assign.Dst.Proj[0].Kind != mir.PlaceProjField || ins.Assign.Dst.Proj[0].FieldName != mir.FrameStateField {
		return 0, false
	}
	src := ins.Assign.Src
	if src.Kind != mir.RValueUse || src.Use.Kind != mir.OperandConst || src.Use.Const.Kind != mir.ConstInt {
		return 0, false
	}
	return src.Use.Const.IntValue, true
}
