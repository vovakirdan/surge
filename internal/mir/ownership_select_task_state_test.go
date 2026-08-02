package mir_test

import (
	"strings"
	"testing"

	"surge/internal/ast"
	"surge/internal/mir"
	"surge/internal/types"
)

const ownershipSelectTaskStateSource = ownershipTaskPrelude + `
@intrinsic fn timeout<T>(task: Task<T>, ms: uint) -> TaskResult<T>;

async fn select_task_state(first: Task<int>, second: Task<int>) -> int {
    let winner = select {
        first.await() => 1;
        second.await() => 2;
    };
    return winner;
}

async fn select_timeout_state(first: Task<int>, second: Task<int>) -> int {
    let winner = select {
        timeout(first, 1:uint) => 1;
        second.await() => 2;
    };
    return winner;
}

async fn race_task_state(first: Task<int>, second: Task<int>) -> int {
    let winner = race {
        first.await() => 1;
        second.await() => 2;
    };
    return winner;
}
`

// A select only borrows each task handle while polling because the pending
// edge must retry with the same handles. The task operand is already stable at
// this point, so materialising one more COPY temp would create a second alleged
// owner. The select and race keep the original operand instead; once the
// pending edge suspends, the async-state tag transfers that original local.
func TestOwnershipSelectTaskPendingStateTransfersHandles(t *testing.T) {
	compiled := compileCrossingMIR(t, ownershipSelectTaskStateSource, nil)
	if err := mir.LowerAsyncStateMachine(compiled.mod, compiled.sema, compiled.symbols.Table); err != nil {
		t.Fatalf("lower async state machine: %v", err)
	}

	wantFunctions := map[string]bool{
		"select_task_state$poll":    false,
		"select_timeout_state$poll": false,
		"race_task_state$poll":      false,
	}
	for _, id := range compiled.mod.SortedFuncIDs() {
		f := compiled.mod.Funcs[id]
		if f == nil {
			continue
		}
		name := baseName(f.Name)
		if _, ok := wantFunctions[name]; !ok {
			continue
		}
		wantFunctions[name] = true
		var checkedSelect bool
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				if ins.Kind != mir.InstrSelect {
					continue
				}
				checkedSelect = true
				assertSelectTaskPendingTransfers(t, f, &ins.Select)
			}
		}
		if !checkedSelect {
			t.Fatalf("real lowering did not emit %s InstrSelect", name)
		}
		if name == "race_task_state$poll" {
			assertRaceCancelsOriginalTasks(t, f)
		}

		findings := mir.VerifyOwnership(compiled.mod, compiled.types, compiled.sema)
		if got := findingsIn(findings, name); len(got) != 0 {
			t.Fatalf("real-lowered %s should be clean, got:\n%s", name, joinLines(got))
		}
	}
	for name, found := range wantFunctions {
		if !found {
			t.Errorf("real lowering did not emit %s", name)
		}
	}
}

func assertSelectTaskPendingTransfers(t *testing.T, f *mir.Func, sel *mir.SelectInstr) {
	t.Helper()
	if f == nil || sel == nil || sel.PendBB == mir.NoBlockID || int(sel.PendBB) >= len(f.Blocks) {
		t.Fatalf("select has no valid pending block: %+v", sel)
	}

	for i := range f.Locals {
		if strings.HasPrefix(f.Locals[i].Name, "tmp_select_task") {
			t.Fatalf("select lowering created redundant task alias L%d(%s)", i, f.Locals[i].Name)
		}
	}

	taskLocals := make(map[mir.LocalID]bool)
	taskNames := make(map[string]bool)
	for i := range sel.Arms {
		arm := &sel.Arms[i]
		if arm.Kind != mir.SelectArmTask && arm.Kind != mir.SelectArmTimeout {
			continue
		}
		if arm.Task.Kind != mir.OperandCopy || arm.Task.Place.Kind != mir.PlaceLocal ||
			len(arm.Task.Place.Proj) != 0 || arm.Task.Place.Local == mir.NoLocalID {
			t.Fatalf("select task arm %d is not a retry-safe bare-local COPY: %+v", i, arm.Task)
		}
		local := arm.Task.Place.Local
		if int(local) >= len(f.Locals) {
			t.Fatalf("select task arm %d has invalid local L%d", i, local)
		}
		name := f.Locals[local].Name
		if name != "first" && name != "second" {
			t.Fatalf("select task arm %d did not keep the original task operand: L%d(%s)", i, local, name)
		}
		taskLocals[local] = false
		taskNames[name] = true
	}
	if len(taskLocals) != 2 {
		t.Fatalf("select task locals = %d, want 2", len(taskLocals))
	}
	if !taskNames["first"] || !taskNames["second"] {
		t.Fatalf("select task operands = %v, want first and second", taskNames)
	}

	pending := &f.Blocks[sel.PendBB]
	for ii := range pending.Instrs {
		ins := &pending.Instrs[ii]
		if ins.Kind != mir.InstrCall {
			continue
		}
		for i := range ins.Call.Args {
			arg := &ins.Call.Args[i]
			if arg.Place.Kind != mir.PlaceLocal {
				continue
			}
			if _, ok := taskLocals[arg.Place.Local]; !ok {
				continue
			}
			if i >= len(ins.Call.ArgContracts) || ins.Call.ArgContracts[i] != mir.ArgContractStore {
				t.Fatalf("pending task L%d is not in a STORE position", arg.Place.Local)
			}
			if arg.Kind != mir.OperandMove {
				t.Fatalf("pending STORE for task L%d = %s, want MOVE", arg.Place.Local, arg.Kind)
			}
			taskLocals[arg.Place.Local] = true
		}
	}
	for local, found := range taskLocals {
		if !found {
			t.Errorf("pending state did not store select task L%d", local)
		}
	}
}

