package vm_test

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"surge/internal/mir"
)

// The ready rendezvous proves the child has entered the loop and initialized
// x. It cannot complete while gate has no sender. Cancellation may arrive
// during the ready continuation or the gate wait; both suspend states must own
// the live x/cursor/source. No delay or scheduler-speed guess is the witness.
func numericIteratorAsyncSource(cancel bool) string {
	action := "gate.send(9);"
	outcome := "let answer: float = compare child.await() { Success(value) => value; Cancelled() => -1.0; };\n    if answer != 1.5 { return 2; }"
	marker := "numeric-iterator-resumed-witness"
	if cancel {
		action = "child.cancel();"
		outcome = "let cancelled: bool = compare child.await() { Success(_) => false; Cancelled() => true; };\n    if !cancelled { return 3; }"
		marker = "numeric-iterator-cancelled-witness"
	}
	return fmt.Sprintf(`
fn numeric_async_values() -> float[] { return [1.5, 2.5, 3.5]; }

async fn numeric_suspended_loop(ready: Channel<int>, gate: Channel<int>) -> float {
    for x in numeric_async_values() {
        ready.send(1);
        let token: int = compare gate.recv() { Some(value) => value; nothing => -1; };
        if token != 9 { return -2.0; }
        return x;
    }
    return -3.0;
}

async fn numeric_async_driver() -> int {
    let ready = Channel::<int>::new(0:uint);
    let gate = Channel::<int>::new(0:uint);
    let child: Task<float> = spawn numeric_suspended_loop(ready, gate);
    let entered: int = compare ready.recv() { Some(value) => value; nothing => -1; };
    if entered != 1 { return 1; }
    %s
    %s
    ready.close();
    gate.close();
    print("%s");
    return 0;
}

@entrypoint
fn main() -> int {
    let task = spawn numeric_async_driver();
    return compare task.await() { Success(code) => code; Cancelled() => 90; };
}
`, action, outcome, marker)
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

func TestRuntimeV2NumericIteratorSuspendValgrindZero(t *testing.T) {
	requireOwnershipValgrind(t, exec.LookPath)
	for _, row := range []struct {
		name, workers, marker string
		cancel                bool
	}{
		{"workers_1_resume", "1", "numeric-iterator-resumed-witness\n", false},
		{"workers_1_cancel", "1", "numeric-iterator-cancelled-witness\n", true},
		{"workers_8_resume", "8", "numeric-iterator-resumed-witness\n", false},
		{"workers_8_cancel", "8", "numeric-iterator-cancelled-witness\n", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			output := buildRuntimeV2CrossingSource(t, numericIteratorAsyncSource(row.cancel), nil)
			env := overrideEnvVar(envWithStdlib(repoRoot(t)), "SURGE_SHARDS", "1")
			env = overrideEnvVar(env, "SURGE_THREADS", row.workers)
			stdout, stderr, code := runBinaryUnderValgrind(t, output, env, 120*time.Second)
			if code != 0 || stdout != row.marker {
				t.Fatalf("iterator %s: exit=%d stdout=%q\nstderr:\n%s", row.name, code, stdout, stderr)
			}
			requireNumericIteratorHeapZero(t, stderr)
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
