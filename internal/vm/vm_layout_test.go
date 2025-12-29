package vm_test

import (
	"bytes"
	"context"
	"os"
	"regexp"
	"testing"

	"surge/internal/driver"
	"surge/internal/layout"
	"surge/internal/symbols"
	"surge/internal/vm"
)

func TestVMLayoutSizeAlignPrimitives(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    if (size_of::<bool>() != 1:uint) { return 1; }
    if (align_of::<bool>() != 1:uint) { return 2; }

    if (size_of::<int8>() != 1:uint) { return 10; }
    if (align_of::<int8>() != 1:uint) { return 11; }
    if (size_of::<int16>() != 2:uint) { return 12; }
    if (align_of::<int16>() != 2:uint) { return 13; }
    if (size_of::<int32>() != 4:uint) { return 14; }
    if (align_of::<int32>() != 4:uint) { return 15; }
    if (size_of::<int64>() != 8:uint) { return 16; }
    if (align_of::<int64>() != 8:uint) { return 17; }

    if (size_of::<uint8>() != 1:uint) { return 20; }
    if (align_of::<uint8>() != 1:uint) { return 21; }
    if (size_of::<uint16>() != 2:uint) { return 22; }
    if (align_of::<uint16>() != 2:uint) { return 23; }
    if (size_of::<uint32>() != 4:uint) { return 24; }
    if (align_of::<uint32>() != 4:uint) { return 25; }
    if (size_of::<uint64>() != 8:uint) { return 26; }
    if (align_of::<uint64>() != 8:uint) { return 27; }

    // "int/uint/float" are dynamic-sized objects in the v1 ABI contract.
    if (size_of::<int>() != 8:uint) { return 30; }
    if (align_of::<int>() != 8:uint) { return 31; }
    if (size_of::<uint>() != 8:uint) { return 32; }
    if (align_of::<uint>() != 8:uint) { return 33; }
    if (size_of::<float>() != 8:uint) { return 34; }
    if (align_of::<float>() != 8:uint) { return 35; }

    if (size_of::<float32>() != 4:uint) { return 40; }
    if (align_of::<float32>() != 4:uint) { return 41; }
    if (size_of::<float64>() != 8:uint) { return 42; }
    if (align_of::<float64>() != 8:uint) { return 43; }

    return 0;
}
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
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
	sourceCode := `fn align_up(x: uint, align: uint) -> uint {
    return (x + align - 1:uint) / align * align;
}

type S = {
    a: uint8,
    b: uint64,
    c: uint16,
}

// Prefixes for deriving offsets using ABI size/align.
type S0 = { a: uint8 }
type S1 = { a: uint8, b: uint64 }

@entrypoint
fn main() -> int {
    let size_s: uint = size_of::<S>();
    let align_s: uint = align_of::<S>();

    let off_b: uint = align_up(size_of::<S0>(), align_of::<uint64>());
    let off_c: uint = align_up(size_of::<S1>(), align_of::<uint16>());

    if (!(off_b == 8:uint)) { return 1; }
    if (!(off_c == 16:uint)) { return 2; }
    if (!(align_s == 8:uint)) { return 3; }
    if (!(size_s == 24:uint)) { return 4; }

    return 0;
}
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
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
	sourceCode := `fn align_up(x: uint, align: uint) -> uint {
    return (x + align - 1:uint) / align * align;
}

type A = int32[3];

type S = {
    x: uint8,
    a: A,
    y: uint16,
}

type S0 = { x: uint8 }
type S1 = { x: uint8, a: A }

@entrypoint
fn main() -> int {
    if (!(size_of::<A>() == 12:uint)) { return 1; }
    if (!(align_of::<A>() == 4:uint)) { return 2; }

    let off_a: uint = align_up(size_of::<S0>(), align_of::<A>());
    let off_y: uint = align_up(size_of::<S1>(), align_of::<uint16>());
    if (!(off_a == 4:uint)) { return 3; }
    if (!(off_y == 16:uint)) { return 4; }

    if (!(align_of::<S>() == 4:uint)) { return 5; }
    if (!(size_of::<S>() == 20:uint)) { return 6; }

    return 0;
}
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
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
	sourceCode := `@packed
type S = {
    a: uint8,
    b: uint64,
}

@entrypoint
fn main() -> int {
    let size_s: uint = size_of::<S>();
    let align_s: uint = align_of::<S>();

    if (!(size_s == 9:uint)) { return 1; }
    if (!(align_s == 1:uint)) { return 2; }
    return 0;
}
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
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
	sourceCode := `@align(16)
