package llvm

import (
	"fmt"
	"regexp"
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
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", findMIRFunc(t, mirMod, "wait_once$poll").ID))

	// `TaskResult<nothing>` is four bytes of tag and nothing else, so its
	// storage is a four-byte slot and there is no byte past the tag to address.
	// The slot used to be a heap allocation of the same four bytes; what the
	// test watches for is unchanged — a payload store into a value that has no
	// payload — and only where the four bytes live has moved.
	//
	// The slot and the store are no longer adjacent, though: inline storage is
	// declared once at the top of the function while the tag is written
	// wherever the value is built, so the two are matched by name rather than
	// by position.
	tagOnlySlots := 0
	for _, slot := range regexp.MustCompile(`(%\w+) = alloca \[4 x i8\], align 4`).FindAllStringSubmatch(body, -1) {
		mem := slot[1]
		if !regexp.MustCompile(`store i32 \d+, ptr ` + regexp.QuoteMeta(mem) + `, align 4`).MatchString(body) {
			continue
		}
		tagOnlySlots++
		if strings.Contains(body, "ptr "+mem+", i64 4") {
			t.Fatalf("tag-only TaskResult<nothing> slot %s must not address a payload past its 4-byte layout:\n%s", mem, body)
		}
	}
	if tagOnlySlots == 0 {
		t.Fatalf("missing 4-byte tag-only TaskResult<nothing> slot:\n%s", body)
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
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
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
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
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
