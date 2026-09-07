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

// Runtime handles are identified by their core declaration, never by spelling.
// Marking a family covers instances created both before and after the marker.
func TestIsValueCompositeUsesExplicitRuntimeHandleIdentity(t *testing.T) {
	for _, name := range []string{"Range", "Task", "Channel"} {
		in := NewInterner()
		in.Strings = source.NewInterner()
		nameID := in.Strings.Intern(name)
		builtinDecl := source.Span{File: 1, Start: 10, End: 20}
		userDecl := source.Span{File: 2, Start: 10, End: 20}
		payloadA := in.RegisterStruct(in.Strings.Intern("PayloadA"), source.Span{})
		payloadB := in.RegisterStruct(in.Strings.Intern("PayloadB"), source.Span{})

		base := in.RegisterStruct(nameID, builtinDecl)
		beforeMark := in.RegisterStructInstance(nameID, builtinDecl, []TypeID{payloadA})
		in.MarkRuntimeHandleType(base)
		afterMark := in.RegisterStructInstance(nameID, builtinDecl, []TypeID{payloadB})
		for _, id := range []TypeID{base, beforeMark, afterMark} {
			if in.IsValueComposite(id) {
				t.Errorf("marked %s family member: IsValueComposite = true, want false", name)
			}
		}

		userType := in.RegisterStruct(nameID, userDecl)
		in.SetStructFields(userType, []StructField{{Type: in.Builtins().Int32}})
		if !in.IsValueComposite(userType) {
			t.Errorf("user-defined %s: IsValueComposite = false, want true", name)
		}
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

// DynamicArrayElem is the one question the relinquishing walk, sema's
// crossing predicate and the handle roster all ask of a dynamic array: which
// element type does its buffer hold. Both spellings of the array answer --
// the structural `[T]` and the nominal `Array<T>` -- through `own` and through
// an alias, and every OTHER handle answers false: a fixed array lives inline,
// and a map, a string or a channel is a handle whose storage no per-element
// walk reaches.
func TestDynamicArrayElemNamesTheBufferElement(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	b := in.Builtins()

	structural := in.Intern(MakeArray(b.Int32, ArrayDynamicLength))
	if base, _ := in.EnsureArrayNominal(in.Strings.Intern("Array"), in.Strings.Intern("T"), source.Span{}, 0); base == NoTypeID {
		t.Fatalf("failed to register the nominal Array")
	}
	nominal := in.RegisterStructInstance(in.Strings.Intern("Array"), source.Span{}, []TypeID{b.Int32})
	owned := in.Intern(MakeOwn(structural))
	alias := in.RegisterAlias(in.Strings.Intern("Ints"), source.Span{})
	in.SetAliasTarget(alias, nominal)
	fixed := in.Intern(MakeArray(b.Int32, 4))
	if base, _ := in.EnsureMapNominal(in.Strings.Intern("Map"), in.Strings.Intern("K"), in.Strings.Intern("V"), source.Span{}, 0); base == NoTypeID {
		t.Fatalf("failed to register the nominal Map")
	}
	mapped := in.RegisterStructInstance(in.Strings.Intern("Map"), source.Span{}, []TypeID{b.Int32, b.Int32})
	if _, _, ok := in.MapInfo(mapped); !ok {
		t.Fatalf("registered instance is not recognised as Map<K, V>; its row would pin nothing")
	}
	payload := in.RegisterStruct(in.Strings.Intern("Payload"), source.Span{})
	channelDecl := source.Span{File: 1, Start: 10, End: 20}
	in.MarkRuntimeHandleType(in.RegisterStruct(in.Strings.Intern("Channel"), channelDecl))
	channel := in.RegisterStructInstance(in.Strings.Intern("Channel"), channelDecl, []TypeID{payload})

	cases := []struct {
		name     string
		id       TypeID
		wantElem TypeID
		want     bool
	}{
		{"structural [int32]", structural, b.Int32, true},
		{"nominal Array<int32>", nominal, b.Int32, true},
		{"own [int32]", owned, b.Int32, true},
		{"alias of Array<int32>", alias, b.Int32, true},
		{"fixed [int32; 4]", fixed, NoTypeID, false},
		{"Map<int32, int32>", mapped, NoTypeID, false},
		{"string", in.Intern(Type{Kind: KindString}), NoTypeID, false},
		{"Channel<Payload>", channel, NoTypeID, false},
		{"invalid", NoTypeID, NoTypeID, false},
	}
	for _, tc := range cases {
		elem, ok := in.DynamicArrayElem(tc.id)
		if ok != tc.want || elem != tc.wantElem {
			t.Errorf("%s: DynamicArrayElem = (%d, %v), want (%d, %v)", tc.name, elem, ok, tc.wantElem, tc.want)
		}
	}
	if elem, ok := (*Interner)(nil).DynamicArrayElem(structural); ok || elem != NoTypeID {
		t.Errorf("nil interner: DynamicArrayElem = (%d, %v), want (0, false)", elem, ok)
	}
	// The handle roster still answers for both spellings through the same
	// helper, so the two cannot drift on which element a buffer holds.
	for _, id := range []TypeID{structural, nominal} {
		payloads, ok := in.RuntimeHandlePayloads(id)
		if !ok || len(payloads) != 1 || payloads[0] != b.Int32 {
			t.Errorf("type %d: RuntimeHandlePayloads = %v, %v; want [%d], true", id, payloads, ok, b.Int32)
		}
	}
}

func TestRuntimeHandlePayloadsAreAuthoritativeAndOwned(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	payload := in.RegisterStruct(in.Strings.Intern("Payload"), source.Span{})
	for _, name := range []string{"Range", "Task", "Channel"} {
		nameID := in.Strings.Intern(name)
		decl := source.Span{File: 1, Start: 10, End: 20}
		base := in.RegisterStruct(nameID, decl)
		in.MarkRuntimeHandleType(base)
		id := in.RegisterStructInstance(nameID, decl, []TypeID{payload})
		got, ok := in.RuntimeHandlePayloads(id)
		if !ok || len(got) != 1 || got[0] != payload {
			t.Fatalf("%s payloads = %v, %t; want [%d], true", name, got, ok, payload)
		}
		got[0] = NoTypeID
		again, _ := in.RuntimeHandlePayloads(id)
		if again[0] != payload {
			t.Fatalf("%s payload metadata escaped by mutable slice", name)
		}
	}

	plain := in.RegisterStruct(in.Strings.Intern("Plain"), source.Span{})
	if payloads, ok := in.RuntimeHandlePayloads(plain); ok || payloads != nil {
		t.Fatalf("plain struct reported as runtime handle: %v, %t", payloads, ok)
	}
	ref := in.Intern(MakeReference(payload, false))
	if payloads, ok := in.RuntimeHandlePayloads(ref); ok || payloads != nil {
		t.Fatalf("reference reported as runtime owner: %v, %t", payloads, ok)
	}
}
