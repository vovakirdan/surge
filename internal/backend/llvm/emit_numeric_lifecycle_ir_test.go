package llvm

import (
	"fmt"
	"testing"

	"surge/internal/types"
)

const numericLifecycleIRSource = `
fn copy_int(value: int) -> int { let other: int = value; return other; }
fn copy_uint(value: uint) -> uint { let other: uint = value; return other; }
fn discard_int(value: int) { let other: int = value; }
fn discard_uint(value: uint) { let other: uint = value; }
fn array_int(value: int[]) -> int { return 0; }
fn array_uint(value: uint[]) -> uint { return 0; }
`

// The two inline legs read ordinary source-produced function bodies. The other
// six ask the real per-type body emitters over those same finalized numeric
// layouts, so every root/element operation is named even when this particular
// source has no runtime container that would demand its glue. The executing
// stand separately proves how source demands and calls those bodies.
func TestEmitNumericLifecycleHeapGuardsDominate(t *testing.T) {
	e, result := prepareEmitterAndResultForTest(t, numericLifecycleIRSource)
	ir, err := EmitModule(e.mod, e.types, e.syms, result.FileSet)
	if err != nil {
		t.Fatalf("emit numeric source: %v", err)
	}
	for _, kind := range []string{"int", "uint"} {
		id := e.types.Builtins().Int
		if kind == "uint" {
			id = e.types.Builtins().Uint
		}
		for _, row := range []struct{ name, operation string }{
			{"inline-retain", "retain"}, {"glue-retain", "retain"},
			{"inline-release", "release"}, {"glue-release", "release"},
			{"unshare", "unshare"}, {"cross-clone", "clone"},
			{"drop-elem", "release"}, {"clone-elem", "retain"},
		} {
			t.Run(kind+"/"+row.name, func(t *testing.T) {
				var body string
				switch row.name {
				case "inline-retain", "inline-release":
					name := "copy_" + kind
					if row.name == "inline-release" {
						name = "discard_" + kind
					}
					f := findMIRFunc(t, e.mod, name)
					body = findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(f.ID))
				default:
					body = numericGlueBody(t, e, id, row.name)
				}
				requireNumericOperation(t, assertNumericHeapGuards(t, body, kind), row.operation, 1)
			})
		}
	}
}

func numericGlueBody(t *testing.T, e *Emitter, id types.TypeID, operation string) string {
	t.Helper()
	e.buf.Reset()
	var err error
	switch operation {
	case "glue-retain":
		err = e.emitDuplicateGlueBody(id)
	case "glue-release":
		err = e.emitReleaseGlueBody(id)
	case "unshare":
		err = e.emitUnshareWalkBody(id)
	case "cross-clone":
		err = e.emitCrossCloneWalkBody(id)
	case "drop-elem":
		e.emitDropElemGlueBody(id)
	case "clone-elem":
		err = e.emitCloneElemGlueBody(id)
	default:
		t.Fatalf("unknown numeric glue operation %q", operation)
	}
	if err != nil {
		t.Fatalf("emit %s for type#%d: %v", operation, id, err)
	}
	return e.buf.String()
}

// Two populated union arms force the guard's g temporaries to advance before
// another case label is emitted. Fixed arrays exercise successive guards in
// one straight-line walk; the ordinary function bodies also copy these types.
func TestEmitNumericLifecycleNestedControlFlow(t *testing.T) {
	e, result := prepareEmitterAndResultForTest(t, `
@copy type NumericStruct = { a: int, b: int };
tag NumericOne(int);
tag NumericTwo(int, int);
tag NumericEmpty();
type NumericUnion = NumericOne(int) | NumericTwo(int, int) | NumericEmpty();
fn keep_struct(x: NumericStruct) -> NumericStruct { return x; }
fn keep_tuple(x: (int, int)) -> (int, int) { return x; }
fn keep_fixed(x: int[3]) -> int[3] { return x; }
fn keep_union(x: NumericUnion) -> NumericUnion { return x; }
`)
	ir, err := EmitModule(e.mod, e.types, e.syms, result.FileSet)
	if err != nil {
		t.Fatalf("emit nested numeric source: %v", err)
	}
	for _, row := range []struct {
		name, function string
		leaves         int
	}{
		{"struct", "keep_struct", 2}, {"tuple", "keep_tuple", 2},
		{"fixed-array", "keep_fixed", 3}, {"union", "keep_union", 3},
	} {
		t.Run(row.name, func(t *testing.T) {
			f := findMIRFunc(t, e.mod, row.function)
			locals, err := e.paramLocals(f)
			if err != nil || len(locals) != 1 {
				t.Fatalf("%s parameter census: %v, %v", row.function, locals, err)
			}
			id := f.Locals[locals[0]].Type
			for _, op := range []struct{ glue, leaf string }{
				{"glue-retain", "retain"}, {"glue-release", "release"},
				{"unshare", "unshare"}, {"cross-clone", "clone"},
			} {
				body := numericGlueBody(t, e, id, op.glue)
				requireNumericOperation(t, assertNumericHeapGuards(t, body, "int"), op.leaf, row.leaves)
			}
			// Read the whole source function from the normal module emitter;
			// guards must resume before the MIR block's own terminator.
			numericIRBlocks(t, findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(f.ID)))
		})
	}
}

func TestEmitNumericBoundsStepSkipsOnlyIntegerOneRelease(t *testing.T) {
	for _, kind := range []string{"int", "uint", "float"} {
		t.Run(kind, func(t *testing.T) {
			zero := "0"
			constructor := "rt_bigint_from_i64"
			switch kind {
			case "uint":
				constructor = "rt_biguint_from_u64"
			case "float":
				zero, constructor = "0.0", "rt_bigfloat_from_i64"
			}
			e, result := prepareEmitterAndResultForTest(t, fmt.Sprintf(`
fn step(r: Range<%s>) -> %s {
    for n: %s in r { return n; }
    return %s;
}
`, kind, kind, kind, zero))
			ir, err := EmitModule(e.mod, e.types, e.syms, result.FileSet)
			if err != nil {
				t.Fatalf("emit bounds step: %v", err)
			}
			f := findMIRFunc(t, e.mod, "step")
			body := findLLVMFuncBody(t, ir, "fn."+itoaMIRFuncID(f.ID))
			assertNumericOneRelease(t, body, kind, constructor)
		})
	}
}
