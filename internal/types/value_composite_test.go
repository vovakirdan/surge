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

// ContainsDynamicArray is the structural half of the question a value answers
// before it is given up across a thread boundary, and the rows below are the
// shapes a relinquishing walk actually meets. Two of them are the whole point
// of the predicate: a struct that merely CARRIES an array answers true, and a
// map that carries one answers FALSE -- no per-element walk steps a map's
// table, so arming the runtime's view check there would ask it to walk storage
// it cannot address.
//
// Every row carries the companion answer too, because the pair is what makes a
// shape safe. `walk` says the runtime is handed this array's slot; `behind`
// says there is an array here it will never be handed, which the crossing gate
// turns into a refusal. A row with neither carries no array at all. A row with
// BOTH -- the struct holding a plain array and a map of arrays -- is the shape
// that proves these are not each other's negation: they answer about different
// arrays inside one value. What must never exist is a value carrying an array
// with false in both columns; that is the silent admission the map and the
// channel used to be, and the last loop below is what forbids it.
func TestContainsDynamicArray(t *testing.T) {
	in := NewInterner()
	in.Strings = source.NewInterner()
	b := in.Builtins()
	str := in.Intern(Type{Kind: KindString})

	dyn := in.Intern(MakeArray(b.Int32, ArrayDynamicLength))
	nested := in.Intern(MakeArray(dyn, ArrayDynamicLength))
	fixedOfPlain := in.Intern(MakeArray(b.Int32, 4))
	fixedOfArrays := in.Intern(MakeArray(dyn, 4))

	holder := in.RegisterStruct(in.Strings.Intern("Holder"), source.Span{})
	in.SetStructFields(holder, []StructField{{Type: b.Int32}, {Type: dyn}})
	plainStruct := in.RegisterStruct(in.Strings.Intern("Plain"), source.Span{})
	in.SetStructFields(plainStruct, []StructField{{Type: b.Int32}, {Type: b.Bool}})
	deep := in.RegisterStruct(in.Strings.Intern("Deep"), source.Span{})
	in.SetStructFields(deep, []StructField{{Type: holder}})

	tupleWithArray := in.RegisterTuple([]TypeID{dyn, b.Int32})
	tuplePlain := in.RegisterTuple([]TypeID{b.Int32, b.Bool})

	unionWithArray := in.RegisterUnion(in.Strings.Intern("Maybe"), source.Span{})
	in.SetUnionMembers(unionWithArray, []UnionMember{
		{Kind: UnionMemberTag, TagName: in.Strings.Intern("Some"), TagArgs: []TypeID{dyn}},
		{Kind: UnionMemberNothing},
	})
	unionPlain := in.RegisterUnion(in.Strings.Intern("Flag"), source.Span{})
	in.SetUnionMembers(unionPlain, []UnionMember{
		{Kind: UnionMemberType, Type: b.Int32},
		{Kind: UnionMemberNothing},
	})

	if base, _ := in.EnsureMapNominal(in.Strings.Intern("Map"), in.Strings.Intern("K"), in.Strings.Intern("V"), source.Span{}, 0); base == NoTypeID {
		t.Fatalf("failed to register the nominal Map")
	}
	mapOfArrays := in.RegisterStructInstance(in.Strings.Intern("Map"), source.Span{}, []TypeID{str, dyn})
	if _, _, ok := in.MapInfo(mapOfArrays); !ok {
		t.Fatalf("registered instance is not recognised as Map<K, V>; its row would pin nothing")
	}
	mapOfPlain := in.RegisterStructInstance(in.Strings.Intern("Map"), source.Span{}, []TypeID{str, b.Int32})
	channelDecl := source.Span{File: 1, Start: 10, End: 20}
	in.MarkRuntimeHandleType(in.RegisterStruct(in.Strings.Intern("Channel"), channelDecl))
	channelOfArrays := in.RegisterStructInstance(in.Strings.Intern("Channel"), channelDecl, []TypeID{dyn})
	channelOfPlain := in.RegisterStructInstance(in.Strings.Intern("Channel"), channelDecl, []TypeID{b.Int32})

	arrayOfMaps := in.Intern(MakeArray(mapOfArrays, ArrayDynamicLength))
	bothKinds := in.RegisterStruct(in.Strings.Intern("Both"), source.Span{})
	in.SetStructFields(bothKinds, []StructField{{Type: dyn}, {Type: mapOfArrays}})
	unionWithMap := in.RegisterUnion(in.Strings.Intern("Boxed"), source.Span{})
	in.SetUnionMembers(unionWithMap, []UnionMember{
		{Kind: UnionMemberTag, TagName: in.Strings.Intern("In"), TagArgs: []TypeID{mapOfArrays}},
		{Kind: UnionMemberNothing},
	})

	alias := in.RegisterAlias(in.Strings.Intern("Ints"), source.Span{})
	in.SetAliasTarget(alias, dyn)

	cases := []struct {
		name         string
		id           TypeID
		walk, behind bool
	}{
		{"dynamic array", dyn, true, false},
		{"array of arrays", nested, true, false},
		{"own array", in.Intern(MakeOwn(dyn)), true, false},
		{"alias of an array", alias, true, false},
		{"struct carrying an array", holder, true, false},
		{"struct carrying a struct carrying an array", deep, true, false},
		{"tuple carrying an array", tupleWithArray, true, false},
		{"union arm carrying an array", unionWithArray, true, false},
		{"fixed array of arrays", fixedOfArrays, true, false},

		{"int", b.Int32, false, false},
		{"bool", b.Bool, false, false},
		{"string", str, false, false},
		{"plain struct", plainStruct, false, false},
		{"plain tuple", tuplePlain, false, false},
		{"plain union", unionPlain, false, false},
		{"fixed array of plain words", fixedOfPlain, false, false},
		{"reference to an array", in.Intern(MakeReference(dyn, false)), false, false},
		{"pointer to an array", in.Intern(MakePointer(dyn)), false, false},
		{"invalid", NoTypeID, false, false},

		// The two shapes the walk stops at. It still stops -- their storage is
		// reachable by no per-element callback -- and the companion says so out
		// loud, which is what turns the stop into a refusal at the crossing
		// gate instead of a silent admission.
		{"Map<string, int32[]>", mapOfArrays, false, true},
		{"Channel<int32[]>", channelOfArrays, false, true},
		// A handle whose payload carries no array is nothing to refuse.
		{"Map<string, int32>", mapOfPlain, false, false},
		{"Channel<int32>", channelOfPlain, false, false},
		// The stop travels outward: through an array's element, through a
		// struct field, through a union arm.
		{"array of maps of arrays", arrayOfMaps, true, true},
		{"struct holding both an array and a map of arrays", bothKinds, true, true},
		{"union arm carrying a map of arrays", unionWithMap, false, true},
	}
	for _, tc := range cases {
		if got := in.ContainsDynamicArray(tc.id); got != tc.walk {
			t.Errorf("%s: ContainsDynamicArray = %v, want %v", tc.name, got, tc.walk)
		}
		if got := in.ContainsDynamicArrayBehindHandle(tc.id); got != tc.behind {
			t.Errorf("%s: ContainsDynamicArrayBehindHandle = %v, want %v", tc.name, got, tc.behind)
		}
	}
	// No shape in the table carries an array that neither predicate names. A
	// row that did would be a value crossing with an array nothing ever looks
	// at, which is exactly the defect both predicates exist to end.
	for _, tc := range cases {
		if !tc.walk && !tc.behind && carriesAnArrayAnywhere(in, tc.id) {
			t.Errorf("%s: carries a dynamic array and is neither walked nor refused", tc.name)
		}
	}
	if (*Interner)(nil).ContainsDynamicArray(dyn) {
		t.Errorf("nil interner: ContainsDynamicArray = true, want false")
	}
	if (*Interner)(nil).ContainsDynamicArrayBehindHandle(dyn) {
		t.Errorf("nil interner: ContainsDynamicArrayBehindHandle = true, want false")
	}
}

