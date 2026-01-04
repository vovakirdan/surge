package vm_test

import (
	"strings"
	"testing"

	"surge/internal/types"
	"surge/internal/vm"
)

func TestVMHeapArgvRoundtrip(t *testing.T) {
	sourceCode := `@entrypoint("argv") fn main(x: int) -> int { return x; }
`
	result := runProgramFromSource(t, sourceCode, runOptions{argv: []string{"7"}})
	if result.exitCode != 7 {
		t.Errorf("expected exit code 7, got %d", result.exitCode)
	}
}

func TestVMHeapStringLiteralNoLeaks(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let s: string = "x";
    return 0;
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.exitCode)
	}
}

func TestVMHeapStructLitAndTo(t *testing.T) {
	sourceCode := `type MyExitCode = {
    code: int,
}

extern<MyExitCode> {
    fn __to(self: MyExitCode, _target: int) -> int {
        return self.code;
    }
}

@entrypoint
fn main() -> MyExitCode {
    return MyExitCode { code = 42 };
}
`
	result := runProgramFromSource(t, sourceCode, runOptions{})
	if result.exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", result.exitCode)
	}
}

func TestVMHeapOOBPanics(t *testing.T) {
	requireVMBackend(t)
	sourceCode := `@entrypoint
fn main() -> int {
    let argv: string[] = rt_argv();
    let s: string = argv[0];
    return 0;
}
`
	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	_, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr == nil {
		t.Fatal("expected panic, got nil")
	}
	if vmErr.Code != vm.PanicArrayIndexOutOfRange {
		t.Fatalf("expected %v, got %v", vm.PanicArrayIndexOutOfRange, vmErr.Code)
	}

	out := vmErr.FormatWithFiles(files)
	if !strings.Contains(out, "panic VM2105") {
		t.Fatalf("expected panic code in output, got:\n%s", out)
	}
	if !strings.Contains(out, ".sg:") {
		t.Fatalf("expected span with file path in output, got:\n%s", out)
	}
	if !strings.Contains(out, "backtrace:") || !strings.Contains(out, "main") {
		t.Fatalf("expected backtrace with main frame, got:\n%s", out)
	}
}

func TestVMHeapDivByZeroPanics(t *testing.T) {
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

func TestVMHeapDoubleFreePanics(t *testing.T) {
	requireVMBackend(t)
	h := &vm.Heap{}
	handle := h.AllocString(types.NoTypeID, "x")
	h.Release(handle)

	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(*vm.VMError); ok {
				if err.Code != vm.PanicRCUseAfterFree {
					t.Fatalf("expected %v, got %v", vm.PanicRCUseAfterFree, err.Code)
				}
				return
			}
			t.Fatalf("unexpected panic type: %T", r)
		}
		t.Fatal("expected panic, got nil")
	}()
	h.Release(handle)
}
