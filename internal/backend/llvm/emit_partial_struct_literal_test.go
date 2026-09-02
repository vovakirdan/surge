package llvm

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

// A struct literal that names only SOME of its type's fields must clear the
// storage first.
//
// The async frame constructor is what makes this shape real: a place promoted to
// a resident field has no value yet when the frame is built, so the literal
// deliberately says nothing about it. MIR has no generic zero of an arbitrary
// type to say it with, and the tree's answer is that unnamed means uninitialized
// -- the VM's buildComposite zeroes an extent for exactly this reason and says
// so, and rt_frame_alloc memsets a frame block for it too.
//
// This backend built the literal in a bare alloca, so the same program would have
// left an unwritten resident holding whatever that slot last contained. That is
// not a leak: rt_frame_release runs the type's GENERATED member-wise drop over a
// frame whose lifecycle word says PACKED, and the constructor sets that word from
// the frame's first instant. A frame discarded before its body ran would drop a
// field nobody wrote -- on LLVM only, while the VM stayed correct, which is the
// worst place for a difference to live.
func TestPartialStructLiteralClearsItsStorage(t *testing.T) {
	sourceCode := `async fn partial_worker(x: &int) -> int {
    return *x + 1;
}

async fn partial_parent() -> int {
    let mut v: int = 0;
    let t = spawn partial_worker(&v);
    let r = t.await();
    return compare r {
        Success(x) => x;
        Cancelled() => 0;
    };
}

@entrypoint
fn main() -> int {
    compare partial_parent().await() {
        Success(v) => return v;
        Cancelled() => return 9;
    };
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)

	// The async function IS its own frame constructor after lowering, and the
	// resident it promotes is a body local, so its literal names every field but
	// that one.
	ctor := findMIRFunc(t, mirMod, "partial_parent")
	frameType, ok := partialLiteralType(ctor)
	if !ok {
		t.Fatalf("this program no longer builds a partial struct literal, so it cannot test one")
	}

	// The assertion is on the memset's SIZE, not on the presence of a memset.
	// This function body already contains one for other reasons -- a union
	// materialisation clears its own storage -- so `contains a memset` passes with
	// the clearing removed entirely, which is what a mutant run showed before this
	// was tightened. The frame struct's own byte count is what distinguishes the
	// store being asserted from every other one in the function.
	emitter := &Emitter{mod: mirMod, types: result.Sema.TypeInterner}
	layoutInfo, err := emitter.layoutOf(frameType)
	if err != nil {
		t.Fatalf("layout of the frame struct: %v", err)
	}

	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", ctor.ID))
	want := fmt.Sprintf("i8 0, i64 %d, i1 false)", layoutInfo.Size)
	if !strings.Contains(body, want) {
		t.Fatalf("the %d-byte frame was not cleared before its partial literal, so the unnamed resident holds whatever the slot last did; wanted a memset of %q in:\n%s",
			layoutInfo.Size, want, body)
	}
}

// The clearing is for PARTIAL literals only. A literal that names every field
// overwrites every byte that matters, and zeroing first would be dead stores in
// the overwhelmingly common case -- so a function whose only literal is complete
// must emit none.
func TestCompleteStructLiteralDoesNotClearItsStorage(t *testing.T) {
	sourceCode := `type Pair = {
    a: int,
    b: int,
};

fn build() -> int {
    let p: Pair = Pair { a = 1, b = 2 };
    return p.a + p.b;
}

@entrypoint
fn main() -> int {
    return build();
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	fn := findMIRFunc(t, mirMod, "build")
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", fn.ID))
	if strings.Contains(body, "@llvm.memset.p0.i64") {
		t.Fatalf("a complete struct literal cleared storage it was about to overwrite entirely:\n%s", body)
	}
}

// partialLiteralType finds the struct literal fn builds that names fewer fields
// than its type has, and returns that type.
//
// The premise of the test above is that this program produces such a literal.
// Asserting the premise separately is what keeps a change that stops producing
// one from leaving behind a test that passes while checking nothing.
func partialLiteralType(fn *mir.Func) (types.TypeID, bool) {
	if fn == nil {
		return types.NoTypeID, false
	}
	for bi := range fn.Blocks {
		for ii := range fn.Blocks[bi].Instrs {
			instr := &fn.Blocks[bi].Instrs[ii]
			if instr.Kind != mir.InstrAssign || instr.Assign.Src.Kind != mir.RValueStructLit {
				continue
			}
			lit := &instr.Assign.Src.StructLit
			named := false
			for _, field := range lit.Fields {
				if strings.HasPrefix(field.Name, "__resident$") {
					named = true
					break
				}
			}
			// The frame carries its lifecycle word, its resume point and its
			// payload. A resident named here would be a promoted PARAMETER, whose
			// value the constructor does have; the shape under test is the one
			// that omits it.
			if !named && len(lit.Fields) == 3 {
				return lit.TypeID, true
			}
		}
	}
	return types.NoTypeID, false
}
