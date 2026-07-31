package crossinggate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/mir"
)

// Unpacking a crossing capture says in the TREE whether it takes the value or
// duplicates it.
//
// The two are the same shape — a field read out of the state box — and the
// difference is invisible from the surface: an owned capture is moved into the
// state and continues into the body, while a copy capture leaves the state
// holding its own and makes the body a second holder. The envelope is released
// SHALLOWLY afterwards on the premise that nothing but the box is left, so the
// distinction decides whether a value is reclaimed once, twice, or never.
//
// It was carried by a comment until now, and a comment is not enough for what
// comes next: with inline storage a "copy by convention" becomes a bitwise
// duplicate with two owners, and the pass that has to know the difference reads
// the tree, not the comment.
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
    let t: far Task<int> = spawn on distributed { ret take_owned(own j) + read_copied(k); };
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

	// Every capture unpack in the module, keyed by the BINDING it lands in.
	// The state's fields are positional (`__cap0`, `__cap1`), so the readable
	// half of the pair is the destination local, which keeps the name the
	// enclosing function gave it.
	modes := map[string]bool{}
	seen := 0
	for fi := range res.MIR.Funcs {
		fn := res.MIR.Funcs[fi]
		if fn == nil {
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
				modes[fn.Locals[dst.Local].Name] = ins.Assign.Src.Field.MoveOut
				seen++
			}
		}
	}

	if seen == 0 {
		t.Fatal("no capture unpack found: the probe stopped measuring what it claims to")
	}
	moveOut, ok := modes["j"]
	if !ok {
		t.Fatal("the owned capture was never unpacked")
	}
	if !moveOut {
		t.Error("an OWNED capture unpacks as a plain read: the state keeps nothing after this, " +
			"so a second holder here is a reference nobody gives back")
	}
	copied, ok := modes["k"]
	if !ok {
		t.Fatal("the copy capture was never unpacked")
	}
	if copied {
		t.Error("a COPY capture unpacks as a transfer: the state still holds its own, " +
			"and taking it out leaves that one with no owner")
	}
}
