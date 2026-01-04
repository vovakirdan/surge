package vm_test

import (
	"strings"
	"testing"

	"surge/internal/vm"
)

func TestVMNumbersDivByZeroPanics(t *testing.T) {
	requireVMBackend(t)
	sourceCode := `@entrypoint
fn main() -> int {
    return 1 / 0;
}
`
	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	_, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr == nil {
		t.Fatal("expected panic, got nil")
	}
	if vmErr.Code != vm.PanicDivisionByZero {
		t.Fatalf("expected %v, got %v", vm.PanicDivisionByZero, vmErr.Code)
	}

	out := vmErr.FormatWithFiles(files)
	if !strings.Contains(out, "panic VM3203") {
		t.Fatalf("expected panic code in output, got:\n%s", out)
	}
	if !strings.Contains(out, ".sg:") {
		t.Fatalf("expected span with file path in output, got:\n%s", out)
	}
}
