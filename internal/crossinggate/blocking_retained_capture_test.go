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

// A blocking body gives back the reference its frame was built holding.
//
// The frame's lifecycle word says SPENT from the instruction after the unpack,
// and SPENT means nothing left in the frame owns anything. The unpack that is
// meant to make that true takes out only the captures the state literal MOVED
// in. A reference-counted capture is not one of those: the literal RETAINED it
// into the field, and the unpack copies the handle word out without touching the
// count. So the reference has to become the local's, and the local has to give
// it back — otherwise the frame is the only thing left holding it while the word
// says the frame holds nothing, and nothing anywhere releases it.
//
// The release is a drop obligation, registered by sema for this body in
// registerBlockingBodyOwnership, so this row reads the drops MIR emitted for it
// rather than a shape MIR invented.
//
// `Channel<T>` is the reachable shape and, today, the only one. The predicate
// that decides the unpack mode says "does not transfer" for exactly two
// families: a capture owning no heap, which has nothing to hand on, and a
// reference-counted one. Of the reference-counted pair the SCALAR (`float`)
// never arrives — sema refuses a blocking capture carrying one, because its
// count is not atomic and the worker is another thread — which leaves the
// handle.
//
// The runtime cannot cover for the compiler here, and that is the point. The
// worker CLAIMS the job's state cell immediately before it calls this body, so
// `blocking_job_release` frees the block WITHOUT walking a field
// (runtime/native/rt_async_blocking.c). There is no second place the field's
// reference could be given back.
//
// It lives in this package for the reason TestAsyncStateEnvelopeIsReleasedNotDropped
// does: the shape needs the real `Channel<T>` from the standard library. The
// reference-counted mark is put on the declaration only when its symbol carries
// the BUILTIN flag, and that flag is applied by the driver as it loads the core
// module from the stdlib root -- so the snippet harness inside `internal/mir`,
// which resolves its own `type Channel<T>` without a driver, gets an ordinary
// struct that owns nothing and exercises none of this. `internal/mir` cannot
// import `buildpipeline` from its own tests without a cycle.
func TestBlockingRetainedCaptureIsGivenBackAtEveryReturn(t *testing.T) {
	t.Setenv("SURGE_STDLIB", repoRoot(t))

	const source = `
fn peek(s: &string) -> int { return 1; }

async fn run() -> int {
    let ch: own Channel<int> = Channel::<int>::new(1:uint);
    let note: string = "moved in, and taken back out";
    let job: Task<int> = blocking {
        ch.close();
        ret peek(&note);
    };
    let got: TaskResult<int> = job.await();
    return compare got { Success(x) => x; Cancelled() => 0 - 1; };
}

@entrypoint
fn main() -> int {
    let r: TaskResult<int> = run().await();
    return compare r { Success(x) => x; Cancelled() => 1; };
}
`

	dir := t.TempDir()
	path := filepath.Join(dir, "blocking_retained_capture.sg")
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

	// The submission really did take a reference. Without this the rows below
	// could all pass against a program in which the frame never held anything,
	// which is the state the whole test is about telling apart.
	if kind, ok := blockingStateFieldOperand(res.MIR, "__cap0"); !ok {
		t.Fatal("no blocking submission with a capture field in the module: the probe stopped " +
			"measuring what it claims to")
	} else if kind != mir.OperandRetain {
		t.Fatalf("the blocking state literal stores its channel capture as %v, not a retain; this "+
			"program no longer builds a frame that owns a reference, so it can no longer witness "+
			"a frame that fails to give one back", kind)
	}

	body := requireBlockingBody(t, res.MIR)
	capture, ok := blockingCaptureLocal(body, "ch")
	if !ok {
		t.Fatalf("%s never unpacks a local named `ch`: the probe stopped measuring what it claims to",
			body.Name)
	}
	// The unpack MUST take the reference out of the field, and this assertion
	// was the other way round for a day. Reading it as a COPY left the field
	// looking initialized while the body was made to give the reference back,
	// which is two owners of one reference — the ownership verifier reported
	// exactly that, on `stdlib/term/term.sg`, and was right. Taking it out
	// makes the frame's SPENT word true of this field as well and leaves the
	// local as the single holder the release below belongs to.
	if !capture.moveOut {
		t.Fatalf("%s unpacks its channel capture as a COPY, so the frame's field still looks "+
			"initialized while the local is released below: two owners of one reference", body.Name)
	}
	if !bodyMarksFrameSpent(body) {
		t.Fatalf("%s never marks its frame spent, so there is no claim here for the drop below to "+
			"make true", body.Name)
	}

	returns := 0
	for bi := range body.Blocks {
		bb := &body.Blocks[bi]
		if bb.Term.Kind != mir.TermReturn {
			continue
		}
		returns++
		if !blockDropsLocal(bb, capture.local) {
			t.Errorf("%s bb%d returns without releasing the channel capture `ch`. The state literal "+
				"retained a reference into the frame's field, the unpack copied the handle out "+
				"without a retain, and the frame is marked spent — so nothing on this path gives "+
				"that reference back and the channel object is never destroyed", body.Name, bi)
		}
	}
	if returns == 0 {
		t.Fatalf("%s has no returning block: the probe stopped measuring what it claims to", body.Name)
	}
}

