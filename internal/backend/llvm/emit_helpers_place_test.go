package llvm

import (
	"testing"

	"surge/internal/mir"
	"surge/internal/types"
)

func TestCollectAddrOfTargetsProjectsFieldBorrowThroughReferenceLocal(t *testing.T) {
	sourceCode := `type Entry = {
    key: string,
    value: string,
};

type Doc = {
    entries: Entry[],
};

@entrypoint
fn main() -> int {
    let doc: Doc = Doc {
        entries = [
            Entry { key = "HOST", value = "localhost" },
            Entry { key = "PORT", value = "5432" },
        ],
    };
    let i: int = 1;
    let entry = &doc.entries[i];
    let key = &entry.key;
    print(key.__clone());
    return 0;
}
`

	mirMod, result := lowerMIRFromSource(t, sourceCode)
	mainFn := findMIRFunc(t, mirMod, "main")
	docLocal := findMIRLocal(t, mainFn, "doc")
	entryLocal := findMIRLocal(t, mainFn, "entry")
	keyLocal := findMIRLocal(t, mainFn, "key")
	fe := &funcEmitter{
		emitter: &Emitter{mod: mirMod, types: result.Sema.TypeInterner},
		f:       mainFn,
	}

	targets := fe.collectAddrOfTargets()
	entryTarget, ok := targets[entryLocal]
	if !ok {
		t.Fatalf("missing addr_of target for entry local")
	}
	keyTarget, ok := targets[keyLocal]
	if !ok {
		t.Fatalf("missing addr_of target for key local")
	}
	if entryTarget.ty == mainFn.Locals[entryLocal].Type {
		t.Fatalf("entry target kept reference-cell type %s", types.Label(result.Sema.TypeInterner, entryTarget.ty))
	}
	if keyTarget.ty != result.Sema.TypeInterner.Builtins().String {
		t.Fatalf("key target type = %s, want string", types.Label(result.Sema.TypeInterner, keyTarget.ty))
	}

	projected, err := fe.projectType(
		placeProjectionType{ty: mainFn.Locals[entryLocal].Type, storageLocal: entryLocal},
		mir.PlaceProj{Kind: mir.PlaceProjDeref},
		targets,
	)
	if err != nil {
		t.Fatalf("project entry deref: %v", err)
	}
	if projected.ty != entryTarget.ty {
		t.Fatalf(
			"projected entry deref type = %s, want addr_of target %s",
			types.Label(result.Sema.TypeInterner, projected.ty),
			types.Label(result.Sema.TypeInterner, entryTarget.ty),
		)
	}

	entries, err := fe.projectType(
		placeProjectionType{ty: mainFn.Locals[docLocal].Type, storageLocal: docLocal},
		mir.PlaceProj{Kind: mir.PlaceProjField, FieldName: "entries", FieldIdx: -1},
		targets,
	)
	if err != nil {
		t.Fatalf("project entries field: %v", err)
	}
	if entries.storageLocal != mir.NoLocalID {
		t.Fatalf("field projection storage local = %d, want NoLocalID", entries.storageLocal)
	}
	entryElem, err := fe.projectType(
		entries,
		mir.PlaceProj{Kind: mir.PlaceProjIndex},
		targets,
	)
	if err != nil {
		t.Fatalf("project entries index: %v", err)
	}
	if entryElem.storageLocal != mir.NoLocalID {
		t.Fatalf("index projection storage local = %d, want NoLocalID", entryElem.storageLocal)
	}
}

func findMIRFunc(t *testing.T, mod *mir.Module, name string) *mir.Func {
	t.Helper()
	for _, fn := range mod.Funcs {
		if fn != nil && fn.Name == name {
			return fn
		}
	}
	t.Fatalf("missing MIR function %q", name)
	return nil
}

func findMIRLocal(t *testing.T, fn *mir.Func, name string) mir.LocalID {
	t.Helper()
	for i, local := range fn.Locals {
		if local.Name == name {
			return mir.LocalID(i)
		}
	}
	t.Fatalf("missing MIR local %q", name)
	return mir.NoLocalID
}
