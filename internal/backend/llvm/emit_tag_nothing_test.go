package llvm

import (
	"fmt"
	"strings"
	"testing"

	"surge/internal/types"
)

func TestEmitAsyncNothingResultDoesNotStorePayloadPastTag(t *testing.T) {
	sourceCode := `async fn wait_once() -> nothing {
    checkpoint().await();
    return nothing;
}

@entrypoint
fn main() -> int {
    compare wait_once().await() {
        Success(_) => return 0;
        Cancelled() => return 1;
    };
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", findMIRFunc(t, mirMod, "wait_once$poll").ID))

	lines := strings.Split(body, "\n")
	const allocSuffix = " = call ptr @rt_alloc(i64 4, i64 4)"
	tagOnlyAllocs := 0
	for i, line := range lines {
		mem, found := strings.CutSuffix(line, allocSuffix)
		if !found || i+1 >= len(lines) {
			continue
		}
		mem = strings.TrimSpace(mem)
		tagStore := strings.TrimSpace(lines[i+1])
		if !strings.HasPrefix(tagStore, "store i32 ") || !strings.HasSuffix(tagStore, ", ptr "+mem) {
			continue
		}
		tagOnlyAllocs++
		if strings.Contains(body, "ptr "+mem+", i64 4") {
			t.Fatalf("tag-only TaskResult<nothing> allocation %s must not address a payload past its 4-byte layout:\n%s", mem, body)
		}
	}
	if tagOnlyAllocs == 0 {
		t.Fatalf("missing 4-byte tag-only TaskResult<nothing> allocation:\n%s", body)
	}
}

func TestAsyncTaskRuntimeHandleIdentitySurvivesModuleClosure(t *testing.T) {
	withRepoStdlib(t)
	sourceCode := `async fn wait_once() -> nothing {
    checkpoint().await();
    return nothing;
}

@entrypoint
fn main() -> int {
    compare wait_once().await() {
        Success(_) => return 0;
        Cancelled() => return 1;
    };
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	taskType := types.NoTypeID
	for _, fn := range mirMod.Funcs {
		if fn == nil || fn.Name != "wait_once$poll" {
			continue
		}
		for _, local := range fn.Locals {
			label := types.Label(result.Sema.TypeInterner, local.Type)
			if label == "Task<nothing>" {
				taskType = local.Type
				break
			}
		}
	}
	if taskType == types.NoTypeID {
		t.Fatal("missing Task<nothing> local in wait_once$poll")
	}
	if !result.Sema.TypeInterner.IsRuntimeHandleType(taskType) {
		t.Fatalf("Task<nothing> type#%d lost its core runtime-handle identity", taskType)
	}
	payloads, ok := result.Sema.TypeInterner.RuntimeHandlePayloads(taskType)
	if !ok || len(payloads) != 1 || payloads[0] != result.Sema.TypeInterner.Builtins().Nothing {
		t.Fatalf("Task<nothing> runtime payloads = %v, %t; want [nothing], true", payloads, ok)
	}
	if result.Sema.TypeInterner.IsValueComposite(taskType) {
		t.Fatalf("Task<nothing> type#%d must remain a handle, not a boxed value composite", taskType)
	}
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	if taskGlue := fmt.Sprintf("@drop.type%d", taskType); strings.Contains(ir, taskGlue) {
		t.Fatalf("runtime-owned Task<nothing> must not receive generic boxed drop glue %s", taskGlue)
	}
}

func TestDefaultRuntimeHandleUsesNullSentinelWithoutAllocation(t *testing.T) {
	withRepoStdlib(t)
	sourceCode := `@entrypoint
fn main() -> int {
    let task: Task<int> = default::<Task<int>>();
    let _ = task;
    return 0;
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", findMIRFunc(t, mirMod, "main").ID))
	if !strings.Contains(body, "store ptr null") {
		t.Fatalf("default Task<int> must store the null runtime-handle sentinel:\n%s", body)
	}
	if strings.Contains(body, "@rt_alloc") {
		t.Fatalf("default Task<int> must not allocate a fake language struct:\n%s", body)
	}
}
