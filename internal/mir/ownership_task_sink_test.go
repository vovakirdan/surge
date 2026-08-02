package mir_test

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

const ownershipTaskPrelude = `
tag Success<T>(T);
tag Cancelled();
type TaskResult<T> = Success(T) | Cancelled;
type Task<T> = { __opaque: int };
extern<Task<T>> {
    fn await(self: own Task<T>) -> TaskResult<T>;
}
`

const ownershipTaskTypeSource = ownershipTaskPrelude + `
async fn task_shape(task: Task<int>) -> int {
    let _ = task.await();
    return 0;
}
`

func newTaskOwnershipEnv(t *testing.T) (*ownershipEnv, types.TypeID) {
	t.Helper()
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipTaskTypeSource)
	for _, id := range mod.SortedFuncIDs() {
		f := mod.Funcs[id]
		if f != nil && baseName(f.Name) == "task_shape" && len(f.Locals) > 0 {
			return &ownershipEnv{typesIn: typesIn, semaRes: semaRes}, f.Locals[0].Type
		}
	}
	t.Fatal("task_shape did not lower with its Task<int> parameter")
	return nil, types.NoTypeID
}

func taskSinkInstr(kind mir.InstrKind, dst mir.Place, task mir.Operand, ready, pending mir.BlockID) mir.Instr {
	switch kind {
	case mir.InstrAwait:
		return mir.Instr{Kind: kind, Await: mir.AwaitInstr{Dst: dst, Task: task}}
	case mir.InstrPoll:
		return mir.Instr{Kind: kind, Poll: mir.PollInstr{
			Dst: dst, Task: task, ReadyBB: ready, PendBB: pending,
		}}
	case mir.InstrTimeout:
		return mir.Instr{Kind: kind, Timeout: mir.TimeoutInstr{
			Dst: dst, Task: task, ReadyBB: ready, PendBB: pending,
		}}
	default:
		panic("not a task-consuming instruction")
	}
}

func buildTaskSinkFunc(name string, env *ownershipEnv, taskTy types.TypeID, kind mir.InstrKind, alias bool) *mir.Func {
	b := newFn(name)
	task := b.param("task", taskTy, true)
	dst := b.local("result", env.typesIn.Builtins().Int, false)
	taskOp := opCopy(task, taskTy)
	instrs := make([]mir.Instr, 0, 2)
	if alias {
		taskAlias := b.local("task_alias", taskTy, true)
		instrs = append(instrs, assign(taskAlias, useRV(opCopy(task, taskTy))))
		taskOp = opCopy(taskAlias, taskTy)
	}
	instrs = append(instrs, taskSinkInstr(kind, place(dst), taskOp, 1, 2))
	b.block(instrs, retTerm())
	if kind == mir.InstrPoll || kind == mir.InstrTimeout {
		b.block(nil, retTerm())
		b.block(nil, retTerm())
	}
	return b.done()
}

func buildTaskSinkOperandFunc(name string, env *ownershipEnv, kind mir.InstrKind, task mir.Operand) *mir.Func {
	b := newFn(name)
	dst := b.local("result", env.typesIn.Builtins().Int, false)
	b.block([]mir.Instr{taskSinkInstr(kind, place(dst), task, 1, 2)}, retTerm())
	if kind == mir.InstrPoll || kind == mir.InstrTimeout {
		b.block(nil, retTerm())
		b.block(nil, retTerm())
	}
	return b.done()
}

func TestOwnershipTaskConsumeCanonicalCopy(t *testing.T) {
	env, taskTy := newTaskOwnershipEnv(t)
	for _, kind := range []mir.InstrKind{mir.InstrAwait, mir.InstrPoll, mir.InstrTimeout} {
		t.Run(kind.String(), func(t *testing.T) {
			requireClean(t, env.verify(buildTaskSinkFunc("owned_task", env, taskTy, kind, false)))

			got := env.verify(buildTaskSinkFunc("aliased_task", env, taskTy, kind, true))
			requireFindings(t, got,
				"aliased_task: task_consume of L2(task_alias) (def bb0#0) at bb0#1")
		})
	}
	if got := mir.OwnershipSinkTaskConsume.String(); got != "task_consume" {
		t.Fatalf("task sink label = %q, want task_consume", got)
	}
}

// Poll and Timeout consume the task only when they branch to ReadyBB. Their
// PendBB must still be able to transfer the same owned handle into a persisted
// async-state envelope for the next retry.
func TestOwnershipTaskConsumePendingRetainsHandle(t *testing.T) {
	env, taskTy := newTaskOwnershipEnv(t)
	for _, kind := range []mir.InstrKind{mir.InstrPoll, mir.InstrTimeout} {
		t.Run(kind.String(), func(t *testing.T) {
			b := newFn("pending_task")
			task := b.param("task", taskTy, true)
			dst := b.local("result", env.typesIn.Builtins().Int, false)
			b.block([]mir.Instr{
				taskSinkInstr(kind, place(dst), opCopy(task, taskTy), 1, 2),
			}, retTerm())
			b.block(nil, retTerm())
			b.block([]mir.Instr{{Kind: mir.InstrCall, Call: mir.CallInstr{
				Args:         []mir.Operand{opMove(task, taskTy)},
				ArgContracts: []mir.ArgContract{mir.ArgContractStore},
			}}}, retTerm())

			requireClean(t, env.verify(b.done()))
		})
	}
}

