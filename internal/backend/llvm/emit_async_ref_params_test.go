package llvm

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// A shared reference parameter of an async fn is stored as the pointer it is,
// exactly as a mutable one always was. The constructor used to copy the
// referent into a heap box here (RV2-DEBT-303, "rt_alloc(i64 8, i64 8)" in
// this prologue, freed by nothing); the box is gone because the borrowed place
// is promoted to the creator's frame and the child is pinned to the creator's
// carrier, and this row is what keeps it gone. It is built through the affine
// constructor, which is the other half of the same fact.
func TestEmitAsyncSharedRefParamKeepsCallerAlias(t *testing.T) {
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
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", fn.ID))

	prologueEnd := strings.Index(body, "\n  br ")
	if prologueEnd < 0 {
		t.Fatalf("missing constructor prologue terminator:\n%s", body)
	}
	prologue := body[:prologueEnd]
	if strings.Contains(prologue, "call ptr @rt_alloc(") {
		t.Fatalf("async shared ref constructor must not box the referent:\n%s", prologue)
	}
	if !strings.Contains(body, "store ptr %p0, ptr %l0") {
		t.Fatalf("async shared ref constructor should store the original pointer alias:\n%s", body)
	}
	if !strings.Contains(body, "call ptr @__task_create_affine(") {
		t.Fatalf("a constructor holding a borrow must build its task through __task_create_affine:\n%s", body)
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
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", fn.ID))

	prologueEnd := strings.Index(body, "\n  br ")
	if prologueEnd < 0 {
		t.Fatalf("missing constructor prologue terminator:\n%s", body)
	}
	prologue := body[:prologueEnd]
	if strings.Contains(prologue, "call ptr @rt_alloc(") {
		t.Fatalf("async mutable ref constructor must not box the caller alias:\n%s", prologue)
	}
	if !strings.Contains(body, "store ptr %p0, ptr %l0") {
		t.Fatalf("async mutable ref constructor should store original pointer alias:\n%s", body)
	}
}

func findLLVMFuncBody(t *testing.T, ir, name string) string {
	t.Helper()

	// A parameter list nests one level of parentheses: an aggregate passed or
	// returned by address carries its storage type inside the attribute, as in
	// `ptr sret([16 x i8]) align 8`. Scanning to the first `)` stops inside
	// that attribute and finds no function at all.
	re := regexp.MustCompile(`(?s)define [^{]+ @` + regexp.QuoteMeta(name) +
		`\((?:[^()]|\([^()]*\))*\) \{.*?\n\}`)
	body := re.FindString(ir)
	if body == "" {
		t.Fatalf("missing LLVM function %s:\n%s", name, ir)
	}
	return body
}
