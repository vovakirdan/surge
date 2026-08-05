package llvm

import (
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

func fixedCompositeArrayEmitter(t *testing.T) (*Emitter, types.TypeID) {
	t.Helper()
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()

	item := typesIn.RegisterStruct(typesIn.Strings.Intern("Item"), source.Span{})
	typesIn.SetStructFields(item, []types.StructField{
		{Type: typesIn.Builtins().String},
		{Type: typesIn.Builtins().Int},
	})
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
	return emitter, fixed
}

func TestFixedCompositeArrayCloneAndDropUseCanonicalStride(t *testing.T) {
	for _, operation := range []string{"clone", "drop"} {
		t.Run(operation, func(t *testing.T) {
			emitter, fixed := fixedCompositeArrayEmitter(t)
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
