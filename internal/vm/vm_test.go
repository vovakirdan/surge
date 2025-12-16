package vm_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

	mm, err := mono.MonomorphizeModule(result.HIR, result.Instantiations, result.Sema, mono.Options{
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

// runVM executes the MIR with the given runtime and returns exit code and any error.
func runVM(mirMod *mir.Module, rt vm.Runtime, files *source.FileSet, types *types.Interner, tracer *vm.Tracer) (int, *vm.VMError) {
	vmInstance := vm.New(mirMod, rt, files, types, tracer)
	vmErr := vmInstance.Run()
	return vmInstance.ExitCode, vmErr
}

func TestVMEntrypointReturnsNothing(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm", "vm_entrypoint_returns_nothing.sg")

	// Change to project root
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, types := compileToMIR(t, filePath)
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
	filePath := filepath.Join("testdata", "golden", "vm", "vm_entrypoint_returns_int.sg")

	// Change to project root
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, types := compileToMIR(t, filePath)
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
	filePath := filepath.Join("testdata", "golden", "vm", "vm_entrypoint_argv_int.sg")

	// Change to project root
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, types := compileToMIR(t, filePath)
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
	filePath := filepath.Join("testdata", "golden", "vm", "vm_entrypoint_stdin_int.sg")

	// Change to project root
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, types := compileToMIR(t, filePath)
	rt := vm.NewTestRuntime(nil, "9")
	exitCode, vmErr := runVM(mirMod, rt, files, types, nil)

	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}

	if exitCode != 9 {
		t.Errorf("expected exit code 9, got %d", exitCode)
	}
}

func TestVMTraceSmokeTest(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm", "vm_trace_smoke.sg")

	// Change to project root
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	mirMod, files, types := compileToMIR(t, filePath)

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
