package vm_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"surge/internal/driver"
	"surge/internal/layout"
	"surge/internal/symbols"
	"surge/internal/types"
)

const abiLayoutSource = `tag Wrap<T>(T);

type BytesViewAlias = BytesView;
type DynArray = int[];
type FixedArray = int[3];
type IntRange = Range<int>;

type Mix = { a: int, b: string, c: uint8 };
type Simple = { a: int, b: uint8, c: int };
type Nested = { s: string, a: int[], b: BytesView };
type WithFixed = { head: uint8, mid: int[3], tail: uint16 };

type SampleUnion = Wrap(int) | string | nothing;
`

type abiTypeIDs struct {
	builtins    types.Builtins
	bytesView   types.TypeID
	dynArray    types.TypeID
	fixedArray  types.TypeID
	intRange    types.TypeID
	mix         types.TypeID
	simple      types.TypeID
	nested      types.TypeID
	withFixed   types.TypeID
	sampleUnion types.TypeID
}

func diagnoseFromSource(t *testing.T, sourceCode string) *driver.DiagnoseResult {
	t.Helper()

	tmpFile, err := os.CreateTemp(t.TempDir(), "abi_layout_*.sg")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() {
		if removeErr := os.Remove(tmpFile.Name()); removeErr != nil {
			t.Fatalf("failed to remove temp file: %v", removeErr)
		}
	}()

	if _, err = tmpFile.WriteString(sourceCode); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			t.Fatalf("failed to close temp file: %v", closeErr)
		}
		t.Fatalf("write source: %v", err)
	}
	if err = tmpFile.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	opts := driver.DiagnoseOptions{Stage: driver.DiagnoseStageSema}
	result, err := driver.DiagnoseWithOptions(context.Background(), tmpFile.Name(), &opts)
	if err != nil {
		t.Fatalf("diagnose failed: %v", err)
	}
	if result.Bag.HasErrors() {
		t.Fatalf("compilation errors: %v", result.Bag.Items())
	}
	if result.Sema == nil || result.Symbols == nil {
		t.Fatal("missing sema/symbols result")
	}
	return result
}

func lookupTypeID(t *testing.T, result *driver.DiagnoseResult, name string) types.TypeID {
	t.Helper()

	nameID := result.Symbols.Table.Strings.Intern(name)
	resolver := symbols.NewResolver(result.Symbols.Table, result.Symbols.FileScope, symbols.ResolverOptions{
		CurrentFile: result.FileID,
	})
	symID, ok := resolver.LookupOne(nameID, symbols.SymbolType.Mask())
	if !ok {
		t.Fatalf("type symbol %s not found", name)
	}
	sym := result.Symbols.Table.Symbols.Get(symID)
	if sym == nil {
		t.Fatalf("invalid symbol for %s", name)
	}
	return sym.Type
}

func loadABITypes(t *testing.T) (*types.Interner, *layout.LayoutEngine, abiTypeIDs) {
	t.Helper()

	result := diagnoseFromSource(t, abiLayoutSource)
	typesIn := result.Sema.TypeInterner
	if typesIn == nil {
		t.Fatal("missing type interner")
	}
	le := layout.New(layout.X86_64LinuxGNU(), typesIn)

	ids := abiTypeIDs{
		builtins:    typesIn.Builtins(),
		bytesView:   lookupTypeID(t, result, "BytesViewAlias"),
		dynArray:    lookupTypeID(t, result, "DynArray"),
		fixedArray:  lookupTypeID(t, result, "FixedArray"),
		intRange:    lookupTypeID(t, result, "IntRange"),
		mix:         lookupTypeID(t, result, "Mix"),
		simple:      lookupTypeID(t, result, "Simple"),
		nested:      lookupTypeID(t, result, "Nested"),
		withFixed:   lookupTypeID(t, result, "WithFixed"),
		sampleUnion: lookupTypeID(t, result, "SampleUnion"),
	}
	return typesIn, le, ids
}

func assertSizeAlign(t *testing.T, le *layout.LayoutEngine, id types.TypeID, wantSize, wantAlign int, label string) {
	t.Helper()

	size, err := le.SizeOf(id)
	if err != nil {
		t.Fatalf("size_of(%s): %v", label, err)
	}
	if size != wantSize {
		t.Fatalf("size_of(%s): want %d, got %d", label, wantSize, size)
	}

	align, err := le.AlignOf(id)
	if err != nil {
		t.Fatalf("align_of(%s): %v", label, err)
	}
	if align != wantAlign {
		t.Fatalf("align_of(%s): want %d, got %d", label, wantAlign, align)
	}
}

