package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/mono"
)

const asyncChannelOwnershipSource = `async fn named(ch: Channel<int64>) -> int {
    ch.send(7:int64);
    return 0;
}
fn subject(ch: Channel<int64>) -> Task<int> {
    return async { ch.send(7:int64); ret 0; };
}
@entrypoint
fn main() -> int {
    let ch = Channel::<int64>::new(2:uint);
    compare subject(ch).await() { Success(_) => {} Cancelled() => { return 1; } };
    compare named(ch).await() { Success(_) => {} Cancelled() => { return 2; } };
    return 0;
}
`

// Both the named async parameter and the captured Channel need one independent
// frame reference. The caller keeps its own copy through constructor return.
func TestAsyncChannelCaptureOwnsOneFrameReference(t *testing.T) {
	assertAsyncChannelFrameOwnership(t, asyncChannelOwnershipSource, 2)
}

func TestAsyncChannelCaptureMultipleFrameReferences(t *testing.T) {
	for _, row := range asyncChannelCapturePrograms() {
		bodies := 1
		switch row.name {
		case "nested_async":
			bodies = 2
		case "same_handle_twice", "captured_heap_int", "captured_float", "captured_heap_uint":
		default:
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			assertAsyncChannelFrameOwnership(t, row.source, bodies)
		})
	}
}

func assertAsyncChannelFrameOwnership(t *testing.T, source string, wantBodies int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "capture.sg")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := DiagnoseWithOptions(t.Context(), path, &DiagnoseOptions{
		Stage: DiagnoseStageSema, EmitHIR: true, EmitInstantiations: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Bag.HasErrors() {
		t.Fatalf("source diagnostics: %+v", result)
	}
	combined, err := CombineHIRWithModules(t.Context(), result)
	if err != nil {
		t.Fatal(err)
	}
	mm, err := mono.MonomorphizeModule(combined, result.Instantiations, result.Sema, mono.Options{EnableDCE: true})
	if err != nil {
		t.Fatal(err)
	}
	mod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		t.Fatal(err)
	}
	types := result.Sema.TypeInterner
	var constructors []*mir.Func
	captured := map[string]bool{}
	handles := map[string]map[mir.LocalID]bool{}
	for _, fn := range mod.Funcs {
		if !strings.HasPrefix(fn.Name, "__async_block$") && fn.Name != "named" {
			continue
		}
		constructors = append(constructors, fn)
		captured[fn.Name] = true
		params := map[mir.LocalID]bool{}
		for i := range fn.ParamCount {
			if types.IsRefCounted(fn.Locals[i].Type) {
				params[mir.LocalID(i)] = true
			}
		}
		if len(params) == 0 {
			t.Fatalf("%s must have a counted capture parameter", fn.Name)
		}
		handles[fn.Name] = params
		for local := range params {
			drops := 0
			for _, block := range fn.Blocks {
				for _, ins := range block.Instrs {
					if ins.Kind == mir.InstrDrop && ins.Drop.Place.Kind == mir.PlaceLocal &&
						ins.Drop.Place.Local == local && len(ins.Drop.Place.Proj) == 0 {
						drops++
					}
				}
			}
			if drops != 1 {
				t.Errorf("%s local%d releases its captured reference %d times, want 1", fn.Name, local, drops)
			}
		}
	}
	if len(constructors) != wantBodies {
		t.Fatalf("async bodies=%d, want %d", len(constructors), wantBodies)
	}
	calls := 0
	for _, fn := range mod.Funcs {
		for _, block := range fn.Blocks {
			for _, ins := range block.Instrs {
				if ins.Kind != mir.InstrCall || !captured[ins.Call.Callee.Name] {
					continue
				}
				calls++
				for i, arg := range ins.Call.Args {
					if !types.IsRefCounted(arg.Type) {
						continue
					}
					if arg.Kind != mir.OperandCopy || i >= len(ins.Call.ArgContracts) ||
						ins.Call.ArgContracts[i] != mir.ArgContractBorrow {
						t.Errorf("constructor call creates an extra caller reference: %+v", ins.Call)
					}
				}
			}
		}
	}
	if calls != wantBodies {
		t.Fatalf("constructor calls=%d, want %d", calls, wantBodies)
	}
	for _, fn := range mod.Funcs {
		mir.SimplifyCFG(fn)
	}
	if err := mir.LowerAsyncStateMachine(mod, result.Sema, result.Symbols.Table); err != nil {
		t.Fatal(err)
	}
	for _, constructor := range constructors {
		retains := 0
		for _, block := range constructor.Blocks {
			for _, ins := range block.Instrs {
				if ins.Kind != mir.InstrCall {
					continue
				}
				for i, arg := range ins.Call.Args {
					if arg.Kind == mir.OperandRetain && types.IsRefCounted(arg.Type) {
						if !handles[constructor.Name][arg.Place.Local] || len(arg.Place.Proj) != 0 || i >= len(ins.Call.ArgContracts) || ins.Call.ArgContracts[i] != mir.ArgContractStore {
							t.Errorf("%s frame does not retain its exact counted parameter into a store", constructor.Name)
						}
						retains++
					}
				}
			}
		}
		if want := len(handles[constructor.Name]); retains != want {
			t.Errorf("%s constructor frame retains=%d, want %d", constructor.Name, retains, want)
		}
	}
	if err := mir.ValidateStructure(mod, types); err != nil {
		t.Fatal(err)
	}
}
