package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/vm"
)

func TestVMNumbersDivByZeroPanics(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_numbers", "div_by_zero_panics.sg")

	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, typesInterner := compileToMIR(t, filePath)
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
	if !strings.Contains(out, "testdata/golden/vm_numbers/div_by_zero_panics.sg:") {
		t.Fatalf("expected span with file path in output, got:\n%s", out)
	}
}
