package vm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"surge/internal/types"
	"surge/internal/vm"
)

func TestVMHeapArgvRoundtrip(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_heap", "vm_argv_roundtrip.sg")

	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, typesInterner := compileToMIR(t, filePath)
	rt := vm.NewTestRuntime([]string{"7"}, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if exitCode != 7 {
		t.Errorf("expected exit code 7, got %d", exitCode)
	}
}

func TestVMHeapStringLiteralNoLeaks(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_heap", "vm_string_literal.sg")

	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, typesInterner := compileToMIR(t, filePath)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMHeapStructLitAndTo(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_heap", "vm_struct_lit_and_to.sg")

	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, typesInterner := compileToMIR(t, filePath)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", exitCode)
	}
}

func TestVMHeapOOBPanics(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_heap", "vm_oob_panics.sg")

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
	if vmErr.Code != vm.PanicOutOfBounds {
		t.Fatalf("expected %v, got %v", vm.PanicOutOfBounds, vmErr.Code)
	}

	out := vmErr.FormatWithFiles(files)
	if !strings.Contains(out, "panic VM1004") {
		t.Fatalf("expected panic code in output, got:\n%s", out)
	}
	if !strings.Contains(out, "testdata/golden/vm_heap/vm_oob_panics.sg:") {
		t.Fatalf("expected span with file path in output, got:\n%s", out)
	}
	if !strings.Contains(out, "backtrace:") || !strings.Contains(out, "main") {
		t.Fatalf("expected backtrace with main frame, got:\n%s", out)
	}
}

func TestVMHeapOverflowPanics(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_heap", "vm_overflow_panics.sg")

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
	if vmErr.Code != vm.PanicIntOverflow {
		t.Fatalf("expected %v, got %v", vm.PanicIntOverflow, vmErr.Code)
	}

	out := vmErr.FormatWithFiles(files)
	if !strings.Contains(out, "panic VM1101") {
		t.Fatalf("expected panic code in output, got:\n%s", out)
	}
	if !strings.Contains(out, "testdata/golden/vm_heap/vm_overflow_panics.sg:") {
		t.Fatalf("expected span with file path in output, got:\n%s", out)
	}
}

func TestVMHeapDoubleFreePanics(t *testing.T) {
	h := &vm.Heap{}
	handle := h.AllocString(types.NoTypeID, "x")
	h.Free(handle)

	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(*vm.VMError); ok {
				if err.Code != vm.PanicDoubleFree {
					t.Fatalf("expected %v, got %v", vm.PanicDoubleFree, err.Code)
				}
				return
			}
			t.Fatalf("unexpected panic type: %T", r)
		}
		t.Fatal("expected panic, got nil")
	}()
	h.Free(handle)
}
