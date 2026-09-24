package llvm

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// RV2-DEBT-370: every task a call of an `async fn` makes is built cold -- through
// __task_create_cold, or __task_create_cold_affine when it borrows its creator's frame --
// and the constructor hands the runtime the start frame's own descriptor as its last
// operand, which is what a handle dropped before the first poll releases the frame with.
// The hot constructors are the stand driver's; compiled code never calls them.
func TestEmitTaskConstructorIsColdAndNamesItsFrame(t *testing.T) {
	sourceCode := `async fn one() -> int {
    return 1;
}

async fn read_ref(x: &int) -> int {
    return *x;
}

@entrypoint
fn main() -> int {
    let value: int = 3;
    let _ = one().await();
    let _ = read_ref(&value).await();
    return 0;
}
`
	mirMod, result := lowerMIRFromSource(t, sourceCode)
	ir, err := EmitModule(mirMod, result.Sema.TypeInterner, result.Symbols.Table, result.FileSet)
	if err != nil {
		t.Fatalf("emit LLVM IR: %v", err)
	}
	for _, row := range []struct{ fn, ctor string }{
		{"one", "__task_create_cold"},
		{"read_ref", "__task_create_cold_affine"},
	} {
		t.Run(row.fn, func(t *testing.T) {
			fn := findMIRFunc(t, mirMod, row.fn)
			body := findLLVMFuncBody(t, ir, fmt.Sprintf("fn.%d", fn.ID))
			call := regexp.MustCompile(`call ptr @` + row.ctor + `\(i64 \d+, ptr %[^,]+, ptr [^,]+, ptr @([^)]+)\)`)
			m := call.FindStringSubmatch(body)
			if m == nil {
				t.Fatalf("the constructor must build its task through %s with the frame's descriptor last:\n%s", row.ctor, body)
			}
			if !strings.Contains(ir, "@"+m[1]+" =") {
				t.Fatalf("the frame descriptor @%s is named but not defined in the module", m[1])
			}
			for _, hot := range []string{"call ptr @__task_create(", "call ptr @__task_create_affine("} {
				if strings.Contains(body, hot) {
					t.Fatalf("compiled code calls the hot constructor %q:\n%s", hot, body)
				}
			}
		})
	}
}