func TestABILayoutCoreSizes(t *testing.T) {
	_, le, ids := loadABITypes(t)

	assertSizeAlign(t, le, ids.builtins.Bool, 1, 1, "bool")
	assertSizeAlign(t, le, ids.builtins.Int, 8, 8, "int")
	assertSizeAlign(t, le, ids.builtins.Uint, 8, 8, "uint")
	assertSizeAlign(t, le, ids.builtins.Float, 8, 8, "float")
	assertSizeAlign(t, le, ids.builtins.String, 8, 8, "string")

	assertSizeAlign(t, le, ids.bytesView, 24, 8, "BytesView")
	assertSizeAlign(t, le, ids.dynArray, 8, 8, "int[]")
	assertSizeAlign(t, le, ids.fixedArray, 24, 8, "int[3]")
	assertSizeAlign(t, le, ids.intRange, 8, 8, "Range<int>")
	assertSizeAlign(t, le, ids.mix, 24, 8, "Mix")
}

func TestABILayoutStructOffsets(t *testing.T) {
	_, le, ids := loadABITypes(t)

	offA, err := le.FieldOffset(ids.simple, 0)
	if err != nil {
		t.Fatalf("Simple.a offset: %v", err)
	}
	offB, err := le.FieldOffset(ids.simple, 1)
	if err != nil {
		t.Fatalf("Simple.b offset: %v", err)
	}
	offC, err := le.FieldOffset(ids.simple, 2)
	if err != nil {
		t.Fatalf("Simple.c offset: %v", err)
	}
	if offA != 0 || offB != 8 || offC != 16 {
		t.Fatalf("Simple offsets want [0 8 16], got [%d %d %d]", offA, offB, offC)
	}

	offS, err := le.FieldOffset(ids.nested, 0)
	if err != nil {
		t.Fatalf("Nested.s offset: %v", err)
	}
	offArr, err := le.FieldOffset(ids.nested, 1)
	if err != nil {
		t.Fatalf("Nested.a offset: %v", err)
	}
	offBV, err := le.FieldOffset(ids.nested, 2)
	if err != nil {
		t.Fatalf("Nested.b offset: %v", err)
	}
	if offS != 0 || offArr != 8 || offBV != 16 {
		t.Fatalf("Nested offsets want [0 8 16], got [%d %d %d]", offS, offArr, offBV)
	}

	offHead, err := le.FieldOffset(ids.withFixed, 0)
	if err != nil {
		t.Fatalf("WithFixed.head offset: %v", err)
	}
	offMid, err := le.FieldOffset(ids.withFixed, 1)
	if err != nil {
		t.Fatalf("WithFixed.mid offset: %v", err)
	}
	offTail, err := le.FieldOffset(ids.withFixed, 2)
	if err != nil {
		t.Fatalf("WithFixed.tail offset: %v", err)
	}
	if offHead != 0 || offMid != 8 || offTail != 32 {
		t.Fatalf("WithFixed offsets want [0 8 32], got [%d %d %d]", offHead, offMid, offTail)
	}
}

func TestABILayoutBytesViewOffsets(t *testing.T) {
	_, le, ids := loadABITypes(t)

	offOwner, err := le.FieldOffset(ids.bytesView, 0)
	if err != nil {
		t.Fatalf("BytesView.owner offset: %v", err)
	}
	offPtr, err := le.FieldOffset(ids.bytesView, 1)
	if err != nil {
		t.Fatalf("BytesView.ptr offset: %v", err)
	}
	offLen, err := le.FieldOffset(ids.bytesView, 2)
	if err != nil {
		t.Fatalf("BytesView.len offset: %v", err)
	}
	if offOwner != 0 || offPtr != 8 || offLen != 16 {
		t.Fatalf("BytesView offsets want [0 8 16], got [%d %d %d]", offOwner, offPtr, offLen)
	}
}

