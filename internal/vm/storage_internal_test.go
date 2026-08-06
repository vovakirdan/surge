package vm

import (
	"math"
	"strings"
	"testing"

	"surge/internal/layout"
	"surge/internal/source"
	"surge/internal/types"
)

// planFor lays out slots whose sizes and alignments are stated directly, so a
// plan can be checked without standing a module up first.
func planFor(t *testing.T, sizes, aligns []uint64) StoragePlan {
	t.Helper()
	if len(sizes) != len(aligns) {
		t.Fatalf("sizes and aligns disagree: %d vs %d", len(sizes), len(aligns))
	}
	plan := StoragePlan{Offsets: make([]uint64, len(sizes)), Align: 1}
	cursor := uint64(0)
	for i := range sizes {
		start, ok := alignUpChecked(cursor, aligns[i])
		if !ok {
			t.Fatalf("slot %d: alignment %d is not a power of two", i, aligns[i])
		}
		plan.Offsets[i] = start
		cursor = start + sizes[i]
		if aligns[i] > plan.Align {
			plan.Align = aligns[i]
		}
	}
	plan.Size = cursor
	return plan
}

func TestStorageArenaExtentStaysInsideItsOwnBytes(t *testing.T) {
	arena := newArena(&StoragePlan{Size: 16, Align: 8}, 1)

	if _, err := arena.extent(8, 8); err != nil {
		t.Fatalf("the last extent of an arena must be readable: %v", err)
	}
	if _, err := arena.extent(9, 8); err == nil {
		t.Fatal("an extent running past the arena must be refused")
	}
	if _, err := arena.extent(math.MaxUint64-2, 8); err == nil {
		t.Fatal("an extent whose end overflows must be refused")
	}
}

func TestStorageRefResolvesToItsOwnDisjointBytes(t *testing.T) {
	plan := planFor(t, []uint64{8, 8}, []uint64{8, 8})
	arena := newArena(&plan, 1)
	first := StorageRef{Arena: arena, Offset: plan.OffsetOf(0), TypeID: types.TypeID(1), Gen: arena.Generation(), Align: 8}
	second := StorageRef{Arena: arena, Offset: plan.OffsetOf(1), TypeID: types.TypeID(1), Gen: arena.Generation(), Align: 8}

	firstBytes, err := first.resolve(8)
	if err != nil {
		t.Fatalf("resolving the first value must succeed: %v", err)
	}
	putBits(firstBytes, 0x0102030405060708)

	secondBytes, err := second.resolve(8)
	if err != nil {
		t.Fatalf("resolving the second value must succeed: %v", err)
	}
	if got := unsignedBits(secondBytes); got != 0 {
		t.Fatalf("writing one value changed its neighbour: %#x", got)
	}
	putBits(secondBytes, 0x1111111111111111)

	reread, err := first.resolve(8)
	if err != nil {
		t.Fatalf("re-resolving must succeed: %v", err)
	}
	if got := unsignedBits(reread); got != 0x0102030405060708 {
		t.Fatalf("the first value reads back as %#x after its neighbour was written", got)
	}
}

func TestStorageRefRefusesAStaleGeneration(t *testing.T) {
	plan := planFor(t, []uint64{8}, []uint64{8})
	arena := newArena(&plan, 7)
	ref := StorageRef{Arena: arena, Offset: 0, TypeID: types.TypeID(11), Gen: arena.Generation(), Align: 8}

	if _, err := ref.resolve(8); err != nil {
		t.Fatalf("a live reference must resolve: %v", err)
	}

	if !arena.retire() {
		t.Fatal("an unpinned arena must retire")
	}
	_, err := ref.resolve(8)
	if err == nil {
		t.Fatal("a reference into a retired arena must be refused")
	}
	for _, want := range []string{"stale", "type#11", "offset 0"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("a stale-storage refusal must name the location; %q lacks %q", err, want)
		}
	}
}

func TestStorageArenaWithALivePinIsNotRetired(t *testing.T) {
	plan := planFor(t, []uint64{8}, []uint64{8})
	arena := newArena(&plan, 1)
	ref := StorageRef{Arena: arena, TypeID: types.TypeID(3), Gen: arena.Generation(), Align: 8}

	arena.pin()
	if arena.retire() {
		t.Fatal("an arena backing a pinned slot must refuse to retire")
	}
	if _, err := ref.resolve(8); err != nil {
		t.Fatalf("a pinned frame's storage must stay readable after the frame leaves the stack: %v", err)
	}

	arena.unpin()
	if !arena.retire() {
		t.Fatal("the last unpin must let the arena retire")
	}
	if _, err := ref.resolve(8); err == nil {
		t.Fatal("storage retired after the last unpin must refuse its old references")
	}
}

