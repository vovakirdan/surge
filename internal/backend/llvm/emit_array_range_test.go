package llvm

import (
	"strings"
	"testing"
)

func TestEmitDynamicArrayRangeIndexUsesRuntimeSlice(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let data: int[] = [1, 2, 3, 4];
    let sub: int[] = data[[1..3]];
    return sub.__len() to int;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if !strings.Contains(ir, "call ptr @rt_array_slice(") {
		t.Fatalf("expected dynamic array range indexing to call rt_array_slice:\n%s", ir)
	}
	if strings.Contains(ir, "call void @rt_panic_bounds") {
		t.Fatalf("range operand was lowered through element indexing:\n%s", ir)
	}
}

func TestEmitFixedArrayRangeIndexUsesRuntimeSlice(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let data: int[4] = [1, 2, 3, 4];
    let sub: int[] = data[[1..3]];
    return sub.__len() to int;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if !strings.Contains(ir, "call ptr @rt_array_slice_fixed(") {
		t.Fatalf("expected fixed array range indexing to call rt_array_slice_fixed:\n%s", ir)
	}
	if strings.Contains(ir, "call void @rt_panic_bounds") {
		t.Fatalf("range operand was lowered through element indexing:\n%s", ir)
	}
}

func TestEmitArrayGrowthSyncsSliceViews(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let mut data: int[] = [1, 2];
    data.push(3);
    return data.__len() to int;
}
`

	ir := emitLLVMFromSource(t, sourceCode)

	if !strings.Contains(ir, "call i1 @rt_array_is_view(") {
		t.Fatalf("expected array resize guard to call rt_array_is_view:\n%s", ir)
	}
	if !strings.Contains(ir, "call void @rt_array_sync_views(") {
		t.Fatalf("expected array growth to sync slice views after realloc:\n%s", ir)
	}
	if strings.Contains(ir, "icmp eq i64") && strings.Contains(ir, "-1") {
		t.Fatalf("array view sentinel should stay in the runtime helper, not inline IR:\n%s", ir)
	}
}
