package types //nolint:revive

import (
	"testing"

	"surge/internal/source"
)

// IsValueComposite is the predicate the whole copy/move/drop boundary keys off,
// so its answers are pinned here rather than inferred from the behavior of the
// passes that call it. The two halves it must never confuse: a FIXED array is
// stored inline, a DYNAMIC one is a handle; and the handle-backed builtins are
// nominal STRUCTS, so a bare KindStruct test would sweep them in.
func TestIsValueComposite(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	b := in.Builtins()

	tuple := in.RegisterTuple([]TypeID{b.Int32, b.Bool})
	plainStruct := in.RegisterStruct(in.Strings.Intern("Point"), source.Span{})
	union := in.RegisterUnion(in.Strings.Intern("Outcome"), source.Span{})
	fixedArr := in.Intern(MakeArray(b.Int32, 4))
	dynArr := in.Intern(MakeArray(b.Int32, ArrayDynamicLength))

	cases := []struct {
		name string
		id   TypeID
		want bool
	}{
		{"struct", plainStruct, true},
		{"tuple", tuple, true},
		{"union", union, true},
		{"fixed array", fixedArr, true},

		{"dynamic array", dynArr, false},
		{"string", in.Intern(Type{Kind: KindString}), false},
		{"int", b.Int32, false},
		{"bool", b.Bool, false},
		{"reference to a struct", in.Intern(MakeReference(plainStruct, false)), false},
		{"mutable reference to a struct", in.Intern(MakeReference(plainStruct, true)), false},
		{"pointer to a struct", in.Intern(MakePointer(plainStruct)), false},
		{"invalid", NoTypeID, false},
	}

	for _, tc := range cases {
		if got := in.IsValueComposite(tc.id); got != tc.want {
			t.Errorf("%s: IsValueComposite = %v, want %v", tc.name, got, tc.want)
		}
	}

	if (*Interner)(nil).IsValueComposite(plainStruct) {
		t.Errorf("nil interner: IsValueComposite = true, want false")
	}
}

// The handle-backed builtins are structs by construction, which is exactly why
// they need naming out: `Range`, `Task` and `Channel` carry a handle to
// runtime-owned storage and are duplicated on their own terms.
func TestIsValueCompositeExcludesHandleBackedNominals(t *testing.T) {
	for _, name := range []string{"Range", "Task", "Channel"} {
		in := NewInterner()
		in.Strings = source.NewInterner()
		id := in.RegisterStruct(in.Strings.Intern(name), source.Span{})
		if in.IsValueComposite(id) {
			t.Errorf("%s: IsValueComposite = true, want false (handle-backed)", name)
		}
	}

	// A user type that merely SHARES a prefix with one of those names is a
	// value composite: the exclusion is by exact name, not by resemblance.
	in := NewInterner()
	in.Strings = source.NewInterner()
	id := in.RegisterStruct(in.Strings.Intern("RangeStats"), source.Span{})
	if !in.IsValueComposite(id) {
		t.Errorf("RangeStats: IsValueComposite = false, want true")
	}
}

// The dynamic `Array<T>` is the exclusion that costs the most if it slips: it
// is a nominal STRUCT registered on the interner, so it reaches the same
// KindStruct branch a real value composite does and is separated only by the
// identity check. `MakeArray` types exercise a different branch, so this needs
// its own probe.
func TestIsValueCompositeExcludesNominalDynamicArray(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	b := in.Builtins()

	base, _ := in.EnsureArrayNominal(in.Strings.Intern("Array"), in.Strings.Intern("T"), source.Span{}, 0)
	if base == NoTypeID {
		t.Fatalf("failed to register the nominal Array")
	}
	inst := in.RegisterStructInstance(in.Strings.Intern("Array"), source.Span{}, []TypeID{b.Int32})
	if _, ok := in.ArrayInfo(inst); !ok {
		t.Fatalf("registered instance is not recognised as Array<T>; the probe would pass vacuously")
	}
	if in.IsValueComposite(inst) {
		t.Errorf("Array<int32>: IsValueComposite = true, want false (handle-backed)")
	}
}

// An alias must answer for what it names, or the same type would get two
// different storage answers depending on how it was spelled.
func TestIsValueCompositeResolvesAliases(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()

	target := in.RegisterStruct(in.Strings.Intern("Point"), source.Span{})
	alias := in.RegisterAlias(in.Strings.Intern("Coord"), source.Span{})
	in.SetAliasTarget(alias, target)

	if !in.IsValueComposite(alias) {
		t.Errorf("alias of a struct: IsValueComposite = false, want true")
	}
}