// requireBlockingBody returns the one synthesized blocking body in the module.
func requireBlockingBody(t *testing.T, mod *mir.Module) *mir.Func {
	t.Helper()
	var found *mir.Func
	for fi := range mod.Funcs {
		fn := mod.Funcs[fi]
		if fn == nil || !strings.HasPrefix(fn.Name, "__blocking_block$") {
			continue
		}
		if found != nil {
			t.Fatalf("more than one blocking body: %s and %s", found.Name, fn.Name)
		}
		found = fn
	}
	if found == nil {
		t.Fatal("no synthesized blocking body in the module")
	}
	return found
}

// blockingStateFieldOperand returns how a blocking submission's state literal
// stores the named field.
func blockingStateFieldOperand(mod *mir.Module, field string) (mir.OperandKind, bool) {
	for fi := range mod.Funcs {
		fn := mod.Funcs[fi]
		if fn == nil {
			continue
		}
		for bi := range fn.Blocks {
			for ii := range fn.Blocks[bi].Instrs {
				ins := &fn.Blocks[bi].Instrs[ii]
				if ins.Kind != mir.InstrBlocking {
					continue
				}
				for li := range ins.Blocking.State.Fields {
					if ins.Blocking.State.Fields[li].Name == field {
						return ins.Blocking.State.Fields[li].Value.Kind, true
					}
				}
			}
		}
	}
	return 0, false
}

type blockingCapture struct {
	local   mir.LocalID
	moveOut bool
}

// blockingCaptureLocal finds the unpack that initializes the named local out of
// the body's frame.
func blockingCaptureLocal(f *mir.Func, name string) (blockingCapture, bool) {
	for bi := range f.Blocks {
		for ii := range f.Blocks[bi].Instrs {
			ins := &f.Blocks[bi].Instrs[ii]
			if ins.Kind != mir.InstrAssign || ins.Assign.Src.Kind != mir.RValueField {
				continue
			}
			if !strings.HasPrefix(ins.Assign.Src.Field.FieldName, "__cap") {
				continue
			}
			dst := ins.Assign.Dst
			if dst.Kind != mir.PlaceLocal || len(dst.Proj) != 0 ||
				int(dst.Local) < 0 || int(dst.Local) >= len(f.Locals) {
				continue
			}
			if f.Locals[dst.Local].Name != name {
				continue
			}
			return blockingCapture{local: dst.Local, moveOut: ins.Assign.Src.Field.MoveOut}, true
		}
	}
	return blockingCapture{}, false
}

// bodyMarksFrameSpent reports whether the body writes SPENT into its frame.
func bodyMarksFrameSpent(f *mir.Func) bool {
	for bi := range f.Blocks {
		for ii := range f.Blocks[bi].Instrs {
			ins := &f.Blocks[bi].Instrs[ii]
			if ins.Kind != mir.InstrAssign {
				continue
			}
			dst := ins.Assign.Dst
			if dst.Kind != mir.PlaceLocal || len(dst.Proj) != 1 ||
				dst.Proj[0].Kind != mir.PlaceProjField || dst.Proj[0].FieldName != mir.FrameStateField {
				continue
			}
			src := ins.Assign.Src
			if src.Kind != mir.RValueUse || src.Use.Kind != mir.OperandConst ||
				src.Use.Const.Kind != mir.ConstInt {
				continue
			}
			if src.Use.Const.IntValue == mir.FrameStateSpent {
				return true
			}
		}
	}
	return false
}

// blockDropsLocal reports whether bb drops the given local.
func blockDropsLocal(bb *mir.Block, local mir.LocalID) bool {
	for ii := range bb.Instrs {
		ins := &bb.Instrs[ii]
		if ins.Kind != mir.InstrDrop {
			continue
		}
		place := ins.Drop.Place
		if place.Kind == mir.PlaceLocal && len(place.Proj) == 0 && place.Local == local {
			return true
		}
	}
	return false
}