func TestStorageRefRefusesAMisalignedProjection(t *testing.T) {
	plan := planFor(t, []uint64{16}, []uint64{8})
	arena := newArena(&plan, 1)
	ref := StorageRef{Arena: arena, Offset: 4, TypeID: types.TypeID(5), Gen: arena.Generation(), Align: 8}

	_, err := ref.resolve(4)
	if err == nil {
		t.Fatal("a reference whose offset does not satisfy its proven alignment must be refused")
	}
	if !strings.Contains(err.Error(), "aligned") {
		t.Fatalf("an alignment refusal must say so: %q", err)
	}
}

func TestStorageOffsetAboveMaxInt32IsNotTruncated(t *testing.T) {
	const offset = uint64(math.MaxInt32) + 8

	ref := StorageRef{Offset: offset, TypeID: types.TypeID(1), Align: 8}
	projected, err := ref.field(16, types.TypeID(2), 8)
	if err != nil {
		t.Fatalf("projecting inside a large value must succeed: %v", err)
	}
	if projected.Offset != offset+16 {
		t.Fatalf("a field offset above the range of an int32 must survive intact: got %d, want %d",
			projected.Offset, offset+16)
	}

	if _, err := (StorageRef{Offset: math.MaxUint64 - 4}).field(16, types.TypeID(2), 1); err == nil {
		t.Fatal("a field projection that overflows must be refused rather than wrapped")
	}
}

func TestStoragePlanPacksEachSlotAtItsOwnAlignment(t *testing.T) {
	plan := planFor(t, []uint64{1, 8, 4, 64}, []uint64{1, 8, 4, 64})

	want := []uint64{0, 8, 16, 64}
	for i, offset := range plan.Offsets {
		if offset != want[i] {
			t.Fatalf("slot %d sits at %d, want %d", i, offset, want[i])
		}
	}
	if plan.Size != 128 {
		t.Fatalf("the plan spans %d bytes, want 128", plan.Size)
	}
	if plan.Align != 64 {
		t.Fatalf("the plan must adopt its strictest member alignment, got %d", plan.Align)
	}
}

func TestStoragePlanGivesNoOffsetToNonComposites(t *testing.T) {
	registry, ids := storageTestRegistry(t)
	composite := map[types.TypeID]bool{ids.pair: true}

	plan := buildStoragePlan(registry, []types.TypeID{ids.i64, ids.pair, ids.i64}, func(id types.TypeID) bool {
		return composite[id]
	})

	if plan.OffsetOf(0) != NoStorageOffset || plan.OffsetOf(2) != NoStorageOffset {
		t.Fatal("a scalar slot must own no arena bytes")
	}
	if plan.OffsetOf(1) == NoStorageOffset {
		t.Fatal("a composite slot must own arena bytes")
	}
	if plan.OffsetOf(7) != NoStorageOffset {
		t.Fatal("a slot index outside the plan must read as owning no storage")
	}
}

