package vm_test

import (
	"strings"
	"testing"

	"surge/internal/vm"
)

func TestVMRefsRead(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let x: int = 7;
    let r: &int = &x;
    return *r;
}
`

	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if exitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", exitCode)
	}
}

func TestVMRefsRefMutWrite(t *testing.T) {
	sourceCode := `fn set(x: &mut int) -> nothing {
    *x = 9;
    return;
}

@entrypoint
fn main() -> int {
    let mut v: int = 1;
    set(&mut v);
    return v;
}
`

	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if exitCode != 9 {
		t.Fatalf("expected exit code 9, got %d", exitCode)
	}
}

func TestVMRefsStructFieldWrite(t *testing.T) {
	sourceCode := `type S = { a: int, b: int }

fn set(x: &mut int) -> nothing {
    *x = 10;
    return;
}

@entrypoint
fn main() -> int {
    let mut s: S = S { a = 1, b = 2 };
    set(&mut s.a);
    return s.a;
}
`

	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if exitCode != 10 {
		t.Fatalf("expected exit code 10, got %d", exitCode)
	}
}

func TestVMRefsArrayElemWrite(t *testing.T) {
	sourceCode := `fn set(x: &mut int) -> nothing {
    *x = 9;
    return;
}

@entrypoint
fn main() -> int {
    let mut a: int[] = [1, 2, 3];
    set(&mut a[1]);
    return a[1];
}
`

	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if exitCode != 9 {
		t.Fatalf("expected exit code 9, got %d", exitCode)
	}
}

func TestVMRefsStoreThroughSharedRefPanics(t *testing.T) {
	sourceCode := `fn set(x: &int) -> nothing {
    *x = 2;
    return;
}

@entrypoint
fn main() -> int {
    let x: int = 1;
    set(&x);
    return 0;
}
`

	mirMod, files, typesInterner := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	_, vmErr := runVM(mirMod, rt, files, typesInterner, nil)

	if vmErr == nil {
		t.Fatal("expected panic, got nil")
	}
	if vmErr.Code != vm.PanicStoreThroughNonMutRef {
		t.Fatalf("expected %v, got %v", vm.PanicStoreThroughNonMutRef, vmErr.Code)
	}

	out := vmErr.FormatWithFiles(files)
	if !strings.Contains(out, "panic VM2102") {
		t.Fatalf("expected panic code in output, got:\n%s", out)
	}
	// Проверяем, что путь к файлу присутствует (будет временный файл)
	if !strings.Contains(out, ".sg:") {
		t.Fatalf("expected span with file path in output, got:\n%s", out)
	}
	if !strings.Contains(out, "backtrace:") || !strings.Contains(out, "main") {
		t.Fatalf("expected backtrace with main frame, got:\n%s", out)
	}
}
