package llvm

import (
	"fmt"
	"testing"

	"surge/internal/types"
)

// The storage spelling and every alignment it carries come from the frozen
// layout registry and from nowhere else.
//
// The point of spelling inline storage as a byte run is that LLVM is given no
// opportunity to compute a layout of its own — so the test that matters is not
// "does the spelling look right" but "is every number in it the number
// `internal/layout` published". A second calculator that agreed today and
// drifted tomorrow is exactly the failure this design exists to prevent, and it
// would be invisible in the emitted code.
func TestStorageSpellingComesFromTheLayoutRegistry(t *testing.T) {
	const src = `
@copy type Leaf = { x: int, y: int }
@packed type Packed = { flag: bool, wide: int64, middle: bool, second: int64 }
@align(64) type OverAligned = { a: int, b: int }
type Empty = { }
tag Wrapped(Leaf);
tag Bare();
type Choice = Wrapped(Leaf) | Bare;
type Nested = {
	head: Leaf,
	pair: (int, int),
	choice: Choice,
	cells: Leaf[3],
	packed: Packed,
	wide: OverAligned,
	blank: Empty,
}

fn build() -> int {
	let n: Nested = Nested{
		head: Leaf{ x: 1, y: 2 },
		pair: (3, 4),
		choice: Wrapped(Leaf{ x: 5, y: 6 }),
		cells: [Leaf{x=1,y=1}, Leaf{x=2,y=2}, Leaf{x=3,y=3}],
		packed: Packed{ flag = true, wide = 7:int64, middle = false, second = 8:int64 },
		wide: OverAligned{ a: 9, b: 10 },
		blank: Empty { },
	};
	return n.head.x;
}

@entrypoint
fn main() -> int { return build(); }
`
	e := prepareEmitterForTest(t, src)

	checked := 0
	for _, name := range []string{"Leaf", "Packed", "OverAligned", "Empty", "Choice", "Nested"} {
		id := findNamedType(t, e, name)
		facts, err := e.storageFactsOf(id)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		published, err := e.layoutOf(resolveAliasAndOwn(e.types, id))
		if err != nil {
			t.Fatalf("%s: layout: %v", name, err)
		}
		if facts.Size != published.Size || facts.Align != published.Align || facts.Stride != published.Stride {
			t.Errorf("%s: storage facts %+v differ from the published layout (size=%d align=%d stride=%d)",
				name, facts, published.Size, published.Align, published.Stride)
		}
		spelling, err := e.storageTypeOf(id)
		if err != nil {
			t.Fatalf("%s: spelling: %v", name, err)
		}
		if want := fmt.Sprintf("[%d x i8]", published.Size); spelling != want {
			t.Errorf("%s: spelled %s, want %s", name, spelling, want)
		}
		checked++
	}
	if checked != 6 {
		t.Fatalf("checked %d types, want 6", checked)
	}
}

// A zero-sized value keeps its size. Rounding it up to one byte would give two
// adjacent zero-sized members different addresses and put a byte of nothing
// inside every composite holding one.
func TestZeroSizedStorageStaysEmpty(t *testing.T) {
	if got := storageTypeForSize(0); got != "[0 x i8]" {
		t.Errorf("a zero-sized value is spelled %s, want [0 x i8]", got)
	}
}

// Asking for the byte-run spelling of something that is not carried inline is a
// bug at the call site, and is refused rather than answered with a plausible
// size — a scalar handed a byte run would be storage the rest of the program
// does not agree about.
func TestStorageSpellingRefusesValuesCarriedInRegisters(t *testing.T) {
	const src = `
type Holder = { note: string, count: int }

@entrypoint
fn main() -> int {
	let h: Holder = Holder{ note: "n", count: 1 };
	return h.count;
}
`
	e := prepareEmitterForTest(t, src)
	holder := findNamedType(t, e, "Holder")
	info, ok := e.types.StructInfo(resolveAliasAndOwn(e.types, holder))
	if !ok || info == nil || len(info.Fields) != 2 {
		t.Fatalf("fixture no longer has the expected shape")
	}
	for _, field := range info.Fields {
		if _, err := e.storageFactsOf(field.Type); err == nil {
			t.Errorf("%s answered with inline storage facts instead of refusing",
				types.Label(e.types, field.Type))
		}
		if e.hasInlineStorage(field.Type) {
			t.Errorf("%s reported as living in inline storage", types.Label(e.types, field.Type))
		}
	}
	if !e.hasInlineStorage(holder) {
		t.Errorf("the struct itself does not report inline storage")
	}
}

// A member is only as aligned as its address actually is. A packed field sits
// at an offset its own type does not divide, and an access claiming the type's
// natural alignment would promise the hardware something false.
func TestMemberAlignmentFollowsTheOffsetNotTheType(t *testing.T) {
	for _, tc := range []struct {
		name      string
		baseAlign uint64
		offset    uint64
		want      uint64
	}{
		{"the start of storage is as aligned as the storage", 8, 0, 8},
		{"an odd offset is aligned to one byte", 8, 1, 1},
		{"an offset of two carries two", 8, 2, 2},
		{"an offset of four carries four", 8, 4, 4},
		{"a well-aligned offset is capped by the base", 8, 16, 8},
		{"an over-aligned base still caps at its own alignment", 64, 128, 64},
		{"an offset of three carries one", 8, 3, 1},
		{"a base with no alignment carries one", 0, 0, 1},
	} {
		if got := memberAccessAlign(tc.baseAlign, tc.offset); got != tc.want {
			t.Errorf("%s: base align %d at offset %d claimed %d, want %d",
				tc.name, tc.baseAlign, tc.offset, got, tc.want)
		}
	}
}

// A packed field really does land at an offset its own type does not divide —
// otherwise the rule above would be checked against a shape that never occurs.
func TestPackedFieldsProduceUnderAlignedOffsets(t *testing.T) {
	const src = `
@packed type Packed = { flag: bool, wide: int64, middle: bool, second: int64 }

@entrypoint
fn main() -> int {
	let p: Packed = Packed{ flag = true, wide = 7:int64, middle = false, second = 8:int64 };
	if p.wide != 7:int64 { return 1; }
	return 0;
}
`
	e := prepareEmitterForTest(t, src)
	packed := findNamedType(t, e, "Packed")
	facts, err := e.layoutOf(resolveAliasAndOwn(e.types, packed))
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	offsets := facts.FieldOffsets()
	if len(offsets) != 4 {
		t.Fatalf("packed struct has %d field offsets, want 4", len(offsets))
	}
	underAligned := 0
	for i, offset := range offsets {
		if memberAccessAlign(facts.Align, offset) < 8 {
			underAligned++
		} else if i == 1 {
			t.Errorf("the packed 64-bit field at offset %d is fully aligned; the fixture stopped being packed", offset)
		}
	}
	if underAligned == 0 {
		t.Fatalf("no packed field landed at an under-aligned offset: %v", offsets)
	}
}

// findNamedType resolves a type by the name the fixture declares it with.
func findNamedType(t *testing.T, e *Emitter, name string) types.TypeID {
	t.Helper()
	for id := types.TypeID(1); ; id++ {
		if _, ok := e.types.Lookup(id); !ok {
			break
		}
		if types.Label(e.types, id) == name {
			return id
		}
	}
	t.Fatalf("no type named %q in the fixture", name)
	return types.NoTypeID
}
