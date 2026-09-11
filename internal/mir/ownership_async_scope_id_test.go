package mir_test

import (
	"strings"
	"testing"

	"surge/internal/mir"
)

// Scope values must remain fixed-width and uncounted when int/uint acquire
// heap ownership. The executor, not a frame field, owns the scope object.
func TestAsyncScopeIDIsUncountedUint64AcrossSuspension(t *testing.T) {
	compiled := compileCrossingMIR(t, ownershipAsyncStateSource+`
async fn synthetic_scope_task(task: Task<int>) -> int {
    let result = (async {
        let _ = task.await();
        ret 0;
    }).await();
    return compare result {
        Success(value) => value;
        Cancelled() => 0;
    };
}
`, nil)
	u64 := compiled.types.Builtins().Uint64
	checkLocal := func(f *mir.Func) {
		t.Helper()
		if f.ScopeLocal == mir.NoLocalID || int(f.ScopeLocal) >= len(f.Locals) {
			t.Fatalf("%s has no scope local", f.Name)
		}
		local := f.Locals[f.ScopeLocal]
		if local.Type != u64 || local.Flags&mir.LocalFlagCopy == 0 || local.Flags&mir.LocalFlagOwnsHeap != 0 {
			t.Fatalf("%s scope must be uncounted Uint64 Copy, got type=%d flags=%d", f.Name, local.Type, local.Flags)
		}
	}
	namedScopes, syntheticScopes := 0, 0
	expectedPolls := make(map[string]bool)
	for _, f := range compiled.mod.Funcs {
		if f != nil && f.IsAsync {
			checkLocal(f)
			synthetic := !f.Sym.IsValid()
			expectedPolls[f.Name+"$poll"] = synthetic
			if synthetic {
				syntheticScopes++
			} else {
				namedScopes++
			}
		}
	}
	if namedScopes != 5 || syntheticScopes != 1 {
		t.Fatalf("scope creation paths were not exercised: named=%d synthetic=%d, want 5/1", namedScopes, syntheticScopes)
	}
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}
	polls := 0
	for _, f := range compiled.mod.Funcs {
		if f == nil || !strings.HasSuffix(baseName(f.Name), "$poll") {
			continue
		}
		checkLocal(f)
		synthetic, expected := expectedPolls[f.Name]
		if !expected {
			t.Fatalf("unexpected scope poll %s", f.Name)
		}
		polls++
		packed, joined := 0, 0
		for _, block := range f.Blocks {
			for _, ins := range block.Instrs {
				if ins.Kind == mir.InstrJoinAll && ins.JoinAll.Scope.Place.Local == f.ScopeLocal {
					joined++
				}
				if ins.Kind != mir.InstrCall || !ins.Call.HasDst || ins.Call.Dst.Kind != mir.PlaceLocal || len(ins.Call.Dst.Proj) != 0 {
					continue
				}
				dstType := f.Locals[ins.Call.Dst.Local].Type
				for _, tag := range compiled.mod.Meta.TagLayouts[dstType] {
					if tag.TagSym != ins.Call.Callee.Sym {
						continue
					}
					for index, arg := range ins.Call.Args {
						if arg.Kind != mir.OperandCopy && arg.Kind != mir.OperandMove && arg.Kind != mir.OperandRetain {
							continue
						}
						if arg.Place.Kind != mir.PlaceLocal || arg.Place.Local != f.ScopeLocal || len(arg.Place.Proj) != 0 {
							continue
						}
						if arg.Kind != mir.OperandCopy || index >= len(tag.PayloadTypes) || tag.PayloadTypes[index] != u64 {
							t.Fatalf("%s suspension must pack its scope as uncounted Uint64, got operand=%+v payload=%v",
								f.Name, arg, tag.PayloadTypes)
						}
						packed++
					}
				}
			}
		}
		if packed == 0 || joined == 0 {
			t.Fatalf("%s synthetic=%t scope proof was not exercised: packed=%d joined=%d", f.Name, synthetic, packed, joined)
		}
		t.Logf("%s synthetic=%t Uint64 scope: packed=%d joined=%d", f.Name, synthetic, packed, joined)
	}
	if polls != len(expectedPolls) {
		t.Fatalf("scope polls=%d, want %d named/synthetic functions", polls, len(expectedPolls))
	}
}
