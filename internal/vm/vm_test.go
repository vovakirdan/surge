package vm_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/mir"
	"surge/internal/mono"
	"surge/internal/source"
	"surge/internal/types"
	"surge/internal/vm"
)

// compileToMIR compiles a .sg file to MIR.
func compileToMIR(t *testing.T, filePath string) (*mir.Module, *source.FileSet, *types.Interner) {
	t.Helper()

	opts := driver.DiagnoseOptions{
		Stage:              driver.DiagnoseStageSema,
		EmitHIR:            true,
		EmitInstantiations: true,
	}

	result, err := driver.DiagnoseWithOptions(context.Background(), filePath, opts)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}

	if result.Bag.HasErrors() {
		var sb strings.Builder
		for _, d := range result.Bag.Items() {
			sb.WriteString(d.Message)
			sb.WriteString("\n")
		}
		t.Fatalf("compilation errors:\n%s", sb.String())
	}

	if result.HIR == nil {
		t.Fatal("HIR not available")
	}
	if result.Instantiations == nil {
		t.Fatal("instantiation map not available")
	}
	if result.Sema == nil {
		t.Fatal("semantic analysis result not available")
	}

	hirModule, err := driver.CombineHIRWithCore(context.Background(), result)
	if err != nil {
		t.Fatalf("HIR merge failed: %v", err)
	}
	if hirModule == nil {
		hirModule = result.HIR
	}

	mm, err := mono.MonomorphizeModule(hirModule, result.Instantiations, result.Sema, mono.Options{
		MaxDepth: 64,
	})
	if err != nil {
		t.Fatalf("monomorphization failed: %v", err)
	}

	mirMod, err := mir.LowerModule(mm, result.Sema)
	if err != nil {
		t.Fatalf("MIR lowering failed: %v", err)
	}

	for _, f := range mirMod.Funcs {
		mir.SimplifyCFG(f)
		mir.RecognizeSwitchTag(f)
		mir.SimplifyCFG(f)
	}

	if err := mir.Validate(mirMod, result.Sema.TypeInterner); err != nil {
		t.Fatalf("MIR validation failed: %v", err)
	}

	return mirMod, result.FileSet, result.Sema.TypeInterner
}

// compileToMIRFromSource compiles source code from a string via temp file.
// Note: Changes to project root temporarily because driver.DiagnoseWithOptions
// requires it for stdlib resolution.
func compileToMIRFromSource(t *testing.T, sourceCode string) (*mir.Module, *source.FileSet, *types.Interner) {
	t.Helper()

	// Change to project root for driver
	if err := os.Chdir("../.."); err != nil {
		t.Fatalf("failed to change to project root: %v", err)
	}
	defer func() {
		if err := os.Chdir("internal/vm"); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	}()

	tmpFile, err := os.CreateTemp(".", "test_*.sg")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(sourceCode); err != nil {
		tmpFile.Close()
		t.Fatalf("failed to write source code: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	return compileToMIR(t, tmpFile.Name())
}

// runVM executes the MIR with the given runtime and returns exit code and any error.
func runVM(mirMod *mir.Module, rt vm.Runtime, files *source.FileSet, types *types.Interner, tracer *vm.Tracer) (int, *vm.VMError) {
	vmInstance := vm.New(mirMod, rt, files, types, tracer)
	vmErr := vmInstance.Run()
	return vmInstance.ExitCode, vmErr
}

func TestVMEntrypointReturnsNothing(t *testing.T) {
	sourceCode := `@entrypoint fn main() { }
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, types, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}

	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMEntrypointReturnsInt(t *testing.T) {
	sourceCode := `@entrypoint fn main() -> int { return 42; }
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, types, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}

	if exitCode != 42 {
		t.Errorf("expected exit code 42, got %d", exitCode)
	}
}

func TestVMEntrypointArgvInt(t *testing.T) {
	sourceCode := `@entrypoint("argv") fn main(x: int) -> int { return x; }
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime([]string{"7"}, "")
	exitCode, vmErr := runVM(mirMod, rt, files, types, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}

	if exitCode != 7 {
		t.Errorf("expected exit code 7, got %d", exitCode)
	}
}

func TestVMEntrypointStdinInt(t *testing.T) {
	sourceCode := `@entrypoint("stdin") fn main(x: int) -> int { return x; }
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "9")
	exitCode, vmErr := runVM(mirMod, rt, files, types, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}

	if exitCode != 9 {
		t.Errorf("expected exit code 9, got %d", exitCode)
	}
}

// TestVMEmptyArgvBoundsCheck verifies that accessing argv[0] when no "--" args
// are provided causes a bounds panic. This tests the behavior of:
//
//	surge run file.sg   (no "--")
//
// where rt_argv returns empty [] and indexing panics.
func TestVMEmptyArgvBoundsCheck(t *testing.T) {
	sourceCode := `@entrypoint("argv") fn main(x: int) -> int { return x; }
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
	// Empty argv - simulates running without "--" separator
	rt := vm.NewRuntimeWithArgs(nil)
	_, vmErr := runVM(mirMod, rt, files, types, nil)

	if vmErr == nil {
		t.Fatal("expected panic with empty argv, got nil")
	}
	// Should panic with out of bounds
	if vmErr.Code != vm.PanicOutOfBounds {
		t.Fatalf("expected PanicOutOfBounds (VM1004), got %v", vmErr.Code)
	}
}

func TestVMTraceSmokeTest(t *testing.T) {
	sourceCode := `@entrypoint fn main() -> int {
    let a: int = 1;
    let b: int = 2;
    return a + b;
}
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)

	var traceBuf bytes.Buffer
	tracer := vm.NewTracer(&traceBuf, files)

	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, types, tracer)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}

	if exitCode != 3 {
		t.Errorf("expected exit code 3 (1+2), got %d", exitCode)
	}

	// Verify trace contains expected elements
	trace := traceBuf.String()
	if !strings.Contains(trace, "__surge_start") {
		t.Error("trace should contain __surge_start function")
	}
	if !strings.Contains(trace, "main") {
		t.Error("trace should contain main function")
	}
	if !strings.Contains(trace, "bb") {
		t.Error("trace should contain basic block references (bb)")
	}
}