// carriesAnArrayAnywhere is the test's own reading of the type graph, written
// without the predicates it audits: it descends every edge -- inline members,
// handle payloads, aliases and `own` -- and says whether a dynamic array is
// reachable at all. A predicate bug that answered false twice would still be
// caught, because this walk shares no code with either answer.
func carriesAnArrayAnywhere(in *Interner, id TypeID) bool {
	seen := map[TypeID]bool{}
	var walk func(TypeID) bool
	walk = func(t TypeID) bool {
		if t == NoTypeID || seen[t] {
			return false
		}
		seen[t] = true
		resolved := resolveAliasAndOwn(in, t)
		if resolved != t && walk(resolved) {
			return true
		}
		if _, ok := in.DynamicArrayElem(resolved); ok {
			return true
		}
		tt, ok := in.Lookup(resolved)
		if !ok {
			return false
		}
		switch tt.Kind {
		case KindReference, KindPointer, KindFar, KindFn:
			return false
		}
		if payloads, handleBacked := in.RuntimeHandlePayloads(resolved); handleBacked {
			for _, p := range payloads {
				if walk(p) {
					return true
				}
			}
			return false
		}
		if elem, _, isFixed := in.ArrayFixedInfo(resolved); isFixed && walk(elem) {
			return true
		}
		for _, f := range in.StructFields(resolved) {
			if walk(f.Type) {
				return true
			}
		}
		if info, ok := in.TupleInfo(resolved); ok && info != nil {
			for _, el := range info.Elems {
				if walk(el) {
					return true
				}
			}
		}
		if info, ok := in.UnionInfo(resolved); ok && info != nil {
			for i := range info.Members {
				m := &info.Members[i]
				if m.Kind == UnionMemberType && walk(m.Type) {
					return true
				}
				for _, arg := range m.TagArgs {
					if walk(arg) {
						return true
					}
				}
			}
		}
		return walk(tt.Elem)
	}
	return walk(id)
}

