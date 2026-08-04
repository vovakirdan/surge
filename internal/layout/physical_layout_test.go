package layout

import (
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"surge/internal/source"
	"surge/internal/types"
)

func testInterner() *types.Interner {
	in := types.NewInterner()
	in.Strings = source.NewInterner()
	return in
}

func registerStruct(in *types.Interner, name string, fields ...types.StructField) types.TypeID {
	id := in.RegisterStruct(in.Strings.Intern(name), source.Span{})
	in.SetStructFields(id, fields)
	return id
}

func layoutErrorKind(t *testing.T, err error, want ErrorKind) *LayoutError {
	t.Helper()
	var got *LayoutError
	if !errors.As(err, &got) {
		t.Fatalf("error = %T %v, want *LayoutError", err, err)
	}
	if got.Kind != want {
		t.Fatalf("error kind = %v, want %v (%v)", got.Kind, want, got)
	}
	return got
}

func TestPhysicalLayoutAlignmentTargetLimit(t *testing.T) {
	in := testInterner()
	for _, tc := range []struct {
		name    string
		align   uint64
		wantErr bool
	}{
		{name: "maximum accepted", align: 1 << 32},
		{name: "one bit beyond target", align: 1 << 33, wantErr: true},
		{name: "host-sign-bit scale", align: 1 << 63, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := registerStruct(in, tc.name)
			in.SetTypeLayoutAttrs(id, types.LayoutAttrs{AlignOverride: &tc.align})
			l, err := New(X86_64LinuxGNU(), in).LayoutOf(id)
			if tc.wantErr {
				layoutErrorKind(t, err, ErrUnsupportedAlignment)
				if l.State() != StateError {
					t.Fatalf("state = %s, want Error", l.State())
				}
				if _, ok := l.Physical(); ok {
					t.Fatal("Error exposed physical facts")
				}
				return
			}
			if err != nil {
				t.Fatalf("LayoutOf: %v", err)
			}
			facts, ok := l.Physical()
			if l.State() != StateZST || !ok || facts.Size != 0 || facts.Stride != 0 || facts.Align != tc.align {
				t.Fatalf("layout state=%s facts=%+v ok=%t, want aligned ZST", l.State(), facts, ok)
			}
		})
	}
}

func TestCheckedMathUsesTargetAddressWidth(t *testing.T) {
	target := Target{Triple: "test-32", AddressBits: 32, PointerSize: 4, PointerAlign: 4, MaxABIAlign: 1 << 31}
	id := types.TypeID(1)
	if _, err := checkedAdd(target, id, math.MaxUint32, 1, "add"); err == nil || err.Kind != ErrOverflow {
		t.Fatalf("checkedAdd error = %v, want overflow", err)
	}
	if _, err := checkedMul(target, id, 1<<31, 2, "mul"); err == nil || err.Kind != ErrOverflow {
		t.Fatalf("checkedMul error = %v, want overflow", err)
	}
	if _, err := checkedRoundUp(target, id, math.MaxUint32, 2, "roundup"); err == nil || err.Kind != ErrOverflow {
		t.Fatalf("checkedRoundUp error = %v, want overflow", err)
	}
}

func TestPhysicalLayoutArrayAndUnionOverflowOn32BitTarget(t *testing.T) {
	target := Target{Triple: "test-32", AddressBits: 32, PointerSize: 4, PointerAlign: 4, MaxABIAlign: 1 << 31}
	in := testInterner()
	hugeAlign := uint64(1 << 31)
	huge := registerStruct(in, "Huge", types.StructField{Type: in.Builtins().Bool})
	in.SetTypeLayoutAttrs(huge, types.LayoutAttrs{AlignOverride: &hugeAlign})

	array := in.Intern(types.MakeArray(huge, 2))
	_, err := New(target, in).LayoutOf(array)
	layoutErrorKind(t, err, ErrOverflow)

	union := in.RegisterUnion(in.Strings.Intern("HugeUnion"), source.Span{})
	in.SetUnionMembers(union, []types.UnionMember{
		{Kind: types.UnionMemberNothing},
		{Kind: types.UnionMemberTag, TagName: in.Strings.Intern("Payload"), TagArgs: []types.TypeID{huge}},
	})
	_, err = New(target, in).LayoutOf(union)
	layoutErrorKind(t, err, ErrOverflow)
}

