package crossinggate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"surge/internal/buildpipeline"
	"surge/internal/mir"
)

// The async state envelope is RELEASED, never dropped, and only on the paths
// that finished with it.
//
// A suspension packs the locals that outlive it into a payload box and takes
// them back out on resume. The protocol that makes that safe has three parts,
// and each one is a silent memory error if it slips:
//
//   - the restore TAKES a local out of the payload rather than duplicating it,
//     which is why the payload box may then be freed without walking its fields;
//   - nothing DROPS the state or payload box, because a drop would walk those
//     fields and free what the restored locals now hold;
//   - a completing poll releases the envelope exactly once, while a CANCELLED
//     one deliberately does not — the state is still reachable from the runtime
//     there, and releasing it was a use-after-free (RV2-DEBT-044).
//
// All three live in the lowering as construction choices, not as checks, and
// the censuses that would notice them run only with SURGE_SKIP_TIMEOUT_TESTS=0
// — which `make check` does not set. This asserts them where a commit is gated,
// off one compiled async function.
//
// It lives in this package because it needs `buildpipeline`, and `internal/mir`
// cannot import that from its own tests without a cycle.
func TestAsyncStateEnvelopeIsReleasedNotDropped(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRoot(t))

	const source = `
async fn tick(n: int) -> int {
    let mut i = 0;
    while i < n { i = i + 1; }
    return i;
}

// The string is what makes this worth checking: it must survive the
// suspension and be reclaimed exactly once afterwards.
async fn carry(n: int) -> int {
    let mut s = "v-";
    let mut k = 0;
    while k < 4 { s = s + "x"; k = k + 1; }
    let t = spawn tick(n);
    let got = compare t.await() { Success(v) => v; Cancelled() => 0; };
    return got + (len(s) to int);
}

@entrypoint
fn main() -> int {
    let r = spawn carry(3);
    return compare r.await() { Success(v) => v; Cancelled() => 1; };
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "state_envelope.sg")
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

	poll := findPollFunc(t, res.MIR, "carry$poll")
	stateLocals := envelopeLocals(poll)
	if len(stateLocals) == 0 {
		t.Fatal("the poll function has no state or payload local: the probe stopped measuring what it claims to")
	}

	restores := 0
	for bi := range poll.Blocks {
		for ii := range poll.Blocks[bi].Instrs {
			ins := &poll.Blocks[bi].Instrs[ii]
			switch ins.Kind {
			case mir.InstrAssign:
				if ins.Assign.Src.Kind == mir.RValueTagPayload {
					if op := ins.Assign.Src.TagPayload.Value; op.Place.Kind == mir.PlaceLocal && stateLocals[op.Place.Local] {
						restores++
					}
				}
			case mir.InstrDrop:
				if ins.Drop.Place.Kind == mir.PlaceLocal && stateLocals[ins.Drop.Place.Local] {
					t.Errorf("block %d drops the state envelope: a drop walks its fields and frees "+
						"what the restored locals now hold", bi)
				}
			}
		}
	}
	if restores == 0 {
		t.Error("no local is restored out of the state payload: either the shape stopped suspending, " +
			"or the restore stopped going through the channel that TAKES its value")
	}

	// A completing poll hands the envelope back; a cancelled one leaves it to
	// the runtime, which still reaches it.
	suspends := 0
	for bi := range poll.Blocks {
		term := poll.Blocks[bi].Term
		releases := envelopeReleaseCount(poll.Blocks[bi])
		switch term.Kind {
		case mir.TermAsyncReturn:
			if releases != 1 {
				t.Errorf("block %d returns with %d envelope releases; want exactly one", bi, releases)
			}
		case mir.TermAsyncReturnCancelled:
			if releases != 0 {
				t.Errorf("block %d cancels after releasing the envelope %d times; the runtime still "+
					"reaches that state (RV2-DEBT-044)", bi, releases)
			}
		case mir.TermAsyncYield:
			suspends++
			// The envelope belongs to the ACTIVATION: a suspension writes the
			// resume point and the repacked payload through the one it was
			// entered with. Releasing and rebuilding it here would be two
			// allocations on every re-entry, and re-entry is decided by the
			// scheduler — so the cost would land in a measurement nobody can
			// hold still.
			if releases != 0 {
				t.Errorf("block %d suspends after releasing the envelope %d times; a suspension "+
					"rewrites the envelope it holds, it does not rebuild one", bi, releases)
			}
			if rebuilds := envelopeRebuildCount(poll.Blocks[bi], frameLocals(poll)); rebuilds != 0 {
				t.Errorf("block %d suspends after rebuilding the envelope %d times; the pointer the "+
					"runtime already holds is the one that must carry the new state", bi, rebuilds)
			}
		}
	}
	if suspends == 0 {
		t.Error("the poll function has no suspend block: the probe stopped measuring what it claims to")
	}
}

// frameLocals returns the poll function's state-frame local by the name the
// lowering gives it. The payload local is deliberately not here: it is an
// ordinary value the suspension rebuilds, and only the frame is the pointer
// the runtime is holding.
func frameLocals(fn *mir.Func) map[mir.LocalID]bool {
	out := map[mir.LocalID]bool{}
	for i := range fn.Locals {
		if fn.Locals[i].Name == "__state" {
			out[mir.LocalID(i)] = true
		}
	}
	return out
}

// envelopeRebuildCount counts the assignments in one block that replace a whole
// envelope local rather than writing through it.
func envelopeRebuildCount(bb mir.Block, envelope map[mir.LocalID]bool) int {
	n := 0
	for ii := range bb.Instrs {
		ins := &bb.Instrs[ii]
		if ins.Kind != mir.InstrAssign {
			continue
		}
		dst := ins.Assign.Dst
		if dst.Kind == mir.PlaceLocal && len(dst.Proj) == 0 && envelope[dst.Local] {
			n++
		}
	}
	return n
}

// findPollFunc returns the poll function whose name ends in suffix.
func findPollFunc(t *testing.T, mod *mir.Module, suffix string) *mir.Func {
	t.Helper()
	for fi := range mod.Funcs {
		fn := mod.Funcs[fi]
		if fn != nil && fn.Name == suffix {
			return fn
		}
	}
	t.Fatalf("no function named %q in the module", suffix)
	return nil
}

// envelopeLocals returns the poll function's state and payload locals by the
// names the lowering gives them.
func envelopeLocals(fn *mir.Func) map[mir.LocalID]bool {
	out := map[mir.LocalID]bool{}
	for i := range fn.Locals {
		switch fn.Locals[i].Name {
		case "__state", "__payload":
			out[mir.LocalID(i)] = true
		}
	}
	return out
}

// envelopeReleaseCount counts the shallow releases in one block.
func envelopeReleaseCount(bb mir.Block) int {
	n := 0
	for ii := range bb.Instrs {
		ins := &bb.Instrs[ii]
		if ins.Kind == mir.InstrCall && ins.Call.Callee.Name == mir.AsyncStateFreeBuiltin {
			n++
		}
	}
	return n
}
