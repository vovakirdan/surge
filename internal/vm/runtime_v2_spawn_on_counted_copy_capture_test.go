//go:build runtime_v2_pending

package vm_test

import (
	"crypto/sha256"
	"os"
	"strconv"
	"strings"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/diag"
	"surge/internal/mir"
	"surge/internal/sema"
	"surge/internal/source"
	"surge/internal/types"
	"surge/internal/vm"
)

// Each iteration creates one capture, awaits its fixed-width reply, then reads
// the caller's value again. The scope ends before the final heap snapshot.
// Integer construction uses the fixed-width witness from the numeric ownership
// tests; no decimal bignum parser or floating-point conversion supplies it.
func spawnCopyCaptureCensusSource(row, kind, literal string) string {
	return strings.NewReplacer("$ROW", row, "$K", kind, "$L", literal).Replace(`
async fn capture_window(n: int64) -> int64 {
    let before: HeapStats = rt_heap_stats();
    let mut i: int64 = 0:int64;
    while i < n {
        let value: $K = ($L:uint64):$K;
        let task: far Task<uint64> = spawn on distributed {
            ret value:uint64;
        };
        let answer: uint64 = compare task.await() {
            Success(x) => x;
            Cancelled() => 0:uint64;
        };
        if answer != $L:uint64 || (value:uint64) != $L:uint64 {
            panic("spawn-copy result or caller value changed");
        }
        i = i + 1:int64;
    }
    let after: HeapStats = rt_heap_stats();
    return ((after.alloc_count:int64) - (before.alloc_count:int64)) -
           ((after.free_count:int64) - (before.free_count:int64));
}

@entrypoint
fn main() -> int {
    let one: int64 = compare capture_window(1:int64).await() {
        Success(x) => x;
        Cancelled() => -999999:int64;
    };
    let eight: int64 = compare capture_window(8:int64).await() {
        Success(x) => x;
        Cancelled() => -999999:int64;
    };
    print("spawn-copy-census $ROW");
    print(one to string);
    print(eight to string);
    print("spawn-copy-results-and-caller-ok");
    return 0;
}
`)
}

func spawnCopyWritesSpent(instruction *mir.Instr, state mir.LocalID) bool {
	if instruction.Kind != mir.InstrAssign {
		return false
	}
	dst, src := instruction.Assign.Dst, instruction.Assign.Src
	return dst.Kind == mir.PlaceLocal && dst.Local == state && len(dst.Proj) == 1 &&
		dst.Proj[0].Kind == mir.PlaceProjField && dst.Proj[0].FieldName == mir.FrameStateField &&
		src.Kind == mir.RValueUse && src.Use.Kind == mir.OperandConst &&
		src.Use.Const.Kind == mir.ConstInt && src.Use.Const.IntValue == mir.FrameStateSpent
}

// Use the executable pipeline so the public backend grants the crossing
// capabilities before HIR lowering. The syntax-only helper cannot do that.
func compileSpawnCopyCapture(t *testing.T, sourceCode string) (*mir.Module, *source.FileSet, *types.Interner) {
	t.Helper()
	root := repoRoot(t)
	artifacts := newTestArtifacts(t, root)
	path := artifactSourcePath(artifacts)
	if err := os.WriteFile(path, []byte(sourceCode), 0o600); err != nil {
		t.Fatal(err)
	}
	compiled, err := buildpipeline.Compile(t.Context(), &buildpipeline.CompileRequest{
		TargetPath: path, BaseDir: root, MaxDiagnostics: 200,
		Analysis: true, Backend: buildpipeline.BackendVM,
	})
	if err != nil || compiled.MIR == nil || compiled.Diagnose == nil || compiled.Diagnose.Sema == nil {
		if d := compiled.Diagnose; d != nil && d.Bag != nil {
			t.Logf("spawn-copy source diagnostics:\n%s", diag.FormatShortDiagnostics(d.Bag.Items(), d.FileSet, true))
		}
		t.Fatalf("compile spawn-copy witness: %v; artifacts=%s", err, artifacts.Dir)
	}
	return compiled.MIR, compiled.Diagnose.FileSet, compiled.Diagnose.Sema.TypeInterner
}

