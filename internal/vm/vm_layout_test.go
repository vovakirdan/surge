package vm_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"surge/internal/driver"
	"surge/internal/layout"
	"surge/internal/symbols"
	"surge/internal/vm"
)

func TestVMLayoutSizeAlignPrimitives(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_layout", "size_align_primitives.sg")

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
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMLayoutSizeAlignStruct(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_layout", "size_align_struct.sg")

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
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMLayoutSizeAlignArrayFixed(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_layout", "size_align_array_fixed.sg")

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
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMLayoutPackedStruct(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_layout", "layout_packed_struct.sg")

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
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMLayoutAlignType(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_layout", "layout_align_type.sg")

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
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMLayoutAlignField(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_layout", "layout_align_field.sg")

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
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMLayoutAlignFieldOffset(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_layout", "layout_align_field.sg")

	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}
	defer os.Chdir(filepath.Join("internal", "vm"))

	opts := driver.DiagnoseOptions{
		Stage: driver.DiagnoseStageSema,
	}
	result, err := driver.DiagnoseWithOptions(context.Background(), filePath, opts)
	if err != nil {
		t.Fatalf("compilation failed: %v", err)
	}
	if result.Bag.HasErrors() {
		t.Fatalf("compilation errors: %v", result.Bag.Items())
	}
	if result.Sema == nil || result.Symbols == nil {
		t.Fatal("missing sema/symbols result")
	}

	nameID := result.Symbols.Table.Strings.Intern("S")
	resolver := symbols.NewResolver(result.Symbols.Table, result.Symbols.FileScope, symbols.ResolverOptions{
		CurrentFile: result.FileID,
	})
	symID, ok := resolver.LookupOne(nameID, symbols.SymbolType.Mask())
	if !ok {
		t.Fatal("type symbol S not found")
	}
	sym := result.Symbols.Table.Symbols.Get(symID)
	if sym == nil {
		t.Fatal("invalid symbol for S")
	}

	le := layout.New(layout.X86_64LinuxGNU(), result.Sema.TypeInterner)
	if got := le.FieldOffset(sym.Type, 1); got != 8 {
		t.Fatalf("expected field offset 8, got %d", got)
	}
}

func TestVMDropOrderReverseLocals(t *testing.T) {
	filePath := filepath.Join("testdata", "golden", "vm_layout", "drop_order_reverse_locals.sg")

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
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	trace := traceBuf.String()

	handleA := mustLastStringHandleForVar(t, trace, "a")
	handleB := mustLastStringHandleForVar(t, trace, "b")
	if handleA == handleB {
		t.Fatalf("expected distinct handles for a and b, got %d", handleA)
	}

	freeOrder := allFreedHandles(trace)
	idxA := indexOf(freeOrder, handleA)
	idxB := indexOf(freeOrder, handleB)
	if idxA < 0 || idxB < 0 {
		t.Fatalf("missing frees: a=%d idx=%d b=%d idx=%d frees=%v\ntrace:\n%s", handleA, idxA, handleB, idxB, freeOrder, trace)
	}
	if idxB >= idxA {
		t.Fatalf("expected reverse-local drop order (b before a), got free order %v\ntrace:\n%s", freeOrder, trace)
	}
}

func mustLastStringHandleForVar(t *testing.T, trace string, name string) int {
	t.Helper()
	re := regexp.MustCompile(`write L[0-9]+\(` + regexp.QuoteMeta(name) + `\) = string#([0-9]+)\(`)
	m := re.FindAllStringSubmatch(trace, -1)
	if len(m) == 0 {
		t.Fatalf("missing trace write for %q\ntrace:\n%s", name, trace)
	}
	last := m[len(m)-1]
	n, err := strconv.Atoi(last[1])
	if err != nil {
		t.Fatalf("parse handle for %q: %v", name, err)
	}
	return n
}

func allFreedHandles(trace string) []int {
	re := regexp.MustCompile(`\[heap\] free handle#([0-9]+)`)
	m := re.FindAllStringSubmatch(trace, -1)
	out := make([]int, 0, len(m))
	for _, s := range m {
		if len(s) < 2 {
			continue
		}
		if n, err := strconv.Atoi(s[1]); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func indexOf(list []int, x int) int {
	for i, v := range list {
		if v == x {
			return i
		}
	}
	return -1
}