// A recursive shape must terminate, and its answer must come from its other
// members rather than from the edge that closed the loop. `Node` holds a
// `Chain`, and `Chain`'s tag arm holds a `Node` again: without the seen set
// the walk never returns. The pair is built twice, once with an array field on
// the node and once without, so the row shows the cycle guard ends the walk
// without also swallowing a real answer.
func TestContainsDynamicArrayTerminatesOnACycleThroughAUnion(t *testing.T) {
	build := func(withArray bool) (*Interner, TypeID) {
		in := NewInterner()
		in.Strings = source.NewInterner()
		b := in.Builtins()
		node := in.RegisterStruct(in.Strings.Intern("Node"), source.Span{})
		chain := in.RegisterUnion(in.Strings.Intern("Chain"), source.Span{})
		fields := []StructField{{Type: chain}}
		if withArray {
			fields = append(fields, StructField{Type: in.Intern(MakeArray(b.Int32, ArrayDynamicLength))})
		}
		in.SetStructFields(node, fields)
		in.SetUnionMembers(chain, []UnionMember{
			{Kind: UnionMemberTag, TagName: in.Strings.Intern("More"), TagArgs: []TypeID{node}},
			{Kind: UnionMemberNothing},
		})
		return in, node
	}

	plain, node := build(false)
	if plain.ContainsDynamicArray(node) {
		t.Errorf("a cyclic Node with no array: ContainsDynamicArray = true, want false")
	}
	withArray, nodeWithArray := build(true)
	if !withArray.ContainsDynamicArray(nodeWithArray) {
		t.Errorf("a cyclic Node with an array field: ContainsDynamicArray = false, want true")
	}
}

// The companion walk crosses a handle and then walks freely, which is a second
// place a cycle can close: `Node` holds a `Map<int32, Node>`, so stepping the
// table lands back on `Node`. Built twice again -- once with an array behind
// the table, once without -- so the guard is shown to end the walk without
// eating the answer.
func TestContainsDynamicArrayBehindHandleTerminatesOnACycleThroughAHandle(t *testing.T) {
	build := func(withArray bool) (*Interner, TypeID) {
		in := NewInterner()
		in.Strings = source.NewInterner()
		b := in.Builtins()
		if base, _ := in.EnsureMapNominal(in.Strings.Intern("Map"), in.Strings.Intern("K"),
			in.Strings.Intern("V"), source.Span{}, 0); base == NoTypeID {
			t.Fatalf("failed to register the nominal Map")
		}
		node := in.RegisterStruct(in.Strings.Intern("Node"), source.Span{})
		table := in.RegisterStructInstance(in.Strings.Intern("Map"), source.Span{}, []TypeID{b.Int32, node})
		fields := []StructField{{Type: table}}
		if withArray {
			inner := in.RegisterStructInstance(in.Strings.Intern("Map"), source.Span{},
				[]TypeID{b.Int32, in.Intern(MakeArray(b.Int32, ArrayDynamicLength))})
			fields = append(fields, StructField{Type: inner})
		}
		in.SetStructFields(node, fields)
		return in, node
	}

	plain, node := build(false)
	if plain.ContainsDynamicArrayBehindHandle(node) {
		t.Errorf("a cyclic Node with no array: ContainsDynamicArrayBehindHandle = true, want false")
	}
	withArray, nodeWithArray := build(true)
	if !withArray.ContainsDynamicArrayBehindHandle(nodeWithArray) {
		t.Errorf("a cyclic Node with an array behind a table: ContainsDynamicArrayBehindHandle = false, want true")
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
