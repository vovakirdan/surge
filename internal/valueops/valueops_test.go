package valueops

import (
	"testing"

	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/symbols"
	"surge/internal/types"
)

// realStructFacts builds genuine physical facts through internal/layout so the
// tests exercise facts that carry field offsets, not only hand-built scalars.
func realStructFacts(t *testing.T, name string, fieldCount int) layout.PhysicalFacts {
	t.Helper()
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	fields := make([]types.StructField, 0, fieldCount)
	for range fieldCount {
		fields = append(fields, types.StructField{Type: in.Builtins().Uint64})
	}
	id := in.RegisterStruct(in.Strings.Intern(name), source.Span{})
	in.SetStructFields(id, fields)
	physical, err := layout.New(layout.X86_64LinuxGNU(), in).LayoutOf(id)
	if err != nil {
		t.Fatalf("LayoutOf(%s) failed: %v", name, err)
	}
	facts, ok := physical.Physical()
	if !ok {
		t.Fatalf("LayoutOf(%s) produced no physical facts (state %s)", name, physical.State())
	}
	return facts
}

func scalarFacts(size, align uint64) layout.PhysicalFacts {
	return layout.PhysicalFacts{Size: size, Align: align, Stride: size}
}

// testEntry is a fully populated, invariant-satisfying entry: both ABI-true bits
// set, clone_init emitted, every staged verdict and every evidence field
// non-zero, so a test that changes one field is changing something the alias
// predicate and the dump both have to notice.
func testEntry(id types.TypeID) Entry {
	return Entry{
		Type:  id,
		Facts: scalarFacts(16, 8),
		Flags: FlagCopy | FlagClonable,
		Capabilities: Capabilities{
			Traceable:     true,
			CrossClonable: true,
		},
		CloneInit:   symbols.SymbolID(42),
		RecipeIndex: 3,
		Evidence: Evidence{
			CloneMethodKey:   "mod/Pair.__clone",
			CloneReason:      "valid method",
			DroppableReason:  "carries a refcounted scalar",
			TraceableReason:  "contains a vm root",
			ShardReason:      "component refused",
			CrossCloneReason: "crossing duplicate is defined",
			ShardPath:        []types.TypeID{7, 8},
			CrossClonePath:   []types.TypeID{9},
		},
	}
}

// valueInput builds an Input whose roots are exactly the given entries, in the
// given order, all as plain value roots.
func valueInput(entries ...Entry) Input {
	in := Input{
		Values: make(map[types.TypeID]Entry, len(entries)),
		Keys:   map[types.TypeID]KeyOps{},
	}
	for _, entry := range entries {
		in.Roots = append(in.Roots, Root{Type: entry.Type, Role: RoleValue})
		in.Values[entry.Type] = entry
	}
	return in
}

func mustFinalize(t *testing.T, in Input) *Registry {
	t.Helper()
	registry, err := Finalize(in)
	if err != nil {
		t.Fatalf("Finalize failed: %v", err)
	}
	return registry
}

func mustHash(t *testing.T, r *Registry) string {
	t.Helper()
	hash, err := r.Hash()
	if err != nil {
		t.Fatalf("Hash failed: %v", err)
	}
	return hash
}
