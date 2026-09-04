package valueops

import (
	"bytes"
	"strings"
	"testing"

	"surge/internal/symbols"
	"surge/internal/types"
)

// TestFinalizeSharesOneEntryForIndistinguishablePlans is the alias-sharing half
// of requirement 5: two exact semantic types stay two keys, but they may sit
// behind one stored entry when nothing about their plans differs.
func TestFinalizeSharesOneEntryForIndistinguishablePlans(t *testing.T) {
	first := testEntry(types.TypeID(30))
	second := testEntry(types.TypeID(31))

	registry := mustFinalize(t, valueInput(first, second))

	if got := registry.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2 exact roots", got)
	}
	if got := len(registry.repOrder); got != 1 {
		t.Fatalf("stored entries = %d, want 1 shared entry", got)
	}
	for _, id := range []types.TypeID{30, 31} {
		entry, err := registry.Value(id)
		if err != nil {
			t.Fatalf("Value(type#%d) failed: %v", id, err)
		}
		if entry.Type != id {
			t.Errorf("Value(type#%d).Type = %d; sharing must be invisible to callers", id, entry.Type)
		}
	}
}

// TestFinalizeSeparatesPlansThatDifferOnlyInCapabilities is the inequality half:
// same physical layout, same ABI flags, different staged verdict. The alias
// predicate has to keep them apart even though nothing ABI-visible differs.
func TestFinalizeSeparatesPlansThatDifferOnlyInCapabilities(t *testing.T) {
	pinned := testEntry(types.TypeID(32))
	movable := testEntry(types.TypeID(33))
	// The staged verdict that still exists is traceable; shard_movable and
	// cross_clonable are ABI flags now and would be an inequality in Flags,
	// which the other test covers.
	movable.Capabilities.Traceable = !pinned.Capabilities.Traceable

	registry := mustFinalize(t, valueInput(pinned, movable))

	if got := len(registry.repOrder); got != 2 {
		t.Fatalf("stored entries = %d, want 2", got)
	}
	first, err := registry.Value(32)
	if err != nil {
		t.Fatalf("Value(type#32) failed: %v", err)
	}
	second, err := registry.Value(33)
	if err != nil {
		t.Fatalf("Value(type#33) failed: %v", err)
	}
	if first.Capabilities.Traceable == second.Capabilities.Traceable {
		t.Fatal("two entries collapsed into one capability verdict")
	}
}

// TestFinalizeSeparatesPlansThatDifferOnlyInEvidence guards the part of the
// predicate that is easiest to shortcut away.
func TestFinalizeSeparatesPlansThatDifferOnlyInEvidence(t *testing.T) {
	first := testEntry(types.TypeID(34))
	second := testEntry(types.TypeID(35))
	second.Evidence.ShardReason = "a different component refused"

	registry := mustFinalize(t, valueInput(first, second))

	if got := len(registry.repOrder); got != 2 {
		t.Fatalf("stored entries = %d, want 2", got)
	}
}

func TestFinalizeRecordsRootsInFirstSeenOrder(t *testing.T) {
	third := testEntry(types.TypeID(3))
	first := testEntry(types.TypeID(1))
	second := testEntry(types.TypeID(2))
	second.RecipeIndex = 9

	registry := mustFinalize(t, valueInput(third, first, second))

	want := []types.TypeID{3, 1, 2}
	got := registry.TypeIDs()
	if len(got) != len(want) {
		t.Fatalf("TypeIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TypeIDs() = %v, want %v", got, want)
		}
	}
}

func TestFinalizeAcceptsARepeatedRootWithTheSamePlan(t *testing.T) {
	entry := testEntry(types.TypeID(36))
	in := valueInput(entry)
	in.Roots = append(in.Roots, Root{Type: entry.Type, Role: RoleValue})

	registry := mustFinalize(t, in)

	if got := registry.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1", got)
	}
}