type S = {
    a: uint8,
    b: uint64,
}

@entrypoint
fn main() -> int {
    let size_s: uint = size_of::<S>();
    let align_s: uint = align_of::<S>();

    if (!(align_s == 16:uint)) { return 1; }
    if (!(((size_s / 16:uint) * 16:uint) == size_s)) { return 2; }
    return 0;
}
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
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
	sourceCode := `type S = {
    a: uint8,
    @align(8)
    b: uint8,
}

@entrypoint
fn main() -> int {
    let size_s: uint = size_of::<S>();
    let align_s: uint = align_of::<S>();

    if (!(align_s == 8:uint)) { return 1; }
    if (!(size_s == 16:uint)) { return 2; }
    return 0;
}
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
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
	sourceCode := `type S = {
    a: uint8,
    @align(8)
    b: uint8,
}

@entrypoint
fn main() -> int {
    let size_s: uint = size_of::<S>();
    let align_s: uint = align_of::<S>();

    if (!(align_s == 8:uint)) { return 1; }
    if (!(size_s == 16:uint)) { return 2; }
    return 0;
}
`
	var err error
	var tmpFile *os.File
	// Create temp file for driver.DiagnoseWithOptions
	tmpFile, err = os.CreateTemp("", "test_*.sg")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err = tmpFile.WriteString(sourceCode); err != nil {
		tmpFile.Close()
		t.Fatalf("failed to write source code: %v", err)
	}
	if err = tmpFile.Close(); err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	opts := driver.DiagnoseOptions{
		Stage: driver.DiagnoseStageSema,
	}
	result, err := driver.DiagnoseWithOptions(context.Background(), tmpFile.Name(), opts)
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
	got, err := le.FieldOffset(sym.Type, 1)
	if err != nil {
		t.Fatalf("layout error: %v", err)
	}
	if got != 8 {
		t.Fatalf("expected field offset 8, got %d", got)
	}
}

func TestVMLayoutSizeAlignCoreTypes(t *testing.T) {
	sourceCode := `type Mix = { a: int, b: string, c: int[] }

@entrypoint
fn main() -> int {
    if (size_of::<string>() != 8:uint) { return 1; }
    if (align_of::<string>() != 8:uint) { return 2; }
    if (size_of::<BytesView>() != 24:uint) { return 3; }
    if (align_of::<BytesView>() != 8:uint) { return 4; }
    if (size_of::<int[]>() != 8:uint) { return 5; }
    if (align_of::<int[]>() != 8:uint) { return 6; }
    if (size_of::<int[3]>() != 24:uint) { return 7; }
    if (align_of::<int[3]>() != 8:uint) { return 8; }
    if (size_of::<Mix>() != 24:uint) { return 9; }
    if (align_of::<Mix>() != 8:uint) { return 10; }
    return 0;
}
`
	mirMod, files, types := compileToMIRFromSource(t, sourceCode)
	rt := vm.NewTestRuntime(nil, "")
	exitCode, vmErr := runVM(mirMod, rt, files, types, nil)
	if vmErr != nil {
		t.Fatalf("unexpected error: %v", vmErr.Error())
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestVMDropOrderReverseLocals(t *testing.T) {
	sourceCode := `@entrypoint
fn main() -> int {
    let a: string = "a";
    let b: string = "b";
    return 0;
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
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	trace := traceBuf.String()

	freeOrder := freedStringPreviews(trace)
	idxA := indexOfString(freeOrder, "a")
	idxB := indexOfString(freeOrder, "b")
	if idxA < 0 || idxB < 0 {
		t.Fatalf("missing frees: a=%d b=%d frees=%v\ntrace:\n%s", idxA, idxB, freeOrder, trace)
	}
	if idxB >= idxA {
		t.Fatalf("expected reverse-local drop order (b before a), got free order %v\ntrace:\n%s", freeOrder, trace)
	}
}

func freedStringPreviews(trace string) []string {
	re := regexp.MustCompile(`\[heap\] free string\([^)]*preview="([^"]*)"`)
	matches := re.FindAllStringSubmatch(trace, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		out = append(out, match[1])
	}
	return out
}

func indexOfString(list []string, x string) int {
	for i, v := range list {
		if v == x {
			return i
		}
	}
	return -1
}