func TestPhysicalLayoutDistinguishesZSTDeferredErrorAndSubstitution(t *testing.T) {
	in := testInterner()
	zst := registerStruct(in, "Empty")
	generic := in.RegisterTypeParam(in.Strings.Intern("T"), 1, 0, false, types.NoTypeID)
	genericBox := registerStruct(in, "Box", types.StructField{Type: generic})

	builder := New(X86_64LinuxGNU(), in)
	zstLayout, err := builder.LayoutOf(zst)
	zstFacts, zstOK := zstLayout.Physical()
	if err != nil || zstLayout.State() != StateZST || !zstOK || zstFacts.Stride != 0 {
		t.Fatalf("ZST = %+v, %v", zstLayout, err)
	}
	deferred, err := builder.LayoutOf(genericBox)
	if err != nil || deferred.State() != StateDeferred {
		t.Fatalf("generic = %+v, %v", deferred, err)
	}
	if _, ok := deferred.Physical(); ok {
		t.Fatal("Deferred exposed physical facts")
	}
	proof := NewWithSubstitutions(X86_64LinuxGNU(), in, map[types.TypeID]types.TypeID{
		generic: in.Builtins().Int32,
	})
	proved, err := proof.LayoutOf(genericBox)
	provedFacts, provedOK := proved.Physical()
	if err != nil || proved.State() != StateConcrete || !provedOK || provedFacts.Size != 4 {
		t.Fatalf("substituted proof = %+v, %v", proved, err)
	}
	// A proof must not materialize Box<int32> or any other canonical type.
	if _, ok := in.FindStructInstance(in.Strings.Intern("Box"), []types.TypeID{in.Builtins().Int32}); ok {
		t.Fatal("substituted proof mutated the canonical interner")
	}
	concreteBox := in.RegisterStructInstance(in.Strings.Intern("Box"), source.Span{}, []types.TypeID{in.Builtins().Int32})
	in.SetStructFields(concreteBox, []types.StructField{{Type: in.Builtins().Int32}})
	concrete, err := builder.LayoutOf(concreteBox)
	concreteFacts, concreteOK := concrete.Physical()
	if err != nil || concrete.State() != StateConcrete || !concreteOK || concreteFacts.Size != 4 || concreteFacts.Stride != 4 {
		t.Fatalf("concrete = %+v, %v", concrete, err)
	}
	unknown, err := builder.LayoutOf(types.NoTypeID)
	layoutErrorKind(t, err, ErrUnknownType)
	if unknown.State() != StateError {
		t.Fatalf("unknown = %+v, want Error without fabricated facts", unknown)
	}
	if _, ok := unknown.Physical(); ok {
		t.Fatal("unknown Error exposed physical facts")
	}
}

func TestPhysicalLayoutRuntimeHandleNominalsArePointerSized(t *testing.T) {
	in := testInterner()
	payload := registerStruct(in, "Payload", types.StructField{Type: in.Builtins().Uint64})
	handles := make([]types.TypeID, 0, 4)
	for _, name := range []string{"Task", "Channel", "Range"} {
		nameID := in.Strings.Intern(name)
		decl := source.Span{File: 1, Start: 10, End: 20}
		base := in.RegisterStruct(nameID, decl)
		in.MarkRuntimeHandleType(base)
		id := in.RegisterStructInstance(nameID, decl, []types.TypeID{payload})
		// Empty nominal metadata used to be fabricated as a ZST. A real field
		// would be equally wrong: the language value is the runtime handle.
		handles = append(handles, id)
	}
	placement := registerStruct(in, "Placement", types.StructField{Type: payload})
	in.MarkRuntimePlacementType(placement)
	handles = append(handles, placement)

	builder := New(X86_64LinuxGNU(), in)
	for _, id := range handles {
		physical, err := builder.LayoutOf(id)
		if err != nil {
			t.Fatalf("LayoutOf(%s): %v", types.Label(in, id), err)
		}
		facts, ok := physical.Physical()
		if !ok || physical.State() != StateConcrete || facts.Size != 8 || facts.Align != 8 || facts.Stride != 8 {
			t.Fatalf("%s layout = state %s facts %+v, want pointer-sized concrete", types.Label(in, id), physical.State(), facts)
		}
	}
}