// TestFinalizeFailsClosed collects the refusals. Each one is a state that would
// otherwise reach a descriptor writer as a plausible-looking zero.
func TestFinalizeFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func() Input
		want  []string
	}{
		{
			name: "root with no type",
			build: func() Input {
				in := valueInput(testEntry(types.TypeID(40)))
				in.Roots = append(in.Roots, Root{Type: types.NoTypeID, Role: RoleValue})
				return in
			},
			want: []string{"operation root 1", "no type"},
		},
		{
			name: "root with no value entry",
			build: func() Input {
				in := valueInput(testEntry(types.TypeID(41)))
				delete(in.Values, types.TypeID(41))
				return in
			},
			want: []string{"type#41", "no value entry"},
		},
		{
			name: "entry disagreeing with its root",
			build: func() Input {
				in := valueInput(testEntry(types.TypeID(42)))
				mislabelled := testEntry(types.TypeID(999))
				in.Values[types.TypeID(42)] = mislabelled
				return in
			},
			want: []string{"type#42", "type#999"},
		},
		{
			name: "root that is not a value root",
			build: func() Input {
				in := valueInput(testEntry(types.TypeID(43)))
				in.Roots[0].Role = RoleKey
				in.Keys[types.TypeID(43)] = KeyOps{}
				return in
			},
			want: []string{"type#43", "every root is a value root"},
		},
		{
			name: "root with unknown role bits",
			build: func() Input {
				in := valueInput(testEntry(types.TypeID(44)))
				in.Roots[0].Role |= 1 << 4
				return in
			},
			want: []string{"type#44", "unknown role bits", "0x10"},
		},
		{
			name: "negative recipe index",
			build: func() Input {
				entry := testEntry(types.TypeID(45))
				entry.RecipeIndex = -1
				return valueInput(entry)
			},
			want: []string{"type#45", "recipe index"},
		},
		{
			name: "entry violating the slot invariant",
			build: func() Input {
				entry := testEntry(types.TypeID(46))
				entry.CloneInit = symbols.NoSymbolID
				return valueInput(entry)
			},
			want: []string{"type#46", "RT_VALUE_FLAG_CLONABLE", "clone_init"},
		},
		{
			name: "key root with no key operations",
			build: func() Input {
				in := valueInput(testEntry(types.TypeID(48)))
				in.Roots[0].Role |= RoleKey
				return in
			},
			want: []string{"type#48", "no key operations"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry, err := Finalize(tc.build())
			if err == nil {
				t.Fatal("Finalize accepted the input")
			}
			if registry != nil {
				t.Error("Finalize returned a partial registry alongside its error")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

func TestValueFailsClosedForAnUnknownType(t *testing.T) {
	registry := mustFinalize(t, valueInput(testEntry(types.TypeID(50))))

	entry, err := registry.Value(types.TypeID(51))
	if err == nil {
		t.Fatal("Value returned an entry for a type the registry never saw")
	}
	if entry.Type != types.NoTypeID || entry.Flags != 0 || entry.CloneInit != symbols.NoSymbolID {
		t.Error("Value returned a populated entry alongside its error")
	}
	if !strings.Contains(err.Error(), "type#51") {
		t.Errorf("error %q does not name the type", err)
	}
}

func TestKeyFailsClosedForATypeThatIsNotAKey(t *testing.T) {
	registry := mustFinalize(t, valueInput(testEntry(types.TypeID(52))))

	if _, err := registry.Key(types.TypeID(52)); err == nil {
		t.Fatal("Key returned key operations for a plain value root")
	} else if !strings.Contains(err.Error(), "type#52") {
		t.Errorf("error %q does not name the type", err)
	}
	if _, err := registry.Key(types.TypeID(53)); err == nil {
		t.Fatal("Key returned key operations for an unknown type")
	}
}

// TestKeyRootsCarryTheirValuePlan checks the manifest's rt_key_ops shape: a key
// type needs both, so Key returns the whole value plan plus the two callbacks.
func TestKeyRootsCarryTheirValuePlan(t *testing.T) {
	entry := testEntry(types.TypeID(54))
	in := valueInput(entry)
	in.Roots[0].Role |= RoleKey
	in.Keys[entry.Type] = KeyOps{}

	registry := mustFinalize(t, in)

	key, err := registry.Key(entry.Type)
	if err != nil {
		t.Fatalf("Key failed: %v", err)
	}
	if key.Value.Type != entry.Type || key.Value.Flags != entry.Flags {
		t.Errorf("key entry value plan = %+v, want the value root's plan", key.Value)
	}
	if key.Hash != symbols.NoSymbolID || key.Equal != symbols.NoSymbolID {
		t.Errorf("hash/equal = %d/%d, want the staged NoSymbolID pair", key.Hash, key.Equal)
	}
	if got := registry.KeyLen(); got != 1 {
		t.Fatalf("KeyLen() = %d, want 1", got)
	}
	ids := registry.KeyTypeIDs()
	if len(ids) != 1 || ids[0] != entry.Type {
		t.Fatalf("KeyTypeIDs() = %v, want [%d]", ids, entry.Type)
	}
	// A key root is still a value root.
	if _, err := registry.Value(entry.Type); err != nil {
		t.Fatalf("Value(key root) failed: %v", err)
	}
}

// TestHashIsStableAcrossShuffledInputConstruction is the determinism gate. The
// root order is the caller's and is the registry's order; nothing may depend on
// Go's map iteration order, which differs run to run.
func TestHashIsStableAcrossShuffledInputConstruction(t *testing.T) {
	entries := []Entry{}
	for id := types.TypeID(60); id < 70; id++ {
		entry := testEntry(id)
		entry.RecipeIndex = int(id)
		entry.Facts = realStructFacts(t, "Struct", int(id)%4)
		entries = append(entries, entry)
	}

	forward := valueInput(entries...)
	reversed := Input{Values: map[types.TypeID]Entry{}, Keys: map[types.TypeID]KeyOps{}}
	reversed.Roots = append(reversed.Roots, forward.Roots...)
	for i := len(entries) - 1; i >= 0; i-- {
		reversed.Values[entries[i].Type] = entries[i]
	}

	first := mustHash(t, mustFinalize(t, forward))
	second := mustHash(t, mustFinalize(t, reversed))
	if first != second {
		t.Fatalf("hash depends on map construction order: %s vs %s", first, second)
	}
	if again := mustHash(t, mustFinalize(t, forward)); again != first {
		t.Fatalf("hash is unstable across two Finalize calls: %s vs %s", first, again)
	}
}

// TestDumpStatesTheStaging keeps the staging visible to whoever reads a dump or
// chases a hash mismatch, rather than only to whoever reads the source.
func TestDumpStatesTheStaging(t *testing.T) {
	entry := testEntry(types.TypeID(80))
	entry.Facts = realStructFacts(t, "Pair", 2)
	alias := testEntry(types.TypeID(81))
	alias.Facts = entry.Facts
	in := valueInput(entry, alias)
	in.Roots[0].Role |= RoleKey
	in.Keys[entry.Type] = KeyOps{}

	var buf bytes.Buffer
	if err := mustFinalize(t, in).Dump(&buf); err != nil {
		t.Fatalf("Dump failed: %v", err)
	}
	dump := buf.String()

	for _, want := range []string{
		StagingNotice,
		"abi-true bits only",
		"flags=copy|clonable",
		"caps=traceable",
		"clone_init=sym#42",
		"key type#80 hash=none equal=none",
		"alias type#81 -> type#80",
		"evidence clone_method=",
	} {
		if !strings.Contains(dump, want) {
			t.Errorf("dump does not contain %q\n%s", want, dump)
		}
	}
}

func TestNilRegistryFailsClosed(t *testing.T) {
	var registry *Registry
	if _, err := registry.Value(types.TypeID(1)); err == nil {
		t.Error("Value on a nil registry returned an entry")
	}
	if _, err := registry.Key(types.TypeID(1)); err == nil {
		t.Error("Key on a nil registry returned an entry")
	}
	if err := registry.Dump(&bytes.Buffer{}); err == nil {
		t.Error("Dump on a nil registry succeeded")
	}
	if registry.Len() != 0 || registry.KeyLen() != 0 || registry.TypeIDs() != nil || registry.KeyTypeIDs() != nil {
		t.Error("a nil registry reported contents")
	}
	if err := mustFinalize(t, valueInput(testEntry(types.TypeID(90)))).Dump(nil); err == nil {
		t.Error("Dump to a nil writer succeeded")
	}
}
