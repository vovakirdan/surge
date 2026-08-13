package sema

import (
	"testing"

	"surge/internal/diag"
)

// An array or tuple literal TAKES the value it is given, exactly as a struct
// literal does. Both spellings are here because they are two independent call
// sites in `type_expr_values.go`: a test covering only arrays leaves the tuple
// free to regress, which is how the defect arrived in the first place — the
// struct literal was wired to `observeMove` and its two siblings were not.
//
// Without the move, the binding stays readable while the aggregate owns the same
// string, and native arrays carry no refcount to arbitrate between two owners.
// Measured at the time: `free(): double free detected in tcache 2`, exit 255.
//
// The diagnostic is the half a unit test can see. The RUNTIME half — a program
// that never reads the binding again, so no diagnostic fires and only ownership
// is under test — is `TestRuntimeV2AggregateLiteralTakesItsOperand` in
// internal/vm, and it has to live there: the same program exits 0 with correct
// output on the VM lane whether or not the fix is present, so the behavioural
// corpus is structurally unable to catch this one.
func TestAggregateLiteralTakesItsOperand(t *testing.T) {
	for name, src := range map[string]string{
		"array literal": `
fn f() -> int {
    let a: string = "hello";
    let xs: string[] = [a, "world"];
    let b = a;
    return 0;
}
`,
		"tuple literal": `
fn f() -> int {
    let a: string = "hello";
    let t: (string, int) = (a, 1);
    let b = a;
    return 0;
}
`,
	} {
		t.Run(name, func(t *testing.T) {
			parseBag, semaBag := runSemaOnSnippet(t, src)
			if parseBag.HasErrors() {
				t.Fatalf("unexpected parse diagnostics: %s", diagnosticsSummary(parseBag))
			}
			if !hasCode(semaBag, diag.SemaUseAfterMove) {
				t.Fatalf("reading a binding the literal took must be refused; got %s",
					diagnosticsSummary(semaBag))
			}
		})
	}
}
