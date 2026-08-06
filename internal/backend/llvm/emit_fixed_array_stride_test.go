package llvm

import (
	"regexp"
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

func fixedCompositeArrayEmitter(t *testing.T) (*Emitter, types.TypeID, types.TypeID) {
	t.Helper()
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()

	item := typesIn.RegisterStruct(typesIn.Strings.Intern("Item"), source.Span{})
	typesIn.SetStructFields(item, []types.StructField{
		{Type: typesIn.Builtins().Int},
		{Type: typesIn.Builtins().Int},
	})
	typesIn.MarkCopyType(item)
	arrayName := typesIn.Strings.Intern("ArrayFixed")
	typesIn.EnsureArrayFixedNominal(
		arrayName,
		typesIn.Strings.Intern("T"),
		typesIn.Strings.Intern("N"),
		source.Span{},
		1,
		typesIn.Builtins().Uint32,
	)
	fixed := typesIn.RegisterStructInstanceWithValues(arrayName, source.Span{}, []types.TypeID{item}, []uint64{2})
	typesIn.MarkCopyType(fixed)
	if elem, length, ok := typesIn.ArrayFixedInfo(fixed); !ok || elem != item || length != 2 {
		t.Fatalf("ArrayFixedInfo(%d) = (%d, %d, %v), want (%d, 2, true)", fixed, elem, length, ok, item)
	}
	registry, err := layout.FinalizeRegistry(layout.New(layout.X86_64LinuxGNU(), typesIn), []types.TypeID{fixed, item})
	if err != nil {
		t.Fatalf("FinalizeRegistry: %v", err)
	}
	emitter := &Emitter{
		mod:   &mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}},
		types: typesIn,
	}
	if !emitter.isCloneableComposite(item) || !emitter.typeOwnsHeap(item) {
		t.Fatalf("Item clone/drop classification = (%v, %v), want (true, true)", emitter.isCloneableComposite(item), emitter.typeOwnsHeap(item))
	}
	return emitter, fixed, item
}

func TestFixedCompositeArrayCloneAndDropUseCanonicalStride(t *testing.T) {
	for _, operation := range []string{"clone", "drop"} {
		t.Run(operation, func(t *testing.T) {
			emitter, fixed, _ := fixedCompositeArrayEmitter(t)
			var err error
			if operation == "clone" {
				err = emitter.emitCloneGlueBody(fixed)
			} else {
				err = emitter.emitDropGlueBody(fixed)
			}
			if err != nil {
				t.Fatalf("emit %s glue: %v", operation, err)
			}
			ir := emitter.buf.String()
			base := "%p"
			if operation == "clone" {
				base = "%box"
			}
			if !strings.Contains(ir, "getelementptr inbounds i8, ptr "+base+", i64 16") {
				t.Fatalf("fixed composite %s did not use canonical stride 16:\n%s", operation, ir)
			}
			if strings.Contains(ir, "getelementptr inbounds i8, ptr "+base+", i64 8") {
				t.Fatalf("fixed composite %s used emitted pointer stride 8:\n%s", operation, ir)
			}
		})
	}
}

func TestFixedCompositeArrayStoragePathsUseCanonicalStride(t *testing.T) {
	emitter, fixed, item := fixedCompositeArrayEmitter(t)
	fe := &funcEmitter{emitter: emitter}

	canonical, err := fe.canonicalArrayElemStride(item)
	if err != nil {
		t.Fatalf("canonical stride: %v", err)
	}
	emitted, err := fe.emittedArrayElemStride(item)
	if err != nil {
		t.Fatalf("emitted stride: %v", err)
	}
	if canonical != 16 || emitted != 8 {
		t.Fatalf("stride split = canonical %d, emitted %d; want 16 and 8", canonical, emitted)
	}

	if _, _, err = fe.emitDefaultArrayFixed(fixed, item, 2); err != nil {
		t.Fatalf("emit fixed default: %v", err)
	}
	defaultIR := emitter.buf.String()
	if !strings.Contains(defaultIR, "getelementptr inbounds i8, ptr %t1, i64 16") {
		t.Fatalf("fixed default did not place its second element at canonical stride 16:\n%s", defaultIR)
	}

	emitter.buf.Reset()
	fe.tmpID = 0
	fe.inlineBlock = 0
	if _, _, err = fe.emitArrayFixedElemPtr("%slot", "%idx", "i64", emitter.types.Builtins().Int, item, 2); err != nil {
		t.Fatalf("emit fixed element pointer: %v", err)
	}
	indexIR := emitter.buf.String()
	if !strings.Contains(indexIR, "mul i64") || !strings.Contains(indexIR, ", 16") {
		t.Fatalf("fixed index did not use canonical stride 16:\n%s", indexIR)
	}

}

func TestArrayIterKindWordHandlesDegenerateFixedArrayStride(t *testing.T) {
	const hugeStride = uint64(1) << 63

	for _, length := range []uint64{0, 1} {
		got, err := arrayIterKindWord(hugeStride, length, false)
		if err != nil {
			t.Fatalf("fixed length %d: %v", length, err)
		}
		if got != 0 {
			t.Fatalf("fixed length %d kind word = %d, want 0", length, got)
		}
	}

	got, err := arrayIterKindWord(16, 2, false)
	if err != nil {
		t.Fatalf("normal fixed stride: %v", err)
	}
	if got != 32 {
		t.Fatalf("normal fixed kind word = %d, want 32", got)
	}

	if _, err = arrayIterKindWord(hugeStride, 2, false); err == nil {
		t.Fatal("fixed length 2 accepted a stride larger than MaxInt64")
	}

	got, err = arrayIterKindWord(8, 0, true)
	if err != nil {
		t.Fatalf("dynamic stride: %v", err)
	}
	if got != 16 {
		t.Fatalf("dynamic kind word = %d, want 16", got)
	}
}

// A fixed array keeps its elements inline and carries its length in its type,
// so an element store must bounds-check against that static length. Reading a
// length out of the storage instead — the dynamic Array<T> header layout, whose
// first word is a length and whose third is a data pointer — reads element 0 of
// the inline slots, so a freshly defaulted array rejects every in-range index
// against a length of zero.
func TestFixedArrayElementStoreBoundsChecksStaticLength(t *testing.T) {
	withRepoStdlib(t)

	ir := emitLLVMFromSource(t, `
@entrypoint
fn main() -> int {
    let mut samples: int64[7] = default::<int64[7]>();
    samples[3] = 9:int64;
    print(samples[3] to string);
    return 0;
}
`)

	body := llvmFunctionContaining(t, ir, "@rt_alloc(i64 56, i64 8)")
	bounds := regexp.MustCompile(
		`@rt_panic_bounds\(i64 \d+, i64 [^,]+, i64 ([^)]+)\)`,
	).FindAllStringSubmatch(body, -1)
	if len(bounds) < 2 {
		t.Fatalf("expected a bounds check for both the store and the load, got %d:\n%s", len(bounds), body)
	}
	for _, match := range bounds {
		if match[1] != "7" {
			t.Fatalf("fixed array access bounds-checked against %q, want the static length 7:\n%s", match[1], body)
		}
	}
}
