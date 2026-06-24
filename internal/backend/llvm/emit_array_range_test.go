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
