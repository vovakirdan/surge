package mir_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/mono"
)

// The promotion's whole claim is that a place a child borrows stops being a
// per-poll slot and becomes a fixed field of the parent's frame. These tests ask
// for that claim as an observable fact in the lowered MIR, because the lowering
// is otherwise indistinguishable from doing nothing: every existing async test
// keeps passing whether or not a single place is promoted, so a green package is
// not evidence here.
//
// The prefix is spelled literally rather than read from the constant. A rename
// that was not meant to be a behaviour change would otherwise take the test with
// it and stay invisible.
const residentFieldPrefix = "__resident$"

// loweredResidentFields runs the real pipeline over src and returns every
// resident field the lowered module MENTIONS -- both the ones a frame's
// constructor initializes and the ones the body addresses through a projection.
// A promoted body local appears only in the second, which is why both are
// collected rather than just the literal.
func loweredResidentFields(t *testing.T, src string) []string {
	t.Helper()
	tmpFile, err := os.CreateTemp(t.TempDir(), "async_resident_*.sg")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, writeErr := tmpFile.WriteString(src); writeErr != nil {
		t.Fatalf("failed to write source code: %v", writeErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		t.Fatalf("failed to close temp file: %v", closeErr)
	}

	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		EmitHIR:            true,
		EmitInstantiations: true,
	}
	result, err := driver.DiagnoseWithOptions(context.Background(), tmpFile.Name(), &opts)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}
	if result != nil && result.Bag.HasErrors() {
		var sb strings.Builder
		for _, d := range result.Bag.Items() {
			sb.WriteString(d.Message)
			sb.WriteString("\n")
		}
		t.Fatalf("compilation errors:\n%s", sb.String())
	}
	if result.HIR == nil || result.Instantiations == nil || result.Sema == nil {
		t.Fatalf("missing compilation outputs")
	}
	hirModule, err := driver.CombineHIRWithModules(context.Background(), result)
	if err != nil {
		t.Fatalf("HIR merge failed: %v", err)
	}
	if hirModule == nil {
		hirModule = result.HIR
	}
	mm, err := mono.MonomorphizeModule(hirModule, result.Instantiations, result.Sema, mono.Options{MaxDepth: 64})
	if err != nil {
		t.Fatalf("monomorphization failed: %v", err)
	}
	mirMod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		t.Fatalf("MIR lowering failed: %v", err)
	}
	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}
	// What sema constrained is logged beside what the lowering made of it. When a
	// promotion goes missing, the first question is always which of the two sides
	// is empty, and this answers it without re-deriving anything. It is what
	// separated "sema recorded nothing" from "sema recorded it under a key
	// monomorphization had already rewritten".
	t.Logf("sema constrained %d activation(s): %v", len(result.Sema.StableActivationPlaces), result.Sema.StableActivationPlaces)
	if err := mir.LowerAsyncStateMachine(mirMod, result.Sema, result.Symbols.Table); err != nil {
		t.Fatalf("async lowering failed: %v", err)
	}

	seen := map[string]bool{}
	var out []string
	note := func(name string) {
		if !strings.HasPrefix(name, residentFieldPrefix) || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, id := range mirMod.SortedFuncIDs() {
		f := mirMod.Funcs[id]
		if f == nil {
			continue
		}
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				instr := &f.Blocks[bi].Instrs[ii]
				if instr.Kind == mir.InstrAssign {
					for _, field := range instr.Assign.Src.StructLit.Fields {
						note(field.Name)
					}
					for _, proj := range instr.Assign.Dst.Proj {
						note(proj.FieldName)
					}
				}
			}
		}
	}
	return out
}

// A local of an `async fn` that a child borrows must become a resident: the
// parent suspends, and without promotion the child's pointer would name a slot
// the next poll no longer uses.
func TestAsyncResidentPromotesABorrowedParentLocal(t *testing.T) {
	got := loweredResidentFields(t, `
async fn resident_worker(x: &int) -> int {
    return *x + 1;
}

async fn resident_parent() -> int {
    let mut v: int = 0;
    let t = spawn resident_worker(&v);
    let r = t.await();
    return compare r {
        Success(x) => x;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    let res = resident_parent().await();
    return compare res {
        Success(v) => v;
        Cancelled() => 0;
    };
}
`)
	if len(got) == 0 {
		t.Fatalf("no place was promoted to a resident field; the borrowed local is still a per-poll slot")
	}
	for _, name := range got {
		if strings.Contains(name, "v$") {
			return
		}
	}
	t.Fatalf("the borrowed local `v` was not promoted; residents found: %v", got)
}

// The same, for a local owned by an `async {}` BLOCK. A block is a separate
// activation with its own frame, so its local is the BLOCK's to keep at a fixed
// address; promoting it into the host's frame would be a field in the wrong
// storage, and not promoting it at all would leave the child pointing at a slot
// the block's next poll abandons.
//
// This case is also the one that passed while the `async fn` case above was
// silently doing nothing, which is why both are kept: they fail for different
// reasons, and either alone reads as "promotion works".
func TestAsyncResidentPromotesABlocksOwnLocal(t *testing.T) {
	got := loweredResidentFields(t, `
async fn resident_worker(x: &int) -> int {
    return *x + 1;
}

@entrypoint
fn main() -> int {
    let res = (async {
        let mut inner: int = 0;
        let t = spawn resident_worker(&inner);
        checkpoint().await();
        ret 0;
    }).await();
    return compare res {
        Success(v) => v;
        Cancelled() => 0;
    };
}
`)
	if len(got) == 0 {
		t.Fatalf("the block's borrowed local was not promoted; sema's block key and the synthetic function's span may have diverged")
	}
	for _, name := range got {
		if strings.Contains(name, "inner$") {
			return
		}
	}
	t.Fatalf("the block-local `inner` was not promoted; residents found: %v", got)
}

// Promotion is SELECTIVE: an async function that borrows nothing into a child
// constrains no storage and must keep every local in the ordinary pack/unpack
// path. Without this, a promotion that fired for every local would satisfy the
// two tests above while quietly making every async frame bigger and defeating
// the "place-oriented, never type-oriented" rule.
func TestAsyncWithoutABorrowedCapturePromotesNothing(t *testing.T) {
	got := loweredResidentFields(t, `
async fn resident_plain(x: int) -> int {
    return x + 1;
}

async fn resident_quiet() -> int {
    let v: int = 0;
    let t = spawn resident_plain(v);
    let r = t.await();
    return compare r {
        Success(x) => x;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    let res = resident_quiet().await();
    return compare res {
        Success(v) => v;
        Cancelled() => 0;
    };
}
`)
	if len(got) != 0 {
		t.Fatalf("nothing is borrowed into a child, so nothing may be promoted; got %v", got)
	}
}
