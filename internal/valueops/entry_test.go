package valueops

import (
	"strings"
	"testing"

	"surge/internal/symbols"
	"surge/internal/types"
)

// TestSlotInvariantRefusesAClonableEntryWithNoEmittedCloneInit is the invariant's
// headline case: the flag claims clone_init is present, so the symbol has to be.
func TestSlotInvariantRefusesAClonableEntryWithNoEmittedCloneInit(t *testing.T) {
	entry := testEntry(types.TypeID(11))
	entry.CloneInit = symbols.NoSymbolID

	err := entry.checkSlots()
	if err == nil {
		t.Fatal("checkSlots accepted RT_VALUE_FLAG_CLONABLE with no emitted clone_init")
	}
	for _, want := range []string{"type#11", "RT_VALUE_FLAG_CLONABLE", "clone_init"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// TestSlotInvariantAcceptsCopyWithoutAnEmittedSymbol pins the structural
// exemption: copy_init is filled by the runtime's own CopyInitUnboundTrap, so no
// compiler-emitted symbol exists for the invariant to demand. The slot is still
// non-null in the descriptor, and the trap is not what copies — see
// TestTheCopyInitTrapExistsInTheRuntimeAndDoesNotCopy, which is what stops this
// exemption from meaning "nobody fills it" or "the filler copies".
func TestSlotInvariantAcceptsCopyWithoutAnEmittedSymbol(t *testing.T) {
	entry := Entry{Type: types.TypeID(12), Flags: FlagCopy}
	if err := entry.checkSlots(); err != nil {
		t.Fatalf("checkSlots refused a Copy entry with no emitted symbols: %v", err)
	}
}

// TestSlotInvariantRefusesStagedFlagBits proves the staging is structural rather
// than documentary: a verdict cannot reach Flags before its slot can be filled,
// because there is no field to fill and the registry says so instead of shipping
// a descriptor whose callback would be null.
func TestSlotInvariantRefusesStagedFlagBits(t *testing.T) {
	for _, tc := range []struct {
		bit  Flags
		flag string
		slot string
	}{
		// shard_movable left this list with Epic 22's move half: its slot is
		// backend-derived now, so the bit is legal to set and its own test
		// (TestPlanCrossFallsBackToTheModuleStub...) asserts it is ACCEPTED.
		{bit: FlagTraceable, flag: "RT_VALUE_FLAG_TRACEABLE", slot: "trace"},
		{bit: FlagCrossClonable, flag: "RT_VALUE_FLAG_CROSS_CLONABLE", slot: "cross_clone_init"},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			entry := testEntry(types.TypeID(13))
			entry.Flags |= tc.bit

			err := entry.checkSlots()
			if err == nil {
				t.Fatalf("checkSlots accepted %s while its %s slot is staged", tc.flag, tc.slot)
			}
			for _, want := range []string{"type#13", tc.flag, tc.slot, "Capabilities"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}

// TestSlotInvariantAcceptsDroppableWithoutARegistrySymbol is the migration this
// staging existed for. Droppable is an ABI bit now, and the registry accepts it
// while holding no field for its slot: the backend names that body from the type
// id, the way it already names drop glue, so demanding a symbol here would
// demand the wrong thing.
func TestSlotInvariantAcceptsDroppableWithoutARegistrySymbol(t *testing.T) {
	entry := testEntry(types.TypeID(14))
	entry.Flags |= FlagDroppable

	if err := entry.checkSlots(); err != nil {
		t.Fatalf("checkSlots refused a droppable entry the backend fills: %v", err)
	}
	if entry.emittedSlot("drop_in_place") != symbols.NoSymbolID {
		t.Fatal("drop_in_place must not be registry-named: the backend derives it")
	}
}

// TestSlotInvariantAcceptsStagedVerdictsInCapabilities is the other half of the
// staging: the verdicts still staged are recorded, just not as ABI bits.
func TestSlotInvariantAcceptsStagedVerdictsInCapabilities(t *testing.T) {
	entry := testEntry(types.TypeID(14))
	entry.Capabilities = Capabilities{Traceable: true, CrossClonable: true}
	if err := entry.checkSlots(); err != nil {
		t.Fatalf("checkSlots refused staged verdicts held in Capabilities: %v", err)
	}
}

func TestSlotInvariantRefusesUnknownFlagBits(t *testing.T) {
	entry := testEntry(types.TypeID(15))
	entry.Flags |= 1 << 9

	err := entry.checkSlots()
	if err == nil {
		t.Fatal("checkSlots accepted a flag bit the manifest does not define")
	}
	for _, want := range []string{"type#15", "0x200"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// TestSharesWithComparesEveryFieldButType walks the alias predicate field by
// field. Every field except Type must be able to break sharing on its own,
// because an entry that agrees on its verdicts but disagrees on why is a
// different operation plan to anyone reading this registry.
func TestSharesWithComparesEveryFieldButType(t *testing.T) {
	base := testEntry(types.TypeID(20))
	identical := testEntry(types.TypeID(20))
	if !base.sharesWith(&identical) {
		t.Fatal("two identical entries do not share")
	}

	differentID := testEntry(types.TypeID(21))
	if !base.sharesWith(&differentID) {
		t.Fatal("entries differing only in Type do not share; Type is the identity being aliased")
	}

	for _, tc := range []struct {
		name   string
		change func(*Entry)
	}{
		{"facts size", func(e *Entry) { e.Facts = scalarFacts(32, 8) }},
		{"facts field offsets", func(e *Entry) { e.Facts = realStructFacts(t, "Pair", 2) }},
		{"flags", func(e *Entry) { e.Flags = FlagClonable }},
		{"flag droppable", func(e *Entry) { e.Flags |= FlagDroppable }},
		{"capability traceable", func(e *Entry) { e.Capabilities.Traceable = false }},
		{"flag shard movable", func(e *Entry) { e.Flags |= FlagShardMovable }},
		{"capability cross clonable", func(e *Entry) { e.Capabilities.CrossClonable = false }},
		{"clone init symbol", func(e *Entry) { e.CloneInit = symbols.SymbolID(43) }},
		{"recipe index", func(e *Entry) { e.RecipeIndex = 4 }},
		{"evidence clone method key", func(e *Entry) { e.Evidence.CloneMethodKey = "other" }},
		{"evidence clone reason", func(e *Entry) { e.Evidence.CloneReason = "other" }},
		{"evidence droppable reason", func(e *Entry) { e.Evidence.DroppableReason = "other" }},
		{"evidence traceable reason", func(e *Entry) { e.Evidence.TraceableReason = "other" }},
		{"evidence shard reason", func(e *Entry) { e.Evidence.ShardReason = "other" }},
		{"evidence cross clone reason", func(e *Entry) { e.Evidence.CrossCloneReason = "other" }},
		{"evidence shard path element", func(e *Entry) { e.Evidence.ShardPath = []types.TypeID{7, 99} }},
		{"evidence shard path length", func(e *Entry) { e.Evidence.ShardPath = []types.TypeID{7} }},
		{"evidence cross clone path", func(e *Entry) { e.Evidence.CrossClonePath = []types.TypeID{10} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := testEntry(types.TypeID(20))
			tc.change(&changed)
			if base.sharesWith(&changed) {
				t.Fatalf("entries sharing despite differing %s", tc.name)
			}
			if changed.sharesWith(&base) {
				t.Fatalf("sharing is asymmetric for %s", tc.name)
			}
		})
	}
}

// TestSharesWithTreatsEmptyAndNilPathsAlike keeps the predicate agreeing with
// clone, which normalizes an empty path to nil.
func TestSharesWithTreatsEmptyAndNilPathsAlike(t *testing.T) {
	withNil := testEntry(types.TypeID(22))
	withNil.Evidence.ShardPath = nil
	withEmpty := testEntry(types.TypeID(22))
	withEmpty.Evidence.ShardPath = []types.TypeID{}

	if !withNil.sharesWith(&withEmpty) {
		t.Fatal("a nil path and an empty path are different entries")
	}
	cloned := withEmpty.clone()
	if !withNil.sharesWith(&cloned) {
		t.Fatal("clone changed whether an empty path shares")
	}
}
