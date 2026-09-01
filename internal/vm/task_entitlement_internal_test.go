package vm

import (
	"testing"

	"surge/internal/asyncrt"
	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

const (
	taskResultSuccessSym   = symbols.SymbolID(71)
	taskResultCancelledSym = symbols.SymbolID(72)
)

func newTaskResultFixture(t *testing.T) (*VM, types.TypeID, types.TypeID) {
	t.Helper()
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()
	str := interner.Builtins().String
	result := interner.RegisterUnion(interner.Strings.Intern("TaskResultString"), source.Span{})
	interner.SetUnionMembers(result, []types.UnionMember{
		{Kind: types.UnionMemberTag, TagName: interner.Strings.Intern("Success"), TagArgs: []types.TypeID{str}},
		{Kind: types.UnionMemberTag, TagName: interner.Strings.Intern("Cancelled")},
	})
	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{str, result})
	if err != nil {
		t.Fatalf("freezing fixture layouts: %v", err)
	}
	module := &mir.Module{Meta: &mir.ModuleMeta{
		Layouts: registry,
		TagLayouts: map[types.TypeID][]mir.TagCaseMeta{result: {
			{TagName: "Success", TagSym: taskResultSuccessSym, PayloadTypes: []types.TypeID{str}},
			{TagName: "Cancelled", TagSym: taskResultCancelledSym},
		}},
		TagNames: map[symbols.SymbolID]string{
			taskResultSuccessSym: "Success", taskResultCancelledSym: "Cancelled",
		},
	}}
	machine := New(module, nil, nil, interner, nil)
	machine.Async = asyncrt.NewExecutor[asyncPayload](asyncrt.Config{Deterministic: true})
	machine.Stack = []*Frame{machine.activate(&mir.Func{Blocks: []mir.Block{{}}})}
	return machine, str, result
}

func TestTaskResultMovesLastAskerAndClonesEveryEarlierOne(t *testing.T) {
	machine, str, resultType := newTaskResultFixture(t)
	taskID := machine.Async.Spawn(1, nil)
	if vmErr := machine.registerAsyncTaskOwner(taskID, str); vmErr != nil {
		t.Fatalf("register task owner: %v", vmErr)
	}
	handle := machine.Heap.AllocString(str, "the canonical result")
	payload, vmErr := machine.stageAsyncTaskResult(taskID, MakeHandleString(handle, str))
	if vmErr != nil {
		t.Fatalf("stage canonical result: %v", vmErr)
	}
	task := machine.Async.Task(taskID)
	task.Status = asyncrt.TaskDone
	task.ResultKind = asyncrt.TaskResultSuccess
	task.ResultValue = payload
	machine.taskHandleCreated(task.ID)
	machine.taskHandleCreated(task.ID)

	first, vmErr := machine.taskResultFromTask(task, resultType)
	if vmErr != nil {
		t.Fatalf("serve first asker: %v", vmErr)
	}
	_, slot, _, inspectErr := machine.inspectAsyncPayload(task.ResultValue)
	if inspectErr != nil || slot.state != asyncPayloadInitialized {
		t.Fatalf("first asker emptied canonical slot: %v state=%v", inspectErr, slot)
	}
	if got := machine.Heap.Get(handle).RefCount; got != 2 {
		t.Fatalf("earlier asker left refcount %d, want 2", got)
	}

	machine.taskHandleReleased(task.ID)
	second, vmErr := machine.taskResultFromTask(task, resultType)
	if vmErr != nil {
		t.Fatalf("serve last asker: %v", vmErr)
	}
	if task.ResultValue.ownerKind != 0 {
		t.Fatal("last asker cloned instead of terminally moving the canonical slot")
	}
	if got := machine.Heap.Get(handle).RefCount; got != 2 {
		t.Fatalf("last move changed refcount to %d, want 2", got)
	}
	machine.taskHandleReleased(task.ID)

	machine.dropValue(first)
	machine.dropValue(second)
	obj, _ := machine.Heap.lookup(handle)
	if obj == nil || !obj.Freed || obj.RefCount != 0 {
		t.Fatalf("two caller-owned results did not release exactly twice: %#v", obj)
	}
}
