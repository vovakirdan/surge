package mir_test

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/mir"
)

const ownershipAsyncStateSource = ownershipTaskPrelude + `
@intrinsic fn timeout<T>(task: Task<T>, ms: uint) -> TaskResult<T>;

@copy
type Cell = { value: int };

async fn await_task(task: Task<int>) -> int {
    let _ = task.await();
    return 0;
}

async fn timeout_task(task: Task<int>) -> int {
    let _ = timeout(task, 1:uint);
    return 0;
}

async fn copy_cell_task(cell: Cell, task: Task<int>) -> int {
    let _ = task.await();
    return cell.value;
}

async fn float_state_task(value: float, task: Task<int>) -> int {
    let _ = task.await();
    let _ = value + 1.0;
    return 0;
}
`

// The async state boxes are consumed through two MIR spellings that look like
// aliases unless their lowering contract is carried explicitly:
//
//   - resuming transfers state.__payload out of an envelope that is then
//     shallow-freed, so the field read must carry MoveOut;
//   - __async_state_free uses bare-local COPY operands as an ABI spelling, but
//     the builtin frees and nulls both slots, so the verifier must resolve the
//     locals' definitions like a place release instead of rejecting the COPY.
func TestOwnershipAsyncStateResumeProtocol(t *testing.T) {
	compiled := compileCrossingMIR(t, ownershipAsyncStateSource, nil)
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}

	var payloadMoveOut, stateFree bool
	for _, id := range compiled.mod.SortedFuncIDs() {
		f := compiled.mod.Funcs[id]
		if f == nil || !strings.HasSuffix(baseName(f.Name), "$poll") {
			continue
		}
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				if ins.Kind == mir.InstrAssign && ins.Assign.Src.Kind == mir.RValueField &&
					ins.Assign.Src.Field.FieldName == "__payload" {
					payloadMoveOut = true
					if !ins.Assign.Src.Field.MoveOut {
						t.Fatalf("%s resume payload read aliases state instead of transferring: %+v", f.Name, ins.Assign.Src.Field)
					}
				}
				if ins.Kind == mir.InstrCall && ins.Call.Callee.Name == mir.AsyncStateFreeBuiltin {
					stateFree = true
					if len(ins.Call.Args) == 0 || len(ins.Call.Args) > 2 ||
						len(ins.Call.ArgContracts) != len(ins.Call.Args) {
						t.Fatalf("%s async state free shape = %d args/%d contracts", f.Name,
							len(ins.Call.Args), len(ins.Call.ArgContracts))
					}
					for i, contract := range ins.Call.ArgContracts {
						if contract != mir.ArgContractTransferOwned {
							t.Fatalf("%s async state free contract %d = %s, want transfer_owned", f.Name, i, contract)
						}
					}
				}
			}
		}
	}
	if !payloadMoveOut || !stateFree {
		t.Fatalf("async lowering evidence missing: payload_move_out=%t state_free=%t", payloadMoveOut, stateFree)
	}

	findings := mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema)
	for _, fn := range []string{
		"await_task", "await_task$poll",
		"timeout_task", "timeout_task$poll",
		"copy_cell_task", "copy_cell_task$poll",
		"float_state_task", "float_state_task$poll",
	} {
		if got := findingsIn(findings, fn); len(got) != 0 {
			t.Errorf("real-lowered %s should be clean, got:\n%s", fn, joinLines(got))
		}
	}
}

func TestOwnershipAsyncStateFreeExceptionIsExact(t *testing.T) {
	env, taskTy := newTaskOwnershipEnv(t)
	build := func(name, callee string, arity int, alias bool) *mir.Func {
		b := newFn(name)
		params := make([]mir.LocalID, arity)
		for i := range params {
			params[i] = b.param(fmt.Sprintf("arg%d", i), taskTy, true)
		}
		instrs := make([]mir.Instr, 0, 2)
		stateArg := mir.NoLocalID
		if arity != 0 {
			stateArg = params[0]
		}
		if alias {
			stateArg = b.local("alias", taskTy, true)
			instrs = append(instrs, assign(stateArg, useRV(opCopy(params[0], taskTy))))
		}
		args := make([]mir.Operand, 0, arity)
		contracts := make([]mir.ArgContract, 0, arity)
		for i, param := range params {
			arg := param
			if i == 0 {
				arg = stateArg
			}
			args = append(args, opCopy(arg, taskTy))
			contracts = append(contracts, mir.ArgContractTransferOwned)
		}
		instrs = append(instrs, mir.Instr{Kind: mir.InstrCall, Call: mir.CallInstr{
			Callee:       mir.Callee{Kind: mir.CalleeValue, Name: callee},
			Args:         args,
			ArgContracts: contracts,
		}})
		b.block(instrs, retTerm())
		return b.done()
	}

	requireClean(t, env.verify(build("owned_state_free_pair", mir.AsyncStateFreeBuiltin, 2, false)))
	requireClean(t, env.verify(build("owned_state_free_single", mir.AsyncStateFreeBuiltin, 1, false)))
	requireFindings(t, env.verify(build("aliased_state_free", mir.AsyncStateFreeBuiltin, 1, true)),
		"aliased_state_free: call_arg of L1(alias) (def bb0#0) at bb0#1")
	requireFindings(t, env.verify(build("ordinary_transfer", "ordinary_transfer", 2, false)),
		"ordinary_transfer: call_arg of L0(arg0) (def use) at bb0#0",
		"ordinary_transfer: call_arg of L1(arg1) (def use) at bb0#0")

	withDst := build("state_free_with_result", mir.AsyncStateFreeBuiltin, 1, false)
	withDst.Blocks[0].Instrs[0].Call.HasDst = true
	requireFindings(t, env.verify(withDst),
		"state_free_with_result: call_arg of L0(arg0) (def use) at bb0#0")

	requireFindings(t, env.verify(build("state_free_three_args", mir.AsyncStateFreeBuiltin, 3, false)),
		"state_free_three_args: call_arg of L0(arg0) (def use) at bb0#0",
		"state_free_three_args: call_arg of L1(arg1) (def use) at bb0#0",
		"state_free_three_args: call_arg of L2(arg2) (def use) at bb0#0")
}