func TestPhysicalLayoutRecursionPathCacheIsRootRelative(t *testing.T) {
	in := testInterner()
	node := registerStruct(in, "Node")
	in.SetStructFields(node, []types.StructField{{Type: node}})
	left := registerStruct(in, "Left", types.StructField{Type: node})
	right := registerStruct(in, "Right", types.StructField{Type: in.Builtins().Bool}, types.StructField{Type: node})
	builder := New(X86_64LinuxGNU(), in)

	_, leftErrRaw := builder.LayoutOf(left)
	leftErr := layoutErrorKind(t, leftErrRaw, ErrRecursiveUnsized)
	_, rightErrRaw := builder.LayoutOf(right)
	rightErr := layoutErrorKind(t, rightErrRaw, ErrRecursiveUnsized)
	if got := leftErr.Path(); len(got) == 0 || got[0].Index != 0 {
		t.Fatalf("left path = %v", got)
	}
	if got := rightErr.Path(); len(got) == 0 || got[0].Index != 1 {
		t.Fatalf("right path = %v", got)
	}

	mutated := leftErr.Path()
	mutated[0].Index = 99
	_, againRaw := builder.LayoutOf(left)
	again := layoutErrorKind(t, againRaw, ErrRecursiveUnsized)
	if got := again.Path()[0].Index; got != 0 {
		t.Fatalf("cached path mutated through caller: %d", got)
	}
}