func TestOwnershipTaskConsumeRejectsNonCanonicalCopyPlaces(t *testing.T) {
	env, taskTy := newTaskOwnershipEnv(t)
	for _, tc := range []struct {
		name string
		op   func(mir.LocalID) mir.Operand
	}{
		{
			name: "projected",
			op: func(task mir.LocalID) mir.Operand {
				return mir.Operand{Kind: mir.OperandCopy, Type: taskTy, Place: mir.Place{
					Kind: mir.PlaceLocal, Local: task,
					Proj: []mir.PlaceProj{{Kind: mir.PlaceProjField, FieldName: "invalid"}},
				}}
			},
		},
		{
			name: "global",
			op: func(mir.LocalID) mir.Operand {
				return mir.Operand{Kind: mir.OperandCopy, Type: taskTy, Place: mir.Place{
					Kind: mir.PlaceGlobal, Global: 0,
				}}
			},
		},
		{
			name: "no_place",
			op: func(mir.LocalID) mir.Operand {
				return mir.Operand{Kind: mir.OperandCopy, Type: taskTy, Place: mir.Place{
					Kind: mir.PlaceLocal, Local: mir.NoLocalID,
				}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newFn("bad_task_place")
			task := b.param("task", taskTy, true)
			dst := b.local("result", env.typesIn.Builtins().Int, false)
			b.block([]mir.Instr{
				taskSinkInstr(mir.InstrAwait, place(dst), tc.op(task), 0, 0),
			}, retTerm())

			got := env.verify(b.done())
			if len(got) != 1 || got[0].ConsumingKind != mir.OwnershipSinkTaskConsume {
				t.Fatalf("expected one task_consume finding, got:\n%s", formatFindings(got))
			}
		})
	}
}

func TestOwnershipTaskConsumeUnknownCopyFailsClosed(t *testing.T) {
	env, _ := newTaskOwnershipEnv(t)
	cases := []struct {
		name  string
		place mir.Place
	}{
		{
			name:  "missing_local",
			place: mir.Place{Kind: mir.PlaceLocal, Local: mir.NoLocalID},
		},
		{
			name:  "unresolved_global",
			place: mir.Place{Kind: mir.PlaceGlobal, Global: 0},
		},
	}
	for _, kind := range []mir.InstrKind{mir.InstrAwait, mir.InstrPoll, mir.InstrTimeout} {
		for _, tc := range cases {
			t.Run(kind.String()+"/"+tc.name, func(t *testing.T) {
				op := mir.Operand{Kind: mir.OperandCopy, Type: types.NoTypeID, Place: tc.place}
				got := env.verify(buildTaskSinkOperandFunc("unknown_task_copy", env, kind, op))
				if len(got) != 1 || got[0].ConsumingKind != mir.OwnershipSinkTaskConsume {
					t.Fatalf("expected one task_consume finding, got:\n%s", formatFindings(got))
				}
			})
		}
	}
}

func TestOwnershipTaskConsumeKnownNonOwningCopyIsIgnored(t *testing.T) {
	env, _ := newTaskOwnershipEnv(t)
	cases := []struct {
		name  string
		place mir.Place
	}{
		{
			name:  "missing_local",
			place: mir.Place{Kind: mir.PlaceLocal, Local: mir.NoLocalID},
		},
		{
			name:  "unresolved_global",
			place: mir.Place{Kind: mir.PlaceGlobal, Global: 0},
		},
	}
	for _, kind := range []mir.InstrKind{mir.InstrAwait, mir.InstrPoll, mir.InstrTimeout} {
		for _, tc := range cases {
			t.Run(kind.String()+"/"+tc.name, func(t *testing.T) {
				op := mir.Operand{
					Kind:  mir.OperandCopy,
					Type:  env.typesIn.Builtins().Int,
					Place: tc.place,
				}
				requireClean(t, env.verify(buildTaskSinkOperandFunc("non_owning_task_copy", env, kind, op)))
			})
		}
	}
}

const ownershipTaskRealLoweringSource = ownershipTaskPrelude + `
@intrinsic fn timeout<T>(task: Task<T>, ms: uint) -> TaskResult<T>;

async fn await_task(task: Task<int>) -> int {
    let _ = task.await();
    return 0;
}

async fn timeout_task(task: Task<int>) -> int {
    let _ = timeout(task, 1:uint);
    return 0;
}
`

func TestOwnershipTaskConsumeRealLowering(t *testing.T) {
	mod, typesIn, semaRes := lowerForOwnership(t, ownershipTaskRealLoweringSource)
	found := map[mir.InstrKind]bool{}
	for _, id := range mod.SortedFuncIDs() {
		f := mod.Funcs[id]
		if f == nil {
			continue
		}
		name := baseName(f.Name)
		if name != "await_task" && name != "timeout_task" {
			continue
		}
		for bi := range f.Blocks {
			for ii := range f.Blocks[bi].Instrs {
				ins := &f.Blocks[bi].Instrs[ii]
				var task *mir.Operand
				switch ins.Kind {
				case mir.InstrAwait:
					task = &ins.Await.Task
				case mir.InstrTimeout:
					task = &ins.Timeout.Task
				default:
					continue
				}
				found[ins.Kind] = true
				if task.Kind != mir.OperandCopy || task.Place.Kind != mir.PlaceLocal || len(task.Place.Proj) != 0 {
					t.Fatalf("%s task operand is not canonical bare-local COPY: %+v", ins.Kind, *task)
				}
			}
		}
		if got := findingsIn(mir.VerifyOwnership(mod, typesIn, semaRes), name); len(got) != 0 {
			t.Fatalf("real-lowered %s should be clean, got:\n%s", name, joinLines(got))
		}
	}
	if !found[mir.InstrAwait] || !found[mir.InstrTimeout] {
		t.Fatalf("real lowering did not emit both task sinks: await=%t timeout=%t",
			found[mir.InstrAwait], found[mir.InstrTimeout])
	}
}
