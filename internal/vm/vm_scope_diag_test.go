package vm

import (
	"strings"
	"testing"

	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

func TestVMScopeExitInvariantBecomesVMError(t *testing.T) {
	typesIn := types.NewInterner()
	intTy := typesIn.Builtins().Int
	scopeID := int64(1)

	fn := &mir.Func{
		ID:     1,
		Sym:    symbols.NoSymbolID,
		Name:   "scope_exit_test",
		Result: types.NoTypeID,
		Entry:  0,
		Blocks: []mir.Block{{
			ID: 0,
			Instrs: []mir.Instr{{
				Kind: mir.InstrCall,
				Call: mir.CallInstr{
					Callee: mir.Callee{Kind: mir.CalleeSym, Sym: symbols.NoSymbolID, Name: "rt_scope_exit"},
					Args: []mir.Operand{{
						Kind:  mir.OperandConst,
						Type:  intTy,
						Const: mir.Const{Kind: mir.ConstInt, Type: intTy, IntValue: scopeID},
					}},
				},
			}},
			Term: mir.Terminator{Kind: mir.TermReturn},
		}},
		Span: source.Span{Start: 1, End: 1},
	}

	vmInstance := New(&mir.Module{}, NewTestRuntime(nil, ""), nil, typesIn, nil)
	vmInstance.Stack = []*Frame{NewFrame(fn)}
	exec := vmInstance.ensureExecutor()
	owner := exec.Spawn(1, nil)
	actualScopeID := exec.EnterScope(owner, false)
	if actualScopeID != 1 {
		t.Fatalf("expected scope id 1, got %d", actualScopeID)
	}
	child := exec.Spawn(2, nil)
	exec.RegisterChild(actualScopeID, child)

	vmErr := vmInstance.Step()
	if vmErr == nil {
		t.Fatal("expected VM error, got nil")
	}
	if vmErr.Code != PanicUnimplemented {
		t.Fatalf("expected %v, got %v", PanicUnimplemented, vmErr.Code)
	}
	if !strings.Contains(vmErr.Message, "async scope invariant violated") {
		t.Fatalf("expected invariant message, got %q", vmErr.Message)
	}
	if !strings.Contains(vmErr.Message, "scope 1 exited with live children: [2]") {
		t.Fatalf("expected scope details in message, got %q", vmErr.Message)
	}
	if len(vmErr.Backtrace) == 0 || vmErr.Backtrace[0].FuncName != "scope_exit_test" {
		t.Fatalf("expected backtrace for scope_exit_test, got %+v", vmErr.Backtrace)
	}
}
