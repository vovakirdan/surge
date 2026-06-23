package llvm

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestEmitAsyncSharedRefParamIsBoxedAndAllocationChecked(t *testing.T) {
	sourceCode := `async fn read_ref(x: &int) -> int {
    checkpoint().await();
    return *x;
}

@entrypoint
fn main() -> int {
    let value: int = 3;
    compare read_ref(&value).await() {
        Success(v) => return v;
        Cancelled() => return 9;
    };
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	fn := findMIRFunc(t, mirMod, "read_ref")
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", fn.ID))

	for _, want := range []string{
		"call ptr @rt_alloc(i64 8, i64 8)",
		"icmp eq ptr",
		"call void @llvm.trap()",
		"unreachable",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("async shared ref constructor missing %q:\n%s", want, body)
		}
	}
}

func TestEmitAsyncMutableRefParamKeepsCallerAlias(t *testing.T) {
	sourceCode := `async fn bump(x: &mut int) -> nothing {
    checkpoint().await();
    *x = *x + 1;
    return nothing;
}

@entrypoint
fn main() -> int {
    let mut value: int = 1;
    compare bump(&mut value).await() {
        Success(_) => return value;
        Cancelled() => return 9;
    };
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	fn := findMIRFunc(t, mirMod, "bump")
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", fn.ID))

	if strings.Contains(body, "call ptr @rt_alloc(i64 8, i64 8)") {
		t.Fatalf("async mutable ref constructor must not box the caller alias:\n%s", body)
	}
	if !strings.Contains(body, "store ptr %p0, ptr %l0") {
		t.Fatalf("async mutable ref constructor should store original pointer alias:\n%s", body)
	}
}

func findLLVMFuncBody(t *testing.T, ir, name string) string {
	t.Helper()

	re := regexp.MustCompile(`(?s)define [^{]+ @` + regexp.QuoteMeta(name) + `\([^)]*\) \{.*?\n\}`)
	body := re.FindString(ir)
	if body == "" {
		t.Fatalf("missing LLVM function %s:\n%s", name, ir)
	}
	return body
}
