package crossinggate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/mir"
	"surge/internal/types"
)

// A crossing frame acquired its own counted Copy holder before publication.
// Its unpack transfers that holder into the body, whose synthesized drop gives
// it back. The caller keeping its original binding does not make this field
// read a borrow. A heap-free int64 needs no transfer; an owned Job still moves.
//
// This lives at the MIR level rather than in the e2e corpus deliberately. The
// native crossing censuses that would notice the consequence run only with
// SURGE_SKIP_TIMEOUT_TESTS=0, which `make check` does not use — so a regression
// here would land green. Compiling one crossing costs a second.
func TestCrossingCaptureUnpackDeclaresItsTransferMode(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRoot(t))

	const source = `
@shard_movable
type Job = { id: int, note: string };

fn take_owned(j: own Job) -> int {
    return j.id;
}

fn read_copied(n: int) -> int {
    return n;
}

async fn run() -> int {
    let j: own Job = own Job{ id: 4, note: "n" };
    let k: int = 7;
    let k_uint: uint = 7:uint;
    let k_plain: int64 = 7:int64;
    let t: far Task<int> = spawn on distributed {
        ret take_owned(own j) + read_copied(k) + (k_uint:int) + (k_plain:int);
    };
    let got: TaskResult<int> = t.await();
    return compare got { Success(x) => x; Cancelled() => 0 - 1; };
}

@entrypoint
fn main() -> int {
    let r: TaskResult<int> = run().await();
    return compare r { Success(x) => x; Cancelled() => 1; };
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "capture_unpack.sg")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	res, err := buildpipeline.Compile(context.Background(), &buildpipeline.CompileRequest{
		TargetPath:     path,
		Backend:        buildpipeline.BackendLLVM,
		MaxDiagnostics: 200,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if res.MIR == nil {
		t.Fatal("compile produced no MIR")
	}
	if res.Diagnose == nil || res.Diagnose.Sema == nil || res.Diagnose.Sema.TypeInterner == nil {
		t.Fatal("compile produced no semantic type authority")
	}
	interner := res.Diagnose.Sema.TypeInterner
	for _, finding := range mir.VerifyOwnership(res.MIR, interner, res.Diagnose.Sema) {
		// Preserve all findings without preventing the four typed children
		// from running. Before the fix, the counted unpacks are the failures.
		t.Errorf("ownership finding: %+v", finding)
	}

	// Every capture unpack in the module, keyed by the BINDING it lands in.
	// The state's fields are positional (`__cap0`, `__cap1`), so the readable
	// half of the pair is the destination local, which keeps the name the
	// enclosing function gave it.
	modes := map[string]bool{}
	locals := map[string]mir.Local{}
	seen := 0
	for fi := range res.MIR.Funcs {
		fn := res.MIR.Funcs[fi]
		if fn == nil || !strings.HasPrefix(fn.Name, "__spawn_on_block$") {
			continue
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				ins := &fn.Blocks[bi].Instrs[ii]
				if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueField {
					continue
				}
				if !strings.HasPrefix(ins.Assign.Src.Field.FieldName, "__cap") {
					continue
				}
				dst := ins.Assign.Dst
				if dst.Kind != mir.PlaceLocal || len(dst.Proj) != 0 {
					continue
				}
				if int(dst.Local) < 0 || int(dst.Local) >= len(fn.Locals) {
					continue
				}
				local := fn.Locals[dst.Local]
				if _, duplicate := modes[local.Name]; duplicate {
					t.Errorf("capture %q was unpacked more than once", local.Name)
				}
				modes[local.Name] = ins.Assign.Src.Field.MoveOut
				locals[local.Name] = local
				seen++
			}
		}
	}

	if seen != 4 {
		t.Errorf("capture unpacks=%d, want exactly the four declared captures", seen)
	}
	for _, row := range []struct {
		name, typeLabel            string
		copy, counted, owns, moved bool
	}{
		{"j", "", false, false, true, true},
		{"k", "int", true, true, true, true},
		{"k_uint", "uint", true, true, true, true},
		{"k_plain", "int64", true, false, false, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			local, present := locals[row.name]
			if !present {
				t.Fatalf("capture %q was never unpacked", row.name)
			}
			label := types.Label(interner, local.Type)
			t.Logf("capture=%s type_id=%d type=%s flags=%d", row.name, local.Type, label, local.Flags)
			if row.typeLabel != "" && label != row.typeLabel {
				t.Fatalf("type=%s, want %s", label, row.typeLabel)
			}
			if got := res.Diagnose.Sema.IsCopyType(local.Type); got != row.copy {
				t.Fatalf("Copy=%v, want %v", got, row.copy)
			}
			if got := interner.IsRefCountedScalar(local.Type); got != row.counted {
				t.Fatalf("counted=%v, want %v", got, row.counted)
			}
			if got := local.Flags&mir.LocalFlagOwnsHeap != 0; got != row.owns {
				t.Fatalf("owns_heap=%v, want %v", got, row.owns)
			}
			if moved := modes[row.name]; moved != row.moved {
				t.Errorf("MoveOut=%v, want %v", moved, row.moved)
			}
		})
	}
}
