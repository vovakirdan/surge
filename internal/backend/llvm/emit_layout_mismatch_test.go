package llvm

import (
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/mir"
	"surge/internal/source"
	"surge/internal/types"
)

func mismatchedAggregateEmitter(t *testing.T, kind string) (*Emitter, types.TypeID) {
	t.Helper()
	typesIn := types.NewInterner()
	typesIn.Strings = source.NewInterner()
	var id types.TypeID
	switch kind {
	case "struct":
		id = typesIn.RegisterStruct(typesIn.Strings.Intern("Pair"), source.Span{})
		typesIn.SetStructFields(id, []types.StructField{{Type: typesIn.Builtins().String}})
	case "tuple":
		id = typesIn.RegisterTuple([]types.TypeID{typesIn.Builtins().String})
	default:
		t.Fatalf("unknown aggregate kind %q", kind)
	}
	registry, err := layout.FinalizeRegistry(layout.New(layout.X86_64LinuxGNU(), typesIn), []types.TypeID{id})
	if err != nil {
		t.Fatalf("FinalizeRegistry: %v", err)
	}
	// Simulate stale/mismatched type metadata after the immutable registry was
	// built. Lifecycle glue must reject it rather than omit the second member.
	switch kind {
	case "struct":
		typesIn.SetStructFields(id, []types.StructField{
			{Type: typesIn.Builtins().String},
			{Type: typesIn.Builtins().String},
		})
	case "tuple":
		info, ok := typesIn.TupleInfo(id)
		if !ok || info == nil {
			t.Fatal("missing tuple metadata")
		}
		info.Elems = append(info.Elems, typesIn.Builtins().String)
	}
	return &Emitter{
		mod:   &mir.Module{Meta: &mir.ModuleMeta{Layouts: registry}},
		types: typesIn,
	}, id
}

func TestAggregateCloneAndDropGlueFailClosedOnLayoutMetadataMismatch(t *testing.T) {
	for _, kind := range []string{"struct", "tuple"} {
		for _, operation := range []string{"clone", "drop"} {
			t.Run(kind+"_"+operation, func(t *testing.T) {
				emitter, id := mismatchedAggregateEmitter(t, kind)
				var err error
				if operation == "clone" {
					err = emitter.emitCloneGlueBody(id)
				} else {
					err = emitter.emitDropGlueBody(id)
				}
				if err == nil || !strings.Contains(err.Error(), "finalized "+kind+" layout") || !strings.Contains(err.Error(), "field offsets") {
					t.Fatalf("%s %s mismatch error = %v", kind, operation, err)
				}
			})
		}
	}
}