func assertRaceCancelsOriginalTasks(t *testing.T, f *mir.Func) {
	t.Helper()
	seen := map[string]int{}
	for bi := range f.Blocks {
		for ii := range f.Blocks[bi].Instrs {
			ins := &f.Blocks[bi].Instrs[ii]
			if ins.Kind != mir.InstrCall || ins.Call.Callee.Name != "cancel" {
				continue
			}
			if len(ins.Call.Args) != 1 || len(ins.Call.ArgContracts) != 1 ||
				ins.Call.ArgContracts[0] != mir.ArgContractBorrow {
				t.Fatalf("race loser cancel does not have one borrowed task argument: %+v", ins.Call)
			}
			arg := ins.Call.Args[0]
			if arg.Kind != mir.OperandCopy || arg.Place.Kind != mir.PlaceLocal ||
				len(arg.Place.Proj) != 0 || arg.Place.Local == mir.NoLocalID ||
				int(arg.Place.Local) >= len(f.Locals) {
				t.Fatalf("race loser cancel does not reuse a stable task operand: %+v", arg)
			}
			name := f.Locals[arg.Place.Local].Name
			if name != "first" && name != "second" {
				t.Fatalf("race loser cancel borrowed L%d(%s), want original task", arg.Place.Local, name)
			}
			seen[name]++
		}
	}
	if seen["first"] != 1 || seen["second"] != 1 {
		t.Fatalf("race loser cancel operands = %v, want each original task once", seen)
	}
}

// The real-lowering positive above must not be made green by teaching the
// verifier to accept COPY at an ordinary STORE sink. Only the lowering's
// pending-state handoff is licensed to turn that ownership transfer into MOVE.
func TestOwnershipSelectTaskPendingStateCopyStillFails(t *testing.T) {
	env, taskTy := newTaskOwnershipEnv(t)
	build := func(name string, taskArg mir.Operand) *mir.Func {
		b := newFn(name)
		task := b.param("task", taskTy, true)
		taskArg.Place = place(task)
		taskArg.Type = taskTy
		b.block([]mir.Instr{{Kind: mir.InstrCall, Call: mir.CallInstr{
			Args:         []mir.Operand{taskArg},
			ArgContracts: []mir.ArgContract{mir.ArgContractStore},
		}}}, retTerm())
		return b.done()
	}

	requireFindings(t, env.verify(build("copied_task_store", mir.Operand{Kind: mir.OperandCopy})),
		"copied_task_store: call_arg of L0(task) (def use) at bb0#0")
	requireClean(t, env.verify(build("moved_task_store", mir.Operand{Kind: mir.OperandMove})))
}

// Removing the select-specific COPY temp must not turn aliasing task reads
// into owners. A MOVE into the pending state still has to trace through the
// value's definition, where plain copies, projected reads, and dereferences
// through a borrow all remain aliases.
func TestOwnershipSelectTaskPendingStateAliasesStillFail(t *testing.T) {
	env, taskTy := newTaskOwnershipEnv(t)
	taskRefTy := env.typesIn.Intern(types.MakeReference(taskTy, false))

	cases := []struct {
		name     string
		sourceTy types.TypeID
		sourceRV func(mir.LocalID) mir.RValue
	}{
		{
			name:     "copied",
			sourceTy: taskTy,
			sourceRV: func(source mir.LocalID) mir.RValue {
				return useRV(opCopy(source, taskTy))
			},
		},
		{
			name:     "projected",
			sourceTy: taskTy,
			sourceRV: func(source mir.LocalID) mir.RValue {
				return mir.RValue{Kind: mir.RValueField, Field: mir.FieldAccess{
					Object: opCopy(source, taskTy), FieldName: "task", FieldIdx: 0,
				}}
			},
		},
		{
			name:     "dereferenced",
			sourceTy: taskRefTy,
			sourceRV: func(source mir.LocalID) mir.RValue {
				return mir.RValue{Kind: mir.RValueUnaryOp, Unary: mir.UnaryOp{
					Op: ast.ExprUnaryDeref, Operand: opCopy(source, taskRefTy),
				}}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newFn(tc.name + "_task_alias")
			source := b.param("source", tc.sourceTy, tc.sourceTy == taskTy)
			alias := b.local("task_alias", taskTy, true)
			b.block([]mir.Instr{
				assign(alias, tc.sourceRV(source)),
				{Kind: mir.InstrCall, Call: mir.CallInstr{
					Args:         []mir.Operand{opMove(alias, taskTy)},
					ArgContracts: []mir.ArgContract{mir.ArgContractStore},
				}},
			}, retTerm())

			requireFindings(t, env.verify(b.done()),
				tc.name+"_task_alias: call_arg of L1(task_alias) (def bb0#0) at bb0#1")
		})
	}
}