func TestStorageCellRoundTripsEveryEncoding(t *testing.T) {
	t.Run("signed integers sign-extend from their own width", func(t *testing.T) {
		buf := make([]byte, 2)
		negative := int64(-3)
		putBits(buf, uint64(negative))
		if got := signedBits(buf); got != -3 {
			t.Fatalf("a 16-bit -3 read back as %d", got)
		}
		if got := unsignedBits(buf); got != 0xFFFD {
			t.Fatalf("the same bytes read unsigned are %#x, want 0xFFFD", got)
		}
	})

	t.Run("a rewritten cell keeps none of the value it replaced", func(t *testing.T) {
		buf := make([]byte, 8)
		putBits(buf, math.MaxUint64)
		putBits(buf, 1)
		if got := unsignedBits(buf); got != 1 {
			t.Fatalf("a rewritten cell reads back as %#x, want 1", got)
		}
	})

	t.Run("unsized integers carry either the value or its object", func(t *testing.T) {
		bits, err := taggedWordBits(MakeInt(-42, types.NoTypeID))
		if err != nil {
			t.Fatalf("a small integer must encode: %v", err)
		}
		value, handle, inline := decodeTaggedWord(bits)
		if !inline || value != -42 || handle != 0 {
			t.Fatalf("a small integer decoded as inline=%v value=%d handle=%d", inline, value, handle)
		}

		bits, err = taggedWordBits(MakeBigInt(Handle(9), types.NoTypeID))
		if err != nil {
			t.Fatalf("an arbitrary-precision integer must encode: %v", err)
		}
		value, handle, inline = decodeTaggedWord(bits)
		if inline || handle != 9 || value != 0 {
			t.Fatalf("a big integer decoded as inline=%v value=%d handle=%d", inline, value, handle)
		}

		if _, _, inline := decodeTaggedWord(0); inline {
			t.Fatal("zeroed storage must not decode as a plausible number")
		}
	})

	t.Run("an integer too large for the tag is refused", func(t *testing.T) {
		if _, err := taggedWordBits(MakeInt(math.MaxInt64, types.NoTypeID)); err == nil {
			t.Fatal("an integer that does not fit the tagged encoding must be refused, not truncated")
		}
	})

	t.Run("handles and functions round-trip", func(t *testing.T) {
		bits, err := handleBits(MakeHandleString(Handle(12), types.NoTypeID))
		if err != nil || Handle(bits) != 12 {
			t.Fatalf("a string handle round-tripped as %d (%v)", bits, err)
		}
		if _, badErr := handleBits(MakeInt(1, types.NoTypeID)); badErr == nil {
			t.Fatal("a non-handle value must not encode as a handle")
		}

		bits, err = funcBits(MakeFunc(31, types.NoTypeID))
		if err != nil || decodeFuncBits(bits) != 31 {
			t.Fatalf("a function value round-tripped as %d (%v)", bits, err)
		}
	})
}

func TestStorageReferenceTableDistinguishesEmptyFromFirst(t *testing.T) {
	arena := newArena(&StoragePlan{Size: 8, Align: 8}, 1)

	if _, ok := arena.refAt(0); ok {
		t.Fatal("a zeroed reference cell must not name a referent")
	}
	encoded, err := arena.addRef(Location{Kind: LKLocal, Local: 4})
	if err != nil {
		t.Fatalf("recording a referent must succeed: %v", err)
	}
	if encoded == 0 {
		t.Fatal("a recorded referent must not encode as the empty cell")
	}
	loc, ok := arena.refAt(encoded)
	if !ok || loc.Local != 4 {
		t.Fatalf("the referent read back as %+v (ok=%v)", loc, ok)
	}
	if _, ok := arena.refAt(encoded + 1); ok {
		t.Fatal("an encoding past the table must not resolve")
	}
}

func TestStorageCellExtentMustHoldItsEncoding(t *testing.T) {
	if err := checkCellExtent(cellTaggedWord, 4); err == nil {
		t.Fatal("an unsized integer does not fit four bytes and must be refused")
	}
	if err := checkCellExtent(cellHandle, 2); err == nil {
		t.Fatal("a handle does not fit two bytes and must be refused")
	}
	if err := checkCellExtent(cellBool, 1); err != nil {
		t.Fatalf("one byte is enough for a bool: %v", err)
	}
	if err := checkCellExtent(cellZeroSized, 0); err != nil {
		t.Fatalf("a zero-sized cell needs no bytes: %v", err)
	}
}

type storageTestTypes struct {
	i64  types.TypeID
	pair types.TypeID
}

// storageTestRegistry builds the smallest real registry a plan can be measured
// against: the plan must agree with layout, not with a table written here.
func storageTestRegistry(t *testing.T) (*layout.Registry, storageTestTypes) {
	t.Helper()
	interner := types.NewInterner()
	interner.Strings = source.NewInterner()

	i64 := interner.Intern(types.Type{Kind: types.KindInt, Width: 64})
	pair := interner.RegisterStruct(interner.Strings.Intern("Pair"), source.Span{})
	interner.SetStructFields(pair, []types.StructField{
		{Name: interner.Strings.Intern("a"), Type: i64},
		{Name: interner.Strings.Intern("b"), Type: i64},
	})

	engine := layout.New(layout.X86_64LinuxGNU(), interner)
	registry, err := layout.FinalizeRegistry(engine, []types.TypeID{i64, pair})
	if err != nil {
		t.Fatalf("freezing the test layouts must succeed: %v", err)
	}
	return registry, storageTestTypes{i64: i64, pair: pair}
}