func requireSpawnCopyCaptureMIR(t *testing.T, module *mir.Module, in *types.Interner, kind string, heap bool) {
	t.Helper()
	var poll *mir.Func
	crossings := 0
	for _, function := range module.Funcs {
		if function == nil {
			continue
		}
		if strings.HasPrefix(function.Name, "__spawn_on_block$") {
			if poll != nil {
				t.Fatal("more than one spawn-on poll in the capture witness")
			}
			poll = function
		}
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				if instruction.Kind != mir.InstrCrossing || instruction.Crossing.Kind != sema.CrossingLoweringSpawnOn {
					continue
				}
				crossings++
				captures := instruction.Crossing.Captures
				if len(captures) != 1 || captures[0].Mode != sema.CrossingCaptureCopy || types.Label(in, captures[0].Type) != kind {
					t.Fatalf("source must cross exactly one Copy %s capture: %+v", kind, captures)
				}
			}
		}
	}
	if poll == nil || crossings != 1 {
		t.Fatalf("capture witness requires one crossing and poll: crossings=%d poll=%v", crossings, poll)
	}
	entry := &poll.Blocks[poll.Entry]
	state, captured, unpack := mir.NoLocalID, mir.NoLocalID, -1
	for index, instruction := range entry.Instrs {
		if instruction.Kind != mir.InstrAssign || instruction.Assign.Src.Kind != mir.RValueField {
			continue
		}
		field := instruction.Assign.Src.Field
		if !strings.HasPrefix(field.FieldName, "__cap") {
			continue
		}
		if unpack != -1 || field.FieldName != "__cap0" || field.MoveOut || field.Object.Kind != mir.OperandCopy {
			t.Fatalf("capture must unpack once with the existing plain-read contract: %+v", field)
		}
		state, captured, unpack = field.Object.Place.Local, instruction.Assign.Dst.Local, index
	}
	if unpack < 0 || unpack+1 >= len(entry.Instrs) || !spawnCopyWritesSpent(&entry.Instrs[unpack+1], state) {
		t.Fatal("capture unpack must be immediately followed by SPENT")
	}
	if state < 0 || int(state) >= len(poll.Locals) || captured < 0 || int(captured) >= len(poll.Locals) {
		t.Fatal("capture unpack names an invalid local")
	}
	if poll.Locals[state].Name != "__state" || poll.Locals[captured].Name != "value" ||
		types.Label(in, poll.Locals[captured].Type) != kind {
		t.Fatal("capture unpack stopped naming the actual state and caller binding")
	}
	returns, drops := 0, 0
	for _, block := range poll.Blocks {
		blockDrops := 0
		for _, instruction := range block.Instrs {
			if instruction.Kind == mir.InstrDrop && instruction.Drop.Place.Local == captured {
				blockDrops++
				drops++
			}
		}
		if block.Term.Kind == mir.TermAsyncReturn {
			returns++
			want := 0
			if heap {
				want = 1
			}
			if blockDrops != want || len(block.Instrs) == 0 || !spawnCopyWritesSpent(&block.Instrs[len(block.Instrs)-1], state) {
				t.Fatalf("capture return requires %d local drops then SPENT, got %d", want, blockDrops)
			}
		}
	}
	wantDrops := 0
	if heap {
		wantDrops = 1
	}
	if returns != 1 || drops != wantDrops {
		t.Fatalf("capture MIR census: returns=%d drops=%d want=%d", returns, drops, wantDrops)
	}
	t.Logf("MIR: one Copy %s capture, MoveOut=false, adjacent SPENT, return drops=%d", kind, drops)
}

// The VM fully drops composite frame fields even after SPENT. Native's
// shallow SPENT release is a different path; this row does not exercise it.
func TestRuntimeV2SpawnOnCountedCopyCaptureCensus(t *testing.T) {
	requireNumericProofRun(t)
	t.Setenv(backendEnvVar, backendVM)
	t.Setenv("SURGE_STDLIB", repoRoot(t))
	for _, row := range []struct {
		name, kind, literal string
		heap                bool
	}{
		{"fixed-int64", "int64", "4611686018427387904", false},
		{"heap-int", "int", "9223372036854775811", true},
		{"heap-uint", "uint", "9223372036854775811", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			source := spawnCopyCaptureCensusSource(row.name, row.kind, row.literal)
			module, files, in := compileSpawnCopyCapture(t, source)
			requireSpawnCopyCaptureMIR(t, module, in, row.kind, row.heap)
			t.Logf("capture source_SHA256=%x", sha256.Sum256([]byte(source)))
			// Like stdout, VM stderr is process-global. This test and its
			// children are serial; a regular file cannot fill a bounded pipe.
			stderrFile, err := os.CreateTemp(t.TempDir(), "spawn-copy-stderr-*")
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stderr
			os.Stderr = stderrFile
			defer func() {
				os.Stderr = previous
				if err := stderrFile.Close(); err != nil {
					t.Error(err)
				}
			}()
			var code int
			var runErr error
			stdout := captureVMStdout(t, func() {
				exitCode, vmErr := runVM(module, vm.NewTestRuntime(nil, ""), files, in, nil)
				code = exitCode
				if vmErr != nil {
					runErr = vmErr
				}
			})
			stderr, err := os.ReadFile(stderrFile.Name())
			if err != nil || len(stderr) != 0 {
				t.Fatalf("capture VM stderr: read=%v stderr=%q VMError=%v", err, stderr, runErr)
			}
			lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			if len(lines) != 4 || lines[0] != "spawn-copy-census "+row.name || lines[3] != "spawn-copy-results-and-caller-ok" {
				t.Fatalf("capture witness incomplete: exit=%d VMError=%v stdout=%q", code, runErr, stdout)
			}
			one, errOne := strconv.ParseInt(lines[1], 10, 64)
			eight, errEight := strconv.ParseInt(lines[2], 10, 64)
			if errOne != nil || errEight != nil || one == -999999 || eight == -999999 {
				t.Fatalf("capture census invalid: one=%q eight=%q", lines[1], lines[2])
			}
			t.Logf("pre-shutdown VM heap census: n1=%d n8=%d growth=%d; replies and caller values exact", one, eight, eight-one)
			if eight != one {
				t.Fatalf("spawn-copy VM heap growth row=%s: n1=%d n8=%d growth=%d; VMError=%v", row.name, one, eight, eight-one, runErr)
			}
			if code != 0 || runErr != nil {
				t.Fatalf("capture VM completion: exit=%d VMError=%v", code, runErr)
			}
		})
	}
}