func TestABILayoutUnionTagLayout(t *testing.T) {
	_, le, ids := loadABITypes(t)

	layoutInfo, err := le.LayoutOf(ids.sampleUnion)
	if err != nil {
		t.Fatalf("union layout: %v", err)
	}
	if layoutInfo.TagSize != 4 || layoutInfo.TagAlign != 4 {
		t.Fatalf("union tag size/align want 4/4, got %d/%d", layoutInfo.TagSize, layoutInfo.TagAlign)
	}
	if layoutInfo.PayloadOffset != 8 {
		t.Fatalf("union payload offset want 8, got %d", layoutInfo.PayloadOffset)
	}
	if layoutInfo.Size != 16 || layoutInfo.Align != 8 {
		t.Fatalf("union size/align want 16/8, got %d/%d", layoutInfo.Size, layoutInfo.Align)
	}
}

func TestABILayoutSnapshotDeterminism(t *testing.T) {
	snap1 := buildABISnapshot(t)
	snap2 := buildABISnapshot(t)

	if snap1 != snap2 {
		t.Fatalf("layout snapshot mismatch:\nfirst:\n%s\nsecond:\n%s", snap1, snap2)
	}
	if strings.Contains(snap1, "0x") {
		t.Fatalf("layout snapshot must not contain pointer values:\n%s", snap1)
	}
}

func buildABISnapshot(t *testing.T) string {
	typesIn, le, ids := loadABITypes(t)

	var sb strings.Builder
	appendSizeAlign(&sb, le, ids.builtins.Bool, "bool")
	appendSizeAlign(&sb, le, ids.builtins.Int, "int")
	appendSizeAlign(&sb, le, ids.builtins.Uint, "uint")
	appendSizeAlign(&sb, le, ids.builtins.Float, "float")
	appendSizeAlign(&sb, le, ids.builtins.String, "string")
	appendSizeAlign(&sb, le, ids.bytesView, "BytesView")
	appendSizeAlign(&sb, le, ids.dynArray, "int[]")
	appendSizeAlign(&sb, le, ids.fixedArray, "int[3]")
	appendSizeAlign(&sb, le, ids.intRange, "Range<int>")

	appendStructSnapshot(&sb, le, typesIn, ids.mix, "Mix")
	appendStructSnapshot(&sb, le, typesIn, ids.simple, "Simple")
	appendStructSnapshot(&sb, le, typesIn, ids.nested, "Nested")
	appendStructSnapshot(&sb, le, typesIn, ids.withFixed, "WithFixed")
	appendUnionSnapshot(&sb, le, ids.sampleUnion, "SampleUnion")

	return sb.String()
}

func appendSizeAlign(sb *strings.Builder, le *layout.LayoutEngine, id types.TypeID, name string) {
	if sb == nil || le == nil {
		return
	}
	size, err := le.SizeOf(id)
	if err != nil {
		size = -1
	}
	align, err := le.AlignOf(id)
	if err != nil {
		align = -1
	}
	sb.WriteString(fmt.Sprintf("%s size=%d align=%d\n", name, size, align))
}

func appendStructSnapshot(sb *strings.Builder, le *layout.LayoutEngine, typesIn *types.Interner, id types.TypeID, name string) {
	if sb == nil || le == nil || typesIn == nil {
		return
	}
	size, err := le.SizeOf(id)
	if err != nil {
		size = -1
	}
	align, err := le.AlignOf(id)
	if err != nil {
		align = -1
	}
	sb.WriteString(fmt.Sprintf("%s size=%d align=%d", name, size, align))

	info, ok := typesIn.StructInfo(id)
	if ok && info != nil && len(info.Fields) > 0 {
		sb.WriteString(" fields=")
		for i, f := range info.Fields {
			if i > 0 {
				sb.WriteString(",")
			}
			fieldName := fmt.Sprintf("field%d", i)
			if typesIn.Strings != nil {
				if nameText, ok := typesIn.Strings.Lookup(f.Name); ok {
					fieldName = nameText
				}
			}
			off, err := le.FieldOffset(id, i)
			if err != nil {
				off = -1
			}
			sb.WriteString(fmt.Sprintf("%s:%d", fieldName, off))
		}
	}
	sb.WriteString("\n")
}

func appendUnionSnapshot(sb *strings.Builder, le *layout.LayoutEngine, id types.TypeID, name string) {
	if sb == nil || le == nil {
		return
	}
	layoutInfo, err := le.LayoutOf(id)
	if err != nil {
		sb.WriteString(fmt.Sprintf("%s error=%v\n", name, err))
		return
	}
	sb.WriteString(fmt.Sprintf("%s size=%d align=%d tag=%d/%d payload_offset=%d\n", name, layoutInfo.Size, layoutInfo.Align, layoutInfo.TagSize, layoutInfo.TagAlign, layoutInfo.PayloadOffset))
}