func TestRegistryOwnsSlicesAndSupportsConcurrentReads(t *testing.T) {
	in := testInterner()
	pair := registerStruct(in, "Pair",
		types.StructField{Type: in.Builtins().Bool},
		types.StructField{Type: in.Builtins().Int64},
	)
	union := in.RegisterUnion(in.Strings.Intern("Choice"), source.Span{})
	in.SetUnionMembers(union, []types.UnionMember{
		{Kind: types.UnionMemberNothing},
		{Kind: types.UnionMemberTag, TagName: in.Strings.Intern("Some"), TagArgs: []types.TypeID{pair}},
	})
	registry, err := FinalizeRegistry(New(X86_64LinuxGNU(), in), []types.TypeID{pair, union})
	if err != nil {
		t.Fatalf("FinalizeRegistry: %v", err)
	}

	pairLayout, _ := registry.Lookup(pair)
	pairFacts, ok := pairLayout.Physical()
	if !ok {
		t.Fatal("pair registry entry is not physical")
	}
	offsets := pairFacts.FieldOffsets()
	offsets[1] = 0
	again, _ := registry.Lookup(pair)
	againFacts, _ := again.Physical()
	if got := againFacts.FieldOffsets(); !reflect.DeepEqual(got, []uint64{0, 8}) {
		t.Fatalf("registry field offsets mutated: %v", got)
	}
	unionLayout, _ := registry.Lookup(union)
	unionFacts, _ := unionLayout.Physical()
	unionCases := unionFacts.UnionCases()
	unionCases[0].PayloadOffset = 99
	againUnion, _ := registry.Lookup(union)
	againUnionFacts, _ := againUnion.Physical()
	if got := againUnionFacts.UnionCases()[0].PayloadOffset; got == 99 {
		t.Fatal("registry union offsets mutated through caller")
	}

	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				l, err := registry.Require(pair)
				if err != nil || l.Size != 16 || l.Align != 8 {
					t.Errorf("lookup = %+v, %v", l, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestUnionCaseCarriesMixedAlignmentArgumentOffsetsAndDeepCopies(t *testing.T) {
	in := testInterner()
	union := in.RegisterUnion(in.Strings.Intern("Mixed"), source.Span{})
	in.SetUnionMembers(union, []types.UnionMember{{
		Kind:    types.UnionMemberTag,
		TagName: in.Strings.Intern("Many"),
		TagArgs: []types.TypeID{in.Builtins().Uint8, in.Builtins().Uint64, in.Builtins().Bool},
	}})
	registry, err := FinalizeRegistry(New(X86_64LinuxGNU(), in), []types.TypeID{union})
	if err != nil {
		t.Fatalf("FinalizeRegistry: %v", err)
	}
	facts, err := registry.Require(union)
	if err != nil {
		t.Fatalf("Require: %v", err)
	}
	unionCase, ok := facts.UnionCase(0)
	if !ok {
		t.Fatal("missing union case")
	}
	if unionCase.PayloadOffset != 8 || unionCase.PayloadSize != 24 || unionCase.PayloadAlign != 8 {
		t.Fatalf("case = %+v, want offset/size/align 8/24/8", unionCase)
	}
	if got := unionCase.FieldOffsets(); !reflect.DeepEqual(got, []uint64{0, 8, 16}) {
		t.Fatalf("field offsets = %v", got)
	}
	mutated := unionCase.FieldOffsets()
	mutated[1] = 0
	unionCase.PayloadOffset = 99
	again, err := registry.Require(union)
	if err != nil {
		t.Fatal(err)
	}
	againCase, _ := again.UnionCase(0)
	if againCase.PayloadOffset != 8 || !reflect.DeepEqual(againCase.FieldOffsets(), []uint64{0, 8, 16}) {
		t.Fatalf("registry union case was mutated: %+v fields=%v", againCase, againCase.FieldOffsets())
	}
}

func TestPhysicalLayoutOffsetAboveMaxInt32IsNotTruncated(t *testing.T) {
	in := testInterner()
	large := in.Intern(types.MakeArray(in.Builtins().Uint8, 1<<31))
	container := registerStruct(in, "LargeOffset",
		types.StructField{Type: large},
		types.StructField{Type: in.Builtins().Bool},
	)
	l, err := New(X86_64LinuxGNU(), in).LayoutOf(container)
	if err != nil {
		t.Fatalf("LayoutOf: %v", err)
	}
	facts, physical := l.Physical()
	if !physical {
		t.Fatal("large layout is not physical")
	}
	offset, ok := facts.FieldOffset(1)
	if !ok || offset != 1<<31 {
		t.Fatalf("field offset = %d, %t; want %d", offset, ok, uint64(1<<31))
	}
}

func TestRegistryDumpAndHashAreDeterministic(t *testing.T) {
	in := testInterner()
	a := registerStruct(in, "A", types.StructField{Type: in.Builtins().Int32})
	b := registerStruct(in, "B", types.StructField{Type: in.Builtins().Bool})
	first, err := FinalizeRegistry(New(X86_64LinuxGNU(), in), []types.TypeID{a, b, a})
	if err != nil {
		t.Fatal(err)
	}
	second, err := FinalizeRegistry(New(X86_64LinuxGNU(), in), []types.TypeID{a, b})
	if err != nil {
		t.Fatal(err)
	}
	firstHash, err := first.Hash()
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := second.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("hashes differ: %s != %s", firstHash, secondHash)
	}
}

func TestRegistryHashCoversExactAliasSet(t *testing.T) {
	in := testInterner()
	base := registerStruct(in, "Base", types.StructField{Type: in.Builtins().Int32})
	alias := in.RegisterAlias(in.Strings.Intern("Alias"), source.Span{})
	in.SetAliasTarget(alias, base)
	withoutAlias, err := FinalizeRegistry(New(X86_64LinuxGNU(), in), []types.TypeID{base})
	if err != nil {
		t.Fatal(err)
	}
	withAlias, err := FinalizeRegistry(New(X86_64LinuxGNU(), in), []types.TypeID{base, alias})
	if err != nil {
		t.Fatal(err)
	}
	withoutHash, err := withoutAlias.Hash()
	if err != nil {
		t.Fatal(err)
	}
	withHash, err := withAlias.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if withoutHash == withHash {
		t.Fatalf("registry hash ignored exact alias set: %s", withoutHash)
	}
}

func TestLayoutErrorPathRetainsAliasAndOwnPrefixesAcrossCache(t *testing.T) {
	in := testInterner()
	align := uint64(1 << 32)
	huge := registerStruct(in, "Huge", types.StructField{Type: in.Builtins().Bool})
	in.SetTypeLayoutAttrs(huge, types.LayoutAttrs{AlignOverride: &align})
	array := in.Intern(types.MakeArray(huge, math.MaxUint32-1))
	overflow := registerStruct(in, "Overflow",
		types.StructField{Type: array},
		types.StructField{Type: huge},
		types.StructField{Type: huge},
	)
	alias := in.RegisterAlias(in.Strings.Intern("OverflowAlias"), source.Span{})
	in.SetAliasTarget(alias, overflow)
	ownedAlias := in.Intern(types.MakeOwn(alias))
	builder := New(X86_64LinuxGNU(), in)

	// Prime the canonical cache first; exact-root path prefixes must still be
	// reconstructed on later alias/own queries.
	if _, err := builder.LayoutOf(overflow); err == nil {
		t.Fatal("overflow layout unexpectedly succeeded")
	}
	_, aliasErrRaw := builder.LayoutOf(alias)
	aliasErr := layoutErrorKind(t, aliasErrRaw, ErrOverflow)
	aliasPath := aliasErr.Path()
	if len(aliasPath) < 2 || aliasPath[0].Kind != PathAliasTarget || aliasPath[1].Kind != PathStructField {
		t.Fatalf("alias path = %v", aliasPath)
	}
	_, ownErrRaw := builder.LayoutOf(ownedAlias)
	ownErr := layoutErrorKind(t, ownErrRaw, ErrOverflow)
	ownPath := ownErr.Path()
	if len(ownPath) < 3 || ownPath[0].Kind != PathOwnValue || ownPath[1].Kind != PathAliasTarget || ownPath[2].Kind != PathStructField {
		t.Fatalf("own alias path = %v", ownPath)
	}
}
